package traefik_bulk_redirects

import (
	"context"
	"net/http"
	"net/http/httptest"
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
