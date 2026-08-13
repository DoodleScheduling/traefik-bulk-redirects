package traefik_bulk_redirects

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

type Config struct {
	Redirects []Redirect `json:"redirects,omitempty"`
}

type Redirect struct {
	SourceURL           string `json:"sourceURL,omitempty"`
	TargetURL           string `json:"targetURL,omitempty"`
	StatusCode          int    `json:"statusCode,omitempty"`
	PreserveQueryString bool   `json:"preserveQueryString,omitempty"`
	SubpathMatching     bool   `json:"subpathMatching,omitempty"`
}

type Target struct {
	URL                 string
	StatusCode          int
	PreserveQueryString bool
}

type PrefixRedirect struct {
	SourcePath string
	Target     Target
}

type compiledRedirects struct {
	exactRedirects  map[string]Target
	prefixRedirects map[string]PrefixRedirect
}

var compiledCache struct {
	sync.Mutex
	hash     [sha256.Size]byte
	compiled *compiledRedirects
}

func CreateConfig() *Config {
	return &Config{
		Redirects: []Redirect{},
	}
}

type BulkRedirects struct {
	next     http.Handler
	name     string
	compiled *compiledRedirects
}

func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	_ = ctx

	hash := configHash(config)

	compiledCache.Lock()
	defer compiledCache.Unlock()

	compiled := compiledCache.compiled
	if compiled == nil || compiledCache.hash != hash {
		var err error
		compiled, err = compileRedirects(config)
		if err != nil {
			return nil, err
		}

		compiledCache.hash = hash
		compiledCache.compiled = compiled
	}

	return &BulkRedirects{
		next:     next,
		name:     name,
		compiled: compiled,
	}, nil
}

func compileRedirects(config *Config) (*compiledRedirects, error) {
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

		target := Target{
			URL:                 redirect.TargetURL,
			StatusCode:          redirect.StatusCode,
			PreserveQueryString: redirect.PreserveQueryString,
		}

		key := buildKey(sourceHost, sourcePath)

		if redirect.SubpathMatching {
			prefixRedirects[key] = PrefixRedirect{
				SourcePath: sourcePath,
				Target:     target,
			}
			continue
		}

		exactRedirects[key] = target
	}

	return &compiledRedirects{
		exactRedirects:  exactRedirects,
		prefixRedirects: prefixRedirects,
	}, nil
}

func configHash(config *Config) [sha256.Size]byte {
	hash := sha256.New()
	var encoded [8]byte

	writeUint64 := func(value uint64) {
		binary.LittleEndian.PutUint64(encoded[:], value)
		_, _ = hash.Write(encoded[:])
	}
	writeString := func(value string) {
		writeUint64(uint64(len(value)))
		_, _ = hash.Write([]byte(value))
	}
	writeBool := func(value bool) {
		if value {
			_, _ = hash.Write([]byte{1})
			return
		}
		_, _ = hash.Write([]byte{0})
	}

	writeUint64(uint64(len(config.Redirects)))
	for _, redirect := range config.Redirects {
		writeString(redirect.SourceURL)
		writeString(redirect.TargetURL)
		writeUint64(uint64(redirect.StatusCode))
		writeBool(redirect.PreserveQueryString)
		writeBool(redirect.SubpathMatching)
	}

	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result
}

func (bulkRedirects *BulkRedirects) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	host := normalizeHost(req.Host)
	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}

	if target, found := bulkRedirects.compiled.exactRedirects[buildKey(host, path)]; found {
		redirect(rw, req, target, "")
		return
	}

	if prefixRedirect, found := bulkRedirects.findPrefixRedirect(host, path); found {
		suffix := strings.TrimPrefix(path, prefixRedirect.SourcePath)
		redirect(rw, req, prefixRedirect.Target, suffix)
		return
	}

	bulkRedirects.next.ServeHTTP(rw, req)
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
	if prefixRedirect, found := bulkRedirects.compiled.prefixRedirects[buildKey(host, candidate)]; found {
		if isSubpathMatch(path, prefixRedirect.SourcePath) {
			return prefixRedirect, true
		}
	}

	if candidate == "/" {
		return PrefixRedirect{}, false
	}

	alternative := toggleTrailingSlash(candidate)

	if prefixRedirect, found := bulkRedirects.compiled.prefixRedirects[buildKey(host, alternative)]; found {
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

	if target.PreserveQueryString && req.URL.RawQuery != "" {
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
