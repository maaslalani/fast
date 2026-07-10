package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

func TestTargetsUsesFallbackTokenFirst(t *testing.T) {
	var apiRequests atomic.Int64
	var pageRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api":
			apiRequests.Add(1)
			if token := r.URL.Query().Get("token"); token != fallbackToken {
				t.Errorf("token = %q, want fallback token", token)
			}
			w.Write([]byte(`{"targets":[{"url":"https://example.com/download"}]}`))
		default:
			pageRequests.Add(1)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := fastService{
		client:  server.Client(),
		siteURL: server.URL + "/",
		apiURL:  server.URL + "/api",
	}
	urls, err := service.targets(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"https://example.com/download"}; !reflect.DeepEqual(urls, want) {
		t.Fatalf("urls = %v, want %v", urls, want)
	}
	if got := apiRequests.Load(); got != 1 {
		t.Errorf("API requests = %d, want 1", got)
	}
	if got := pageRequests.Load(); got != 0 {
		t.Errorf("page requests = %d, want 0", got)
	}
}

func TestTargetsRefreshesRejectedToken(t *testing.T) {
	var mu sync.Mutex
	var tokens []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api":
			token := r.URL.Query().Get("token")
			mu.Lock()
			tokens = append(tokens, token)
			mu.Unlock()
			if token == fallbackToken {
				http.Error(w, "expired token", http.StatusForbidden)
				return
			}
			w.Write([]byte(`{"targets":[{"url":"https://example.com/download"}]}`))
		case "/":
			w.Write([]byte(`<script src="/app-abc123.js"></script>`))
		case "/app-abc123.js":
			w.Write([]byte(`const config={token:"fresh-token"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := fastService{
		client:  server.Client(),
		siteURL: server.URL + "/",
		apiURL:  server.URL + "/api",
	}
	if _, err := service.targets(context.Background(), 1); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if want := []string{fallbackToken, "fresh-token"}; !reflect.DeepEqual(tokens, want) {
		t.Fatalf("tokens = %v, want %v", tokens, want)
	}
}

func TestTargetsDoesNotRefreshOtherErrors(t *testing.T) {
	var pageRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		pageRequests.Add(1)
		http.NotFound(w, r)
	}))
	defer server.Close()

	service := fastService{
		client:  server.Client(),
		siteURL: server.URL + "/",
		apiURL:  server.URL + "/api",
	}
	if _, err := service.targets(context.Background(), 1); err == nil {
		t.Fatal("expected an error")
	}
	if got := pageRequests.Load(); got != 0 {
		t.Errorf("page requests = %d, want 0", got)
	}
}
