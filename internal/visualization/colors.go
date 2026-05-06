package visualization

import "math"

type ImageColor struct {
	X, Y    int
	R, G, B uint8
}

type ColorTheme string

const (
	ThemeLight ColorTheme = "light"
	ThemeDark  ColorTheme = "dark"
)

func Clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

func RotateImageColorsHue(colors []ImageColor, degrees float64) []ImageColor {
	hueShift := normalizeHue(degrees)
	ret := make([]ImageColor, len(colors))
	for i, c := range colors {
		h, s, v := RGBToHSV(c.R, c.G, c.B)
		r, g, b := HSVToRGB(h+hueShift, s, v)
		ret[i] = ImageColor{X: c.X, Y: c.Y, R: r, G: g, B: b}
	}
	return ret
}

func ThemeImageColors(colors []ImageColor, theme ColorTheme) []ImageColor {
	ret := make([]ImageColor, len(colors))
	for i, c := range colors {
		h, s, v := RGBToHSV(c.R, c.G, c.B)
		switch theme {
		case ThemeLight:
			s = Clamp(0.35*s+0.02, 0, 1)
			v = Clamp(0.93+0.07*v, 0, 1)
		case ThemeDark:
			s = Clamp(1.15*s, 0, 1)
			v = Clamp(0.08+0.52*v, 0, 1)
		}
		r, g, b := HSVToRGB(h, s, v)
		ret[i] = ImageColor{
			X: c.X,
			Y: c.Y,
			R: r,
			G: g,
			B: b,
		}
	}
	return ret
}

func HSVToRGB(h, s, v float64) (uint8, uint8, uint8) {
	h = normalizeHue(h)
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
	return quantize(Clamp(r+m, 0, 1)), quantize(Clamp(g+m, 0, 1)), quantize(Clamp(b+m, 0, 1))
}

func RGBToHSV(r, g, b uint8) (float64, float64, float64) {
	rf, gf, bf := invQuantize(r), invQuantize(g), invQuantize(b)
	max := math.Max(rf, math.Max(gf, bf))
	min := math.Min(rf, math.Min(gf, bf))
	delta := max - min

	h := 0.0
	switch {
	case delta == 0:
		h = 0
	case max == rf:
		h = 60 * math.Mod((gf-bf)/delta, 6)
	case max == gf:
		h = 60 * ((bf-rf)/delta + 2)
	default:
		h = 60 * ((rf-gf)/delta + 4)
	}
	h = normalizeHue(h)

	s := 0.0
	if max > 0 {
		s = delta / max
	}
	return h, s, max
}

func normalizeHue(h float64) float64 {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	return h
}

func quantize(x float64) uint8 { return uint8(255*x + 0.5) }

func invQuantize(x uint8) float64 { return float64(x) / 255 }
