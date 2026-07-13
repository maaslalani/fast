package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync/atomic"
	"time"
)

const (
	latencySamples     = 5
	payloadSize        = 25 * 1024 * 1024
	transferBufferSize = 64 * 1024
)

func (s fastService) latency(ctx context.Context, url string) (time.Duration, error) {
	probe := func() (time.Duration, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Range", "bytes=0-0")

		start := time.Now()
		resp, err := s.client.Do(req)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusPartialContent {
			return 0, fmt.Errorf("unexpected status %s", resp.Status)
		}
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			return 0, err
		}
		return time.Since(start), nil
	}

	if _, err := probe(); err != nil {
		return 0, err
	}

	samples := make([]time.Duration, 0, latencySamples)
	for range latencySamples {
		if d, err := probe(); err == nil {
			samples = append(samples, d)
		}
	}
	if len(samples) == 0 {
		return 0, fmt.Errorf("latency: no successful samples")
	}

	slices.Sort(samples)
	return samples[len(samples)/2], nil
}

func (s fastService) download(ctx context.Context, url string, total *atomic.Int64) {
	buffer := make([]byte, transferBufferSize)
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return
		}
		req.Header.Set("Accept-Encoding", "identity")

		resp, err := s.client.Do(req)
		if err != nil {
			return
		}
		if !isHTTPSuccess(resp.StatusCode) {
			resp.Body.Close()
			return
		}

		_, copyErr := io.CopyBuffer(counter{total}, resp.Body, buffer)
		closeErr := resp.Body.Close()
		if ctx.Err() != nil || copyErr != nil || closeErr != nil {
			return
		}
	}
}

type counter struct {
	total *atomic.Int64
}

func (c counter) Write(p []byte) (int, error) {
	c.total.Add(int64(len(p)))
	return len(p), nil
}

func (s fastService) upload(ctx context.Context, url string, total *atomic.Int64) {
	for ctx.Err() == nil {
		body := &uploadReader{remaining: payloadSize, total: total}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
		if err != nil {
			return
		}
		req.ContentLength = payloadSize
		req.Header.Set("Content-Type", "application/octet-stream")

		resp, err := s.client.Do(req)
		if err != nil {
			return
		}
		_, copyErr := io.Copy(io.Discard, resp.Body)
		closeErr := resp.Body.Close()
		if ctx.Err() != nil || copyErr != nil || closeErr != nil {
			return
		}
		if !isHTTPSuccess(resp.StatusCode) {
			return
		}
	}
}

func (s fastService) warm(ctx context.Context, url string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	req.Header.Set("Range", "bytes=0-0")

	resp, err := s.client.Do(req)
	if err != nil {
		return
	}
	if resp.StatusCode == http.StatusPartialContent {
		io.Copy(io.Discard, resp.Body)
	}
	resp.Body.Close()
}

type uploadReader struct {
	remaining int64
	total     *atomic.Int64
}

func (u *uploadReader) Read(p []byte) (int, error) {
	if u.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > u.remaining {
		p = p[:u.remaining]
	}
	clear(p)
	u.remaining -= int64(len(p))
	u.total.Add(int64(len(p)))
	return len(p), nil
}

func (u *uploadReader) Close() error {
	return nil
}
