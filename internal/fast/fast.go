package fast

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

const (
	downloadPayloadBytes = 25 * 1024 * 1024
	latencyRequests      = 5
)

type target struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	Location location `json:"location"`
}

type location struct {
	City    string `json:"city"`
	Country string `json:"country"`
}

var (
	scriptExpr = regexp.MustCompile(`app-[a-z0-9]+\.js`)
	tokenExpr  = regexp.MustCompile(`token:"(\w+)"`)
	ipv4Client = &http.Client{Transport: ipTransport("tcp4")}
	ipv6Client = &http.Client{Transport: ipTransport("tcp6")}
)

type ipPreference int

const (
	preferIPv4 ipPreference = iota
	preferIPv6
)

func ipTransport(family string) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, family, address)
	}
	return transport
}

// fetchToken extracts the API token from the fast.com JavaScript bundle. fast.com
// embeds it in a script tag, so we fetch the page, find the script and pull the
// token out of it.
func fetchToken() (string, error) {
	page, err := get("https://fast.com/")
	if err != nil {
		return "", err
	}

	scriptName := scriptExpr.FindString(string(page))
	if scriptName == "" {
		return "", fmt.Errorf("could not find fast.com script")
	}

	script, err := get("https://fast.com/" + scriptName)
	if err != nil {
		return "", err
	}

	match := tokenExpr.FindSubmatch(script)
	if len(match) < 2 {
		return "", fmt.Errorf("could not find fast.com token")
	}
	return string(match[1]), nil
}

// targets asks fast.com for count URLs to download from. fast.com is powered by
// Netflix, so these point at the Netflix Open Connect servers nearest to us.
func targets(count int, token string, preference ipPreference) ([]target, error) {
	if token == "" {
		var err error
		token, err = fetchToken()
		if err != nil {
			return nil, err
		}
	}

	url := fmt.Sprintf("https://api.fast.com/netflix/speedtest/v2?https=true&token=%s&urlCount=%d", token, count)
	body, err := getPreferred(url, preference)
	if err != nil {
		return nil, err
	}

	var response struct {
		Targets []target `json:"targets"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	return response.Targets, nil
}

// download repeatedly downloads from url until the context is cancelled, adding
// the number of bytes it reads to total as it goes. We run a few of these in
// parallel to saturate the connection.
func download(ctx context.Context, url string, total *atomic.Int64) {
	url = downloadURL(url)

	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return
		}
		req.Header.Set("Accept-Encoding", "identity")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return
		}

		io.Copy(counter{total}, resp.Body)
		resp.Body.Close()
	}
}

func ping(ctx context.Context, targets []target) (time.Duration, error) {
	if len(targets) == 0 {
		return 0, fmt.Errorf("no speed test targets")
	}

	best := time.Duration(1<<63 - 1)
	var lastErr error
	for i := 0; i < latencyRequests; i++ {
		duration, err := pingTarget(ctx, targets[i%len(targets)].URL)
		if err != nil {
			lastErr = err
			continue
		}
		if duration < best {
			best = duration
		}
	}
	if best == time.Duration(1<<63-1) {
		return 0, lastErr
	}
	return best, nil
}

func pingTarget(ctx context.Context, url string) (time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latencyURL(url), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept-Encoding", "identity")

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("latency request failed: %s", resp.Status)
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return 0, err
	}
	return time.Since(start), nil
}

func downloadURL(raw string) string {
	return rangeURL(raw, downloadPayloadBytes-1)
}

func latencyURL(raw string) string {
	return rangeURL(raw, 0)
}

func rangeURL(raw string, end int) string {
	u, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(u.Path, "/speedtest") {
		return raw
	}

	u.Path = strings.TrimSuffix(u.Path, "/speedtest") + fmt.Sprintf("/speedtest/range/0-%d", end)
	return u.String()
}

func targetLabel(targets []target) string {
	if len(targets) == 0 {
		return "unknown"
	}

	target := targets[0]
	name := target.Name
	if name == "" {
		name = target.URL
	}
	if parsed, err := url.Parse(name); err == nil && parsed.Hostname() != "" {
		name = parsed.Hostname()
	}

	location := target.Location.String()
	if location != "" {
		return fmt.Sprintf("%s (%s)", name, location)
	}
	return name
}

func (l location) String() string {
	if l.City != "" && l.Country != "" {
		return l.City + ", " + l.Country
	}
	if l.City != "" {
		return l.City
	}
	return l.Country
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
	return getWithClient(http.DefaultClient, url)
}

func getIPv4(url string) ([]byte, error) {
	return getWithClient(ipv4Client, url)
}

func getIPv6(url string) ([]byte, error) {
	return getWithClient(ipv6Client, url)
}

func getPreferred(url string, preference ipPreference) ([]byte, error) {
	var body []byte
	var err error
	if preference == preferIPv6 {
		body, err = getIPv6(url)
	} else {
		body, err = getIPv4(url)
	}
	if err != nil {
		body, err = get(url)
	}
	return body, err
}

func getWithClient(client *http.Client, url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
