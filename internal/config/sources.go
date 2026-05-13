package config

import (
	"log/slog"
	"strings"
	"time"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/source"
)

// SourceInstanceConfig holds configuration for a single source instance.
// Sources are parsed from the DNSWEAVER_SOURCES environment variable.
type SourceInstanceConfig struct {
	// Name is the source type (e.g., "traefik", "caddy", "nginx").
	Name string

	// FileDiscovery contains file-based discovery configuration.
	// Presence of FilePaths implies enablement (per design in #22).
	FileDiscovery source.FileDiscoveryConfig

	// DefaultEntryPoints is a Traefik-source-specific setting that lists the
	// entrypoints an unlabeled router should be considered bound to. Mirrors
	// Traefik's `entryPoints.<name>.asDefault = true` configuration.
	// See sources/traefik.WithDefaultEntryPoints. Ignored for non-traefik sources.
	DefaultEntryPoints []string

	// HTTP contains HTTP polling discovery configuration.
	// Used by the "http" source for Traefik-style dynamic config payloads.
	HTTP HTTPDiscoveryConfig
}

// HTTPDiscoveryConfig holds settings for HTTP-based discovery.
type HTTPDiscoveryConfig struct {
	Endpoint     string
	PollInterval time.Duration
	PollTimeout  time.Duration
	Headers      map[string]string
}

func defaultHTTPDiscoveryConfig() HTTPDiscoveryConfig {
	return HTTPDiscoveryConfig{
		PollInterval: 60 * time.Second,
		PollTimeout:  5 * time.Second,
	}
}

func (c HTTPDiscoveryConfig) IsEnabled() bool {
	return c.Endpoint != ""
}

// SourceConfig holds all source configuration.
type SourceConfig struct {
	// Sources is the ordered list of source instance names from DNSWEAVER_SOURCES.
	Names []string

	// Instances contains configuration for each source.
	Instances []*SourceInstanceConfig
}

// parseSources parses the DNSWEAVER_SOURCES environment variable.
// Returns the list of source names in order. Defaults to "traefik" if not set.
//
// For backward compatibility, DNSWEAVER_SOURCE (singular) is also accepted
// but deprecated. If DNSWEAVER_SOURCES is not set but DNSWEAVER_SOURCE is,
// the singular value is used as a single-element source list.
func parseSources() []string {
	sourcesStr := getEnv("DNSWEAVER_SOURCES")
	if sourcesStr == "" {
		// Fall back to deprecated DNSWEAVER_SOURCE (singular)
		if sourceStr := getEnv("DNSWEAVER_SOURCE"); sourceStr != "" {
			slog.Warn("DNSWEAVER_SOURCE is deprecated, use DNSWEAVER_SOURCES instead")
			sourcesStr = sourceStr
		} else {
			// Default to traefik for backward compatibility
			return []string{"traefik"}
		}
	}

	var sources []string
	for _, s := range strings.Split(sourcesStr, ",") {
		s = strings.TrimSpace(s)
		s = strings.ToLower(s)
		if s != "" {
			sources = append(sources, s)
		}
	}
	return sources
}

// loadSourceConfig loads source-specific configuration from environment variables.
//
// Environment variable patterns:
//
//	DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS=/rules,/config/traefik
//	DNSWEAVER_SOURCE_TRAEFIK_FILE_PATTERN=*.yml,*.yaml
//	DNSWEAVER_SOURCE_TRAEFIK_POLL_INTERVAL=30s
//	DNSWEAVER_SOURCE_TRAEFIK_WATCH_METHOD=auto
func loadSourceConfig() *SourceConfig {
	names := parseSources()

	cfg := &SourceConfig{
		Names:     names,
		Instances: make([]*SourceInstanceConfig, 0, len(names)),
	}

	for _, name := range names {
		inst := loadSourceInstanceConfig(name)
		cfg.Instances = append(cfg.Instances, inst)
	}

	return cfg
}

// sourceEnvPrefix returns the environment variable prefix for a source.
// Example: "traefik" -> "DNSWEAVER_SOURCE_TRAEFIK_"
func sourceEnvPrefix(name string) string {
	return "DNSWEAVER_SOURCE_" + strings.ToUpper(name) + "_"
}

