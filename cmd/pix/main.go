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
	flag.Usage = usage

	var input string
	flag.StringVar(&input, "input", "", "input image or wav file (required!)")
	flag.StringVar(&input, "i", "", "input image or wav file (required!)")

	var output string
	flag.StringVar(&output, "output", "", "output image")
	flag.StringVar(&output, "o", "", "output image")

	var width int
	flag.IntVar(&width, "width", 300, "width of the output image")
	flag.IntVar(&width, "w", 300, "width of the output image")

	var height int
	flag.IntVar(&height, "height", 300, "height of the output image")
	flag.IntVar(&height, "H", 300, "height of the output image")

	whitePercent := flag.Int("white-percent", 0, "percentage (0 to 100) determining the area left white on the canvas")
	hue := flag.Float64("hue", 0, "hue rotation in degrees applied to generated source colors")
	color := flag.Int("color-sort", 90, "magic parameter (0 to 100) determining sort order. A higher value will give more weight to color similarity, while lower values will better preserve proximity in the source image.")
	random := flag.Int("random", 0, "randomness weight for similarity sort")
	reverse := flag.Bool("reverse", true, "reverse sort order")
	sweep := flag.Bool("sweep", false, "sweep across {color-sort, random, reverse, seeds} parameters, ignoring any explicitly set values")
	seed := flag.Int64("random-seed", 0, "random seed")

	var variations int
	flag.IntVar(&variations, "variations", 1, "number of outputs to generate for each set of input parameters")
	flag.IntVar(&variations, "v", 1, "number of outputs to generate for each set of input parameters")

	audioPalette := flag.String("audio-palette", string(pix.PaletteNatural), "audio palette for wav input")
	audioOffset := flag.String("audio-offset", "0s", "start offset for wav input, as a Go duration or seconds")
	audioDuration := flag.String("audio-duration", "0s", "duration to read from wav input, as a Go duration or seconds; 0 uses the rest of the file")
	audioMono := flag.Bool("audio-mono", true, "downmix wav input to mono")
	audioFFTSize := flag.Int("audio-fft-size", 2048, "fft size for wav analysis")
	audioHopSize := flag.Int("audio-hop-size", 512, "hop size for wav analysis")

	var compressionLevel png.CompressionLevel
	setCompressionLevel := func(s string) error {
		i, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("could not parse compression level: %w", err)
		}
		if i < -3 || i > 0 {
			return fmt.Errorf("compression level out of range (valid values: -3, -2, -1, 0)")
		}
		compressionLevel = png.CompressionLevel(i)
		return nil
	}
	flag.Func("compress", "png compression level: https://pkg.go.dev/image/png#CompressionLevel", setCompressionLevel)
	flag.Func("c", "png compression level: https://pkg.go.dev/image/png#CompressionLevel", setCompressionLevel)

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

	if err := validateStandardFlags(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n\n", err)
		flag.Usage()
		os.Exit(2)
	}
	flag.Parse()

	if input == "" {
		fmt.Println("please specify an input image or wav file via -i or --input.")
		flag.Usage()
		os.Exit(1)
	}

	image := new(int)
	*image = 100 - *color

	// If no output file is specified, generate a file name based on the input.
	if output == "" {
		_, file := path.Split(input)
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("could not get working directory: %v", err)
		}
		ext := path.Ext(file)
		if ext != ".png" {
			file = file[:len(file)-len(ext)] + ".png"
		}
		output = path.Join(wd, "pix."+file)
	}
	// Parse the output path into components in order to synthesize variation outputs
	dir, file := path.Split(output)
	ext := path.Ext(file)
	name := file[:len(file)-len(ext)]

	offset, err := pix.ParseAudioDuration(*audioOffset)
	if err != nil {
		log.Fatalf("failed to parse --audio-offset: %v", err)
	}
	duration, err := pix.ParseAudioDuration(*audioDuration)
	if err != nil {
		log.Fatalf("failed to parse --audio-duration: %v", err)
	}

	w, h := width, height
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
	img, err := pix.LoadSource(input, sourceOpts)
	if err != nil {
		log.Fatalf("failed to load source: %v", err)
	}
	if *hue != 0 {
		img = pix.RotateImageColorsHue(img, *hue)
	}

	numVariations := variations

	// Configure parameter values to cartesian-product over. If the --sweep option
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

						status := fmt.Sprintf("generating variation %v: seeds:%v, color-sort: %v, random: %v, reverse: %v\n", variation, seedsString, sortOpts.Color, sortOpts.Random, sortOpts.Reverse)
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

