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

// target is a Netflix Open Connect server we measure against.
type target struct {
	URL      string
	Location string
}

// targets asks fast.com for count servers to measure against. fast.com is
// powered by Netflix, so these point at the Netflix Open Connect servers
// nearest to us.
func targets(count int) ([]target, error) {
	url := fmt.Sprintf("https://api.fast.com/netflix/speedtest/v2?https=true&token=%s&urlCount=%d", token(), count)
	body, err := get(url)
	if err != nil {
		return nil, err
	}

	var response struct {
		Targets []struct {
			URL      string `json:"url"`
			Location struct {
				City    string `json:"city"`
				Country string `json:"country"`
			} `json:"location"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	targets := make([]target, len(response.Targets))
	for i, t := range response.Targets {
		targets[i] = target{
			URL:      t.URL,
			Location: fmt.Sprintf("%s, %s", t.Location.City, t.Location.Country),
		}
	}
	return targets, nil
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

// uploadChunkSize is how much data we send per upload request. It's comfortably
// under the size the Open Connect servers will accept in one go, so we repeat
// requests back-to-back to fill the measurement window.
const uploadChunkSize = 10 << 20 // 10MB

// upload repeatedly posts generated data to url until the context is
// cancelled, adding the number of bytes sent to total as it goes. We run a few
// of these in parallel to saturate the connection.
func upload(ctx context.Context, url string, total *atomic.Int64) {
	for ctx.Err() == nil {
		body := countingReader{r: io.LimitReader(zeroReader{}, uploadChunkSize), total: total}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
		if err != nil {
			return
		}
		req.ContentLength = uploadChunkSize

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

// zeroReader is an infinite source of zero bytes, used as upload payload since
// we only care about the transfer rate, not the content.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// countingReader wraps an io.Reader, adding every byte read from it to total.
type countingReader struct {
	r     io.Reader
	total *atomic.Int64
}

func (c countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.total.Add(int64(n))
	return n, err
}

// latency measures the median round-trip time of a handful of small,
// unloaded HTTP requests to url, giving a rough ping reading before the link
// is saturated by the download.
func latency(ctx context.Context, url string, samples int) time.Duration {
	times := make([]time.Duration, 0, samples)
	for range samples {
		start := time.Now()
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
		if err != nil {
			continue
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		times = append(times, time.Since(start))
	}
	if len(times) == 0 {
		return 0
	}

	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	return times[len(times)/2]
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
