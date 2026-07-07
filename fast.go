package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"sync/atomic"
	"time"
)

// fallbackToken is used when we can't extract a fresh token from the fast.com
// JavaScript bundle. It rarely changes, so this is usually good enough.
const fallbackToken = "YXNkZmFzZGxmbnNkYWZoYXNkZmhrYWxm"

// latencySamples is how many timed round trips we average to estimate
// latency, after a warm-up request that we discard.
const latencySamples = 5

var (
	scriptExpr = regexp.MustCompile(`app-[a-z0-9]+\.js`)
	tokenExpr  = regexp.MustCompile(`token:"(\w+)"`)
)

// token extracts the API token from the fast.com JavaScript bundle. fast.com
// embeds it in a script tag, so we fetch the page, find the script and pull the
// token out of it.
func token() string {
	page, err := get("https://fast.com/")
	if err != nil {
		return fallbackToken
	}

	script, err := get("https://fast.com/" + scriptExpr.FindString(string(page)))
	if err != nil {
		return fallbackToken
	}

	match := tokenExpr.FindSubmatch(script)
	if len(match) < 2 {
		return fallbackToken
	}
	return string(match[1])
}

// targets asks fast.com for count URLs to download from. fast.com is powered by
// Netflix, so these point at the Netflix Open Connect servers nearest to us.
func targets(count int) ([]string, error) {
	url := fmt.Sprintf("https://api.fast.com/netflix/speedtest/v2?https=true&token=%s&urlCount=%d", token(), count)
	body, err := get(url)
	if err != nil {
		return nil, err
	}

	var response struct {
		Targets []struct {
			URL string `json:"url"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	urls := make([]string, len(response.Targets))
	for i, target := range response.Targets {
		urls[i] = target.URL
	}
	return urls, nil
}

// latency estimates the round-trip time to url by requesting a single byte
// repeatedly, returning the median of however many of latencySamples timed
// requests succeed. A first, untimed request warms up the connection so its
// setup cost doesn't skew the result. Sampling the same target repeatedly,
// rather than round-robining across targets, keeps the connection warm so
// each timed sample measures round-trip time rather than a fresh TCP/TLS
// handshake.
func latency(ctx context.Context, url string) (time.Duration, error) {
	probe := func() (time.Duration, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Range", "bytes=0-0")

		start := time.Now()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		// A server is free to ignore an unsupported Range header (RFC 7233 §3.1)
		// and return the full body instead of a single byte, so make sure we
		// actually got the partial response we asked for before timing it.
		if resp.StatusCode != http.StatusPartialContent {
			return 0, fmt.Errorf("unexpected status %s", resp.Status)
		}
		io.Copy(io.Discard, resp.Body)
		return time.Since(start), nil
	}

	if _, err := probe(); err != nil {
		return 0, err
	}

	var samples []time.Duration
	for range latencySamples {
		if d, err := probe(); err == nil {
			samples = append(samples, d)
		}
	}
	if len(samples) == 0 {
		return 0, fmt.Errorf("latency: no successful samples")
	}

	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return samples[len(samples)/2], nil
}

// download repeatedly downloads from url until the context is cancelled, adding
// the number of bytes it reads to total as it goes. We run a few of these in
// parallel to saturate the connection.
func download(ctx context.Context, url string, total *atomic.Int64) {
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}

		io.Copy(counter{total}, resp.Body)
		resp.Body.Close()
	}
}

// counter is an io.Writer that keeps a running total of how many bytes have
// been written to it, without keeping any of them around.
type counter struct {
	total *atomic.Int64
}

func (c counter) Write(p []byte) (int, error) {
	c.total.Add(int64(len(p)))
	return len(p), nil
}

// get performs an HTTP GET request and returns the response body.
func get(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
