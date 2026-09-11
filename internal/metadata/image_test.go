package metadata

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestNormalizeCoverScalesLargeImage(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1800, 900))
	for y := 0; y < 900; y++ {
		for x := 0; x < 1800; x++ {
			src.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, src, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	got, mime, err := NormalizeCover(encoded.Bytes(), "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if mime != "image/jpeg" || len(got) > maxCoverBytes {
		t.Fatalf("mime=%q bytes=%d", mime, len(got))
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(got))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 1200 || config.Height != 600 {
		t.Fatalf("dimensions=%dx%d", config.Width, config.Height)
	}
}

func TestNormalizeCoverRejectsPixelBomb(t *testing.T) {
	// A valid JPEG header with dimensions above the configured pixel limit is
	// sufficient for DecodeConfig and avoids allocating the full image.
	data := []byte{0xff, 0xd8, 0xff, 0xc0, 0x00, 0x11, 0x08, 0x27, 0x10, 0x27, 0x10, 0x03, 0x01, 0x11, 0x00, 0x02, 0x11, 0x00, 0x03, 0x11, 0x00, 0xff, 0xd9}
	if _, _, err := NormalizeCover(data, "image/jpeg"); err == nil {
		t.Fatal("expected excessive pixel count to be rejected")
	}
}
