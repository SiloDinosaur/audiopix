package placement

import "testing"

func TestGrayscale(t *testing.T) {
	if got, want := grayscale(255, 0, 0), uint8(76); got != want {
		t.Fatalf("grayscale red = %v; want %v", got, want)
	}
	if got, want := grayscale(0, 255, 0), uint8(150); got != want {
		t.Fatalf("grayscale green = %v; want %v", got, want)
	}
	if got, want := grayscale(0, 0, 255), uint8(29); got != want {
		t.Fatalf("grayscale blue = %v; want %v", got, want)
	}
}

func TestImageDataBlackAndWhite(t *testing.T) {
	canvas := NewCanvas(1, 1, 0)
	rgb := Color{255, 0, 0}
	lab := rgbToOkLab(rgb)
	code := mortonCode(lab.x, lab.y, lab.z)
	canvas.PlaceSeed(SampledColor{lab: lab, labCode: code}, 0, 0)

	colorData := canvas.ImageData(false)
	bwData := canvas.ImageData(true)

	if colorData[0] == colorData[1] && colorData[1] == colorData[2] {
		t.Fatalf("test setup produced grayscale color data: %v", colorData[:4])
	}
	if bwData[0] != bwData[1] || bwData[1] != bwData[2] {
		t.Fatalf("black-and-white image data should have equal RGB channels: %v", bwData[:4])
	}
	if bwData[3] != 255 {
		t.Fatalf("black-and-white image data changed alpha: got %v; want 255", bwData[3])
	}
}
