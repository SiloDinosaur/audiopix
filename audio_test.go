package pix

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
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

func TestExtractAndResampleAudioFeatures(t *testing.T) {
	audio := AudioData{Samples: sineSamples(128, 8000, 440), SampleRate: 8000, Channels: 1, SourceFrames: 128}
	features, err := ExtractAudioFeatures(audio, AudioOptions{FFTSize: 64, HopSize: 16})
	if err != nil {
		t.Fatalf("ExtractAudioFeatures returned error: %v", err)
	}
	if len(features) == 0 {
		t.Fatal("expected at least one feature frame")
	}
	for _, f := range features {
		if !finiteFeature(f) {
			t.Fatalf("feature contains non-finite values: %+v", f)
		}
	}
	resampled := ResampleAudioFeatures(features, 17)
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
	lowAudio := AudioData{Samples: sineSamples(4096, 44100, 110), SampleRate: 44100, Channels: 1, SourceFrames: 4096}
	highAudio := AudioData{Samples: sineSamples(4096, 44100, 8000), SampleRate: 44100, Channels: 1, SourceFrames: 4096}

	low, err := ExtractAudioFeatures(lowAudio, AudioOptions{FFTSize: 4096, HopSize: 4096})
	if err != nil {
		t.Fatalf("ExtractAudioFeatures low returned error: %v", err)
	}
	high, err := ExtractAudioFeatures(highAudio, AudioOptions{FFTSize: 4096, HopSize: 4096})
	if err != nil {
		t.Fatalf("ExtractAudioFeatures high returned error: %v", err)
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
	audio := AudioData{Samples: alternatingSamples(64), SampleRate: 8000, Channels: 1, SourceFrames: 64}
	features, err := ExtractAudioFeatures(audio, AudioOptions{FFTSize: 64, HopSize: 64})
	if err != nil {
		t.Fatalf("ExtractAudioFeatures returned error: %v", err)
	}
	if features[0].ZeroCrossingRate < 0.9 {
		t.Fatalf("alternating waveform should have high zero-crossing rate: %+v", features[0])
	}
}

func TestExtractAudioFeatureFlatnessAndBandwidth(t *testing.T) {
	toneAudio := AudioData{Samples: sineSamples(4096, 44100, 440), SampleRate: 44100, Channels: 1, SourceFrames: 4096}
	noiseAudio := AudioData{Samples: noiseSamples(4096), SampleRate: 44100, Channels: 1, SourceFrames: 4096}

	tone, err := ExtractAudioFeatures(toneAudio, AudioOptions{FFTSize: 4096, HopSize: 4096})
	if err != nil {
		t.Fatalf("ExtractAudioFeatures tone returned error: %v", err)
	}
	noise, err := ExtractAudioFeatures(noiseAudio, AudioOptions{FFTSize: 4096, HopSize: 4096})
	if err != nil {
		t.Fatalf("ExtractAudioFeatures noise returned error: %v", err)
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
	brightness, err := FeaturesToImageColors([]AudioFeatures{
		{RMS: 0, Flatness: 0.1, LowEnergy: 1, BassEnergy: 1},
		{RMS: 1, Flatness: 0.1, LowEnergy: 1, BassEnergy: 1},
	}, 2, 1, PaletteNatural)
	if err != nil {
		t.Fatalf("FeaturesToImageColors returned error: %v", err)
	}
	if colorSum(brightness[1]) <= colorSum(brightness[0]) {
		t.Fatalf("loud frame should be brighter: quiet=%+v loud=%+v", brightness[0], brightness[1])
	}

	hues, err := FeaturesToImageColors([]AudioFeatures{
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

	saturation, err := FeaturesToImageColors([]AudioFeatures{
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

func TestBuildImageColorsFromWAVDeterministic(t *testing.T) {
	path := writeTempWAV(t, 8000, 1, pcmFromFloat(sineSamples(64, 8000, 440)))
	opts := AudioOptions{Width: 4, Height: 4, Mono: true, FFTSize: 16, HopSize: 8, Palette: PaletteNatural}
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

func TestLoadSourceImageMatchesLoadImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.png")
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	img.Set(1, 0, color.RGBA{R: 40, G: 50, B: 60, A: 255})
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	fromImage, err := LoadImage(path)
	if err != nil {
		t.Fatalf("LoadImage returned error: %v", err)
	}
	fromSource, err := LoadSource(path, SourceOptions{Width: 8, Height: 8, Audio: AudioOptions{Palette: PaletteNatural}})
	if err != nil {
		t.Fatalf("LoadSource returned error: %v", err)
	}
	if !reflect.DeepEqual(fromImage, fromSource) {
		t.Fatalf("LoadSource image path differs from LoadImage: %v != %v", fromSource, fromImage)
	}
}

func TestCLIWAVInputGeneratesPNG(t *testing.T) {
	wavPath := writeTempWAV(t, 8000, 1, pcmFromFloat(sineSamples(128, 8000, 440)))
	outPath := filepath.Join(t.TempDir(), "out.png")
	cmd := exec.Command("go", "run", "./cmd/pix",
		"-in", wavPath,
		"-width", "8",
		"-height", "8",
		"-out", outPath,
		"-audio-fft-size", "16",
		"-audio-hop-size", "8",
		"-random-seed", "1",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pix wav command failed: %v\n%s", err, output)
	}
	f, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("opening generated png: %v", err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("generated output is not a png: %v", err)
	}
	if got := img.Bounds().Size(); got.X != 8 || got.Y != 8 {
		t.Fatalf("generated png size = %v; want 8x8", got)
	}
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
		ret[i] = int16(clamp(s, -1, 1) * 32767)
	}
	return ret
}

func finiteFeature(f AudioFeatures) bool {
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

func colorSum(c ImageColor) int {
	return int(c.R) + int(c.G) + int(c.B)
}

func rgbSaturation(c ImageColor) int {
	minV := minInt(int(c.R), int(c.G), int(c.B))
	maxV := maxInt(int(c.R), int(c.G), int(c.B))
	return maxV - minV
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
