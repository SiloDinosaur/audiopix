package audio

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yurivish/pix/internal/visualization"
)

func TestDecodeWAVMono16(t *testing.T) {
	data := wavPCM16(8000, 1, []int16{-32768, 0, 32767})
	audio, err := DecodeWAV(data, true)
	if err != nil {
		t.Fatalf("DecodeWAV returned error: %v", err)
	}
	if audio.SampleRate != 8000 || audio.Channels != 1 || audio.SourceFrames != 3 {
		t.Fatalf("unexpected metadata: %+v", audio)
	}
	want := []float64{-1, 0, float64(32767) / 32768}
	for i := range want {
		if math.Abs(audio.Samples[i]-want[i]) > 1e-9 {
			t.Fatalf("sample %d: got %v; want %v", i, audio.Samples[i], want[i])
		}
	}
}

func TestDecodeWAVStereoDownmix(t *testing.T) {
	data := wavPCM16(8000, 2, []int16{
		32767, 32767,
		32767, -32768,
	})
	audio, err := DecodeWAV(data, true)
	if err != nil {
		t.Fatalf("DecodeWAV returned error: %v", err)
	}
	if audio.Channels != 1 || len(audio.Samples) != 2 {
		t.Fatalf("unexpected downmixed audio: %+v", audio)
	}
	if audio.Samples[0] < 0.99 {
		t.Fatalf("first sample should stay loud after downmix; got %v", audio.Samples[0])
	}
	if math.Abs(audio.Samples[1]) > 0.001 {
		t.Fatalf("second sample should cancel after downmix; got %v", audio.Samples[1])
	}
}

func TestDecodeWAVErrors(t *testing.T) {
	if _, err := DecodeWAV([]byte("not a wav"), true); err == nil {
		t.Fatal("DecodeWAV accepted invalid data")
	}
	if _, err := DecodeWAV(wavPCM16(8000, 1, nil), true); err == nil {
		t.Fatal("DecodeWAV accepted empty audio")
	}
}

func TestExtractAndResampleFeatures(t *testing.T) {
	audio := Data{Samples: sineSamples(128, 8000, 440), SampleRate: 8000, Channels: 1, SourceFrames: 128}
	features, err := ExtractFeatures(audio, Options{FFTSize: 64, HopSize: 16})
	if err != nil {
		t.Fatalf("ExtractFeatures returned error: %v", err)
	}
	if len(features) == 0 {
		t.Fatal("expected at least one feature frame")
	}
	for _, f := range features {
		if !finiteFeature(f) {
			t.Fatalf("feature contains non-finite values: %+v", f)
		}
	}
	resampled := ResampleFeatures(features, 17)
	if len(resampled) != 17 {
		t.Fatalf("got %d resampled features; want 17", len(resampled))
	}
	for _, f := range resampled {
		if !finiteFeature(f) {
			t.Fatalf("resampled feature contains non-finite values: %+v", f)
		}
	}
}

func TestExtractAudioFeatureBandsAndRolloff(t *testing.T) {
	lowAudio := Data{Samples: sineSamples(4096, 44100, 110), SampleRate: 44100, Channels: 1, SourceFrames: 4096}
	highAudio := Data{Samples: sineSamples(4096, 44100, 8000), SampleRate: 44100, Channels: 1, SourceFrames: 4096}

	low, err := ExtractFeatures(lowAudio, Options{FFTSize: 4096, HopSize: 4096})
	if err != nil {
		t.Fatalf("ExtractFeatures low returned error: %v", err)
	}
	high, err := ExtractFeatures(highAudio, Options{FFTSize: 4096, HopSize: 4096})
	if err != nil {
		t.Fatalf("ExtractFeatures high returned error: %v", err)
	}
	lf, hf := low[0], high[0]
	if lf.LowEnergy <= lf.HighEnergy {
		t.Fatalf("low sine should have stronger low energy than high energy: %+v", lf)
	}
	if lf.BassEnergy != lf.LowEnergy {
		t.Fatalf("BassEnergy should alias LowEnergy: bass=%v low=%v", lf.BassEnergy, lf.LowEnergy)
	}
	if hf.Centroid <= lf.Centroid {
		t.Fatalf("high sine centroid should exceed low sine centroid: low=%v high=%v", lf.Centroid, hf.Centroid)
	}
	if hf.Rolloff <= lf.Rolloff {
		t.Fatalf("high sine rolloff should exceed low sine rolloff: low=%v high=%v", lf.Rolloff, hf.Rolloff)
	}
}