func validateStandardFlags(args []string) error {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return nil
		}
		if len(arg) < 2 || arg[0] != '-' || strings.HasPrefix(arg, "--") || arg == "-" {
			if name, hasValue := longFlagName(arg); hasValue && flagTakesValue(name) {
				i++
			}
			continue
		}

		name := strings.TrimPrefix(arg, "-")
		hasInlineValue := strings.Contains(name, "=")
		if hasInlineValue {
			name = strings.SplitN(name, "=", 2)[0]
		}
		if isNegativeNumber(name) {
			continue
		}
		if len(name) > 1 {
			return fmt.Errorf("invalid option %q: use -x for single-character switches and --%s for long switches", arg, canonicalLongFlag(name))
		}
		if !hasInlineValue && flagTakesValue(name) {
			i++
		}
	}
	return nil
}

func longFlagName(arg string) (string, bool) {
	if !strings.HasPrefix(arg, "--") || arg == "--" {
		return "", false
	}
	name := strings.TrimPrefix(arg, "--")
	if strings.Contains(name, "=") {
		return strings.SplitN(name, "=", 2)[0], false
	}
	return name, true
}

func flagTakesValue(name string) bool {
	switch name {
	case "input", "i",
		"output", "o",
		"width", "w",
		"height", "H",
		"white-percent",
		"hue",
		"color-sort",
		"random",
		"random-seed",
		"variations", "v",
		"audio-palette",
		"audio-offset",
		"audio-duration",
		"audio-fft-size",
		"audio-hop-size",
		"compress", "c",
		"seeds":
		return true
	default:
		return false
	}
}

func canonicalLongFlag(name string) string {
	switch name {
	case "in":
		return "input"
	case "out":
		return "output"
	case "colorsort":
		return "color-sort"
	default:
		return name
	}
}

func isNegativeNumber(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.ParseFloat("-"+s, 64)
	return err == nil
}

func usage() {
	out := flag.CommandLine.Output()
	name := path.Base(os.Args[0])
	fmt.Fprintf(out, `Usage:
  %s -i INPUT [options]
  %s --input INPUT [options]

Essential:
  -i, --input <path>           Input image or WAV file. Required.
  -o, --output <path>          Output PNG path. Defaults to pix.<input>.png.
  -w, --width <int>            Output image width. Default 300.
  -H, --height <int>           Output image height. Default 300.

Audio range:
      --audio-offset <duration>    Start offset for WAV input. Default 0s.
      --audio-duration <duration>  Duration to read; 0s uses the rest. Default 0s.

Output and batches:
      --white-percent <0-100>  Percentage of the canvas to leave white. Default 0.
      --hue <degrees>          Rotate source colors in HSV hue space. Default 0.
  -v, --variations <int>       Number of outputs per parameter set. Default 1.
  -c, --compress <-3|-2|-1|0>  PNG compression level. Default 0.

Color sorting and growth:
      --color-sort <0-100>     Weight between source position and color similarity. Default 90.
      --random <int>           Randomness weight for similarity sort. Default 0.
      --reverse[=true|false]   Reverse sort order. Default true.
      --random-seed <int>      Base random seed for reproducible placement. Default 0.
      --seeds "x y[ x y...]"   Seed positions. Defaults to the center pixel.
      --sweep[=true|false]     Generate a preset parameter sweep. Default false.

FFT and audio analysis:
      --audio-palette <name>   Audio-to-color palette. Default natural.
      --audio-mono[=true|false]    Downmix WAV input to mono. Default true.
      --audio-fft-size <int>   FFT frame size. Default 2048.
      --audio-hop-size <int>   Hop size between analysis frames. Default 512.

Help:
  -h, --help                   Show this help.
`, name, name)
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
