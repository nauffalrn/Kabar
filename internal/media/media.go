// Package media validates uploaded images and renders the stored variants.
package media

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png" // register decoder
	"net/http"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register decoder

	"article-service/internal/article"
)

const (
	largeWidth  = 1600
	thumbWidth  = 480
	thumbHeight = 300 // 16:10, the ratio every card uses
	maxPixels   = 40_000_000
	jpegQuality = 82
)

var allowed = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}

type Processor struct{}

// Process checks the real file type from its bytes, never the client's
// filename or header, and re-encodes it, which also strips metadata.
func (Processor) Process(data []byte) ([]article.ProcessedFile, error) {
	if !allowed[http.DetectContentType(data)] {
		return nil, article.ErrUnsupportedImage
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, article.ErrUnsupportedImage
	}
	if cfg.Width*cfg.Height > maxPixels {
		return nil, article.ErrUnsupportedImage // decompression bomb guard
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, article.ErrUnsupportedImage
	}

	large, err := encode(fit(src, largeWidth))
	if err != nil {
		return nil, err
	}
	thumb, err := encode(cover(src, thumbWidth, thumbHeight))
	if err != nil {
		return nil, err
	}
	return []article.ProcessedFile{
		{Name: "large.jpg", ContentType: "image/jpeg", Data: large},
		{Name: "thumb.jpg", ContentType: "image/jpeg", Data: thumb},
	}, nil
}

// fit scales down to maxWidth, keeping the aspect ratio; it never upscales.
func fit(src image.Image, maxWidth int) image.Image {
	b := src.Bounds()
	if b.Dx() <= maxWidth {
		return flatten(src, b.Dx(), b.Dy(), b)
	}
	h := b.Dy() * maxWidth / b.Dx()
	return flatten(src, maxWidth, h, b)
}

// cover crops the centre to the target ratio, then scales it.
func cover(src image.Image, w, h int) image.Image {
	b := src.Bounds()
	crop := b
	if b.Dx()*h > b.Dy()*w {
		cw := b.Dy() * w / h
		crop.Min.X = b.Min.X + (b.Dx()-cw)/2
		crop.Max.X = crop.Min.X + cw
	} else {
		ch := b.Dx() * h / w
		crop.Min.Y = b.Min.Y + (b.Dy()-ch)/2
		crop.Max.Y = crop.Min.Y + ch
	}
	return flatten(src, w, h, crop)
}

// flatten draws onto white, because JPEG has no transparency.
func flatten(src image.Image, w, h int, from image.Rectangle) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, from, draw.Over, nil)
	return dst
}

func encode(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}
