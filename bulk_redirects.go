package traefik_plugin_bulk_redirects

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Config struct {
	Redirects []Redirect `json:"redirects,omitempty"`
}

type Redirect struct {
	SourceURL           string `json:"sourceURL,omitempty"`
	TargetURL           string `json:"targetURL,omitempty"`
	StatusCode          int    `json:"statusCode,omitempty"`
	PreserveQueryString string `json:"preserveQueryString,omitempty"`
	SubpathMatching     string `json:"subpathMatching,omitempty"`
}

type Target struct {
	URL                 string
	StatusCode          int
	PreserveQueryString string
}

type PrefixRedirect struct {
	SourcePath string
	Target     Target
}

var (
	registerMetricsOnce sync.Once
	registerMetricsErr  error

	bulkRedirectsTotal    *prometheus.CounterVec
	bulkRedirectsDuration *prometheus.HistogramVec
	bulkRedirectsMisses   *prometheus.CounterVec
)

func CreateConfig() *Config {
	return &Config{
		Redirects: []Redirect{},
	}
}

type BulkRedirects struct {
	next            http.Handler
	name            string
	exactRedirects  map[string]Target
	prefixRedirects map[string]PrefixRedirect
}

func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	_ = ctx

	registerMetricsOnce.Do(func() {
		registerMetricsErr = registerBulkRedirectsMetrics()
	})

	if registerMetricsErr != nil {
		return nil, registerMetricsErr
	}

	exactRedirects := make(map[string]Target, len(config.Redirects))
	prefixRedirects := make(map[string]PrefixRedirect)

	for _, redirect := range config.Redirects {
		if redirect.StatusCode == 0 {
			redirect.StatusCode = http.StatusMovedPermanently
		}

		if redirect.SourceURL == "" {
			return nil, fmt.Errorf("sourceURL is required")
		}

		sourceHost, sourcePath, err := parseSourceURL(redirect.SourceURL)
		if err != nil {
			return nil, err
		}

		if redirect.TargetURL == "" {
			return nil, fmt.Errorf("targetURL is required for %s", redirect.SourceURL)
		}

		if err := validateTargetURL(redirect.TargetURL); err != nil {
			return nil, fmt.Errorf("invalid targetURL %q for %s: %w", redirect.TargetURL, redirect.SourceURL, err)
		}

		if !isValidRedirectStatusCode(redirect.StatusCode) {
			return nil, fmt.Errorf("invalid statusCode %d for %s", redirect.StatusCode, redirect.SourceURL)
		}

		if !isValidEnabledDisabledValue(redirect.PreserveQueryString) {
			return nil, fmt.Errorf("invalid preserveQueryString %q for %s", redirect.PreserveQueryString, redirect.SourceURL)
		}

		if !isValidEnabledDisabledValue(redirect.SubpathMatching) {
			return nil, fmt.Errorf("invalid subpathMatching %q for %s", redirect.SubpathMatching, redirect.SourceURL)
		}

		target := Target{
			URL:                 redirect.TargetURL,
			StatusCode:          redirect.StatusCode,
			PreserveQueryString: redirect.PreserveQueryString,
		}

		key := buildKey(sourceHost, sourcePath)

		if strings.EqualFold(redirect.SubpathMatching, "enabled") {
			prefixRedirects[key] = PrefixRedirect{
				SourcePath: sourcePath,
				Target:     target,
			}
			continue
		}

		exactRedirects[key] = target
	}

	return &BulkRedirects{
		next:            next,
		name:            name,
		exactRedirects:  exactRedirects,
		prefixRedirects: prefixRedirects,
	}, nil
}

func (bulkRedirects *BulkRedirects) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	start := time.Now()

	host := normalizeHost(req.Host)
	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}

	if target, found := bulkRedirects.exactRedirects[buildKey(host, path)]; found {
		bulkRedirects.recordRedirect("exact", target, start)
		redirect(rw, req, target, "")
		return
	}

	if prefixRedirect, found := bulkRedirects.findPrefixRedirect(host, path); found {
		bulkRedirects.recordRedirect("prefix", prefixRedirect.Target, start)

		suffix := strings.TrimPrefix(path, prefixRedirect.SourcePath)
		redirect(rw, req, prefixRedirect.Target, suffix)
		return
	}

	bulkRedirects.recordMiss()
	bulkRedirects.next.ServeHTTP(rw, req)
}

func (bulkRedirects *BulkRedirects) recordRedirect(matchType string, target Target, start time.Time) {
	statusCode := strconv.Itoa(target.StatusCode)

	bulkRedirectsTotal.WithLabelValues(
		bulkRedirects.name,
		matchType,
		statusCode,
	).Inc()

	bulkRedirectsDuration.WithLabelValues(
		bulkRedirects.name,
		matchType,
		statusCode,
	).Observe(time.Since(start).Seconds())
}

func (bulkRedirects *BulkRedirects) recordMiss() {
	bulkRedirectsMisses.WithLabelValues(bulkRedirects.name).Inc()
}

