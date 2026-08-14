package traefik_bulk_redirects

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

const benchmarkRedirectCount = 2000

var (
	benchmarkHandler  http.Handler
	benchmarkHash     [32]byte
	benchmarkCompiled *compiledRedirects
)

func BenchmarkNewSameConfig(b *testing.B) {
	config := benchmarkConfig(benchmarkRedirectCount)
	resetCaches()
	if _, err := New(context.Background(), nextHandler(), config, "bulk-redirects"); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		benchmarkHandler, err = New(context.Background(), nextHandler(), config, "bulk-redirects")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNewEquivalentDifferentPointers(b *testing.B) {
	redirects := benchmarkConfig(benchmarkRedirectCount).Redirects
	configs := []*Config{
		{Redirects: append([]Redirect(nil), redirects...)},
		{Redirects: append([]Redirect(nil), redirects...)},
	}
	resetCaches()
	if _, err := New(context.Background(), nextHandler(), configs[0], "bulk-redirects"); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		benchmarkHandler, err = New(context.Background(), nextHandler(), configs[(i+1)%len(configs)], "bulk-redirects")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConfigHash(b *testing.B) {
	config := benchmarkConfig(benchmarkRedirectCount)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkHash = configHash(config)
	}
}

func BenchmarkCompileRedirects(b *testing.B) {
	config := benchmarkConfig(benchmarkRedirectCount)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		benchmarkCompiled, err = compileRedirects(config.Redirects)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNewFileCached(b *testing.B) {
	for _, count := range []int{2000, 10000} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			config := benchmarkFileConfig(b, count)
			resetFileCache()
			if _, err := New(context.Background(), nextHandler(), config, "bulk-redirects"); err != nil {
				b.Fatal(err)
			}
			if err := os.Remove(config.FilePath); err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				currentConfig := &Config{
					Mode:     modeFile,
					FilePath: config.FilePath,
				}
				var err error
				benchmarkHandler, err = New(context.Background(), nextHandler(), currentConfig, "bulk-redirects")
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkNewFileInitialLoad(b *testing.B) {
	config := benchmarkFileConfig(b, benchmarkRedirectCount)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetFileCache()
		var err error
		benchmarkHandler, err = New(context.Background(), nextHandler(), config, "bulk-redirects")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkFileConfig(b *testing.B, count int) *Config {
	b.Helper()
	fileBytes, err := json.Marshal(fileRedirects{Redirects: benchmarkConfig(count).Redirects})
	if err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(b.TempDir(), "redirects.json")
	if err := os.WriteFile(path, fileBytes, 0o600); err != nil {
		b.Fatal(err)
	}
	return fileConfig(path)
}

func benchmarkConfig(count int) *Config {
	redirects := make([]Redirect, count)
	for i := range redirects {
		id := strconv.Itoa(i)
		redirects[i] = Redirect{
			SourceURL:           "https://benchmark.example/source/" + id,
			TargetURL:           "https://benchmark.example/target/" + id,
			StatusCode:          http.StatusMovedPermanently,
			PreserveQueryString: i%2 == 0,
			SubpathMatching:     i%3 == 0,
		}
	}

	return &Config{Redirects: redirects}
}
