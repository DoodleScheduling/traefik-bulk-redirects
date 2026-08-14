package traefik_bulk_redirects

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestExactRedirect(t *testing.T) {
	handler := newTestHandler(t, []Redirect{
		{
			SourceURL:           "https://example.com/premium/coupon",
			TargetURL:           "https://example.com/en/premium/",
			StatusCode:          http.StatusFound,
			PreserveQueryString: false,
			SubpathMatching:     false,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/premium/coupon", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusFound)
	assertLocation(t, rec, "https://example.com/en/premium/")
}

func TestPassThroughWhenRedirectIsNotFound(t *testing.T) {
	handler := newTestHandler(t, []Redirect{
		{
			SourceURL:           "https://example.com/premium/coupon",
			TargetURL:           "https://example.com/en/premium/",
			StatusCode:          http.StatusMovedPermanently,
			PreserveQueryString: true,
			SubpathMatching:     false,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/unknown", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusTeapot)
}

func TestExactRedirectPreservesQueryString(t *testing.T) {
	handler := newTestHandler(t, []Redirect{
		{
			SourceURL:           "https://example.com/premium/coupon",
			TargetURL:           "https://example.com/en/premium/",
			StatusCode:          http.StatusFound,
			PreserveQueryString: true,
			SubpathMatching:     false,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/premium/coupon?utm_source=google&campaign=test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusFound)
	assertLocation(t, rec, "https://example.com/en/premium/?utm_source=google&campaign=test")
}

func TestExactRedirectDoesNotPreserveQueryStringWhenDisabled(t *testing.T) {
	handler := newTestHandler(t, []Redirect{
		{
			SourceURL:           "https://example.com/premium/coupon",
			TargetURL:           "https://example.com/en/premium/",
			StatusCode:          http.StatusFound,
			PreserveQueryString: false,
			SubpathMatching:     false,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/premium/coupon?utm_source=google", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusFound)
	assertLocation(t, rec, "https://example.com/en/premium/")
}

func TestRedirectAppendsQueryStringWithAmpersandWhenTargetAlreadyHasQueryString(t *testing.T) {
	handler := newTestHandler(t, []Redirect{
		{
			SourceURL:           "https://example.com/premium/coupon",
			TargetURL:           "https://example.com/en/premium/?plan=pro",
			StatusCode:          http.StatusFound,
			PreserveQueryString: true,
			SubpathMatching:     false,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/premium/coupon?utm_source=google", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusFound)
	assertLocation(t, rec, "https://example.com/en/premium/?plan=pro&utm_source=google")
}

func TestRequestHostIsNormalizedWhenItContainsPort(t *testing.T) {
	handler := newTestHandler(t, []Redirect{
		{
			SourceURL:           "https://example.com/premium/coupon",
			TargetURL:           "https://example.com/en/premium/",
			StatusCode:          http.StatusMovedPermanently,
			PreserveQueryString: false,
			SubpathMatching:     false,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com:443/premium/coupon", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusMovedPermanently)
	assertLocation(t, rec, "https://example.com/en/premium/")
}

func TestSourceURLHostIsNormalizedWhenConfiguredHostHasUppercase(t *testing.T) {
	handler := newTestHandler(t, []Redirect{
		{
			SourceURL:           "https://EXAMPLE.com/premium/coupon",
			TargetURL:           "https://example.com/en/premium/",
			StatusCode:          http.StatusMovedPermanently,
			PreserveQueryString: false,
			SubpathMatching:     false,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/premium/coupon", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusMovedPermanently)
	assertLocation(t, rec, "https://example.com/en/premium/")
}

func TestDefaultStatusCodeIsMovedPermanently(t *testing.T) {
	handler := newTestHandler(t, []Redirect{
		{
			SourceURL:           "https://example.com/premium/coupon",
			TargetURL:           "https://example.com/en/premium/",
			StatusCode:          0,
			PreserveQueryString: false,
			SubpathMatching:     false,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/premium/coupon", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusMovedPermanently)
	assertLocation(t, rec, "https://example.com/en/premium/")
}

func TestPrefixRedirectExactSourcePath(t *testing.T) {
	handler := newTestHandler(t, []Redirect{
		{
			SourceURL:           "https://example.com/docs",
			TargetURL:           "https://example.com/en/resources",
			StatusCode:          http.StatusMovedPermanently,
			PreserveQueryString: true,
			SubpathMatching:     true,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/docs?utm=test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusMovedPermanently)
	assertLocation(t, rec, "https://example.com/en/resources?utm=test")
}

func TestPrefixRedirectWithSubpath(t *testing.T) {
	handler := newTestHandler(t, []Redirect{
		{
			SourceURL:           "https://example.com/docs",
			TargetURL:           "https://example.com/en/resources",
			StatusCode:          http.StatusMovedPermanently,
			PreserveQueryString: true,
			SubpathMatching:     true,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/docs/api/v1?utm=test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusMovedPermanently)
	assertLocation(t, rec, "https://example.com/en/resources/api/v1?utm=test")
}

func TestPrefixRedirectWithTrailingSlashSourcePath(t *testing.T) {
	handler := newTestHandler(t, []Redirect{
		{
			SourceURL:           "https://example.com/docs/",
			TargetURL:           "https://example.com/en/resources/",
			StatusCode:          http.StatusMovedPermanently,
			PreserveQueryString: true,
			SubpathMatching:     true,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/docs/api/v1", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusMovedPermanently)
	assertLocation(t, rec, "https://example.com/en/resources/api/v1")
}

func TestPrefixRedirectDoesNotMatchSimilarPath(t *testing.T) {
	handler := newTestHandler(t, []Redirect{
		{
			SourceURL:           "https://example.com/docs",
			TargetURL:           "https://example.com/en/resources",
			StatusCode:          http.StatusMovedPermanently,
			PreserveQueryString: true,
			SubpathMatching:     true,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/docs-other", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusTeapot)
}

func TestExactRedirectHasPriorityOverPrefixRedirect(t *testing.T) {
	handler := newTestHandler(t, []Redirect{
		{
			SourceURL:           "https://example.com/docs",
			TargetURL:           "https://example.com/en/resources",
			StatusCode:          http.StatusMovedPermanently,
			PreserveQueryString: true,
			SubpathMatching:     true,
		},
		{
			SourceURL:           "https://example.com/docs/special",
			TargetURL:           "https://example.com/en/special-page",
			StatusCode:          http.StatusFound,
			PreserveQueryString: true,
			SubpathMatching:     false,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/docs/special?utm=test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusFound)
	assertLocation(t, rec, "https://example.com/en/special-page?utm=test")
}

func TestMostSpecificPrefixRedirectWins(t *testing.T) {
	handler := newTestHandler(t, []Redirect{
		{
			SourceURL:           "https://example.com/docs",
			TargetURL:           "https://example.com/en/resources",
			StatusCode:          http.StatusMovedPermanently,
			PreserveQueryString: false,
			SubpathMatching:     true,
		},
		{
			SourceURL:           "https://example.com/docs/api",
			TargetURL:           "https://example.com/en/api-docs",
			StatusCode:          http.StatusFound,
			PreserveQueryString: false,
			SubpathMatching:     true,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/docs/api/v1", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusFound)
	assertLocation(t, rec, "https://example.com/en/api-docs/v1")
}

func TestRootPrefixRedirectMatchesEveryPath(t *testing.T) {
	handler := newTestHandler(t, []Redirect{
		{
			SourceURL:           "https://example.com/",
			TargetURL:           "https://example.com/en",
			StatusCode:          http.StatusMovedPermanently,
			PreserveQueryString: true,
			SubpathMatching:     true,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/foo/bar?utm=test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusMovedPermanently)
	assertLocation(t, rec, "https://example.com/en/foo/bar?utm=test")
}

func TestRootExactRedirectOnlyMatchesRoot(t *testing.T) {
	handler := newTestHandler(t, []Redirect{
		{
			SourceURL:           "https://example.com/",
			TargetURL:           "https://example.com/en",
			StatusCode:          http.StatusMovedPermanently,
			PreserveQueryString: false,
			SubpathMatching:     false,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/foo", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusTeapot)
}

func TestNewReturnsErrorForInvalidStatusCode(t *testing.T) {
	_, err := New(context.Background(), nextHandler(), &Config{
		Redirects: []Redirect{
			{
				SourceURL:           "https://example.com/premium/coupon",
				TargetURL:           "https://example.com/en/premium/",
				StatusCode:          http.StatusOK,
				PreserveQueryString: true,
				SubpathMatching:     false,
			},
		},
	}, "bulk-redirects")

	assertErrorContains(t, err, "invalid statusCode")
}

func TestNewReturnsErrorWhenSourceURLIsMissing(t *testing.T) {
	_, err := New(context.Background(), nextHandler(), &Config{
		Redirects: []Redirect{
			{
				TargetURL:           "https://example.com/en/premium/",
				StatusCode:          http.StatusMovedPermanently,
				PreserveQueryString: true,
				SubpathMatching:     false,
			},
		},
	}, "bulk-redirects")

	assertErrorContains(t, err, "sourceURL is required")
}

func TestNewReturnsErrorWhenSourceURLIsNotAbsolute(t *testing.T) {
	_, err := New(context.Background(), nextHandler(), &Config{
		Redirects: []Redirect{
			{
				SourceURL:           "example.com/premium/coupon",
				TargetURL:           "https://example.com/en/premium/",
				StatusCode:          http.StatusMovedPermanently,
				PreserveQueryString: true,
				SubpathMatching:     false,
			},
		},
	}, "bulk-redirects")

	assertErrorContains(t, err, "sourceURL must be absolute")
}

func TestNewReturnsErrorWhenSourceURLContainsQueryString(t *testing.T) {
	_, err := New(context.Background(), nextHandler(), &Config{
		Redirects: []Redirect{
			{
				SourceURL:           "https://example.com/premium/coupon?utm=test",
				TargetURL:           "https://example.com/en/premium/",
				StatusCode:          http.StatusMovedPermanently,
				PreserveQueryString: true,
				SubpathMatching:     false,
			},
		},
	}, "bulk-redirects")

	assertErrorContains(t, err, "sourceURL must not contain query string")
}

func TestNewReturnsErrorWhenSourceURLContainsFragment(t *testing.T) {
	_, err := New(context.Background(), nextHandler(), &Config{
		Redirects: []Redirect{
			{
				SourceURL:           "https://example.com/premium/coupon#section",
				TargetURL:           "https://example.com/en/premium/",
				StatusCode:          http.StatusMovedPermanently,
				PreserveQueryString: true,
				SubpathMatching:     false,
			},
		},
	}, "bulk-redirects")

	assertErrorContains(t, err, "sourceURL must not contain fragment")
}

func TestNewReturnsErrorWhenTargetURLIsMissing(t *testing.T) {
	_, err := New(context.Background(), nextHandler(), &Config{
		Redirects: []Redirect{
			{
				SourceURL:           "https://example.com/premium/coupon",
				StatusCode:          http.StatusMovedPermanently,
				PreserveQueryString: true,
				SubpathMatching:     false,
			},
		},
	}, "bulk-redirects")

	assertErrorContains(t, err, "targetURL is required")
}

func TestNewReturnsErrorWhenTargetURLIsNotAbsolute(t *testing.T) {
	_, err := New(context.Background(), nextHandler(), &Config{
		Redirects: []Redirect{
			{
				SourceURL:           "https://example.com/premium/coupon",
				TargetURL:           "/en/premium/",
				StatusCode:          http.StatusMovedPermanently,
				PreserveQueryString: true,
				SubpathMatching:     false,
			},
		},
	}, "bulk-redirects")

	assertErrorContains(t, err, "targetURL must be absolute")
}

func TestParseSourceURLDefaultsEmptyPathToRoot(t *testing.T) {
	host, path, err := parseSourceURL("https://example.com")
	if err != nil {
		t.Fatal(err)
	}

	if host != "example.com" {
		t.Fatalf("expected host %q, got %q", "example.com", host)
	}

	if path != "/" {
		t.Fatalf("expected path %q, got %q", "/", path)
	}
}

func TestIsSubpathMatch(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		sourcePath string
		want       bool
	}{
		{
			name:       "exact path matches",
			path:       "/docs",
			sourcePath: "/docs",
			want:       true,
		},
		{
			name:       "subpath matches",
			path:       "/docs/api",
			sourcePath: "/docs",
			want:       true,
		},
		{
			name:       "nested subpath matches",
			path:       "/docs/api/v1",
			sourcePath: "/docs",
			want:       true,
		},
		{
			name:       "similar path does not match",
			path:       "/docs-other",
			sourcePath: "/docs",
			want:       false,
		},
		{
			name:       "source path with trailing slash matches child",
			path:       "/docs/api",
			sourcePath: "/docs/",
			want:       true,
		},
		{
			name:       "source path with trailing slash does not match path without slash",
			path:       "/docs",
			sourcePath: "/docs/",
			want:       false,
		},
		{
			name:       "root matches child",
			path:       "/docs",
			sourcePath: "/",
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSubpathMatch(tt.path, tt.sourcePath)
			if got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestIsValidRedirectStatusCode(t *testing.T) {
	validCodes := []int{
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	}

	for _, code := range validCodes {
		if !isValidRedirectStatusCode(code) {
			t.Fatalf("expected status code %d to be valid", code)
		}
	}

	invalidCodes := []int{
		http.StatusOK,
		http.StatusBadRequest,
		http.StatusInternalServerError,
	}

	for _, code := range invalidCodes {
		if isValidRedirectStatusCode(code) {
			t.Fatalf("expected status code %d to be invalid", code)
		}
	}
}

func TestNewReusesCompiledRedirectsForEquivalentConfigs(t *testing.T) {
	resetCaches()

	redirects := []Redirect{{
		SourceURL:           "https://cache-reuse.example/source",
		TargetURL:           "https://cache-reuse.example/target",
		StatusCode:          http.StatusFound,
		PreserveQueryString: true,
		SubpathMatching:     true,
	}}

	first := newBulkRedirects(t, &Config{Redirects: append([]Redirect(nil), redirects...)})
	second := newBulkRedirects(t, &Config{Redirects: append([]Redirect(nil), redirects...)})

	if first.compiled != second.compiled {
		t.Fatal("equivalent configs did not reuse compiled redirects")
	}
}

func TestNewWithSameConfigPointerReusesCompiledRedirects(t *testing.T) {
	resetCaches()
	config := testCacheConfig("same-pointer")

	first := newBulkRedirects(t, config)
	second := newBulkRedirects(t, config)

	if first.compiled != second.compiled {
		t.Fatal("same config pointer did not reuse compiled redirects")
	}
}

func TestNewUsesDifferentCompiledRedirectsForDifferentConfigs(t *testing.T) {
	first := newBulkRedirects(t, testCacheConfig("one"))
	second := newBulkRedirects(t, testCacheConfig("two"))

	if first.compiled == second.compiled {
		t.Fatal("different configs reused compiled redirects")
	}
}

func TestRelevantRedirectFieldsInvalidateCache(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Redirect)
	}{
		{name: "source URL", change: func(r *Redirect) { r.SourceURL = "https://cache-fields.example/other" }},
		{name: "target URL", change: func(r *Redirect) { r.TargetURL = "https://cache-fields.example/other-target" }},
		{name: "status code", change: func(r *Redirect) { r.StatusCode = http.StatusTemporaryRedirect }},
		{name: "preserve query string", change: func(r *Redirect) { r.PreserveQueryString = !r.PreserveQueryString }},
		{name: "subpath matching", change: func(r *Redirect) { r.SubpathMatching = !r.SubpathMatching }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := testCacheConfig("fields")
			changed := testCacheConfig("fields")
			tt.change(&changed.Redirects[0])

			first := newBulkRedirects(t, original)
			second := newBulkRedirects(t, changed)
			if first.compiled == second.compiled {
				t.Fatal("changed redirect field did not invalidate cache")
			}
		})
	}
}

func TestRedirectOrderAffectsCacheIdentity(t *testing.T) {
	firstRedirect := testCacheConfig("order-one").Redirects[0]
	secondRedirect := testCacheConfig("order-two").Redirects[0]
	firstConfig := &Config{Redirects: []Redirect{firstRedirect, secondRedirect}}
	secondConfig := &Config{Redirects: []Redirect{secondRedirect, firstRedirect}}

	if configHash(firstConfig) == configHash(secondConfig) {
		t.Fatal("redirect order did not affect config hash")
	}

	first := newBulkRedirects(t, firstConfig)
	second := newBulkRedirects(t, secondConfig)
	if first.compiled == second.compiled {
		t.Fatal("redirect order did not invalidate cache")
	}
}

func TestConfigHashEquivalentConfigsMatch(t *testing.T) {
	first := testCacheConfig("equivalent-hash")
	second := &Config{Redirects: append([]Redirect(nil), first.Redirects...)}

	if configHash(first) != configHash(second) {
		t.Fatal("equivalent configs produced different hashes")
	}
}

func TestConfigHashIncludesEveryRedirectField(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Redirect)
	}{
		{name: "source URL", change: func(r *Redirect) { r.SourceURL += "/changed" }},
		{name: "target URL", change: func(r *Redirect) { r.TargetURL += "/changed" }},
		{name: "status code", change: func(r *Redirect) { r.StatusCode = http.StatusTemporaryRedirect }},
		{name: "preserve query string", change: func(r *Redirect) { r.PreserveQueryString = !r.PreserveQueryString }},
		{name: "subpath matching", change: func(r *Redirect) { r.SubpathMatching = !r.SubpathMatching }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := testCacheConfig("hash-fields")
			changed := &Config{Redirects: append([]Redirect(nil), original.Redirects...)}
			tt.change(&changed.Redirects[0])

			if configHash(original) == configHash(changed) {
				t.Fatal("changed field did not affect config hash")
			}
		})
	}
}

func TestConfigHashDetectsChangeInMiddleRedirect(t *testing.T) {
	original := benchmarkConfig(5)
	changed := &Config{Redirects: append([]Redirect(nil), original.Redirects...)}
	changed.Redirects[len(changed.Redirects)/2].TargetURL += "/changed"

	if configHash(original) == configHash(changed) {
		t.Fatal("changed middle redirect did not affect config hash")
	}
}

func TestConfigHashFramesStringsByLength(t *testing.T) {
	first := &Config{Redirects: []Redirect{{SourceURL: "a", TargetURL: "bc"}}}
	second := &Config{Redirects: []Redirect{{SourceURL: "ab", TargetURL: "c"}}}

	if configHash(first) == configHash(second) {
		t.Fatal("differently framed strings produced the same hash")
	}
}

func TestConfigHashMatchesPreviousSerialization(t *testing.T) {
	config := &Config{Redirects: []Redirect{
		{
			SourceURL:           "https://example.com/" + strings.Repeat("long-source/", 40),
			TargetURL:           "https://example.com/" + strings.Repeat("long-target/", 40),
			StatusCode:          http.StatusPermanentRedirect,
			PreserveQueryString: true,
			SubpathMatching:     true,
		},
		{
			SourceURL:  "",
			TargetURL:  "",
			StatusCode: 0,
		},
	}}

	if got, expected := configHash(config), previousConfigHash(config); got != expected {
		t.Fatalf("optimized hash %x differs from previous hash %x", got, expected)
	}
}

func previousConfigHash(config *Config) [sha256.Size]byte {
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

func TestPreviouslyCompiledRedirectsRemainValidAfterCacheReplacement(t *testing.T) {
	oldHandler := newBulkRedirects(t, testCacheConfig("old"))
	oldCompiled := oldHandler.compiled
	newHandler := newBulkRedirects(t, testCacheConfig("new"))

	if oldCompiled == newHandler.compiled {
		t.Fatal("new config reused old compiled redirects")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "https://cache-old.example/source", nil)
	oldHandler.ServeHTTP(rec, req)
	assertStatus(t, rec, http.StatusFound)
	assertLocation(t, rec, "https://cache-old.example/target")
}

func TestConcurrentNewReusesCompiledRedirects(t *testing.T) {
	resetCaches()

	const goroutines = 100
	config := testCacheConfig("concurrent-new")
	compiled := make(chan *compiledRedirects, goroutines)
	errs := make(chan error, goroutines)

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			handler, err := New(context.Background(), nextHandler(), config, "different-name")
			if err != nil {
				errs <- err
				return
			}
			compiled <- handler.(*BulkRedirects).compiled
		}()
	}
	wg.Wait()
	close(errs)
	close(compiled)

	for err := range errs {
		t.Fatal(err)
	}

	var expected *compiledRedirects
	for current := range compiled {
		if expected == nil {
			expected = current
			continue
		}
		if current != expected {
			t.Fatal("concurrent New calls did not reuse compiled redirects")
		}
	}

}

func TestConcurrentServeHTTPWithSharedCompiledRedirects(t *testing.T) {
	const goroutines = 100
	first := newBulkRedirects(t, testCacheConfig("concurrent-serve"))
	second := newBulkRedirects(t, testCacheConfig("concurrent-serve"))
	if first.compiled != second.compiled {
		t.Fatal("handlers do not share compiled redirects")
	}

	var wg sync.WaitGroup
	errs := make(chan string, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			handler := first
			if index%2 == 1 {
				handler = second
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "https://cache-concurrent-serve.example/source?q=1", nil)
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusFound {
				errs <- "unexpected status"
			}
			if rec.Header().Get("Location") != "https://cache-concurrent-serve.example/target?q=1" {
				errs <- "unexpected location"
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}

func TestInlineModesRemainBackwardsCompatible(t *testing.T) {
	for _, mode := range []string{"", modeInline} {
		t.Run("mode="+mode, func(t *testing.T) {
			resetCaches()
			config := testCacheConfig("inline-" + mode)
			config.Mode = mode
			handler := newBulkRedirects(t, config)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, config.Redirects[0].SourceURL+"?q=1", nil)
			handler.ServeHTTP(rec, req)
			assertStatus(t, rec, http.StatusFound)
			assertLocation(t, rec, config.Redirects[0].TargetURL+"?q=1")
		})
	}
}

func TestNewValidatesConfigurationMode(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected string
	}{
		{name: "unknown mode", config: &Config{Mode: "unknown"}, expected: `unsupported bulk redirects mode "unknown"`},
		{name: "inline file path", config: &Config{FilePath: "/redirects.json"}, expected: "filePath cannot be configured in inline mode"},
		{name: "file redirects", config: &Config{Mode: modeFile, Redirects: []Redirect{{}}, FilePath: "/redirects.json"}, expected: `redirects cannot be configured inline when mode is "file"`},
		{name: "file path missing", config: &Config{Mode: modeFile}, expected: "filePath is required in file mode"},
		{name: "relative file path", config: &Config{Mode: modeFile, FilePath: "redirects.json"}, expected: "filePath must be absolute in file mode"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(context.Background(), nextHandler(), tt.config, "bulk-redirects")
			assertErrorContains(t, err, tt.expected)
		})
	}
}

func TestFileModeReadAndJSONErrors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		resetCaches()
		path := filepath.Join(t.TempDir(), "missing.json")
		_, err := New(context.Background(), nextHandler(), fileConfig(path), "bulk-redirects")
		assertErrorContains(t, err, "unable to read redirects file")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		resetCaches()
		path := writeRedirectBytes(t, []byte(`{"redirects":`))
		_, err := New(context.Background(), nextHandler(), fileConfig(path), "bulk-redirects")
		assertErrorContains(t, err, "unable to decode redirects file")
	})

	t.Run("unknown file field", func(t *testing.T) {
		resetCaches()
		path := writeRedirectBytes(t, []byte(`{"redirects":[],"mode":"inline"}`))
		_, err := New(context.Background(), nextHandler(), fileConfig(path), "bulk-redirects")
		assertErrorContains(t, err, "unable to decode redirects file")
	})

	t.Run("multiple JSON values", func(t *testing.T) {
		resetCaches()
		path := writeRedirectBytes(t, []byte(`{"redirects":[]} {"redirects":[]}`))
		_, err := New(context.Background(), nextHandler(), fileConfig(path), "bulk-redirects")
		assertErrorContains(t, err, "multiple JSON values are not allowed")
	})

	t.Run("invalid redirect", func(t *testing.T) {
		resetCaches()
		path := writeRedirectFile(t, []Redirect{{TargetURL: "https://example.com/target"}})
		_, err := New(context.Background(), nextHandler(), fileConfig(path), "bulk-redirects")
		assertErrorContains(t, err, "invalid redirect in file")
		assertErrorContains(t, err, "sourceURL is required")
	})
}

func TestReadRulesFileSizeLimit(t *testing.T) {
	t.Run("exact maximum size", func(t *testing.T) {
		path := writeSizedFile(t, maxRulesFileSize)
		fileBytes, err := readRulesFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(fileBytes) != maxRulesFileSize {
			t.Fatalf("expected %d bytes, got %d", maxRulesFileSize, len(fileBytes))
		}
	})

	t.Run("maximum size plus one", func(t *testing.T) {
		path := writeSizedFile(t, maxRulesFileSize+1)
		_, err := readRulesFile(path)
		assertErrorContains(t, err, path)
		assertErrorContains(t, err, "exceeds maximum size of 16777216 bytes")
	})
}

func TestReadRulesFileRejectsNonRegularFile(t *testing.T) {
	path := t.TempDir()
	_, err := readRulesFile(path)
	assertErrorContains(t, err, `redirects file "`+path+`" is not a regular file`)
}

func TestFileModeRedirectBehavior(t *testing.T) {
	resetCaches()
	path := writeRedirectFile(t, []Redirect{
		{
			SourceURL:           "https://file.example/exact",
			TargetURL:           "https://file.example/exact-target",
			StatusCode:          http.StatusFound,
			PreserveQueryString: true,
		},
		{
			SourceURL:           "https://file.example/docs",
			TargetURL:           "https://file.example/resources",
			StatusCode:          http.StatusMovedPermanently,
			PreserveQueryString: true,
			SubpathMatching:     true,
		},
	})
	handler := newBulkRedirects(t, fileConfig(path))

	tests := []struct {
		name     string
		url      string
		status   int
		location string
	}{
		{name: "exact", url: "https://file.example/exact?q=1", status: http.StatusFound, location: "https://file.example/exact-target?q=1"},
		{name: "prefix", url: "https://file.example/docs/api?q=1", status: http.StatusMovedPermanently, location: "https://file.example/resources/api?q=1"},
		{name: "pass through", url: "https://file.example/unmatched", status: http.StatusTeapot},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			handler.ServeHTTP(rec, req)
			assertStatus(t, rec, tt.status)
			if tt.location != "" {
				assertLocation(t, rec, tt.location)
			}
		})
	}
}

func TestFileModeCacheHitDoesNotReadFileAgain(t *testing.T) {
	resetCaches()
	path := writeRedirectFile(t, testCacheConfig("file-cache").Redirects)
	first := newBulkRedirects(t, fileConfig(path))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	second := newBulkRedirects(t, fileConfig(path))

	if first.compiled != second.compiled {
		t.Fatal("same file path did not reuse compiled redirects")
	}
}

func TestFileModeDifferentPathsUseDifferentCompiledRedirects(t *testing.T) {
	resetCaches()
	pathA := writeRedirectFile(t, testCacheConfig("different-path-a").Redirects)
	pathB := writeRedirectFile(t, testCacheConfig("different-path-b").Redirects)

	handlerA := newBulkRedirects(t, fileConfig(pathA))
	handlerB := newBulkRedirects(t, fileConfig(pathB))
	if handlerA.compiled == handlerB.compiled {
		t.Fatal("different file paths reused compiled redirects")
	}

	assertHandlerRedirect(t, handlerA, "https://cache-different-path-a.example/source", "https://cache-different-path-a.example/target")
	assertHandlerRedirect(t, handlerB, "https://cache-different-path-b.example/source", "https://cache-different-path-b.example/target")
}

func TestFileModeTreatsPathAsImmutableForProcessLifetime(t *testing.T) {
	resetCaches()
	path := writeRedirectFile(t, testCacheConfig("version-a").Redirects)
	handlerA := newBulkRedirects(t, fileConfig(path))

	overwriteRedirectFile(t, path, testCacheConfig("version-b").Redirects)
	handlerB := newBulkRedirects(t, fileConfig(path))
	if handlerA.compiled != handlerB.compiled {
		t.Fatal("same file path did not reuse the immutable compiled snapshot")
	}

	assertHandlerRedirect(t, handlerA, "https://cache-version-a.example/source", "https://cache-version-a.example/target")
	assertHandlerRedirect(t, handlerB, "https://cache-version-a.example/source", "https://cache-version-a.example/target")
}

func TestFileModeEmptyCacheLoadsCurrentFileVersion(t *testing.T) {
	resetCaches()
	path := writeRedirectFile(t, testCacheConfig("version-a").Redirects)
	newBulkRedirects(t, fileConfig(path))
	overwriteRedirectFile(t, path, testCacheConfig("version-b").Redirects)

	resetFileCache()
	handlerB := newBulkRedirects(t, fileConfig(path))
	assertHandlerRedirect(t, handlerB, "https://cache-version-b.example/source", "https://cache-version-b.example/target")
}

func TestFailedFileLoadDoesNotPublishCacheEntry(t *testing.T) {
	tests := []struct {
		name          string
		fileBytes     []byte
		expectedError string
	}{
		{name: "invalid JSON", fileBytes: []byte(`{"redirects":`), expectedError: "unable to decode redirects file"},
		{name: "invalid redirect", fileBytes: []byte(`{"redirects":[{"targetURL":"https://example.com/target"}]}`), expectedError: "invalid redirect in file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetCaches()
			path := writeRedirectBytes(t, tt.fileBytes)
			_, err := New(context.Background(), nextHandler(), fileConfig(path), "bulk-redirects")
			assertErrorContains(t, err, tt.expectedError)

			overwriteRedirectFile(t, path, testCacheConfig("valid-after-failure").Redirects)
			handler := newBulkRedirects(t, fileConfig(path))
			assertHandlerRedirect(t, handler, "https://cache-valid-after-failure.example/source", "https://cache-valid-after-failure.example/target")
			fileCache.Lock()
			cacheSize := len(fileCache.entries)
			fileCache.Unlock()
			if cacheSize != 1 {
				t.Fatalf("expected one valid cached entry, got %d", cacheSize)
			}
		})
	}
}

func TestOversizedFileDoesNotPublishCacheEntry(t *testing.T) {
	resetCaches()
	path := writeSizedFile(t, maxRulesFileSize+1)
	_, err := New(context.Background(), nextHandler(), fileConfig(path), "bulk-redirects")
	assertErrorContains(t, err, "exceeds maximum size of 16777216 bytes")

	overwriteRedirectFile(t, path, testCacheConfig("valid-after-oversized").Redirects)
	handler := newBulkRedirects(t, fileConfig(path))
	assertHandlerRedirect(t, handler, "https://cache-valid-after-oversized.example/source", "https://cache-valid-after-oversized.example/target")
}

func TestConcurrentNewWithFileCacheKeyReusesCompiledRedirects(t *testing.T) {
	resetCaches()
	path := writeRedirectFile(t, testCacheConfig("concurrent-file").Redirects)

	const goroutines = 100
	results := make(chan *compiledRedirects, goroutines)
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			handler, err := New(context.Background(), nextHandler(), fileConfig(path), "bulk-redirects")
			if err != nil {
				errs <- err
				return
			}
			results <- handler.(*BulkRedirects).compiled
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatal(err)
	}
	var expected *compiledRedirects
	for compiled := range results {
		if expected == nil {
			expected = compiled
		} else if compiled != expected {
			t.Fatal("concurrent file New calls did not share compiled redirects")
		}
	}
}

func TestConcurrentServeHTTPWithSharedFileCompiledRedirects(t *testing.T) {
	resetCaches()
	path := writeRedirectFile(t, testCacheConfig("file-serve").Redirects)
	first := newBulkRedirects(t, fileConfig(path))
	second := newBulkRedirects(t, fileConfig(path))

	const goroutines = 100
	errs := make(chan string, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			handler := first
			if index%2 == 1 {
				handler = second
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "https://cache-file-serve.example/source?q=1", nil)
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://cache-file-serve.example/target?q=1" {
				errs <- "unexpected file redirect response"
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestFileCacheRetainsMoreThanEightDistinctPaths(t *testing.T) {
	resetCaches()
	const pathCount = 16
	var firstPath string
	var firstCompiled *compiledRedirects
	for i := 0; i < pathCount; i++ {
		id := string(rune('a' + i))
		path := writeRedirectFile(t, testCacheConfig("retained-"+id).Redirects)
		handler := newBulkRedirects(t, fileConfig(path))
		if i == 0 {
			firstPath = path
			firstCompiled = handler.compiled
		}
	}

	fileCache.Lock()
	cacheSize := len(fileCache.entries)
	fileCache.Unlock()
	if cacheSize != pathCount {
		t.Fatalf("expected file cache size %d, got %d", pathCount, cacheSize)
	}

	if err := os.Remove(firstPath); err != nil {
		t.Fatal(err)
	}
	firstAgain := newBulkRedirects(t, fileConfig(firstPath))
	if firstAgain.compiled != firstCompiled {
		t.Fatal("first file path did not retain its original compiled redirects")
	}
	assertHandlerRedirect(t, firstAgain, "https://cache-retained-a.example/source", "https://cache-retained-a.example/target")
}

func fileConfig(path string) *Config {
	return &Config{Mode: modeFile, FilePath: path}
}

func writeRedirectFile(t *testing.T, redirects []Redirect) string {
	t.Helper()
	fileBytes, err := json.Marshal(fileRedirects{Redirects: redirects})
	if err != nil {
		t.Fatal(err)
	}
	return writeRedirectBytes(t, fileBytes)
}

func writeRedirectBytes(t *testing.T, fileBytes []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "redirects.json")
	if err := os.WriteFile(path, fileBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func overwriteRedirectFile(t *testing.T, path string, redirects []Redirect) {
	t.Helper()
	fileBytes, err := json.Marshal(fileRedirects{Redirects: redirects})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, fileBytes, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeSizedFile(t *testing.T, size int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "redirects.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(int64(size)); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertHandlerRedirect(t *testing.T, handler http.Handler, sourceURL, targetURL string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, sourceURL, nil)
	handler.ServeHTTP(rec, req)
	assertStatus(t, rec, http.StatusFound)
	assertLocation(t, rec, targetURL)
}

func testCacheConfig(id string) *Config {
	return &Config{Redirects: []Redirect{{
		SourceURL:           "https://cache-" + id + ".example/source",
		TargetURL:           "https://cache-" + id + ".example/target",
		StatusCode:          http.StatusFound,
		PreserveQueryString: true,
		SubpathMatching:     false,
	}}}
}

func newBulkRedirects(t *testing.T, config *Config) *BulkRedirects {
	t.Helper()

	handler, err := New(context.Background(), nextHandler(), config, "bulk-redirects")
	if err != nil {
		t.Fatal(err)
	}
	return handler.(*BulkRedirects)
}

func resetCaches() {
	inlineCache.Lock()
	inlineCache.config = nil
	inlineCache.hash = [sha256.Size]byte{}
	inlineCache.compiled = nil
	inlineCache.Unlock()

	resetFileCache()
}

func resetFileCache() {
	fileCache.Lock()
	fileCache.entries = make(map[string]*compiledRedirects)
	fileCache.Unlock()
}

func newTestHandler(t *testing.T, redirects []Redirect) http.Handler {
	t.Helper()

	handler, err := New(context.Background(), nextHandler(), &Config{
		Redirects: redirects,
	}, "bulk-redirects")
	if err != nil {
		t.Fatal(err)
	}

	return handler
}

func nextHandler() http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusTeapot)
	})
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, expected int) {
	t.Helper()

	if rec.Code != expected {
		t.Fatalf("expected status %d, got %d", expected, rec.Code)
	}
}

func assertLocation(t *testing.T, rec *httptest.ResponseRecorder, expected string) {
	t.Helper()

	if got := rec.Header().Get("Location"); got != expected {
		t.Fatalf("expected Location %q, got %q", expected, got)
	}
}

func assertErrorContains(t *testing.T, err error, expected string) {
	t.Helper()

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected error to contain %q, got %q", expected, err.Error())
	}
}
