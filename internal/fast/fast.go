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
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const (
	downloadPayloadBytes = 25 * 1024 * 1024
	uploadPayloadBytes   = 25 * 1024 * 1024
	latencyRequests      = 5
)

type target struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	Location location `json:"location"`
}

type location struct {
	City    string `json:"city,omitempty"`
	Country string `json:"country,omitempty"`
}

type clientInfo struct {
	IP       string   `json:"ip,omitempty"`
	ASN      any      `json:"asn,omitempty"`
	ISP      string   `json:"isp,omitempty"`
	Location location `json:"location,omitempty"`
}

type testConfig struct {
	Targets []target   `json:"targets"`
	Client  clientInfo `json:"client"`
}

// latencySamples is how many timed round trips we average to estimate
// latency, after a warm-up request that we discard.
const latencySamples = 5

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

// speedtestConfig asks fast.com for count test URLs. fast.com is powered by
// Netflix, so these point at the Netflix Open Connect servers nearest to us.
func speedtestConfig(count int, token string, preference ipPreference) (testConfig, error) {
	if token == "" {
		var err error
		token, err = fetchToken()
		if err != nil {
			return testConfig{}, err
		}
	}

	url := fmt.Sprintf("https://api.fast.com/netflix/speedtest/v2?https=true&token=%s&urlCount=%d", token, count)
	body, err := getPreferred(url, preference)
	if err != nil {
		return testConfig{}, err
	}
	var response testConfig
	if err := json.Unmarshal(body, &response); err != nil {
		return testConfig{}, err
	}
	if len(response.Targets) == 0 {
		return testConfig{}, fmt.Errorf("no speed test targets")
	}
	return response, nil
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

func upload(ctx context.Context, url string, total *atomic.Int64) {
	url = uploadURL(url)

	for ctx.Err() == nil {
		body := io.LimitReader(countingReader{reader: zeroReader{}, total: total}, uploadPayloadBytes)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
		if err != nil {
			return
		}
		req.ContentLength = uploadPayloadBytes
		req.Header.Set("Content-Type", "application/octet-stream")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return
		}

		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func ping(ctx context.Context, targets []target) (time.Duration, error) {
	if len(targets) == 0 {
		return 0, fmt.Errorf("no speed test targets")
	}

	var lastErr error
	for _, target := range targets {
		if _, err := pingTarget(ctx, target.URL); err != nil {
			lastErr = err
			continue
		}

		samples := make([]time.Duration, 0, latencyRequests)
		for i := 0; i < latencyRequests; i++ {
			duration, err := pingTarget(ctx, target.URL)
			if err != nil {
				lastErr = err
				continue
			}
			samples = append(samples, duration)
		}
		if len(samples) > 0 {
			return medianDuration(samples), nil
		}
	}
	return 0, lastErr
}

func medianDuration(samples []time.Duration) time.Duration {
	sort.Slice(samples, func(i, j int) bool {
		return samples[i] < samples[j]
	})
	return samples[len(samples)/2]
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

func uploadURL(raw string) string {
	return rangeURL(raw, uploadPayloadBytes-1)
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

func (c clientInfo) Label() string {
	details := []string{}
	if c.ISP != "" {
		details = append(details, c.ISP)
	}
	if asn := c.ASNString(); asn != "" {
		details = append(details, asn)
	}
	if location := c.Location.String(); location != "" {
		details = append(details, location)
	}

	if c.IP == "" {
		return strings.Join(details, ", ")
	}
	if len(details) == 0 {
		return c.IP
	}
	return fmt.Sprintf("%s (%s)", c.IP, strings.Join(details, ", "))
}

func (c clientInfo) ASNString() string {
	var asn string
	switch value := c.ASN.(type) {
	case nil:
		return ""
	case string:
		asn = value
	case float64:
		asn = fmt.Sprintf("%.0f", value)
	default:
		asn = fmt.Sprint(value)
	}
	if asn == "" || strings.EqualFold(asn, "null") {
		return ""
	}
	if strings.HasPrefix(strings.ToUpper(asn), "AS") {
		return asn
	}
	return "AS" + asn
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

type countingReader struct {
	reader io.Reader
	total  *atomic.Int64
}

func (r countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.total.Add(int64(n))
	return n, err
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
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
