package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

const (
	fallbackToken    = "YXNkZmFzZGxmbnNkYWZoYXNkZmhrYWxm"
	metadataTimeout  = 10 * time.Second
	maxMetadataBytes = 8 << 20
)

var (
	scriptExpr = regexp.MustCompile(`app-[a-z0-9]+\.js`)
	tokenExpr  = regexp.MustCompile(`token:"([^"]+)"`)
)

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type fastService struct {
	client  httpDoer
	siteURL string
	apiURL  string
}

func newFastService() fastService {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = connections
	return fastService{
		client:  &http.Client{Transport: transport},
		siteURL: "https://fast.com/",
		apiURL:  "https://api.fast.com/netflix/speedtest/v2",
	}
}

func isHTTPSuccess(statusCode int) bool {
	return statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices
}

func (s fastService) targets(ctx context.Context, count int) ([]string, error) {
	if count <= 0 {
		return nil, fmt.Errorf("target count must be positive")
	}

	urls, err := s.targetsWithToken(ctx, count, fallbackToken)
	if err == nil || !isTokenRejection(err) {
		return urls, err
	}

	freshToken, tokenErr := s.token(ctx)
	if tokenErr != nil {
		return nil, fmt.Errorf("refresh token: %w", tokenErr)
	}
	if freshToken == fallbackToken {
		return nil, err
	}
	return s.targetsWithToken(ctx, count, freshToken)
}

func (s fastService) token(ctx context.Context) (string, error) {
	page, err := s.get(ctx, s.siteURL)
	if err != nil {
		return "", err
	}

	scriptName := scriptExpr.Find(page)
	if len(scriptName) == 0 {
		return "", fmt.Errorf("fast.com script not found")
	}
	siteURL, err := url.Parse(s.siteURL)
	if err != nil {
		return "", err
	}
	scriptURL, err := siteURL.Parse(string(scriptName))
	if err != nil {
		return "", err
	}
	script, err := s.get(ctx, scriptURL.String())
	if err != nil {
		return "", err
	}

	match := tokenExpr.FindSubmatch(script)
	if len(match) < 2 {
		return "", fmt.Errorf("fast.com token not found")
	}
	return string(match[1]), nil
}

func (s fastService) targetsWithToken(ctx context.Context, count int, token string) ([]string, error) {
	endpoint, err := url.Parse(s.apiURL)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("https", "true")
	query.Set("token", token)
	query.Set("urlCount", strconv.Itoa(count))
	endpoint.RawQuery = query.Encode()

	body, err := s.get(ctx, endpoint.String())
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
	if len(response.Targets) == 0 {
		return nil, fmt.Errorf("fast.com returned no targets")
	}

	urls := make([]string, len(response.Targets))
	for i, target := range response.Targets {
		if target.URL == "" {
			return nil, fmt.Errorf("fast.com returned an empty target")
		}
		urls[i] = target.URL
	}
	return urls, nil
}

func (s fastService) get(ctx context.Context, requestURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadataBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxMetadataBytes {
		return nil, fmt.Errorf("response from %s exceeds %d bytes", requestURL, maxMetadataBytes)
	}
	if !isHTTPSuccess(resp.StatusCode) {
		return nil, &statusError{code: resp.StatusCode, status: resp.Status}
	}
	return body, nil
}

type statusError struct {
	code   int
	status string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("unexpected HTTP status %s", e.status)
}

func isTokenRejection(err error) bool {
	var statusErr *statusError
	return errors.As(err, &statusErr) &&
		(statusErr.code == http.StatusUnauthorized || statusErr.code == http.StatusForbidden)
}
