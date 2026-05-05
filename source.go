package pix

import (
	"path"
	"strings"
)

type SourceOptions struct {
	Width, Height int
	Audio         AudioOptions
}

func LoadSource(filepath string, opts SourceOptions) ([]ImageColor, error) {
	ext := strings.ToLower(path.Ext(filepath))
	if ext == ".wav" {
		audioOpts := opts.Audio.withDefaults(opts.Width, opts.Height)
		return BuildImageColorsFromWAV(filepath, audioOpts)
	}
	return LoadImage(filepath)
}