func TestExtractAudioFeatureZeroCrossingRate(t *testing.T) {
	audio := Data{Samples: alternatingSamples(64), SampleRate: 8000, Channels: 1, SourceFrames: 64}
	features, err := ExtractFeatures(audio, Options{FFTSize: 64, HopSize: 64})
	if err != nil {
		t.Fatalf("ExtractFeatures returned error: %v", err)
	}
	if features[0].ZeroCrossingRate < 0.9 {
		t.Fatalf("alternating waveform should have high zero-crossing rate: %+v", features[0])
	}
}

func TestExtractAudioFeatureFlatnessAndBandwidth(t *testing.T) {
	toneAudio := Data{Samples: sineSamples(4096, 44100, 440), SampleRate: 44100, Channels: 1, SourceFrames: 4096}
	noiseAudio := Data{Samples: noiseSamples(4096), SampleRate: 44100, Channels: 1, SourceFrames: 4096}

	tone, err := ExtractFeatures(toneAudio, Options{FFTSize: 4096, HopSize: 4096})
	if err != nil {
		t.Fatalf("ExtractFeatures tone returned error: %v", err)
	}
	noise, err := ExtractFeatures(noiseAudio, Options{FFTSize: 4096, HopSize: 4096})
	if err != nil {
		t.Fatalf("ExtractFeatures noise returned error: %v", err)
	}
	tf, nf := tone[0], noise[0]
	if nf.Flatness <= tf.Flatness {
		t.Fatalf("noise should have higher flatness than tone: tone=%v noise=%v", tf.Flatness, nf.Flatness)
	}
	if nf.Bandwidth <= tf.Bandwidth {
		t.Fatalf("noise should have broader bandwidth than tone: tone=%v noise=%v", tf.Bandwidth, nf.Bandwidth)
	}
}

func TestFeaturesToImageColorsBrightnessAndHue(t *testing.T) {
	brightness, err := FeaturesToImageColors([]Features{
		{RMS: 0, Flatness: 0.1, LowEnergy: 1, BassEnergy: 1},
		{RMS: 1, Flatness: 0.1, LowEnergy: 1, BassEnergy: 1},
	}, 2, 1, PaletteNatural)
	if err != nil {
		t.Fatalf("FeaturesToImageColors returned error: %v", err)
	}
	if colorSum(brightness[1]) <= colorSum(brightness[0]) {
		t.Fatalf("loud frame should be brighter: quiet=%+v loud=%+v", brightness[0], brightness[1])
	}

	hues, err := FeaturesToImageColors([]Features{
		{RMS: 1, Flatness: 0.1, LowEnergy: 10, BassEnergy: 10, Centroid: 110},
		{RMS: 1, Flatness: 0.1, LowMidEnergy: 10, Centroid: 500},
		{RMS: 1, Flatness: 0.1, MidEnergy: 10, Centroid: 2000},
		{RMS: 1, Flatness: 0.1, HighEnergy: 10, Centroid: 8000},
	}, 4, 1, PaletteNatural)
	if err != nil {
		t.Fatalf("FeaturesToImageColors returned error: %v", err)
	}
	bass, lowMid, mid, high := hues[0], hues[1], hues[2], hues[3]
	if !(bass.R > bass.G && bass.B > bass.G) {
		t.Fatalf("bass color should be red/purple biased: %+v", bass)
	}
	if !(lowMid.R > lowMid.B && lowMid.G > lowMid.B) {
		t.Fatalf("low-mid color should be orange/amber biased: %+v", lowMid)
	}
	if !(mid.G > mid.R && mid.G > mid.B) {
		t.Fatalf("mid color should be green biased: %+v", mid)
	}
	if !(high.B > high.R && high.G > high.R) {
		t.Fatalf("high color should be cyan/blue biased: %+v", high)
	}

	saturation, err := FeaturesToImageColors([]Features{
		{RMS: 1, Flatness: 0.05, LowEnergy: 1, BassEnergy: 1, Bandwidth: 20, ZeroCrossingRate: 0.01},
		{RMS: 1, Flatness: 0.95, LowEnergy: 1, BassEnergy: 1, Bandwidth: 2000, ZeroCrossingRate: 0.8},
	}, 2, 1, PaletteNatural)
	if err != nil {
		t.Fatalf("FeaturesToImageColors saturation returned error: %v", err)
	}
	if rgbSaturation(saturation[1]) >= rgbSaturation(saturation[0]) {
		t.Fatalf("noisy/flat frame should be less saturated: tonal=%+v noisy=%+v", saturation[0], saturation[1])
	}
}

