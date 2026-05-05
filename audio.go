package pix

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type AudioPalette string

const (
	PaletteNatural AudioPalette = "natural"
)

type AudioOptions struct {
	Width, Height int
	Offset        time.Duration
	Duration      time.Duration
	Mono          bool
	FFTSize       int
	HopSize       int
	Palette       AudioPalette
}

type AudioData struct {
	Samples      []float64
	SampleRate   int
	Channels     int
	SourceFrames int
}

type AudioFeatures struct {
	Time       float64
	RMS        float64
	Centroid   float64
	Flatness   float64
	BassEnergy float64
	MidEnergy  float64
	HighEnergy float64
}

func (opts AudioOptions) withDefaults(width, height int) AudioOptions {
	if opts.Width == 0 {
		opts.Width = width
	}
	if opts.Height == 0 {
		opts.Height = height
	}
	if opts.FFTSize == 0 {
		opts.FFTSize = 2048
	}
	if opts.HopSize == 0 {
		opts.HopSize = 512
	}
	if opts.Palette == "" {
		opts.Palette = PaletteNatural
	}
	return opts
}

func BuildImageColorsFromWAV(filepath string, opts AudioOptions) ([]ImageColor, error) {
	opts = opts.withDefaults(opts.Width, opts.Height)
	if opts.Width <= 0 || opts.Height <= 0 {
		return nil, fmt.Errorf("wav input requires positive width and height")
	}
	if err := validateAudioOptions(opts); err != nil {
		return nil, err
	}
	audio, err := DecodeWAVFileWithOptions(filepath, opts.Mono)
	if err != nil {
		return nil, err
	}
	audio, err = cropAudio(audio, opts.Offset, opts.Duration)
	if err != nil {
		return nil, err
	}
	features, err := ExtractAudioFeatures(audio, opts)
	if err != nil {
		return nil, err
	}
	features = ResampleAudioFeatures(features, opts.Width*opts.Height)
	return FeaturesToImageColors(features, opts.Width, opts.Height, opts.Palette)
}

func DecodeWAVFile(filepath string) (AudioData, error) {
	return DecodeWAVFileWithOptions(filepath, true)
}

func DecodeWAVFileWithOptions(filepath string, mono bool) (AudioData, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return AudioData{}, fmt.Errorf("error opening wav file: %w", err)
	}
	return DecodeWAV(data, mono)
}

func DecodeWAV(data []byte, mono bool) (AudioData, error) {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return AudioData{}, fmt.Errorf("invalid wav file")
	}

	var fmtChunk []byte
	var sampleBytes []byte
	for off := 12; off+8 <= len(data); {
		id := string(data[off : off+4])
		size := int(binary.LittleEndian.Uint32(data[off+4 : off+8]))
		off += 8
		if size < 0 || off+size > len(data) {
			return AudioData{}, fmt.Errorf("invalid wav chunk size")
		}
		chunk := data[off : off+size]
		switch id {
		case "fmt ":
			fmtChunk = chunk
		case "data":
			sampleBytes = chunk
		}
		off += size
		if size%2 == 1 {
			off++
		}
	}
	if len(fmtChunk) < 16 {
		return AudioData{}, fmt.Errorf("wav file missing fmt chunk")
	}
	if len(sampleBytes) == 0 {
		return AudioData{}, fmt.Errorf("wav file has no audio data")
	}

	format := binary.LittleEndian.Uint16(fmtChunk[0:2])
	channels := int(binary.LittleEndian.Uint16(fmtChunk[2:4]))
	sampleRate := int(binary.LittleEndian.Uint32(fmtChunk[4:8]))
	blockAlign := int(binary.LittleEndian.Uint16(fmtChunk[12:14]))
	bitsPerSample := int(binary.LittleEndian.Uint16(fmtChunk[14:16]))
	if channels <= 0 || sampleRate <= 0 {
		return AudioData{}, fmt.Errorf("wav file has invalid channel count or sample rate")
	}
	if format != 1 && format != 3 {
		return AudioData{}, fmt.Errorf("unsupported wav format %d", format)
	}
	bytesPerSample := (bitsPerSample + 7) / 8
	if format == 1 && bitsPerSample != 8 && bitsPerSample != 16 && bitsPerSample != 24 && bitsPerSample != 32 {
		return AudioData{}, fmt.Errorf("unsupported pcm bit depth %d", bitsPerSample)
	}
	if format == 3 && bitsPerSample != 32 && bitsPerSample != 64 {
		return AudioData{}, fmt.Errorf("unsupported float bit depth %d", bitsPerSample)
	}
	if blockAlign < channels*bytesPerSample || blockAlign == 0 {
		return AudioData{}, fmt.Errorf("invalid wav block alignment")
	}

	frames := len(sampleBytes) / blockAlign
	if frames == 0 {
		return AudioData{}, fmt.Errorf("wav file has no complete frames")
	}
	outChannels := channels
	if mono {
		outChannels = 1
	}
	samples := make([]float64, frames*outChannels)
	for frame := 0; frame < frames; frame++ {
		base := frame * blockAlign
		sum := 0.0
		for ch := 0; ch < channels; ch++ {
			start := base + ch*bytesPerSample
			if start+bytesPerSample > len(sampleBytes) {
				return AudioData{}, fmt.Errorf("truncated wav sample data")
			}
			sample := decodeWAVSample(sampleBytes[start:start+bytesPerSample], format, bitsPerSample)
			if mono {
				sum += sample
			} else {
				samples[frame*outChannels+ch] = sample
			}
		}
		if mono {
			samples[frame] = clamp(sum/float64(channels), -1, 1)
		}
	}
	return AudioData{Samples: samples, SampleRate: sampleRate, Channels: outChannels, SourceFrames: frames}, nil
}

