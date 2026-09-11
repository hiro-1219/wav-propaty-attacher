package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type CoverArt struct {
	BaseURL string
	HTTP    *http.Client
}

func NewCoverArt() *CoverArt {
	return &CoverArt{BaseURL: "https://coverartarchive.org", HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *CoverArt) Front(ctx context.Context, releaseID string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/release/"+releaseID+"/front-1200", nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "WavPropertyAttacher/0.1")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("cover art HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > 10<<20 {
		return nil, "", errors.New("cover art exceeds 10 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (10<<20)+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > 10<<20 {
		return nil, "", errors.New("cover art exceeds 10 MiB")
	}
	mime := http.DetectContentType(data)
	if strings.HasPrefix(mime, "image/jpeg") {
		mime = "image/jpeg"
	} else if strings.HasPrefix(mime, "image/png") {
		mime = "image/png"
	} else {
		return nil, "", errors.New("cover art is not JPEG or PNG")
	}
	return data, mime, nil
}