func TestRotateImageColorsHue(t *testing.T) {
	src := []visualization.ImageColor{{X: 7, Y: 11, R: 255, G: 0, B: 0}}

	green := visualization.RotateImageColorsHue(src, 120)[0]
	if green.X != 7 || green.Y != 11 || green.R != 0 || green.G != 255 || green.B != 0 {
		t.Fatalf("red +120 hue = %+v; want green at original coordinates", green)
	}

	blue := visualization.RotateImageColorsHue(src, 240)[0]
	negativeBlue := visualization.RotateImageColorsHue(src, -120)[0]
	wantBlue := visualization.ImageColor{X: 7, Y: 11, R: 0, G: 0, B: 255}
	if blue != wantBlue {
		t.Fatalf("red +240 hue = %+v; want %+v", blue, wantBlue)
	}
	if negativeBlue != wantBlue {
		t.Fatalf("red -120 hue = %+v; want %+v", negativeBlue, wantBlue)
	}

	for _, degrees := range []float64{0, 360, -360, 720} {
		got := visualization.RotateImageColorsHue(src, degrees)[0]
		if got != src[0] {
			t.Fatalf("red %+v hue = %+v; want original %+v", degrees, got, src[0])
		}
	}

	plus180 := visualization.RotateImageColorsHue(src, 180)[0]
	minus180 := visualization.RotateImageColorsHue(src, -180)[0]
	if plus180 != minus180 {
		t.Fatalf("red +180 hue = %+v; red -180 hue = %+v; want equal", plus180, minus180)
	}
}

