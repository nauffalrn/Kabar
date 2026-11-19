package media

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	"article-service/internal/article"
)

func pngOf(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, h/2, color.RGBA{R: 200, A: 255})
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func size(t *testing.T, data []byte) (int, int) {
	t.Helper()
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if format != "jpeg" {
		t.Fatalf("variants should be jpeg, got %s", format)
	}
	return cfg.Width, cfg.Height
}

func TestProcess_RendersBothVariants(t *testing.T) {
	files, err := Processor{}.Process(pngOf(3200, 1200))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Name != "large.jpg" || files[1].Name != "thumb.jpg" {
		t.Fatalf("unexpected files %+v", files)
	}
	if w, h := size(t, files[0].Data); w != largeWidth || h != 600 {
		t.Fatalf("large should keep aspect at %dpx wide, got %dx%d", largeWidth, w, h)
	}
	if w, h := size(t, files[1].Data); w != thumbWidth || h != thumbHeight {
		t.Fatalf("thumb should be cropped to %dx%d, got %dx%d", thumbWidth, thumbHeight, w, h)
	}
}

func TestProcess_NeverUpscales(t *testing.T) {
	files, err := Processor{}.Process(pngOf(800, 500))
	if err != nil {
		t.Fatal(err)
	}
	if w, _ := size(t, files[0].Data); w != 800 {
		t.Fatalf("small images keep their width, got %d", w)
	}
}

func TestProcess_RejectsNonImages(t *testing.T) {
	for name, data := range map[string][]byte{
		"text":      []byte("bukan gambar"),
		"html":      []byte("<html><script>alert(1)</script></html>"),
		"truncated": pngOf(10, 10)[:20],
	} {
		if _, err := (Processor{}).Process(data); !errors.Is(err, article.ErrUnsupportedImage) {
			t.Errorf("%s: want ErrUnsupportedImage, got %v", name, err)
		}
	}
}