func registerBulkRedirectsMetrics() error {
	redirectsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "traefik_plugin_bulk_redirects_redirects_total",
			Help: "Total redirects handled by the bulk redirects plugin.",
		},
		[]string{"middleware", "match_type", "status_code"},
	)

	redirectDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "traefik_plugin_bulk_redirects_duration_seconds",
			Help: "Time spent by the bulk redirects plugin to match and emit a redirect.",
			Buckets: []float64{
				0.00001,
				0.000025,
				0.00005,
				0.0001,
				0.00025,
				0.0005,
				0.001,
				0.0025,
				0.005,
				0.01,
				0.025,
				0.05,
				0.1,
			},
		},
		[]string{"middleware", "match_type", "status_code"},
	)

	misses := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "traefik_plugin_bulk_redirects_misses_total",
			Help: "Total requests that did not match any bulk redirect rule.",
		},
		[]string{"middleware"},
	)

	var err error

	bulkRedirectsTotal, err = registerOrReuseCounterVec(redirectsTotal)
	if err != nil {
		return err
	}

	bulkRedirectsDuration, err = registerOrReuseHistogramVec(redirectDuration)
	if err != nil {
		return err
	}

	bulkRedirectsMisses, err = registerOrReuseCounterVec(misses)
	if err != nil {
		return err
	}

	return nil
}

func registerOrReuseCounterVec(counter *prometheus.CounterVec) (*prometheus.CounterVec, error) {
	err := prometheus.Register(counter)
	if err == nil {
		return counter, nil
	}

	if alreadyRegistered, ok := err.(prometheus.AlreadyRegisteredError); ok {
		existing, ok := alreadyRegistered.ExistingCollector.(*prometheus.CounterVec)
		if !ok {
			return nil, fmt.Errorf("existing collector is not CounterVec")
		}

		return existing, nil
	}

	return nil, err
}

func registerOrReuseHistogramVec(histogram *prometheus.HistogramVec) (*prometheus.HistogramVec, error) {
	err := prometheus.Register(histogram)
	if err == nil {
		return histogram, nil
	}

	if alreadyRegistered, ok := err.(prometheus.AlreadyRegisteredError); ok {
		existing, ok := alreadyRegistered.ExistingCollector.(*prometheus.HistogramVec)
		if !ok {
			return nil, fmt.Errorf("existing collector is not HistogramVec")
		}

		return existing, nil
	}

	return nil, err
}

func (bulkRedirects *BulkRedirects) findPrefixRedirect(host, path string) (PrefixRedirect, bool) {
	currentPath := path

	for {
		if prefixRedirect, found := bulkRedirects.findPrefixCandidate(host, path, currentPath); found {
			return prefixRedirect, true
		}

		if currentPath == "/" {
			break
		}

		trimmed := strings.TrimRight(currentPath, "/")
		lastSlash := strings.LastIndex(trimmed, "/")
		if lastSlash <= 0 {
			currentPath = "/"
			continue
		}

		currentPath = trimmed[:lastSlash+1]
	}

	return PrefixRedirect{}, false
}

func (bulkRedirects *BulkRedirects) findPrefixCandidate(host, path, candidate string) (PrefixRedirect, bool) {
	if prefixRedirect, found := bulkRedirects.prefixRedirects[buildKey(host, candidate)]; found {
		if isSubpathMatch(path, prefixRedirect.SourcePath) {
			return prefixRedirect, true
		}
	}

	if candidate == "/" {
		return PrefixRedirect{}, false
	}

	alternative := toggleTrailingSlash(candidate)

	if prefixRedirect, found := bulkRedirects.prefixRedirects[buildKey(host, alternative)]; found {
		if isSubpathMatch(path, prefixRedirect.SourcePath) {
			return prefixRedirect, true
		}
	}

	return PrefixRedirect{}, false
}

func toggleTrailingSlash(path string) string {
	if strings.HasSuffix(path, "/") {
		return strings.TrimRight(path, "/")
	}

	return path + "/"
}

func redirect(rw http.ResponseWriter, req *http.Request, target Target, suffix string) {
	targetURL := target.URL

	if suffix != "" && suffix != "/" {
		targetURL = strings.TrimRight(targetURL, "/") + "/" + strings.TrimLeft(suffix, "/")
	}

	if strings.EqualFold(target.PreserveQueryString, "enabled") && req.URL.RawQuery != "" {
		separator := "?"
		if strings.Contains(targetURL, "?") {
			separator = "&"
		}

		targetURL += separator + req.URL.RawQuery
	}

	http.Redirect(rw, req, targetURL, target.StatusCode)
}

func parseSourceURL(sourceURL string) (string, string, error) {
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid sourceURL %q: %w", sourceURL, err)
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		return "", "", fmt.Errorf("sourceURL must be absolute, got %q", sourceURL)
	}

	if parsed.RawQuery != "" {
		return "", "", fmt.Errorf("sourceURL must not contain query string, got %q", sourceURL)
	}

	if parsed.Fragment != "" {
		return "", "", fmt.Errorf("sourceURL must not contain fragment, got %q", sourceURL)
	}

	host := normalizeHost(parsed.Host)
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}

	return host, path, nil
}

func validateTargetURL(targetURL string) error {
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return err
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("targetURL must be absolute")
	}

	return nil
}

func normalizeHost(host string) string {
	host = strings.ToLower(host)

	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}

	return host
}

func buildKey(host, path string) string {
	return host + "\x00" + path
}

func isValidRedirectStatusCode(statusCode int) bool {
	switch statusCode {
	case http.StatusMovedPermanently, // 301
		http.StatusFound,             // 302
		http.StatusSeeOther,          // 303
		http.StatusTemporaryRedirect, // 307
		http.StatusPermanentRedirect: // 308
		return true
	default:
		return false
	}
}

func isSubpathMatch(path, sourcePath string) bool {
	if path == sourcePath {
		return true
	}

	if strings.HasSuffix(sourcePath, "/") {
		return strings.HasPrefix(path, sourcePath)
	}

	return strings.HasPrefix(path, sourcePath+"/")
}

func isValidEnabledDisabledValue(value string) bool {
	return value == "" || strings.EqualFold(value, "enabled") || strings.EqualFold(value, "disabled")
}
