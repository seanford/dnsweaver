package traefik

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// DiscoverFromFiles scans the given paths for Traefik configuration files
// and extracts hostnames from http.routers.*.rule entries.
//
// IMPORTANT: This method ONLY parses router rules. Middleware files,
// service definitions, and other config sections are safely ignored.
// This prevents false positives from middleware configurations.
//
// Parameters:
//   - paths: List of file paths or directories to scan
//   - pattern: Glob pattern for file matching (e.g., "*.yml,*.yaml")
//
// Returns extracted hostnames with router context.
func (p *Parser) DiscoverFromFiles(ctx context.Context, paths []string, pattern string) ([]HostnameExtraction, error) {
	// Split pattern into individual patterns (comma-separated)
	patterns := strings.Split(pattern, ",")
	for i := range patterns {
		patterns[i] = strings.TrimSpace(patterns[i])
	}

	var allFiles []string

	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				p.logger.Warn("traefik config path does not exist",
					"path", path,
				)
				continue
			}
			return nil, fmt.Errorf("stat %s: %w", path, err)
		}

		if info.IsDir() {
			// Find all matching files in directory
			files, err := p.findFilesInDir(path, patterns)
			if err != nil {
				return nil, err
			}
			allFiles = append(allFiles, files...)
		} else {
			// Single file - check if it matches any pattern
			if p.matchesAnyPattern(filepath.Base(path), patterns) {
				allFiles = append(allFiles, path)
			}
		}
	}

	p.logger.Debug("found traefik config files",
		"count", len(allFiles),
		"files", allFiles,
	)

	// Parse each file
	var allExtractions []HostnameExtraction
	type seenKey struct {
		hostname   string
		entrypoint string
	}
	seen := make(map[seenKey]struct{}) // Deduplicate (hostname, entrypoint) across files

	for _, file := range allFiles {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		extractions, err := p.parseConfigFile(file)
		if err != nil {
			p.logger.Warn("failed to parse traefik config file",
				"file", file,
				"error", err.Error(),
			)
			continue
		}

		for _, e := range extractions {
			k := seenKey{hostname: e.Hostname, entrypoint: e.EntryPoint}
			if _, exists := seen[k]; !exists {
				seen[k] = struct{}{}
				allExtractions = append(allExtractions, e)
			}
		}
	}

	return allExtractions, nil
}

// findFilesInDir finds all files matching the patterns in a directory.
func (p *Parser) findFilesInDir(dir string, patterns []string) ([]string, error) {
	var matches []string

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		name := d.Name()
		if p.matchesAnyPattern(name, patterns) {
			matches = append(matches, path)
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walking directory %s: %w", dir, err)
	}

	return matches, nil
}

// matchesAnyPattern checks if a filename matches any of the given patterns.
func (p *Parser) matchesAnyPattern(name string, patterns []string) bool {
	for _, pattern := range patterns {
		matched, err := filepath.Match(pattern, name)
		if err != nil {
			// Invalid pattern, skip
			continue
		}
		if matched {
			return true
		}
	}
	return false
}

// parseConfigFile parses a Traefik config file, detecting format by extension.
// Supports YAML (.yml, .yaml) and TOML (.toml) formats.
func (p *Parser) parseConfigFile(path string) ([]HostnameExtraction, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	return p.ParseConfigContent(data, path)
}

// ParseConfigContent parses Traefik dynamic configuration payloads.
//
// The format is detected from formatHint extension when available:
//   - .toml -> TOML
//   - .yml/.yaml -> YAML
//
// For unknown extensions (or no extension), YAML is attempted first and
// TOML is used as a fallback.
func (p *Parser) ParseConfigContent(data []byte, formatHint string) ([]HostnameExtraction, error) {
	return p.parseConfigData(data, formatHint, formatHint)
}

