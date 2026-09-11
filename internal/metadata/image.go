package metadata

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"math"

	xdraw "golang.org/x/image/draw"
)

const (
	maxCoverDimension = 1200
	maxCoverBytes     = 2 << 20
	maxCoverPixels    = 25_000_000
)

func NormalizeCover(data []byte, mime string) ([]byte, string, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", errors.New("画像をデコードできません")
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > maxCoverPixels {
		return nil, "", errors.New("画像の画素数が上限を超えています")
	}
	if config.Width <= maxCoverDimension && config.Height <= maxCoverDimension && len(data) <= maxCoverBytes {
		return data, mime, nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	scale := math.Min(1, math.Min(float64(maxCoverDimension)/float64(config.Width), float64(maxCoverDimension)/float64(config.Height)))
	w, h := max(1, int(math.Round(float64(config.Width)*scale))), max(1, int(math.Round(float64(config.Height)*scale)))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	var out bytes.Buffer
	quality := 90
	for {
		out.Reset()
		if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: quality}); err != nil {
			return nil, "", err
		}
		if out.Len() <= maxCoverBytes || quality <= 70 {
			break
		}
		quality -= 5
	}
	if out.Len() > maxCoverBytes {
		return nil, "", errors.New("ジャケット画像を2 MiB以下に変換できません")
	}
	return out.Bytes(), "image/jpeg", nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