func ExtractAudioFeatures(audio AudioData, opts AudioOptions) ([]AudioFeatures, error) {
	if len(audio.Samples) == 0 {
		return nil, fmt.Errorf("audio contains no samples")
	}
	if audio.SampleRate <= 0 {
		return nil, fmt.Errorf("audio has invalid sample rate")
	}
	if audio.Channels <= 0 {
		return nil, fmt.Errorf("audio has invalid channel count")
	}
	if err := validateAudioOptions(opts); err != nil {
		return nil, err
	}

	frameSize := opts.FFTSize
	hopSize := opts.HopSize
	nSamples := len(audio.Samples) / audio.Channels
	nFrames := 1
	if nSamples > frameSize {
		nFrames = 1 + int(math.Ceil(float64(nSamples-frameSize)/float64(hopSize)))
	}
	window := hannWindow(frameSize)
	features := make([]AudioFeatures, nFrames)
	for frame := 0; frame < nFrames; frame++ {
		start := frame * hopSize
		buf := make([]complex128, frameSize)
		sumSq := 0.0
		for i := 0; i < frameSize; i++ {
			s := 0.0
			if start+i < nSamples {
				s = monoSampleAt(audio, start+i)
			}
			sumSq += s * s
			buf[i] = complex(s*window[i], 0)
		}
		fft(buf)
		features[frame] = spectrumFeatures(buf, audio.SampleRate, start, sumSq, frameSize)
	}
	return features, nil
}

func ResampleAudioFeatures(features []AudioFeatures, n int) []AudioFeatures {
	if n <= 0 || len(features) == 0 {
		return nil
	}
	if len(features) == n {
		ret := make([]AudioFeatures, n)
		copy(ret, features)
		return ret
	}
	if len(features) == 1 {
		ret := make([]AudioFeatures, n)
		for i := range ret {
			ret[i] = features[0]
		}
		return ret
	}
	ret := make([]AudioFeatures, n)
	scale := float64(len(features)-1) / float64(n-1)
	for i := range ret {
		pos := float64(i) * scale
		j := int(pos)
		t := pos - float64(j)
		if j >= len(features)-1 {
			ret[i] = features[len(features)-1]
			continue
		}
		ret[i] = lerpFeature(features[j], features[j+1], t)
	}
	return ret
}

