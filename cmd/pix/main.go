package main

import (
	"flag"
	"fmt"
	"image/png"
	"log"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"

	"github.com/yurivish/pix"
)

func main() {
	input := flag.String("in", "", "input image or wav file (required!)")
	output := flag.String("out", "", "output image")
	width := flag.Int("width", 300, "width of the output image")
	height := flag.Int("height", 300, "height of the output image")
	whitePercent := flag.Int("white-percent", 0, "percentage (0 to 100) determining the area left white on the canvas")
	color := flag.Int("colorsort", 90, "magic parameter (0 to 100) determining sort order. A higher value will give more weight to color similarity, while lower values will better preserve proximity in the source image.")
	random := flag.Int("random", 0, "randomness weight for similarity sort")
	reverse := flag.Bool("reverse", true, "reverse sort order")
	sweep := flag.Bool("sweep", false, "sweep across {colorsort, random, reverse, seeds} parameters, ignoring any explicitly set values")
	seed := flag.Int64("random-seed", 0, "random seed")
	variations := flag.Int("variations", 1, "number of outputs to generate for each set of input parameters")
	audioPalette := flag.String("audio-palette", string(pix.PaletteNatural), "audio palette for wav input")
	audioOffset := flag.String("audio-offset", "0s", "start offset for wav input, as a Go duration or seconds")
	audioDuration := flag.String("audio-duration", "0s", "duration to read from wav input, as a Go duration or seconds; 0 uses the rest of the file")
	audioMono := flag.Bool("audio-mono", true, "downmix wav input to mono")
	audioFFTSize := flag.Int("audio-fft-size", 2048, "fft size for wav analysis")
	audioHopSize := flag.Int("audio-hop-size", 512, "hop size for wav analysis")

	var compressionLevel png.CompressionLevel
	flag.Func("compress", "png compression level: https://pkg.go.dev/image/png#CompressionLevel", func(s string) error {
		i, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("could not parse compression level: %w", err)
		}
		if i < -3 || i > 0 {
			return fmt.Errorf("compression level out of range (valid values: -3, -2, -1, 0)")
		}
		compressionLevel = png.CompressionLevel(i)
		return nil
	})

	var seeds []int
	flag.Func("seeds", "seed positions: 'x y[ x y...]'", func(s string) error {
		pieces := strings.Split(s, " ")
		for _, piece := range pieces {
			n, err := strconv.ParseInt(piece, 10, 16)
			if err != nil {
				return err
			}
			seeds = append(seeds, int(n))
		}
		if len(seeds)%1 == 1 {
			return fmt.Errorf("seeds must specify an even number of coordinates")
		}
		return nil
	})

	flag.Parse()

	if *input == "" {
		fmt.Println("please specify an input image or wav file via the -in flag.")
		flag.Usage()
		os.Exit(1)
	}

	image := new(int)
	*image = 100 - *color

	// If no output file is specified, generate a file name based on the input.
	if *output == "" {
		_, file := path.Split(*input)
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("could not get working directory: %v", err)
		}
		ext := path.Ext(file)
		if ext != ".png" {
			file = file[:len(file)-len(ext)] + ".png"
		}
		*output = path.Join(wd, "pix."+file)
	}
	// Parse the output path into components in order to synthesize variation outputs
	dir, file := path.Split(*output)
	ext := path.Ext(file)
	name := file[:len(file)-len(ext)]

	offset, err := pix.ParseAudioDuration(*audioOffset)
	if err != nil {
		log.Fatalf("failed to parse -audio-offset: %v", err)
	}
	duration, err := pix.ParseAudioDuration(*audioDuration)
	if err != nil {
		log.Fatalf("failed to parse -audio-duration: %v", err)
	}

	w, h := *width, *height
	sourceOpts := pix.SourceOptions{
		Width:  w,
		Height: h,
		Audio: pix.AudioOptions{
			Width:    w,
			Height:   h,
			Offset:   offset,
			Duration: duration,
			Mono:     *audioMono,
			FFTSize:  *audioFFTSize,
			HopSize:  *audioHopSize,
			Palette:  pix.AudioPalette(*audioPalette),
		},
	}
	img, err := pix.LoadSource(*input, sourceOpts)
	if err != nil {
		log.Fatalf("failed to load source: %v", err)
	}

	numVariations := *variations

	// Configure parameter values to cartesian-product over. If the -sweep option
	// was specified, use the presets; otherwise, use the user-provided or default values.
	var imageSweep, randomSweep []int
	var seedsSweep [][]int
	var reverseSweep []bool
	if *sweep {
		imageSweep = []int{10, 90}
		randomSweep = []int{0, 10}
		reverseSweep = []bool{true, false}
		seedsSweep = [][]int{{w / 2, h / 2}, {0, h - 1}, {w / 2, 0, 0, h / 2, w / 2, h - 1, w - 1, h / 2}}
	} else {
		imageSweep = []int{*image}
		randomSweep = []int{*random}
		reverseSweep = []bool{*reverse}
		seedsSweep = [][]int{seeds}
	}

	// Launch a number of parallel jobs equal to the number of CPUs, then wait for them to  finish.
	//
	numJobs := len(imageSweep) * len(randomSweep) * len(reverseSweep) * len(seedsSweep) * numVariations
	jobs := make(chan Work, numJobs)
	results := make(chan bool, numJobs)

	numWorkers := runtime.NumCPU()
	if numWorkers > numJobs {
		numWorkers = numJobs
	}
	for id := 0; id <= numWorkers; id++ {
		go worker(id, jobs, results)
	}

	// Sample colors from the image
	colors := pix.SampleColors(img, (100-*whitePercent)*w*h/100)

	// Generate variations
	variation := 0
	for _, image := range imageSweep {
		for _, random := range randomSweep {
			for _, reverse := range reverseSweep {
				// sort once per unique set of sort parameters
				sortedColors := make([]pix.SampledColor, len(colors))
				copy(sortedColors, colors)
				sortOpts := pix.SortOptions{
					Image:   float64(image),
					Color:   float64(100 - image),
					Random:  float64(random),
					Reverse: reverse,
				}
				pix.SortBySimilarity(sortedColors, sortOpts)

				for _, seeds := range seedsSweep {
					if len(seeds) == 0 {
						seeds = []int{w / 2, h / 2}
					}

					var seedsString string // seed values to print out for the status message
					// unnecessarily allocates, but probably not a hotspot in normal usage
					for _, seed := range seeds {
						seedsString = seedsString + fmt.Sprintf(" %v", strconv.Itoa(seed))
					}

					for i := 0; i < numVariations; i++ {

						variation++
						// Tag all variations above the first with an integer sequence number
						var variationTag string
						if variation > 1 {
							variationTag = "." + strconv.Itoa(variation)
						}

						opts := pix.Options{
							Width:            w,
							Height:           h,
							Seeds:            seeds,
							Sort:             sortOpts,
							RandomSeed:       *seed + int64(variation),
							CompressionLevel: compressionLevel,
							Output:           path.Join(dir, name+variationTag+ext),
						}

						status := fmt.Sprintf("generating variation %v: seeds:%v, colorsort: %v, random: %v, reverse: %v\n", variation, seedsString, sortOpts.Color, sortOpts.Random, sortOpts.Reverse)
						jobs <- Work{sortedColors, opts, status}
					}
				}
			}
		}
	}

	close(jobs)

	for n := 0; n < numJobs; n++ {
		<-results
	}

}

type Work struct {
	colors []pix.SampledColor
	opts   pix.Options
	status string
}

func worker(id int, jobs <-chan Work, results chan<- bool) {
	for j := range jobs {
		colors, opts := j.colors, j.opts
		fmt.Print(j.status)
		err := pix.Place(colors, opts)
		if err != nil {
			fmt.Printf("!!! error placing pixels: %v\n", err)
			results <- false
		} else {
			results <- true
		}
	}
}