func TestThemeImageColors(t *testing.T) {
	src := []visualization.ImageColor{{X: 7, Y: 11, R: 100, G: 150, B: 200}}
	_, srcSaturation, srcValue := visualization.RGBToHSV(src[0].R, src[0].G, src[0].B)

	themes := []struct {
		name               string
		theme              visualization.ColorTheme
		minValue           float64
		maxValue           float64
		minSaturationRatio float64
	}{
		{name: "light", theme: visualization.ThemeLight, minValue: 0.93, maxValue: 1, minSaturationRatio: 0.30},
		{name: "dark", theme: visualization.ThemeDark, minValue: 0.08, maxValue: 0.60, minSaturationRatio: 0.75},
	}

	for _, tt := range themes {
		t.Run(tt.name, func(t *testing.T) {
			got := visualization.ThemeImageColors(src, tt.theme)
			if len(got) != 1 {
				t.Fatalf("got %d colors; want 1", len(got))
			}
			if got[0].X != src[0].X || got[0].Y != src[0].Y {
				t.Fatalf("theme changed source coordinates: %+v", got[0])
			}
			srcHue, _, _ := visualization.RGBToHSV(src[0].R, src[0].G, src[0].B)
			gotHue, gotSaturation, gotValue := visualization.RGBToHSV(got[0].R, got[0].G, got[0].B)
			if hueDistance(srcHue, gotHue) > 1 {
				t.Fatalf("%s theme changed hue too much: source=%v got=%v color=%+v", tt.name, srcHue, gotHue, got[0])
			}
			if gotValue < tt.minValue || gotValue > tt.maxValue {
				t.Fatalf("%s theme value = %v; want in [%v, %v]", tt.name, gotValue, tt.minValue, tt.maxValue)
			}
			if tt.theme == visualization.ThemeLight && gotValue <= srcValue {
				t.Fatalf("light theme should raise value: source=%v got=%v", srcValue, gotValue)
			}
			if tt.theme == visualization.ThemeDark && gotValue >= srcValue {
				t.Fatalf("dark theme should lower value: source=%v got=%v", srcValue, gotValue)
			}
			if gotSaturation < tt.minSaturationRatio*srcSaturation {
				t.Fatalf("%s theme collapsed saturation: source=%v got=%v color=%+v", tt.name, srcSaturation, gotSaturation, got[0])
			}
		})
	}
}

func TestBuildImageColorsFromWAVDeterministic(t *testing.T) {
	path := writeTempWAV(t, 8000, 1, pcmFromFloat(sineSamples(64, 8000, 440)))
	opts := Options{Width: 4, Height: 4, Mono: true, FFTSize: 16, HopSize: 8, Palette: PaletteNatural}
	a, err := BuildImageColorsFromWAV(path, opts)
	if err != nil {
		t.Fatalf("BuildImageColorsFromWAV returned error: %v", err)
	}
	b, err := BuildImageColorsFromWAV(path, opts)
	if err != nil {
		t.Fatalf("BuildImageColorsFromWAV returned error on second call: %v", err)
	}
	if len(a) != 16 {
		t.Fatalf("got %d colors; want 16", len(a))
	}
	if a[15].X != 3 || a[15].Y != 3 {
		t.Fatalf("last color has wrong virtual coordinate: %+v", a[15])
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("wav virtual source colors are not deterministic")
	}
}

