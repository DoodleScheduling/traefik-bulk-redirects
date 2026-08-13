package traefik_bulk_redirects

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Config struct {
	Mode         string     `json:"mode,omitempty"`
	Redirects    []Redirect `json:"redirects,omitempty"`
	FilePath     string     `json:"filePath,omitempty"`
	FileChecksum string     `json:"fileChecksum,omitempty"`
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

const (
	modeInline          = "inline"
	modeFile            = "file"
	maxFileCacheEntries = 8
	maxRulesFileSize    = 16 << 20
)

var inlineCache struct {
	sync.Mutex
	config   *Config
	hash     [sha256.Size]byte
	compiled *compiledRedirects
}

type fileCacheKey struct {
	path     string
	checksum string
}

var fileCache = struct {
	sync.Mutex
	entries map[fileCacheKey]*compiledRedirects
	order   []fileCacheKey
}{
	entries: make(map[fileCacheKey]*compiledRedirects),
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

	switch config.Mode {
	case "", modeInline:
		if config.FilePath != "" || config.FileChecksum != "" {
			return nil, fmt.Errorf("filePath/fileChecksum cannot be configured in inline mode")
		}

		compiled, err := loadInlineRedirects(config)
		if err != nil {
			return nil, err
		}
		return newHandler(next, name, compiled), nil

	case modeFile:
		compiled, err := loadFileRedirects(config)
		if err != nil {
			return nil, err
		}
		return newHandler(next, name, compiled), nil

	default:
		return nil, fmt.Errorf("unsupported bulk redirects mode %q", config.Mode)
	}
}

func loadInlineRedirects(config *Config) (*compiledRedirects, error) {
	inlineCache.Lock()
	defer inlineCache.Unlock()

	if inlineCache.compiled != nil && inlineCache.config == config {
		return inlineCache.compiled, nil
	}

	hash := configHash(config)
	if inlineCache.compiled != nil && inlineCache.hash == hash {
		inlineCache.config = config
		return inlineCache.compiled, nil
	}

	compiled, err := compileRedirects(config.Redirects)
	if err != nil {
		return nil, err
	}

	inlineCache.hash = hash
	inlineCache.compiled = compiled
	inlineCache.config = config

	return compiled, nil
}

type fileRedirects struct {
	Redirects []Redirect `json:"redirects"`
}

func loadFileRedirects(config *Config) (*compiledRedirects, error) {
	if len(config.Redirects) != 0 {
		return nil, fmt.Errorf("redirects cannot be configured inline when mode is %q", modeFile)
	}
	if config.FilePath == "" {
		return nil, fmt.Errorf("filePath is required in file mode")
	}
	if !filepath.IsAbs(config.FilePath) {
		return nil, fmt.Errorf("filePath must be absolute in file mode, got %q", config.FilePath)
	}
	if config.FileChecksum == "" {
		return nil, fmt.Errorf("fileChecksum is required in file mode")
	}

	expectedChecksum, canonicalChecksum, err := parseFileChecksum(config.FileChecksum)
	if err != nil {
		return nil, err
	}

	key := fileCacheKey{path: config.FilePath, checksum: canonicalChecksum}
	fileCache.Lock()
	defer fileCache.Unlock()

	if compiled, found := fileCache.entries[key]; found {
		return compiled, nil
	}

	fileBytes, err := readRulesFile(config.FilePath)
	if err != nil {
		return nil, err
	}

	actualChecksum := sha256.Sum256(fileBytes)
	if actualChecksum != expectedChecksum {
		return nil, fmt.Errorf("checksum mismatch for redirects file %q: expected %s", config.FilePath, canonicalChecksum)
	}

	redirectFile, err := decodeRedirectsFile(fileBytes)
	if err != nil {
		return nil, fmt.Errorf("unable to decode redirects file %q: %w", config.FilePath, err)
	}

	compiled, err := compileRedirects(redirectFile.Redirects)
	if err != nil {
		return nil, fmt.Errorf("invalid redirect in file %q: %w", config.FilePath, err)
	}

	if len(fileCache.order) == maxFileCacheEntries {
		delete(fileCache.entries, fileCache.order[0])
		fileCache.order = fileCache.order[1:]
	}
	fileCache.entries[key] = compiled
	fileCache.order = append(fileCache.order, key)

	return compiled, nil
}

func readRulesFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read redirects file %q: %w", path, err)
	}
	if err := validateRulesFileInfo(path, info); err != nil {
		return nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read redirects file %q: %w", path, err)
	}
	defer func() {
		_ = file.Close()
	}()

	info, err = file.Stat()
	if err != nil {
		return nil, fmt.Errorf("unable to stat redirects file %q: %w", path, err)
	}
	if err := validateRulesFileInfo(path, info); err != nil {
		return nil, err
	}

	fileBytes, err := io.ReadAll(io.LimitReader(file, int64(maxRulesFileSize)+1))
	if err != nil {
		return nil, fmt.Errorf("unable to read redirects file %q: %w", path, err)
	}
	if len(fileBytes) > maxRulesFileSize {
		return nil, fmt.Errorf("redirects file %q exceeds maximum size of %d bytes", path, maxRulesFileSize)
	}

	return fileBytes, nil
}

