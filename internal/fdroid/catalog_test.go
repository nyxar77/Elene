package fdroid

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleJSON() string {
	return `{
		"packages": {
			"org.example.notes": {
				"metadata": {
					"name": {"en-US": "Notes"},
					"summary": {"en-US": "Private notes"},
					"categories": ["Writing"],
					"lastUpdated": 200,
					"icon": {"en-US": {"name": "/org.example.notes/en-US/icon.png"}}
				},
				"versions": {
					"hash1": {
						"added": 100,
						"file": {
							"name": "/notes_10.apk",
							"sha256": "aaaa",
							"size": 1024
						},
						"manifest": {
							"versionName": "1.0.0",
							"versionCode": 10,
							"usesSdk": {"minSdkVersion": 21}
						}
					},
					"hash2": {
						"added": 200,
						"file": {
							"name": "/notes_20_arm64.apk",
							"sha256": "bbbb",
							"size": 2048
						},
						"manifest": {
							"versionName": "2.0.0",
							"versionCode": 20,
							"nativecode": ["arm64-v8a"],
							"usesSdk": {"minSdkVersion": 26}
						}
					}
				}
			},
			"com.example.music": {
				"metadata": {
					"name": {"en-US": "Music"},
					"summary": {"en-US": "Offline player"},
					"categories": ["Audio"],
					"lastUpdated": 100
				},
				"versions": {
					"hash3": {
						"added": 150,
						"file": {
							"name": "/music_1.apk",
							"sha256": "cccc",
							"size": 512
						},
						"manifest": {
							"versionName": "0.1",
							"versionCode": 1
						}
					}
				}
			}
		}
	}`
}

func TestCatalogLoadsFiltersAndPaginates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleJSON()))
	}))
	defer server.Close()

	catalog := New()
	catalog.SetTestEndpoints(server.URL, server.URL)

	page, err := catalog.List(context.Background(), "", "", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if page.Total != 2 {
		t.Fatalf("expected 2 total apps, got %d", page.Total)
	}
	if page.Apps[0].ID != "org.example.notes" {
		t.Fatalf("expected first app to be org.example.notes, got %s", page.Apps[0].ID)
	}
	if page.Apps[0].VersionName != "2.0.0" || page.Apps[0].VersionCode != 20 {
		t.Fatalf("expected latest version 2.0.0 (20), got %s (%d)", page.Apps[0].VersionName, page.Apps[0].VersionCode)
	}

	// Filter by search query
	page, err = catalog.List(context.Background(), "music", "Audio", 1)
	if err != nil || page.Total != 1 || page.Apps[0].Name != "Music" {
		t.Fatalf("failed filtering: total=%d, err=%v", page.Total, err)
	}

	// Pagination out of bounds
	page, err = catalog.List(context.Background(), "", "", 2)
	if err != nil || len(page.Apps) != 0 || page.Pages != 1 {
		t.Fatalf("unexpected pagination results: %v", page)
	}
}