func FeaturesToImageColors(features []AudioFeatures, width, height int, palette AudioPalette) ([]ImageColor, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("width and height must be positive")
	}
	if len(features) != width*height {
		return nil, fmt.Errorf("feature count %d does not match image size %d", len(features), width*height)
	}
	if palette == "" {
		palette = PaletteNatural
	}
	if palette != PaletteNatural {
		return nil, fmt.Errorf("unknown audio palette %q", palette)
	}

	rms := values(features, func(f AudioFeatures) float64 { return f.RMS })
	flatness := values(features, func(f AudioFeatures) float64 { return f.Flatness })
	bass := values(features, func(f AudioFeatures) float64 { return f.BassEnergy })
	mid := values(features, func(f AudioFeatures) float64 { return f.MidEnergy })
	high := values(features, func(f AudioFeatures) float64 { return f.HighEnergy })
	rmsNorm := normalizer(rms)
	flatNorm := normalizer(flatness)
	bassNorm := normalizer(bass)
	midNorm := normalizer(mid)
	highNorm := normalizer(high)

	colors := make([]ImageColor, width*height)
	for i, f := range features {
		x, y := i%width, i/width
		b := bassNorm(f.BassEnergy)
		m := midNorm(f.MidEnergy)
		h := highNorm(f.HighEnergy)
		total := b + m + h
		hue := 0.0
		if total > 1e-12 {
			hue = (310*b + 95*m + 205*h) / total
		} else {
			centroid := clamp(f.Centroid/16000, 0, 1)
			hue = 260 - 200*centroid
			if hue < 0 {
				hue += 360
			}
		}
		saturation := clamp(0.35+0.65*(1-flatNorm(f.Flatness)), 0, 1)
		value := clamp(0.12+0.88*rmsNorm(f.RMS), 0, 1)
		r, g, bb := hsvToRGB(hue, saturation, value)
		colors[i] = ImageColor{x, y, r, g, bb}
	}
	return colors, nil
}

func validateAudioOptions(opts AudioOptions) error {
	if opts.FFTSize <= 0 || opts.HopSize <= 0 {
		return fmt.Errorf("audio fft size and hop size must be positive")
	}
	if opts.FFTSize&(opts.FFTSize-1) != 0 {
		return fmt.Errorf("audio fft size must be a power of two")
	}
	if opts.HopSize > opts.FFTSize {
		return fmt.Errorf("audio hop size must not exceed fft size")
	}
	return nil
}

func cropAudio(audio AudioData, offset, duration time.Duration) (AudioData, error) {
	if offset < 0 || duration < 0 {
		return AudioData{}, fmt.Errorf("audio offset and duration must not be negative")
	}
	channels := audio.Channels
	if channels <= 0 {
		channels = 1
	}
	nFrames := len(audio.Samples) / channels
	startFrame := int(offset.Seconds() * float64(audio.SampleRate))
	if startFrame > nFrames {
		return AudioData{}, fmt.Errorf("audio offset is beyond the end of the file")
	}
	endFrame := nFrames
	if duration > 0 {
		durationEnd := startFrame + int(duration.Seconds()*float64(audio.SampleRate))
		if durationEnd < endFrame {
			endFrame = durationEnd
		}
	}
	if startFrame >= endFrame {
		return AudioData{}, fmt.Errorf("selected audio range is empty")
	}
	audio.Samples = audio.Samples[startFrame*channels : endFrame*channels]
	audio.SourceFrames = endFrame - startFrame
	return audio, nil
}

func monoSampleAt(audio AudioData, frame int) float64 {
	if audio.Channels == 1 {
		return audio.Samples[frame]
	}
	base := frame * audio.Channels
	sum := 0.0
	for ch := 0; ch < audio.Channels; ch++ {
		sum += audio.Samples[base+ch]
	}
	return sum / float64(audio.Channels)
}

func decodeWAVSample(b []byte, format uint16, bits int) float64 {
	if format == 3 {
		if bits == 32 {
			return clamp(float64(math.Float32frombits(binary.LittleEndian.Uint32(b))), -1, 1)
		}
		return clamp(math.Float64frombits(binary.LittleEndian.Uint64(b)), -1, 1)
	}
	switch bits {
	case 8:
		return clamp((float64(b[0])-128)/128, -1, 1)
	case 16:
		return clamp(float64(int16(binary.LittleEndian.Uint16(b)))/32768, -1, 1)
	case 24:
		v := int32(b[0]) | int32(b[1])<<8 | int32(b[2])<<16
		if v&0x800000 != 0 {
			v |= ^0xffffff
		}
		return clamp(float64(v)/8388608, -1, 1)
	case 32:
		return clamp(float64(int32(binary.LittleEndian.Uint32(b)))/2147483648, -1, 1)
	default:
		return 0
	}
}

func hannWindow(n int) []float64 {
	w := make([]float64, n)
	if n == 1 {
		w[0] = 1
		return w
	}
	for i := range w {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))
	}
	return w
}

