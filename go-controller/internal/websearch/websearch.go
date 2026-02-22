package websearch

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// #region types

// Result holds a single search result.
type Result struct {
	Title   string
	Snippet string
	URL     string
}

// Config holds web search parameters.
type Config struct {
	MaxResults       int
	Timeout          time.Duration
	Enabled          bool
	EntropyThreshold float64
}

// #endregion types

// #region config

// DefaultConfig returns default web search configuration.
// Reads from env vars: WEB_SEARCH_ENABLED, WEB_SEARCH_MAX_RESULTS,
// WEB_SEARCH_TIMEOUT, WEB_SEARCH_ENTROPY_THRESHOLD.
func DefaultConfig() Config {
	cfg := Config{
		MaxResults:       3,
		Timeout:          10 * time.Second,
		Enabled:          true,
		EntropyThreshold: 0.3,
	}
	if v := os.Getenv("WEB_SEARCH_ENABLED"); v != "" {
		cfg.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("WEB_SEARCH_MAX_RESULTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxResults = n
		}
	}
	if v := os.Getenv("WEB_SEARCH_TIMEOUT"); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			cfg.Timeout = time.Duration(sec) * time.Second
		}
	}
	if v := os.Getenv("WEB_SEARCH_ENTROPY_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.EntropyThreshold = f
		}
	}
	return cfg
}

// #endregion config

// #region format

// FormatAsEvidence converts search results to a string suitable for injection
// alongside retrieved evidence.
func FormatAsEvidence(results []Result) string {
	if len(results) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[Web Search Results]\n")
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s\n", i+1, r.Title)
		if r.Snippet != "" {
			fmt.Fprintf(&b, "   %s\n", r.Snippet)
		}
		if r.URL != "" {
			fmt.Fprintf(&b, "   Source: %s\n", r.URL)
		}
	}
	return b.String()
}

// #endregion format
