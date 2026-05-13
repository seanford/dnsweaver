// Package http provides a Source implementation for extracting hostnames
// from Traefik dynamic configuration fetched over HTTP.
package http

import (
	"context"
	"fmt"
	"io"
	stdhttp "net/http"
	"sync"
	"time"

	"log/slog"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/source"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/workload"
	"gitlab.bluewillows.net/root/dnsweaver/sources/traefik"
)

const sourceName = "http"

// Config contains HTTP source settings.
type Config struct {
	Endpoint     string
	PollInterval time.Duration
	PollTimeout  time.Duration
	Headers      map[string]string
}

// DefaultConfig returns HTTP source defaults.
func DefaultConfig() Config {
	return Config{
		PollInterval: 60 * time.Second,
		PollTimeout:  5 * time.Second,
	}
}

// HTTP implements source.Source for HTTP-delivered Traefik dynamic config.
type HTTP struct {
	parser *traefik.Parser
	logger *slog.Logger
	client *stdhttp.Client
	config Config

	mu          sync.Mutex
	lastFetch   time.Time
	lastResult  []source.Hostname
	haveResult  bool
}

// Option configures an HTTP source.
type Option func(*HTTP)

// WithLogger sets a custom logger.
func WithLogger(logger *slog.Logger) Option {
	return func(h *HTTP) {
		h.logger = logger
	}
}

// WithConfig sets HTTP source configuration.
func WithConfig(cfg Config) Option {
	return func(h *HTTP) {
		h.config = cfg
	}
}

// New creates a new HTTP source.
func New(opts ...Option) *HTTP {
	h := &HTTP{
		logger: slog.Default(),
		config: DefaultConfig(),
		client: stdhttp.DefaultClient,
	}
	for _, opt := range opts {
		opt(h)
	}
	if h.config.PollInterval < time.Second {
		h.config.PollInterval = DefaultConfig().PollInterval
	}
	if h.config.PollTimeout < time.Second {
		h.config.PollTimeout = DefaultConfig().PollTimeout
	}
	h.parser = traefik.NewParser(traefik.WithParserLogger(h.logger))
	return h
}

// Name returns the source identifier.
func (h *HTTP) Name() string {
	return sourceName
}

// Extract is not used for HTTP source workload extraction.
func (h *HTTP) Extract(_ context.Context, _ workload.Workload) ([]source.Hostname, error) {
	return nil, nil
}

// Discover fetches endpoint payload and extracts hostnames.
func (h *HTTP) Discover(ctx context.Context) ([]source.Hostname, error) {
	if !h.SupportsDiscovery() {
		return nil, nil
	}

	h.mu.Lock()
	if h.haveResult && time.Since(h.lastFetch) < h.config.PollInterval {
		result := cloneHostnames(h.lastResult)
		h.mu.Unlock()
		return result, nil
	}
	h.mu.Unlock()

	result, err := h.fetchAndParse(ctx)

	h.mu.Lock()
	h.lastFetch = time.Now()
	if err == nil {
		h.lastResult = cloneHostnames(result)
		h.haveResult = true
	}
	h.mu.Unlock()

	if err != nil {
		return nil, err
	}
	return result, nil
}

// SupportsDiscovery returns true when endpoint is configured.
func (h *HTTP) SupportsDiscovery() bool {
	return h.config.Endpoint != ""
}

// SupportedPlatforms returns empty, meaning all platforms.
func (h *HTTP) SupportedPlatforms() []workload.Platform {
	return nil
}

func (h *HTTP) fetchAndParse(ctx context.Context) ([]source.Hostname, error) {
	reqCtx := ctx
	var cancel context.CancelFunc
	if h.config.PollTimeout > 0 {
		reqCtx, cancel = context.WithTimeout(ctx, h.config.PollTimeout)
		defer cancel()
	}

	req, err := stdhttp.NewRequestWithContext(reqCtx, stdhttp.MethodGet, h.config.Endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	for key, value := range h.config.Headers {
		req.Header.Set(key, value)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != stdhttp.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	extractions, err := h.parser.ParseConfigContent(body, h.config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parsing config payload: %w", err)
	}

	result := make([]source.Hostname, 0, len(extractions))
	for _, e := range extractions {
		result = append(result, source.Hostname{
			Name:     e.Hostname,
			Source:   sourceName,
			Router:   e.Router,
			Metadata: entryPointMetadata(e.EntryPoint),
		})
	}
	return result, nil
}

func entryPointMetadata(entrypoint string) map[string]string {
	if entrypoint == "" {
		return nil
	}
	return map[string]string{traefik.MetadataKeyEntryPoint: entrypoint}
}

func cloneHostnames(in []source.Hostname) []source.Hostname {
	out := make([]source.Hostname, len(in))
	copy(out, in)
	return out
}

// Ensure HTTP implements source.Source.
var _ source.Source = (*HTTP)(nil)
