package services

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type AcoustID struct {
	BaseURL string
	APIKey  string
	Client  *RateClient
}

type AcoustIDMatch struct {
	RecordingID string
	Score       float64
}

func NewAcoustID(apiKey string) *AcoustID {
	return &AcoustID{BaseURL: "https://api.acoustid.org/v2", APIKey: apiKey, Client: NewRateClient(time.Second/3, "WavPropertyAttacher/0.1")}
}

func (a *AcoustID) Lookup(ctx context.Context, fingerprint string, duration int) ([]AcoustIDMatch, error) {
	if a.APIKey == "" {
		return nil, errors.New("AcoustID APIキーが設定されていません")
	}
	form := url.Values{"client": {a.APIKey}, "format": {"json"}, "meta": {"recordingids"}, "duration": {strconv.Itoa(duration)}, "fingerprint": {fingerprint}}
	var response struct {
		Status  string `json:"status"`
		Results []struct {
			Score      float64 `json:"score"`
			Recordings []struct {
				ID string `json:"id"`
			} `json:"recordings"`
		} `json:"results"`
	}
	if err := a.Client.DoJSON(ctx, http.MethodPost, a.BaseURL+"/lookup", form, &response); err != nil {
		return nil, err
	}
	var out []AcoustIDMatch
	for _, result := range response.Results {
		for _, recording := range result.Recordings {
			out = append(out, AcoustIDMatch{recording.ID, result.Score})
		}
	}
	return out, nil
}