func (p *Parser) parseConfigData(data []byte, formatHint, source string) ([]HostnameExtraction, error) {
	ext := strings.ToLower(filepath.Ext(formatHint))
	switch ext {
	case ".toml":
		return p.parseTOMLData(data, source)
	case ".yml", ".yaml":
		return p.parseYAMLData(data, source)
	default:
		extractions, yamlErr := p.parseYAMLData(data, source)
		if yamlErr == nil {
			return extractions, nil
		}

		extractions, tomlErr := p.parseTOMLData(data, source)
		if tomlErr == nil {
			return extractions, nil
		}

		return nil, fmt.Errorf("parsing YAML failed: %w; parsing TOML failed: %v", yamlErr, tomlErr)
	}
}

// parseYAMLFile parses a single Traefik YAML config file.
// Only extracts from http.routers.*.rule - ignores everything else.
func (p *Parser) parseYAMLFile(path string) ([]HostnameExtraction, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}
	return p.parseYAMLData(data, path)
}

// parseTOMLFile parses a single Traefik TOML config file.
// Only extracts from [http.routers.NAME] sections - ignores everything else.
func (p *Parser) parseTOMLFile(path string) ([]HostnameExtraction, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}
	return p.parseTOMLData(data, path)
}

func (p *Parser) parseYAMLData(data []byte, source string) ([]HostnameExtraction, error) {
	var config traefikFileConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	return p.extractFromConfig(&config, source)
}

func (p *Parser) parseTOMLData(data []byte, source string) ([]HostnameExtraction, error) {
	var config traefikFileConfig
	if err := toml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing TOML: %w", err)
	}

	return p.extractFromConfig(&config, source)
}

// extractFromConfig extracts hostnames from a parsed Traefik config.
//
// Routers that declare `entryPoints: [webA, webB]` fan out into one
// extraction per (host, entrypoint) pair so per-instance entrypoint
// filtering can claim each pair independently. Routers without
// entrypoints produce a single extraction with EntryPoint="" — wildcard
// semantics that match any DNSWEAVER_{NAME}_ENTRYPOINTS filter.
func (p *Parser) extractFromConfig(config *traefikFileConfig, path string) ([]HostnameExtraction, error) {
	var extractions []HostnameExtraction

	// Only process http.routers.*.rule
	if config.HTTP == nil || config.HTTP.Routers == nil {
		return nil, nil // No routers in this file
	}

	for routerName, router := range config.HTTP.Routers {
		if router.Rule == "" {
			continue
		}

		hosts := extractHostsFromRule(router.Rule)
		eps := router.EntryPoints
		// If router declared no entrypoints and the source was configured
		// with DefaultEntryPoints (Traefik `asDefault` mirror), fan out across
		// those defaults instead of treating the router as wildcard.
		if len(eps) == 0 && len(p.defaultEntryPoints) > 0 {
			eps = p.defaultEntryPoints
		}
		for _, hostname := range hosts {
			if len(eps) == 0 {
				extractions = append(extractions, HostnameExtraction{
					Hostname: hostname,
					Router:   routerName,
				})
				p.logger.Debug("extracted hostname from file",
					"hostname", hostname,
					"router", routerName,
					"file", path,
				)
				continue
			}
			for _, ep := range eps {
				extractions = append(extractions, HostnameExtraction{
					Hostname:   hostname,
					Router:     routerName,
					EntryPoint: ep,
				})
				p.logger.Debug("extracted hostname from file",
					"hostname", hostname,
					"router", routerName,
					"entrypoint", ep,
					"file", path,
				)
			}
		}
	}

	return extractions, nil
}

// traefikFileConfig represents the structure of Traefik config files.
// We only care about http.routers.*.rule - everything else is ignored.
// Supports both YAML and TOML formats via struct tags.
type traefikFileConfig struct {
	HTTP *traefikHTTPConfig `yaml:"http" toml:"http"`
}

type traefikHTTPConfig struct {
	Routers map[string]*traefikRouter `yaml:"routers" toml:"routers"`
	// Services, middlewares, etc. are intentionally ignored
}

type traefikRouter struct {
	Rule        string   `yaml:"rule" toml:"rule"`
	EntryPoints []string `yaml:"entryPoints" toml:"entryPoints"`
	// Service, Middlewares, etc. are intentionally ignored
}
