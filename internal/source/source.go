package source

import (
	"path"
	"strings"

	"github.com/yurivish/pix/internal/audio"
	"github.com/yurivish/pix/internal/visualization"
)

type Options struct {
	Width, Height int
	Audio         audio.Options
}

func Load(filepath string, opts Options) ([]visualization.ImageColor, error) {
	ext := strings.ToLower(path.Ext(filepath))
	if ext == ".wav" {
		audioOpts := opts.Audio
		if audioOpts.Width == 0 {
			audioOpts.Width = opts.Width
		}
		if audioOpts.Height == 0 {
			audioOpts.Height = opts.Height
		}
		return audio.BuildImageColorsFromWAV(filepath, audioOpts)
	}
	return LoadImage(filepath)
}