func TestFindAppAndReleaseCompatibility(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleJSON()))
	}))
	defer server.Close()

	catalog := New()
	catalog.SetTestEndpoints(server.URL, server.URL)

	app, err := catalog.FindApp(context.Background(), "org.example.notes")
	if err != nil {
		t.Fatalf("FindApp failed: %v", err)
	}
	if len(app.Releases) != 2 {
		t.Fatalf("expected 2 releases, got %d", len(app.Releases))
	}

	// Device with ARM64 and SDK 30 matches the newer arm64 release (version 20)
	rel, err := app.CompatibleRelease([]string{"arm64-v8a"}, 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel.VersionCode != 20 {
		t.Fatalf("expected release 20, got %d", rel.VersionCode)
	}

	// Device with x86_64 and SDK 30 doesn't match arm64, so it falls back to the universal release (version 10)
	rel, err = app.CompatibleRelease([]string{"x86_64"}, 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel.VersionCode != 10 {
		t.Fatalf("expected universal release 10, got %d", rel.VersionCode)
	}

	// Device with SDK 19 is too old for both (min is 21)
	_, err = app.CompatibleRelease([]string{"arm64-v8a"}, 19)
	if err == nil {
		t.Fatal("expected error for incompatible SDK level, got nil")
	}
}

func TestDownloadAPKVerification(t *testing.T) {
	fakeAPKContent := []byte("fake android package content for testing")
	hasher := sha256.New()
	hasher.Write(fakeAPKContent)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/test.apk" {
			w.Header().Set("Content-Type", "application/vnd.android.package-archive")
			_, _ = w.Write(fakeAPKContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	catalog := New()
	catalog.SetTestEndpoints(server.URL, server.URL)

	tempDir := t.TempDir()
	destFile := filepath.Join(tempDir, "downloaded.apk")

	// Successful download and hash check
	err := catalog.DownloadAPK(context.Background(), "/test.apk", expectedHash, destFile)
	if err != nil {
		t.Fatalf("expected successful download, got: %v", err)
	}

	content, err := os.ReadFile(destFile)
	if err != nil || string(content) != string(fakeAPKContent) {
		t.Fatalf("file content mismatch: %v", err)
	}

	// Checksum mismatch returns error
	err = catalog.DownloadAPK(context.Background(), "/test.apk", "deadbeef00000000", destFile)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch error, got: %v", err)
	}

	// Traversal attempt rejected
	err = catalog.DownloadAPK(context.Background(), "/../evil.apk", expectedHash, destFile)
	if err == nil {
		t.Fatal("expected directory traversal error, got nil")
	}
}

func TestGetOrDownloadAPKCacheHit(t *testing.T) {
	fakeAPKContent := []byte("cache hit test content for fdroid apk")
	hasher := sha256.New()
	hasher.Write(fakeAPKContent)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))

	downloadHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/test-cache.apk" {
			downloadHits++
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(fakeAPKContent)))
			_, _ = w.Write(fakeAPKContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	catalog := New()
	catalog.SetCacheDir(cacheDir)
	catalog.SetTestEndpoints(server.URL, server.URL)

	var reportedProgress []int64
	onProgress := func(downloaded, total int64) {
		reportedProgress = append(reportedProgress, downloaded)
	}

	// 1st request: Cache miss -> downloads from server
	cachedPath, err := catalog.GetOrDownloadAPK(context.Background(), "/test-cache.apk", expectedHash, onProgress)
	if err != nil {
		t.Fatalf("first download failed: %v", err)
	}
	if downloadHits != 1 {
		t.Fatalf("expected 1 server hit, got %d", downloadHits)
	}
	if len(reportedProgress) == 0 {
		t.Fatal("expected progress reporting during download")
	}

	// Verify cached file exists and matches
	content, err := os.ReadFile(cachedPath)
	if err != nil || string(content) != string(fakeAPKContent) {
		t.Fatalf("cached file content mismatch: %v", err)
	}

	// 2nd request: Cache hit -> immediately returns cached file, zero server hits!
	cachedPath2, err := catalog.GetOrDownloadAPK(context.Background(), "/test-cache.apk", expectedHash, nil)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if cachedPath2 != cachedPath {
		t.Fatalf("expected same cached path %q, got %q", cachedPath, cachedPath2)
	}
	if downloadHits != 1 {
		t.Fatalf("expected server hits to remain 1 (cache hit), got %d", downloadHits)
	}
}

func TestCatalogRejectsInvalidAndOversizedResponses(t *testing.T) {
	for _, body := range []string{`{"packages":{}}`, "not json", strings.Repeat("x", maxIndexBytes+1)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		catalog := New()
		catalog.SetTestEndpoints(server.URL, server.URL)
		if _, err := catalog.List(context.Background(), "", "", 1); err == nil {
			t.Fatalf("accepted invalid index length %d", len(body))
		}
		server.Close()
	}
}

func TestCatalogRejectsBadSearchBeforeFetch(t *testing.T) {
	catalog := New()
	catalog.SetTestEndpoints("http://127.0.0.1:1", "http://127.0.0.1:1")
	if _, err := catalog.List(context.Background(), strings.Repeat("a", 121), "", 1); err == nil || !strings.Contains(err.Error(), "long") {
		t.Fatal(err)
	}
}

func TestCleanIconPath(t *testing.T) {
	for _, path := range []string{"//evil/icon.png", "/../icon.png", "/icon.svg", "/x\\y.png"} {
		if cleanIcon(path) != "" {
			t.Fatalf("expected rejected icon path: %s", path)
		}
	}
	if cleanIcon("/org.example/en-US/icon.png") == "" {
		t.Fatal("valid icon rejected")
	}
}

func TestDiskCachingAndPruning(t *testing.T) {
	tempDir := t.TempDir()
	catDir := filepath.Join(tempDir, "catalog")
	if err := os.MkdirAll(catDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Pre-seed disk cache
	diskFile := filepath.Join(catDir, "test-index-v2.json")
	if err := os.WriteFile(diskFile, []byte(sampleJSON()), 0600); err != nil {
		t.Fatal(err)
	}

	catalog := New()
	catalog.SetCacheDir(tempDir)
	catalog.SetTestEndpoints("http://127.0.0.1:9999/dummy", "http://127.0.0.1:9999/dummy")
	catalog.cacheKey = "test"
	if err := catalog.loadFromDiskCache(); err != nil {
		t.Fatalf("failed to load pre-seeded disk cache: %v", err)
	}

	st := catalog.Status()
	if st.AppCount != 2 {
		t.Fatalf("expected 2 apps loaded from disk cache, got %d", st.AppCount)
	}

	// Test PruneAPKs
	apkDir := filepath.Join(tempDir, "apks")
	if err := os.MkdirAll(apkDir, 0700); err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= 6; i++ {
		fp := filepath.Join(apkDir, fmt.Sprintf("app%d.apk", i))
		_ = os.WriteFile(fp, []byte("data"), 0600)
	}

	// Prune to max 3
	PruneAPKs(apkDir, 0, 3)
	entries, _ := os.ReadDir(apkDir)
	if len(entries) != 3 {
		t.Fatalf("expected 3 files after pruning, got %d", len(entries))
	}
}