func TestCLIStandardSwitchesGeneratePNG(t *testing.T) {
	tests := []struct {
		name string
		args func(wavPath, outPath string) []string
	}{
		{
			name: "short input output width height variations",
			args: func(wavPath, outPath string) []string {
				return []string{
					"-i", wavPath,
					"-w", "8",
					"-H", "8",
					"-o", outPath,
					"-v", "1",
					"--audio-fft-size", "16",
					"--audio-hop-size", "8",
					"--random-seed", "1",
				}
			},
		},
		{
			name: "long input output width height variations",
			args: func(wavPath, outPath string) []string {
				return []string{
					"--input", wavPath,
					"--width", "8",
					"--height", "8",
					"--output", outPath,
					"--variations", "1",
					"--audio-fft-size", "16",
					"--audio-hop-size", "8",
					"--random-seed", "1",
				}
			},
		},
		{
			name: "negative compression value",
			args: func(wavPath, outPath string) []string {
				return []string{
					"--input", wavPath,
					"--width", "8",
					"--height", "8",
					"--output", outPath,
					"--audio-fft-size", "16",
					"--audio-hop-size", "8",
					"--random-seed", "1",
					"--compress", "-2",
				}
			},
		},
		{
			name: "negative compression value with equals",
			args: func(wavPath, outPath string) []string {
				return []string{
					"--input", wavPath,
					"--width", "8",
					"--height", "8",
					"--output", outPath,
					"--audio-fft-size", "16",
					"--audio-hop-size", "8",
					"--random-seed", "1",
					"--compress=-2",
				}
			},
		},
		{
			name: "negative hue value",
			args: func(wavPath, outPath string) []string {
				return []string{
					"--input", wavPath,
					"--width", "8",
					"--height", "8",
					"--output", outPath,
					"--audio-fft-size", "16",
					"--audio-hop-size", "8",
					"--random-seed", "1",
					"--hue", "-180",
				}
			},
		},
		{
			name: "negative hue value with equals",
			args: func(wavPath, outPath string) []string {
				return []string{
					"--input", wavPath,
					"--width", "8",
					"--height", "8",
					"--output", outPath,
					"--audio-fft-size", "16",
					"--audio-hop-size", "8",
					"--random-seed", "1",
					"--hue=-180",
				}
			},
		},
		{
			name: "light theme",
			args: func(wavPath, outPath string) []string {
				return []string{
					"--input", wavPath,
					"--width", "8",
					"--height", "8",
					"--output", outPath,
					"--audio-fft-size", "16",
					"--audio-hop-size", "8",
					"--random-seed", "1",
					"--light",
				}
			},
		},
		{
			name: "dark theme",
			args: func(wavPath, outPath string) []string {
				return []string{
					"--input", wavPath,
					"--width", "8",
					"--height", "8",
					"--output", outPath,
					"--audio-fft-size", "16",
					"--audio-hop-size", "8",
					"--random-seed", "1",
					"--dark",
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wavPath := writeTempWAV(t, 8000, 1, pcmFromFloat(sineSamples(128, 8000, 440)))
			outPath := filepath.Join(t.TempDir(), "out.png")
			output, err := runPix(tt.args(wavPath, outPath)...)
			if err != nil {
				t.Fatalf("pix wav command failed: %v\n%s", err, output)
			}
			assertPNGSize(t, outPath, 8, 8)
		})
	}
}

func TestCLIRejectsOldSingleDashLongFlags(t *testing.T) {
	tests := []struct {
		arg       string
		canonical string
	}{
		{arg: "-in", canonical: "--input"},
		{arg: "-out", canonical: "--output"},
		{arg: "-audio-offset", canonical: "--audio-offset"},
		{arg: "-colorsort", canonical: "--color-sort"},
	}

	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			output, err := runPix(tt.arg, "value")
			if err == nil {
				t.Fatalf("pix accepted old flag %s\n%s", tt.arg, output)
			}
			if !strings.Contains(output, "invalid option "+strconvQuote(tt.arg)) || !strings.Contains(output, tt.canonical) {
				t.Fatalf("rejection output did not explain canonical flag for %s:\n%s", tt.arg, output)
			}
		})
	}
}

func TestCLIRejectsConflictingColorThemes(t *testing.T) {
	wavPath := writeTempWAV(t, 8000, 1, pcmFromFloat(sineSamples(128, 8000, 440)))
	outPath := filepath.Join(t.TempDir(), "out.png")

	output, err := runPix(
		"--input", wavPath,
		"--width", "8",
		"--height", "8",
		"--output", outPath,
		"--audio-fft-size", "16",
		"--audio-hop-size", "8",
		"--light",
		"--dark",
	)
	if err == nil {
		t.Fatalf("pix accepted conflicting color themes\n%s", output)
	}
	if !strings.Contains(output, "--light and --dark cannot be used together") {
		t.Fatalf("color theme rejection output did not explain conflict:\n%s", output)
	}
}

func TestCLIHelpShowsCanonicalSwitches(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			output, err := runPix(arg)
			if err != nil {
				t.Fatalf("pix help failed: %v\n%s", err, output)
			}
			for _, want := range []string{"-i, --input", "-o, --output", "--audio-offset", "--color-sort", "--hue", "--light", "--dark"} {
				if !strings.Contains(output, want) {
					t.Fatalf("help output missing %q:\n%s", want, output)
				}
			}
			for _, old := range []string{"-in", "-out", "-audio-offset", "-colorsort", "--lightness"} {
				if helpContainsOldFlag(output, old) {
					t.Fatalf("help output still contains old flag %q:\n%s", old, output)
				}
			}
		})
	}
}