// loadSourceInstanceConfig loads configuration for a single source.
func loadSourceInstanceConfig(name string) *SourceInstanceConfig {
	prefix := sourceEnvPrefix(name)

	cfg := &SourceInstanceConfig{
		Name:          name,
		FileDiscovery: source.DefaultFileDiscoveryConfig(),
		HTTP:          defaultHTTPDiscoveryConfig(),
	}

	// FILE_PATHS - comma-separated list of paths to watch
	// Per design: presence implies enablement (no ENABLED flag needed)
	if pathsStr := getEnv(prefix + "FILE_PATHS"); pathsStr != "" {
		var paths []string
		for _, p := range strings.Split(pathsStr, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				paths = append(paths, p)
			}
		}
		cfg.FileDiscovery.FilePaths = paths
	}

	// FILE_PATTERN - glob pattern for files to include
	// If not set, source-specific defaults apply (e.g., "*.yml,*.yaml" for traefik)
	if pattern := getEnv(prefix + "FILE_PATTERN"); pattern != "" {
		cfg.FileDiscovery.FilePattern = pattern
	}

	// POLL_INTERVAL - how often to check files for changes (default: 60s)
	if intervalStr := getEnv(prefix + "POLL_INTERVAL"); intervalStr != "" {
		if interval, err := time.ParseDuration(intervalStr); err == nil && interval >= time.Second {
			cfg.FileDiscovery.PollInterval = interval
			cfg.HTTP.PollInterval = interval
		}
		// Silently use default for invalid values (per config design)
	}

	// WATCH_METHOD - auto, inotify, poll (default: auto)
	if method := getEnv(prefix + "WATCH_METHOD"); method != "" {
		cfg.FileDiscovery.WatchMethod = strings.ToLower(method)
	}

	// ENDPOINT - URL for HTTP source payload polling
	if endpoint := getEnv(prefix + "ENDPOINT"); endpoint != "" {
		cfg.HTTP.Endpoint = strings.TrimSpace(endpoint)
	}

	// POLL_TIMEOUT - timeout for HTTP source requests
	if timeoutStr := getEnv(prefix + "POLL_TIMEOUT"); timeoutStr != "" {
		if timeout, err := time.ParseDuration(timeoutStr); err == nil && timeout >= time.Second {
			cfg.HTTP.PollTimeout = timeout
		}
	}

	// HEADERS - comma-separated list of key:value pairs
	// Example: "X-Token:abc,Traefik-Instance-Name:prod"
	if headersStr := getEnv(prefix + "HEADERS"); headersStr != "" {
		headers := make(map[string]string)
		for _, pair := range strings.Split(headersStr, ",") {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			key, value, ok := strings.Cut(pair, ":")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key == "" {
				continue
			}
			headers[key] = value
		}
		if len(headers) > 0 {
			cfg.HTTP.Headers = headers
		}
	}

	// DEFAULT_ENTRYPOINTS - traefik-only: which entrypoints unlabeled routers
	// should be treated as bound to (mirrors Traefik's `asDefault` setting).
	// Comma-separated, whitespace tolerant. Empty/unset preserves wildcard behavior.
	if eps := getEnv(prefix + "DEFAULT_ENTRYPOINTS"); eps != "" {
		var out []string
		for _, e := range strings.Split(eps, ",") {
			e = strings.TrimSpace(e)
			if e != "" {
				out = append(out, e)
			}
		}
		cfg.DefaultEntryPoints = out
	}

	return cfg
}

// GetSourceInstance returns the configuration for a specific source by name.
func (c *SourceConfig) GetSourceInstance(name string) *SourceInstanceConfig {
	for _, inst := range c.Instances {
		if inst.Name == name {
			return inst
		}
	}
	return nil
}

// HasFileDiscovery returns true if any source has file discovery configured.
func (c *SourceConfig) HasFileDiscovery() bool {
	for _, inst := range c.Instances {
		if inst.FileDiscovery.IsEnabled() {
			return true
		}
	}
	return false
}

// HasDiscovery returns true if any source has file or HTTP discovery configured.
func (c *SourceConfig) HasDiscovery() bool {
	for _, inst := range c.Instances {
		if inst.FileDiscovery.IsEnabled() || inst.HTTP.IsEnabled() {
			return true
		}
	}
	return false
}

// DiscoveryPollInterval returns the smallest configured poll interval among
// enabled discovery sources. Falls back to 60s if none are enabled.
func (c *SourceConfig) DiscoveryPollInterval() time.Duration {
	min := 60 * time.Second
	found := false

	for _, inst := range c.Instances {
		if inst.FileDiscovery.IsEnabled() && inst.FileDiscovery.PollInterval >= time.Second {
			if !found || inst.FileDiscovery.PollInterval < min {
				min = inst.FileDiscovery.PollInterval
				found = true
			}
		}
		if inst.HTTP.IsEnabled() && inst.HTTP.PollInterval >= time.Second {
			if !found || inst.HTTP.PollInterval < min {
				min = inst.HTTP.PollInterval
				found = true
			}
		}
	}

	return min
}
