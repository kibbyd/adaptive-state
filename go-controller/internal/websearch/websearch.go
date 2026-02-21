package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	BaseURL          string // override for testing
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
		BaseURL:          "https://api.duckduckgo.com/",
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

// #region search

// duckDuckGoResponse represents the DuckDuckGo instant answer API JSON response.
type duckDuckGoResponse struct {
	Abstract       string              `json:"Abstract"`
	AbstractText   string              `json:"AbstractText"`
	AbstractSource string              `json:"AbstractSource"`
	AbstractURL    string              `json:"AbstractURL"`
	Heading        string              `json:"Heading"`
	RelatedTopics  []relatedTopic      `json:"RelatedTopics"`
}

type relatedTopic struct {
	Text     string `json:"Text"`
	FirstURL string `json:"FirstURL"`
	Result   string `json:"Result"`
}

// Search queries DuckDuckGo instant answer API and returns up to cfg.MaxResults results.
func Search(ctx context.Context, query string, cfg Config) ([]Result, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.duckduckgo.com/"
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("format", "json")
	params.Set("no_html", "1")
	params.Set("skip_disambig", "1")
	reqURL := baseURL + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("websearch: build request: %w", err)
	}
	req.Header.Set("User-Agent", "AdaptiveStateController/1.0")

	client := &http.Client{Timeout: cfg.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("websearch: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("websearch: status %d", resp.StatusCode)
	}

	var ddg duckDuckGoResponse
	if err := json.NewDecoder(resp.Body).Decode(&ddg); err != nil {
		return nil, fmt.Errorf("websearch: decode response: %w", err)
	}

	var results []Result

	// Use abstract if available
	if ddg.AbstractText != "" {
		results = append(results, Result{
			Title:   ddg.Heading,
			Snippet: ddg.AbstractText,
			URL:     ddg.AbstractURL,
		})
	}

	// Collect related topics
	for _, topic := range ddg.RelatedTopics {
		if len(results) >= cfg.MaxResults {
			break
		}
		if topic.Text == "" {
			continue
		}
		results = append(results, Result{
			Title:   extractTitle(topic.Text),
			Snippet: topic.Text,
			URL:     topic.FirstURL,
		})
	}

	return results, nil
}

// extractTitle takes the first sentence or up to 80 chars as a title.
func extractTitle(text string) string {
	if idx := strings.Index(text, " - "); idx > 0 && idx < 80 {
		return text[:idx]
	}
	if len(text) > 80 {
		return text[:80] + "..."
	}
	return text
}

// #endregion search

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
