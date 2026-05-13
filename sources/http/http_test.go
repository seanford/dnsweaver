package http

import (
	"context"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHTTP_Discover_Success(t *testing.T) {
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(`http:
  routers:
    app:
      rule: "Host(` + "`app.example.com`" + `) || Host(` + "`www.example.com`" + `)"
`))
	}))
	defer server.Close()

	src := New(
		WithLogger(testLogger()),
		WithConfig(Config{
			Endpoint:     server.URL,
			PollInterval: 5 * time.Second,
			PollTimeout:  2 * time.Second,
		}),
	)

	hostnames, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(hostnames) != 2 {
		t.Fatalf("Discover() count = %d, want 2", len(hostnames))
	}
}

func TestHTTP_Discover_Headers(t *testing.T) {
	var gotHeader string
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		gotHeader = r.Header.Get("Traefik-Instance-Name")
		_, _ = w.Write([]byte(`http: {routers: {app: {rule: "Host(` + "`app.example.com`" + `)"}}}`))
	}))
	defer server.Close()

	src := New(
		WithLogger(testLogger()),
		WithConfig(Config{
			Endpoint:     server.URL,
			PollInterval: time.Second,
			PollTimeout:  time.Second,
			Headers: map[string]string{
				"Traefik-Instance-Name": "thefordestate-traefik-prod",
			},
		}),
	)

	_, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if gotHeader != "thefordestate-traefik-prod" {
		t.Fatalf("header = %q, want %q", gotHeader, "thefordestate-traefik-prod")
	}
}

func TestHTTP_Discover_Timeout(t *testing.T) {
	server := httptest.NewServer(stdhttp.HandlerFunc(func(_ stdhttp.ResponseWriter, r *stdhttp.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	src := New(
		WithLogger(testLogger()),
		WithConfig(Config{
			Endpoint:     server.URL,
			PollInterval: time.Second,
			PollTimeout:  time.Second,
		}),
	)

	_, err := src.Discover(context.Background())
	if err == nil {
		t.Fatal("Discover() error = nil, want timeout error")
	}
}

func TestHTTP_Discover_Non200(t *testing.T) {
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusBadGateway)
	}))
	defer server.Close()

	src := New(
		WithLogger(testLogger()),
		WithConfig(Config{
			Endpoint:     server.URL,
			PollInterval: time.Second,
			PollTimeout:  time.Second,
		}),
	)

	_, err := src.Discover(context.Background())
	if err == nil {
		t.Fatal("Discover() error = nil, want non-200 error")
	}
}

func TestHTTP_Discover_MalformedPayload(t *testing.T) {
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		_, _ = w.Write([]byte("not valid yaml: ["))
	}))
	defer server.Close()

	src := New(
		WithLogger(testLogger()),
		WithConfig(Config{
			Endpoint:     server.URL,
			PollInterval: time.Second,
			PollTimeout:  time.Second,
		}),
	)

	_, err := src.Discover(context.Background())
	if err == nil {
		t.Fatal("Discover() error = nil, want parse error")
	}
}

func TestHTTP_Discover_PollIntervalCache(t *testing.T) {
	var requests int32
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		atomic.AddInt32(&requests, 1)
		_, _ = w.Write([]byte(`http: {routers: {app: {rule: "Host(` + "`app.example.com`" + `)"}}}`))
	}))
	defer server.Close()

	src := New(
		WithLogger(testLogger()),
		WithConfig(Config{
			Endpoint:     server.URL,
			PollInterval: time.Hour,
			PollTimeout:  time.Second,
		}),
	)

	first, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("first Discover() error = %v", err)
	}
	second, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("second Discover() error = %v", err)
	}

	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("unexpected result lengths: %d, %d", len(first), len(second))
	}
	if atomic.LoadInt32(&requests) != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestHTTP_SupportsDiscovery(t *testing.T) {
	if New().SupportsDiscovery() {
		t.Fatal("SupportsDiscovery() = true, want false")
	}
	if !New(WithConfig(Config{Endpoint: "http://example.com"})).SupportsDiscovery() {
		t.Fatal("SupportsDiscovery() = false, want true")
	}
}