func spectrumFeatures(buf []complex128, sampleRate, start int, sumSq float64, frameSize int) AudioFeatures {
	half := len(buf) / 2
	sumMag, sumPower, weightedFreq := 0.0, 0.0, 0.0
	logPower := 0.0
	bass, mid, high := 0.0, 0.0, 0.0
	const eps = 1e-12
	for bin := 1; bin <= half; bin++ {
		re, im := real(buf[bin]), imag(buf[bin])
		power := re*re + im*im
		mag := math.Sqrt(power)
		freq := float64(bin*sampleRate) / float64(len(buf))
		sumMag += mag
		sumPower += power
		weightedFreq += freq * mag
		logPower += math.Log(power + eps)
		switch {
		case freq >= 20 && freq < 250:
			bass += power
		case freq >= 250 && freq < 4000:
			mid += power
		case freq >= 4000 && freq <= 16000:
			high += power
		}
	}
	centroid := 0.0
	if sumMag > eps {
		centroid = weightedFreq / sumMag
	}
	flatness := 0.0
	if half > 0 && sumPower > eps {
		flatness = math.Exp(logPower/float64(half)) / (sumPower / float64(half))
	}
	return AudioFeatures{
		Time:       float64(start) / float64(sampleRate),
		RMS:        math.Sqrt(sumSq / float64(frameSize)),
		Centroid:   centroid,
		Flatness:   clamp(flatness, 0, 1),
		BassEnergy: bass,
		MidEnergy:  mid,
		HighEnergy: high,
	}
}

func fft(a []complex128) {
	n := len(a)
	j := 0
	for i := 1; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j &^= bit
		}
		j |= bit
		if i < j {
			a[i], a[j] = a[j], a[i]
		}
	}
	for length := 2; length <= n; length <<= 1 {
		angle := -2 * math.Pi / float64(length)
		wlen := complex(math.Cos(angle), math.Sin(angle))
		for i := 0; i < n; i += length {
			w := complex(1, 0)
			for k := 0; k < length/2; k++ {
				u := a[i+k]
				v := a[i+k+length/2] * w
				a[i+k] = u + v
				a[i+k+length/2] = u - v
				w *= wlen
			}
		}
	}
}

func lerpFeature(a, b AudioFeatures, t float64) AudioFeatures {
	return AudioFeatures{
		Time:       lerp(a.Time, b.Time, t),
		RMS:        lerp(a.RMS, b.RMS, t),
		Centroid:   lerp(a.Centroid, b.Centroid, t),
		Flatness:   lerp(a.Flatness, b.Flatness, t),
		BassEnergy: lerp(a.BassEnergy, b.BassEnergy, t),
		MidEnergy:  lerp(a.MidEnergy, b.MidEnergy, t),
		HighEnergy: lerp(a.HighEnergy, b.HighEnergy, t),
	}
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

func values(features []AudioFeatures, get func(AudioFeatures) float64) []float64 {
	ret := make([]float64, len(features))
	for i, f := range features {
		v := get(f)
		if math.IsNaN(v) || math.IsInf(v, 0) {
			v = 0
		}
		ret[i] = v
	}
	return ret
}

func normalizer(vals []float64) func(float64) float64 {
	if len(vals) == 0 {
		return func(float64) float64 { return 0 }
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)
	lo := percentile(sorted, 0.02)
	hi := percentile(sorted, 0.98)
	if math.Abs(hi-lo) < 1e-12 {
		if math.Abs(hi) < 1e-12 {
			return func(float64) float64 { return 0 }
		}
		return func(float64) float64 { return 0.5 }
	}
	return func(x float64) float64 {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return 0
		}
		return clamp((x-lo)/(hi-lo), 0, 1)
	}
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 1 {
		return sorted[0]
	}
	pos := p * float64(len(sorted)-1)
	i := int(pos)
	t := pos - float64(i)
	if i >= len(sorted)-1 {
		return sorted[len(sorted)-1]
	}
	return lerp(sorted[i], sorted[i+1], t)
}

func hsvToRGB(h, s, v float64) (uint8, uint8, uint8) {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return quantize(clamp(r+m, 0, 1)), quantize(clamp(g+m, 0, 1)), quantize(clamp(b+m, 0, 1))
}

func ParseAudioDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" || s == "0s" {
		return 0, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	seconds, ok := parseFloat(s)
	if !ok {
		return 0, fmt.Errorf("could not parse audio duration %q", s)
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func parseFloat(s string) (float64, bool) {
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}
