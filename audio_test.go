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
}

func TestFeaturesToImageColorsBrightnessAndHue(t *testing.T) {
	brightness, err := FeaturesToImageColors([]AudioFeatures{
		{RMS: 0, Flatness: 0.1, BassEnergy: 1},
		{RMS: 1, Flatness: 0.1, BassEnergy: 1},
	}, 2, 1, PaletteNatural)
	if err != nil {
		t.Fatalf("FeaturesToImageColors returned error: %v", err)
	}
	if colorSum(brightness[1]) <= colorSum(brightness[0]) {
		t.Fatalf("loud frame should be brighter: quiet=%+v loud=%+v", brightness[0], brightness[1])
	}

	hues, err := FeaturesToImageColors([]AudioFeatures{
		{RMS: 1, Flatness: 0.1, BassEnergy: 10},
		{RMS: 1, Flatness: 0.1, MidEnergy: 10},
		{RMS: 1, Flatness: 0.1, HighEnergy: 10},
	}, 3, 1, PaletteNatural)
	if err != nil {
		t.Fatalf("FeaturesToImageColors returned error: %v", err)
	}
	bass, mid, high := hues[0], hues[1], hues[2]
	if !(bass.R > bass.G && bass.B > bass.G) {
		t.Fatalf("bass color should be red/purple biased: %+v", bass)
	}
	if !(mid.G > mid.R && mid.G > mid.B) {
		t.Fatalf("mid color should be green biased: %+v", mid)
	}
	if !(high.B > high.R && high.G > high.R) {
		t.Fatalf("high color should be cyan/blue biased: %+v", high)
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

func pcmFromFloat(samples []float64) []int16 {
	ret := make([]int16, len(samples))
	for i, s := range samples {
		ret[i] = int16(clamp(s, -1, 1) * 32767)
	}
	return ret
}

func finiteFeature(f AudioFeatures) bool {
	vals := []float64{f.Time, f.RMS, f.Centroid, f.Flatness, f.BassEnergy, f.MidEnergy, f.HighEnergy}
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