func runPix(args ...string) (string, error) {
	cmd := exec.Command("go", append([]string{"run", "./cmd/pix"}, args...)...)
	cmd.Dir = filepath.Join("..", "..")
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func assertPNGSize(t *testing.T, path string, width, height int) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening generated png: %v", err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("generated output is not a png: %v", err)
	}
	if got := img.Bounds().Size(); got.X != width || got.Y != height {
		t.Fatalf("generated png size = %v; want %dx%d", got, width, height)
	}
}

func helpContainsOldFlag(output, flag string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), flag) {
			return true
		}
	}
	return false
}

func strconvQuote(s string) string {
	return `"` + s + `"`
}

func wavPCM16(sampleRate, channels int, samples []int16) []byte {
	var sampleData bytes.Buffer
	for _, s := range samples {
		_ = binary.Write(&sampleData, binary.LittleEndian, s)
	}
	data := sampleData.Bytes()
	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(36+len(data)))
	out.WriteString("WAVE")
	out.WriteString("fmt ")
	_ = binary.Write(&out, binary.LittleEndian, uint32(16))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))
	_ = binary.Write(&out, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&out, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&out, binary.LittleEndian, uint32(sampleRate*channels*2))
	_ = binary.Write(&out, binary.LittleEndian, uint16(channels*2))
	_ = binary.Write(&out, binary.LittleEndian, uint16(16))
	out.WriteString("data")
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(data)))
	out.Write(data)
	return out.Bytes()
}

func writeTempWAV(t *testing.T, sampleRate, channels int, samples []int16) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.wav")
	if err := os.WriteFile(path, wavPCM16(sampleRate, channels, samples), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func sineSamples(n, sampleRate int, freq float64) []float64 {
	samples := make([]float64, n)
	for i := range samples {
		samples[i] = math.Sin(2 * math.Pi * freq * float64(i) / float64(sampleRate))
	}
	return samples
}

func alternatingSamples(n int) []float64 {
	samples := make([]float64, n)
	for i := range samples {
		if i%2 == 0 {
			samples[i] = 1
		} else {
			samples[i] = -1
		}
	}
	return samples
}

func noiseSamples(n int) []float64 {
	samples := make([]float64, n)
	state := uint32(1)
	for i := range samples {
		state = state*1664525 + 1013904223
		samples[i] = 2*(float64(state)/float64(^uint32(0))) - 1
	}
	return samples
}

func pcmFromFloat(samples []float64) []int16 {
	ret := make([]int16, len(samples))
	for i, s := range samples {
		ret[i] = int16(visualization.Clamp(s, -1, 1) * 32767)
	}
	return ret
}

func finiteFeature(f Features) bool {
	vals := []float64{
		f.Time,
		f.RMS,
		f.Centroid,
		f.Bandwidth,
		f.Rolloff,
		f.ZeroCrossingRate,
		f.Flatness,
		f.LowEnergy,
		f.LowMidEnergy,
		f.BassEnergy,
		f.MidEnergy,
		f.HighEnergy,
	}
	for _, v := range vals {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

func colorSum(c visualization.ImageColor) int {
	return int(c.R) + int(c.G) + int(c.B)
}

func rgbSaturation(c visualization.ImageColor) int {
	minV := minInt(int(c.R), int(c.G), int(c.B))
	maxV := maxInt(int(c.R), int(c.G), int(c.B))
	return maxV - minV
}

func hueDistance(a, b float64) float64 {
	d := math.Abs(a - b)
	if d > 180 {
		return 360 - d
	}
	return d
}

func minInt(vals ...int) int {
	ret := vals[0]
	for _, v := range vals[1:] {
		if v < ret {
			ret = v
		}
	}
	return ret
}

func maxInt(vals ...int) int {
	ret := vals[0]
	for _, v := range vals[1:] {
		if v > ret {
			ret = v
		}
	}
	return ret
}
