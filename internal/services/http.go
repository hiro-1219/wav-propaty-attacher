package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type RateClient struct {
	HTTP      *http.Client
	Interval  time.Duration
	UserAgent string
	mu        sync.Mutex
	next      time.Time
}

func NewRateClient(interval time.Duration, userAgent string) *RateClient {
	return &RateClient{HTTP: &http.Client{Timeout: 30 * time.Second}, Interval: interval, UserAgent: userAgent}
}

func (c *RateClient) DoJSON(ctx context.Context, method, endpoint string, form url.Values, dst any) error {
	c.mu.Lock()
	wait := time.Until(c.next)
	if wait < 0 {
		wait = 0
	}
	c.next = time.Now().Add(wait + c.Interval)
	c.mu.Unlock()
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
		if err != nil {
			return err
		}
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		req.Header.Set("Accept", "application/json")
		if c.UserAgent != "" {
			req.Header.Set("User-Agent", c.UserAgent)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			last = err
		} else {
			func() {
				defer resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					last = json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(dst)
					return
				}
				message, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
				last = fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
				if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusServiceUnavailable && resp.StatusCode < 500 {
					attempt = 3
				}
			}()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 && last == nil {
				return nil
			}
		}
		if attempt < 2 {
			timer := time.NewTimer(time.Duration(1<<attempt) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	if last == nil {
		last = errors.New("request failed")
	}
	return last
}
