package source

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/yurivish/pix/internal/audio"
)

func TestLoadImagePathMatchesLoad(t *testing.T) {
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
	fromSource, err := Load(path, Options{Width: 8, Height: 8, Audio: audio.Options{Palette: audio.PaletteNatural}})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !reflect.DeepEqual(fromImage, fromSource) {
		t.Fatalf("Load image path differs from LoadImage: %v != %v", fromSource, fromImage)
	}
}