func validateRulesFileInfo(path string, info os.FileInfo) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("redirects file %q is not a regular file", path)
	}
	if info.Size() > int64(maxRulesFileSize) {
		return fmt.Errorf("redirects file %q exceeds maximum size of %d bytes", path, maxRulesFileSize)
	}
	return nil
}

func parseFileChecksum(value string) ([sha256.Size]byte, string, error) {
	const prefix = "sha256:"
	var checksum [sha256.Size]byte

	if len(value) != len(prefix)+sha256.Size*2 || !strings.HasPrefix(value, prefix) {
		return checksum, "", fmt.Errorf("invalid fileChecksum %q: expected sha256:<64 lowercase hexadecimal characters>", value)
	}

	encoded := value[len(prefix):]
	for i := range checksum {
		high, highValid := lowercaseHexValue(encoded[i*2])
		low, lowValid := lowercaseHexValue(encoded[i*2+1])
		if !highValid || !lowValid {
			return checksum, "", fmt.Errorf("invalid fileChecksum %q: expected sha256:<64 lowercase hexadecimal characters>", value)
		}
		checksum[i] = high<<4 | low
	}

	return checksum, value, nil
}

func lowercaseHexValue(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	default:
		return 0, false
	}
}

func decodeRedirectsFile(fileBytes []byte) (*fileRedirects, error) {
	decoder := json.NewDecoder(bytes.NewReader(fileBytes))
	decoder.DisallowUnknownFields()

	var redirects fileRedirects
	if err := decoder.Decode(&redirects); err != nil {
		return nil, err
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values are not allowed")
		}
		return nil, err
	}

	return &redirects, nil
}

func newHandler(next http.Handler, name string, compiled *compiledRedirects) *BulkRedirects {
	return &BulkRedirects{
		next:     next,
		name:     name,
		compiled: compiled,
	}
}

func compileRedirects(redirects []Redirect) (*compiledRedirects, error) {
	exactRedirects := make(map[string]Target, len(redirects))
	prefixRedirects := make(map[string]PrefixRedirect)

	for _, redirect := range redirects {
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
	var stringBuffer [256]byte
	var boolBuffer [1]byte

	writeUint64 := func(value uint64) {
		binary.LittleEndian.PutUint64(encoded[:], value)
		_, _ = hash.Write(encoded[:])
	}
	writeString := func(value string) {
		writeUint64(uint64(len(value)))
		for len(value) > 0 {
			written := copy(stringBuffer[:], value)
			_, _ = hash.Write(stringBuffer[:written])
			value = value[written:]
		}
	}
	writeBool := func(value bool) {
		if value {
			boolBuffer[0] = 1
		} else {
			boolBuffer[0] = 0
		}
		_, _ = hash.Write(boolBuffer[:])
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
	hash.Sum(result[:0])
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
