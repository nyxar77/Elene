// Package fdroid reads public app metadata from the official F-Droid index
// and securely downloads verified application packages for local ADB installation.
package fdroid

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultIndexURL    = "https://f-droid.org/repo/index-v2.json"
	defaultRepoBaseURL = "https://f-droid.org/repo"
	izzyIndexURL       = "https://apt.izzysoft.de/fdroid/repo/index-v2.json"
	izzyRepoBaseURL    = "https://apt.izzysoft.de/fdroid/repo"
	maxIndexBytes      = 512 << 20 // 512 MiB (large enough for growing repositories)
	maxAPKBytes        = 512 << 20 // 512 MiB
	pageSize           = 24
)

var packageIDPattern = regexp.MustCompile(`^[A-Za-z0-9_]+(?:[.][A-Za-z0-9_]+)+$`)

// Release represents a specific published build of an application in the repository.
type Release struct {
	VersionName string   `json:"versionName"`
	VersionCode int64    `json:"versionCode"`
	MinSDK      int      `json:"minSdk,omitempty"`
	NativeCode  []string `json:"nativeCode,omitempty"`
	APKPath     string   `json:"apkPath"`
	SHA256      string   `json:"sha256"`
	Size        int64    `json:"size"`
	Added       int64    `json:"added"`
}

// App represents an application catalog entry.
type App struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Summary      string    `json:"summary"`
	Description  string    `json:"description,omitempty"`
	Icon         string    `json:"icon,omitempty"`
	Categories   []string  `json:"categories"`
	Updated      int64     `json:"updated"`
	URL          string    `json:"url"`
	License      string    `json:"license,omitempty"`
	SourceCode   string    `json:"sourceCode,omitempty"`
	IssueTracker string    `json:"issueTracker,omitempty"`
	WebSite      string    `json:"webSite,omitempty"`
	VersionName  string    `json:"versionName,omitempty"`
	VersionCode  int64     `json:"versionCode,omitempty"`
	Size         int64     `json:"size,omitempty"`
	Releases     []Release `json:"releases,omitempty"`
}

// Page represents a paginated list of catalog entries.
type Page struct {
	Apps       []App     `json:"apps"`
	Total      int       `json:"total"`
	Page       int       `json:"page"`
	Pages      int       `json:"pages"`
	Prev       int       `json:"prev,omitempty"`
	Next       int       `json:"next,omitempty"`
	Categories []string  `json:"categories"`
	Fetched    time.Time `json:"fetched"`
}

// CatalogStatus reports live sync and download metrics for a repository index.
type CatalogStatus struct {
	Source          string    `json:"source"`
	State           string    `json:"state"` // "idle", "loading", "ready", "error"
	DownloadedBytes int64     `json:"downloadedBytes"`
	TotalBytes      int64     `json:"totalBytes"`
	Percent         int       `json:"percent"`
	Error           string    `json:"error,omitempty"`
	AppCount        int       `json:"appCount"`
	LoadedAt        time.Time `json:"loadedAt,omitempty"`
}

// Catalog manages the local cache of F-Droid repository metadata and downloads.
type Catalog struct {
	client      *http.Client
	indexURL    string
	repoBaseURL string
	cacheDir    string
	cacheKey    string

	mu         sync.Mutex
	loaded     time.Time
	apps       []App
	categories []string
	icons      map[string]bool
	loading    chan struct{}
	status     CatalogStatus
	etag       string
	lastMod    string
}

// New creates an F-Droid catalog client with safe connection parameters and redirect safety.
func New() *Catalog {
	return NewWithEndpoints(defaultIndexURL, defaultRepoBaseURL)
}

// NewIzzy creates a catalog client for the IzzyOnDroid repository (hosts Aurora Store, Obtainium, MicroG, etc.).
func NewIzzy() *Catalog {
	return NewWithEndpoints(izzyIndexURL, izzyRepoBaseURL)
}

// NewWithEndpoints creates a catalog client configured for a specific F-Droid v2 repository.
func NewWithEndpoints(indexURL, repoBaseURL string) *Catalog {
	parsedRepo, _ := url.Parse(repoBaseURL)
	allowedHost := "f-droid.org"
	if parsedRepo != nil && parsedRepo.Hostname() != "" {
		allowedHost = parsedRepo.Hostname()
	}

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   20 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
	}

	httpClient := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			host := req.URL.Hostname()
			if req.URL.Scheme != "https" || (host != allowedHost && host != "f-droid.org" && host != "apt.izzysoft.de") || len(via) > 3 {
				return errors.New("repository redirected outside its catalog host")
			}
			return nil
		},
	}

	cacheDir := ""
	if userCache, err := os.UserCacheDir(); err == nil {
		cacheDir = filepath.Join(userCache, "elene")
	} else {
		cacheDir = filepath.Join(os.TempDir(), "elene-cache")
	}

	cacheKey := "fdroid"
	if strings.Contains(indexURL, "izzy") {
		cacheKey = "izzy"
	}

	cat := &Catalog{
		client:      httpClient,
		indexURL:    indexURL,
		repoBaseURL: repoBaseURL,
		cacheDir:    cacheDir,
		cacheKey:    cacheKey,
		icons:       make(map[string]bool),
		status: CatalogStatus{
			Source: cacheKey,
			State:  "idle",
		},
	}

	// Try loading from local disk cache immediately so apps are available without waiting for network.
	_ = cat.loadFromDiskCache()

	return cat
}

// Status returns a snapshot of current catalog sync and loading state.
func (c *Catalog) Status() CatalogStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.status
	st.AppCount = len(c.apps)
	st.LoadedAt = c.loaded
	if len(c.apps) > 0 && st.State != "loading" {
		st.State = "ready"
	}
	return st
}

// Refresh triggers an immediate catalog update, optionally discarding existing cached headers.
func (c *Catalog) Refresh(ctx context.Context, force bool) error {
	c.mu.Lock()
	if force {
		c.loaded = time.Time{}
		c.etag = ""
		c.lastMod = ""
	}
	c.mu.Unlock()
	return c.ensure(ctx)
}

// SetCacheDir allows overriding the APK and catalog cache root directory (e.g. for testing).
func (c *Catalog) SetCacheDir(dir string) {
	c.mu.Lock()
	c.cacheDir = dir
	c.mu.Unlock()
	_ = c.loadFromDiskCache()
}

// SetTestEndpoints allows tests to override the index and repository URLs.
func (c *Catalog) SetTestEndpoints(indexURL, repoBaseURL string) {
	c.mu.Lock()
	c.indexURL = indexURL
	c.repoBaseURL = repoBaseURL
	c.cacheKey = "test"
	c.apps = nil
	c.categories = nil
	c.icons = make(map[string]bool)
	c.loaded = time.Time{}
	c.etag = ""
	c.lastMod = ""
	c.status = CatalogStatus{Source: "test", State: "idle"}
	c.mu.Unlock()
}

// CompatibleRelease selects the optimal release for the given device architecture (ABIs)
// and Android SDK level. If no device information is available, it returns the newest release.
func (a *App) CompatibleRelease(deviceABIs []string, deviceSDK int) (*Release, error) {
	if len(a.Releases) == 0 {
		return nil, fmt.Errorf("app %s has no available APK releases", a.ID)
	}

	for i := range a.Releases {
		rel := &a.Releases[i]

		// Check minimum SDK requirement
		if rel.MinSDK > 0 && deviceSDK > 0 && deviceSDK < rel.MinSDK {
			continue
		}

		// Check native code (CPU architecture) compatibility
		if len(rel.NativeCode) > 0 && len(deviceABIs) > 0 {
			supported := false
			for _, abi := range rel.NativeCode {
				if slices.Contains(deviceABIs, abi) {
					supported = true
					break
				}
			}
			if !supported {
				continue
			}
		}

		return rel, nil
	}

	// If specific device criteria were supplied but none matched, explain why
	if len(deviceABIs) > 0 || deviceSDK > 0 {
		return nil, fmt.Errorf("no compatible release found for device (SDK: %d, ABIs: %v)", deviceSDK, deviceABIs)
	}

	return &a.Releases[0], nil
}

// FindApp returns the application entry matching the given package ID.
func (c *Catalog) FindApp(ctx context.Context, id string) (App, error) {
	if err := c.ensure(ctx); err != nil {
		return App{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, app := range c.apps {
		if app.ID == id {
			return app, nil
		}
	}

	return App{}, fmt.Errorf("app %q was not found in the F-Droid catalog", id)
}

// LookupReleaseBySHA256 finds an app and release matching the specified SHA-256 hash.
func (c *Catalog) LookupReleaseBySHA256(sha256Hash string) (App, Release, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, a := range c.apps {
		for _, rel := range a.Releases {
			if strings.EqualFold(rel.SHA256, sha256Hash) {
				return a, rel, true
			}
		}
	}
	return App{}, Release{}, false
}

// ResolvePackages looks up multiple package IDs in the catalog cache, returning a map of matches.
func (c *Catalog) ResolvePackages(ctx context.Context, packageIDs []string) (map[string]App, error) {
	if err := c.ensure(ctx); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	lookup := make(map[string]bool, len(packageIDs))
	for _, id := range packageIDs {
		lookup[id] = true
	}

	results := make(map[string]App)
	for _, app := range c.apps {
		if lookup[app.ID] {
			results[app.ID] = app
		}
	}

	return results, nil
}

// List returns a filtered, paginated subset of the catalog.
func (c *Catalog) List(ctx context.Context, query, category string, page int) (Page, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if len(query) > 120 {
		return Page{}, errors.New("search query is too long (maximum 120 characters)")
	}

	if page < 1 {
		page = 1
	}
	if page > 1000 {
		return Page{}, errors.New("page is out of range")
	}

	if err := c.ensure(ctx); err != nil {
		return Page{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	matches := make([]App, 0, pageSize)
	total := 0

	for _, app := range c.apps {
		if query != "" {
			searchTarget := strings.ToLower(app.Name + " " + app.ID + " " + app.Summary)
			if !strings.Contains(searchTarget, query) {
				continue
			}
		}

		if category != "" && !slices.Contains(app.Categories, category) {
			continue
		}

		if total >= (page-1)*pageSize && len(matches) < pageSize {
			matches = append(matches, app)
		}
		total++
	}

	totalPages := (total + pageSize - 1) / pageSize
	prevPage := 0
	if page > 1 {
		prevPage = page - 1
	}

	nextPage := 0
	if page < totalPages {
		nextPage = page + 1
	}

	return Page{
		Apps:       matches,
		Total:      total,
		Page:       page,
		Pages:      totalPages,
		Prev:       prevPage,
		Next:       nextPage,
		Categories: slices.Clone(c.categories),
		Fetched:    c.loaded,
	}, nil
}

// ProgressFunc reports transferred and total bytes during download.
type ProgressFunc func(downloaded, total int64)

// GetOrDownloadAPK returns a cached APK if present and verified, or streams it from F-Droid.
// Downloaded APKs are saved in the local cache directory and retained across installs/retries.
func (c *Catalog) GetOrDownloadAPK(ctx context.Context, apkRelPath, expectedSHA256 string, onProgress ProgressFunc) (string, error) {
	if strings.Contains(apkRelPath, "..") {
		return "", fmt.Errorf("invalid APK path: %s", apkRelPath)
	}
	cleaned := path.Clean(apkRelPath)
	if !strings.HasPrefix(cleaned, "/") || !strings.HasSuffix(strings.ToLower(cleaned), ".apk") {
		return "", fmt.Errorf("invalid APK path: %s", apkRelPath)
	}

	c.mu.Lock()
	cacheDir := c.cacheDir
	baseURL := strings.TrimRight(c.repoBaseURL, "/")
	c.mu.Unlock()

	apkDir := filepath.Join(cacheDir, "apks")
	if err := os.MkdirAll(apkDir, 0700); err != nil {
		return "", fmt.Errorf("create APK cache directory: %w", err)
	}

	cachedFile := filepath.Join(apkDir, expectedSHA256+".apk")

	// 1. Check if the APK is already cached and verify its SHA-256 integrity
	if info, err := os.Stat(cachedFile); err == nil && info.Size() > 0 {
		if fileMatchesSHA256(cachedFile, expectedSHA256) {
			now := time.Now()
			_ = os.Chtimes(cachedFile, now, now)
			if onProgress != nil {
				onProgress(info.Size(), info.Size())
			}
			return cachedFile, nil
		}
		_ = os.Remove(cachedFile)
	}

	// 2. Cache miss: download from repository mirror
	downloadURL := baseURL + cleaned
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("create download request: %w", err)
	}
	req.Header.Set("User-Agent", "Elene/1.0 (Android APK Installer)")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download APK from F-Droid: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("F-Droid repository returned HTTP %d for %s", resp.StatusCode, cleaned)
	}

	totalExpected := resp.ContentLength

	// Stream into a temporary partial file
	tempFile := filepath.Join(apkDir, fmt.Sprintf("%s-%d.part", expectedSHA256, time.Now().UnixNano()))
	out, err := os.OpenFile(tempFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", fmt.Errorf("create destination APK file: %w", err)
	}

	hasher := sha256.New()
	writer := io.MultiWriter(out, hasher)

	var downloaded int64
	buf := make([]byte, 32*1024)
	limitReader := io.LimitReader(resp.Body, maxAPKBytes+1)

	for {
		if ctx.Err() != nil {
			_ = out.Close()
			_ = os.Remove(tempFile)
			return "", ctx.Err()
		}

		n, rErr := limitReader.Read(buf)
		if n > 0 {
			wN, wErr := writer.Write(buf[:n])
			if wErr != nil {
				_ = out.Close()
				_ = os.Remove(tempFile)
				return "", fmt.Errorf("write APK file: %w", wErr)
			}
			downloaded += int64(wN)
			if onProgress != nil {
				onProgress(downloaded, totalExpected)
			}
		}
		if rErr != nil {
			if errors.Is(rErr, io.EOF) {
				break
			}
			_ = out.Close()
			_ = os.Remove(tempFile)
			return "", fmt.Errorf("stream APK download: %w", rErr)
		}
	}

	_ = out.Close()

	if downloaded > maxAPKBytes {
		_ = os.Remove(tempFile)
		return "", errors.New("downloaded APK exceeds maximum allowed size of 512 MiB")
	}
	if downloaded == 0 {
		_ = os.Remove(tempFile)
		return "", errors.New("downloaded APK was empty")
	}

	calculatedHash := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(calculatedHash, expectedSHA256) {
		_ = os.Remove(tempFile)
		return "", fmt.Errorf("checksum mismatch: expected %s, calculated %s", expectedSHA256, calculatedHash)
	}

	// Rename verified file to content-addressed cache path
	if err := os.Rename(tempFile, cachedFile); err != nil {
		_ = os.Remove(tempFile)
		return "", fmt.Errorf("save cached APK: %w", err)
	}

	// Prune older cached APKs in background to keep disk usage bounded
	go PruneAPKs(apkDir, 14*24*time.Hour, 20)

	return cachedFile, nil
}

// DownloadAPK streams an APK from the F-Droid repo, verifying its size and SHA-256 hash.
// If destFile differs from the cached path, it copies the verified file to destFile.
func (c *Catalog) DownloadAPK(ctx context.Context, apkRelPath, expectedSHA256 string, destFile string) error {
	cachedPath, err := c.GetOrDownloadAPK(ctx, apkRelPath, expectedSHA256, nil)
	if err != nil {
		return err
	}
	if cachedPath == destFile {
		return nil
	}

	src, err := os.Open(cachedPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(destFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

func fileMatchesSHA256(filePath, expected string) bool {
	f, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return false
	}
	return strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), expected)
}

func PruneAPKs(dir string, maxAge time.Duration, maxCount int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	type fileItem struct {
		path    string
		size    int64
		modTime time.Time
	}

	var files []fileItem
	now := time.Now()

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".apk") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		fp := filepath.Join(dir, entry.Name())

		if maxAge > 0 && now.Sub(info.ModTime()) > maxAge {
			_ = os.Remove(fp)
			continue
		}

		files = append(files, fileItem{
			path:    fp,
			size:    info.Size(),
			modTime: info.ModTime(),
		})
	}

	if maxCount > 0 && len(files) > maxCount {
		sort.Slice(files, func(i, j int) bool {
			return files[i].modTime.Before(files[j].modTime)
		})
		toRemove := len(files) - maxCount
		for i := 0; i < toRemove; i++ {
			_ = os.Remove(files[i].path)
		}
	}
}

// ensure guarantees the catalog is loaded into memory if not already present.
func (c *Catalog) ensure(ctx context.Context) error {
	c.mu.Lock()
	if len(c.apps) > 0 {
		c.mu.Unlock()
		return nil
	}

	// If another goroutine is already fetching the catalog, wait for it.
	if c.loading != nil {
		wait := c.loading
		c.mu.Unlock()

		select {
		case <-wait:
			return c.ensure(ctx)
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	wait := make(chan struct{})
	c.loading = wait
	c.mu.Unlock()

	apps, categories, icons, err := c.fetch(ctx)

	c.mu.Lock()
	if err == nil {
		c.apps = apps
		c.categories = categories
		c.icons = icons
		c.loaded = time.Now()
	}
	c.loading = nil
	close(wait)
	hasStaleData := len(c.apps) > 0
	c.mu.Unlock()

	// If refresh failed but we already have cached data, continue using stale data gracefully.
	if err != nil && hasStaleData {
		return nil
	}
	return err
}

type metadataDoc struct {
	Name        map[string]string `json:"name"`
	Summary     map[string]string `json:"summary"`
	Description map[string]string `json:"description"`
	Icon        map[string]struct {
		Name string `json:"name"`
	} `json:"icon"`
	Categories   []string `json:"categories"`
	LastUpdated  int64    `json:"lastUpdated"`
	License      string   `json:"license"`
	SourceCode   string   `json:"sourceCode"`
	IssueTracker string   `json:"issueTracker"`
	WebSite      string   `json:"webSite"`
}

type versionFileDoc struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type versionManifestDoc struct {
	VersionName string   `json:"versionName"`
	VersionCode int64    `json:"versionCode"`
	NativeCode  []string `json:"nativecode"`
	UsesSDK     struct {
		MinSDK int `json:"minSdkVersion"`
	} `json:"usesSdk"`
}

type versionEntryDoc struct {
	Added    int64              `json:"added"`
	File     versionFileDoc     `json:"file"`
	Manifest versionManifestDoc `json:"manifest"`
}

type packageEntryDoc struct {
	Metadata metadataDoc                `json:"metadata"`
	Versions map[string]versionEntryDoc `json:"versions"`
}

type indexDoc struct {
	Packages map[string]packageEntryDoc `json:"packages"`
}

type catalogMeta struct {
	ETag         string    `json:"etag"`
	LastModified string    `json:"lastModified"`
	FetchedAt    time.Time `json:"fetchedAt"`
}

func (c *Catalog) cacheFileName() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cacheKey != "" {
		return c.cacheKey + "-index-v2.json"
	}
	if strings.Contains(c.indexURL, "izzy") {
		return "izzy-index-v2.json"
	}
	return "fdroid-index-v2.json"
}

func (c *Catalog) metaFileName() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cacheKey != "" {
		return c.cacheKey + "-meta.json"
	}
	if strings.Contains(c.indexURL, "izzy") {
		return "izzy-meta.json"
	}
	return "fdroid-meta.json"
}

func (c *Catalog) setStatusError(msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status.State = "error"
	c.status.Error = msg
}

func (c *Catalog) loadFromDiskCache() error {
	c.mu.Lock()
	cacheDir := c.cacheDir
	repoBaseURL := c.repoBaseURL
	c.mu.Unlock()

	diskFile := filepath.Join(cacheDir, "catalog", c.cacheFileName())
	info, err := os.Stat(diskFile)
	if err != nil || info.Size() == 0 {
		return errors.New("no disk cache available")
	}

	data, err := os.ReadFile(diskFile)
	if err != nil {
		return err
	}

	apps, categories, icons, err := parseIndex(data, repoBaseURL)
	if err != nil {
		return err
	}

	metaFile := filepath.Join(cacheDir, "catalog", c.metaFileName())
	var meta catalogMeta
	if mData, mErr := os.ReadFile(metaFile); mErr == nil {
		_ = json.Unmarshal(mData, &meta)
	}

	c.mu.Lock()
	c.apps = apps
	c.categories = categories
	c.icons = icons
	c.loaded = info.ModTime()
	if !meta.FetchedAt.IsZero() {
		c.loaded = meta.FetchedAt
	}
	c.etag = meta.ETag
	c.lastMod = meta.LastModified
	c.status.State = "ready"
	c.status.AppCount = len(apps)
	c.status.DownloadedBytes = info.Size()
	c.status.TotalBytes = info.Size()
	c.status.Percent = 100
	c.status.Error = ""
	c.mu.Unlock()

	return nil
}

type progressReader struct {
	r           io.Reader
	transferred int64
	lastUpdate  time.Time
	onProgress  func(int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	if n > 0 {
		pr.transferred += int64(n)
		if time.Since(pr.lastUpdate) > 100*time.Millisecond || err != nil {
			pr.lastUpdate = time.Now()
			if pr.onProgress != nil {
				pr.onProgress(pr.transferred)
			}
		}
	}
	return n, err
}

func (c *Catalog) fetch(ctx context.Context) ([]App, []string, map[string]bool, error) {
	// Generous timeout (5 minutes) so large indexes on slow connections do not get aborted
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	c.mu.Lock()
	indexURL := c.indexURL
	repoBaseURL := c.repoBaseURL
	cacheDir := c.cacheDir
	etag := c.etag
	lastMod := c.lastMod
	c.status.State = "loading"
	c.status.Error = ""
	c.status.DownloadedBytes = 0
	c.status.Percent = 0
	c.mu.Unlock()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		c.setStatusError(err.Error())
		return nil, nil, nil, err
	}
	request.Header.Set("User-Agent", "Elene/1.0 (Android APK Installer)")
	request.Header.Set("Accept-Encoding", "gzip")
	if etag != "" {
		request.Header.Set("If-None-Match", etag)
	}
	if lastMod != "" {
		request.Header.Set("If-Modified-Since", lastMod)
	}

	response, err := c.client.Do(request)
	if err != nil {
		c.setStatusError(fmt.Sprintf("Failed to reach repository: %v", err))
		return nil, nil, nil, fmt.Errorf("load F-Droid catalog: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotModified {
		c.mu.Lock()
		c.loaded = time.Now()
		c.status.State = "ready"
		c.status.Error = ""
		apps := slices.Clone(c.apps)
		cats := slices.Clone(c.categories)
		icons := make(map[string]bool, len(c.icons))
		for k, v := range c.icons {
			icons[k] = v
		}
		c.mu.Unlock()
		return apps, cats, icons, nil
	}

	if response.StatusCode != http.StatusOK {
		err := fmt.Errorf("F-Droid catalog returned HTTP %d", response.StatusCode)
		c.setStatusError(err.Error())
		return nil, nil, nil, err
	}

	catalogDir := filepath.Join(cacheDir, "catalog")
	if err := os.MkdirAll(catalogDir, 0700); err != nil {
		c.setStatusError(err.Error())
		return nil, nil, nil, err
	}

	totalExpected := response.ContentLength
	c.mu.Lock()
	c.status.TotalBytes = totalExpected
	c.mu.Unlock()

	tempFile := filepath.Join(catalogDir, fmt.Sprintf("%s-%d.part", c.cacheFileName(), time.Now().UnixNano()))
	outFile, err := os.OpenFile(tempFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		c.setStatusError(err.Error())
		return nil, nil, nil, err
	}

	reader := &progressReader{
		r: response.Body,
		onProgress: func(transferred int64) {
			c.mu.Lock()
			c.status.DownloadedBytes = transferred
			if totalExpected > 0 {
				c.status.Percent = int(float64(transferred) / float64(totalExpected) * 100)
				if c.status.Percent > 100 {
					c.status.Percent = 100
				}
			}
			c.mu.Unlock()
		},
	}

	var streamReader io.Reader = reader
	if response.Header.Get("Content-Encoding") == "gzip" {
		gz, gzErr := gzip.NewReader(reader)
		if gzErr != nil {
			_ = outFile.Close()
			_ = os.Remove(tempFile)
			c.setStatusError(fmt.Sprintf("Failed to decompress catalog stream: %v", gzErr))
			return nil, nil, nil, gzErr
		}
		defer gz.Close()
		streamReader = gz
	}

	_, copyErr := io.Copy(outFile, io.LimitReader(streamReader, maxIndexBytes+1))
	_ = outFile.Close()

	if copyErr != nil {
		_ = os.Remove(tempFile)
		c.setStatusError(fmt.Sprintf("Download interrupted: %v", copyErr))
		return nil, nil, nil, copyErr
	}

	data, err := os.ReadFile(tempFile)
	if err != nil {
		_ = os.Remove(tempFile)
		c.setStatusError(err.Error())
		return nil, nil, nil, err
	}

	apps, categories, icons, err := parseIndex(data, repoBaseURL)
	if err != nil {
		_ = os.Remove(tempFile)
		c.setStatusError(fmt.Sprintf("Failed to parse catalog: %v", err))
		return nil, nil, nil, err
	}

	diskFile := filepath.Join(catalogDir, c.cacheFileName())
	if err := os.Rename(tempFile, diskFile); err != nil {
		_ = os.Remove(tempFile)
		c.setStatusError(err.Error())
		return nil, nil, nil, err
	}

	newETag := response.Header.Get("ETag")
	newLastMod := response.Header.Get("Last-Modified")
	meta := catalogMeta{
		ETag:         newETag,
		LastModified: newLastMod,
		FetchedAt:    time.Now(),
	}
	if metaBytes, mErr := json.Marshal(meta); mErr == nil {
		_ = os.WriteFile(filepath.Join(catalogDir, c.metaFileName()), metaBytes, 0600)
	}

	c.mu.Lock()
	c.etag = newETag
	c.lastMod = newLastMod
	c.status.State = "ready"
	c.status.DownloadedBytes = int64(len(data))
	c.status.TotalBytes = int64(len(data))
	c.status.Percent = 100
	c.status.AppCount = len(apps)
	c.status.Error = ""
	c.mu.Unlock()

	return apps, categories, icons, nil
}

func parseIndex(body []byte, repoBaseURL string) ([]App, []string, map[string]bool, error) {
	if len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b {
		gz, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("decompress F-Droid catalog: %w", err)
		}
		defer gz.Close()
		decompressed, err := io.ReadAll(gz)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read decompressed F-Droid catalog: %w", err)
		}
		body = decompressed
	}

	var doc indexDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, nil, nil, fmt.Errorf("parse F-Droid catalog: %w", err)
	}
	if len(doc.Packages) == 0 {
		return nil, nil, nil, errors.New("F-Droid catalog contains no packages")
	}

	apps := make([]App, 0, len(doc.Packages))
	categorySet := make(map[string]bool)
	icons := make(map[string]bool)

	for id, entry := range doc.Packages {
		if !packageIDPattern.MatchString(id) {
			continue
		}

		m := entry.Metadata
		name := selectLocalized(m.Name)
		if name == "" {
			name = id
		}
		summary := selectLocalized(m.Summary)
		icon := selectLocalizedIcon(m.Icon)
		if icon != "" {
			icons[icon] = true
		}

		categories := make([]string, 0, len(m.Categories))
		for _, cat := range m.Categories {
			cat = strings.TrimSpace(cat)
			if cat != "" && len(cat) <= 60 {
				categories = append(categories, cat)
				categorySet[cat] = true
			}
		}

		// Extract package releases / versions
		releases := make([]Release, 0, len(entry.Versions))
		for _, v := range entry.Versions {
			fileName := cleanAPKPath(v.File.Name)
			if fileName == "" || v.File.SHA256 == "" {
				continue
			}

			releases = append(releases, Release{
				VersionName: v.Manifest.VersionName,
				VersionCode: v.Manifest.VersionCode,
				MinSDK:      v.Manifest.UsesSDK.MinSDK,
				NativeCode:  v.Manifest.NativeCode,
				APKPath:     fileName,
				SHA256:      v.File.SHA256,
				Size:        v.File.Size,
				Added:       v.Added,
			})
		}

		// Sort releases newest versionCode first
		slices.SortFunc(releases, func(a, b Release) int {
			if a.VersionCode != b.VersionCode {
				if a.VersionCode > b.VersionCode {
					return -1
				}
				return 1
			}
			if a.Added > b.Added {
				return -1
			}
			return 1
		})

		var latestVersionName string
		var latestVersionCode int64
		var latestSize int64
		if len(releases) > 0 {
			latestVersionName = releases[0].VersionName
			latestVersionCode = releases[0].VersionCode
			latestSize = releases[0].Size
		}

		description := selectLocalized(m.Description)
		license := strings.TrimSpace(m.License)
		sourceCode := strings.TrimSpace(m.SourceCode)
		issueTracker := strings.TrimSpace(m.IssueTracker)
		webSite := strings.TrimSpace(m.WebSite)

		listingURL := "https://f-droid.org/packages/" + url.PathEscape(id) + "/"
		if strings.Contains(repoBaseURL, "izzysoft.de") {
			listingURL = "https://apt.izzysoft.de/fdroid/index/apk/" + url.PathEscape(id)
		}

		apps = append(apps, App{
			ID:           id,
			Name:         name,
			Summary:      summary,
			Description:  description,
			Icon:         icon,
			Categories:   categories,
			Updated:      m.LastUpdated,
			URL:          listingURL,
			License:      license,
			SourceCode:   sourceCode,
			IssueTracker: issueTracker,
			WebSite:      webSite,
			VersionName:  latestVersionName,
			VersionCode:  latestVersionCode,
			Size:         latestSize,
			Releases:     releases,
		})
	}

	// Sort apps by updated date descending, then name
	slices.SortFunc(apps, func(a, b App) int {
		if a.Updated != b.Updated {
			if a.Updated > b.Updated {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})

	categories := make([]string, 0, len(categorySet))
	for cat := range categorySet {
		categories = append(categories, cat)
	}
	slices.Sort(categories)

	return apps, categories, icons, nil
}

func selectLocalized(values map[string]string) string {
	for _, key := range []string{"en-US", "en-GB", "en"} {
		if v := strings.TrimSpace(values[key]); v != "" {
			return v
		}
	}

	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	for _, k := range keys {
		if strings.HasPrefix(strings.ToLower(k), "en") {
			if v := strings.TrimSpace(values[k]); v != "" {
				return v
			}
		}
	}

	for _, k := range keys {
		if v := strings.TrimSpace(values[k]); v != "" {
			return v
		}
	}
	return ""
}

func selectLocalizedIcon(icons map[string]struct {
	Name string `json:"name"`
}) string {
	for _, key := range []string{"en-US", "en-GB", "en"} {
		if icon := cleanIcon(icons[key].Name); icon != "" {
			return icon
		}
	}

	keys := make([]string, 0, len(icons))
	for k := range icons {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	for _, k := range keys {
		if strings.HasPrefix(strings.ToLower(k), "en") {
			if icon := cleanIcon(icons[k].Name); icon != "" {
				return icon
			}
		}
	}

	for _, k := range keys {
		if icon := cleanIcon(icons[k].Name); icon != "" {
			return icon
		}
	}
	return ""
}

func cleanIcon(value string) string {
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.Contains(value, "\\") || strings.Contains(value, "..") || len(value) > 512 {
		return ""
	}
	cleaned := path.Clean(value)
	lower := strings.ToLower(cleaned)
	if !strings.HasSuffix(lower, ".png") && !strings.HasSuffix(lower, ".webp") {
		return ""
	}
	return cleaned
}

func cleanAPKPath(value string) string {
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.Contains(value, "\\") || strings.Contains(value, "..") || len(value) > 512 {
		return ""
	}
	cleaned := path.Clean(value)
	if !strings.HasSuffix(strings.ToLower(cleaned), ".apk") {
		return ""
	}
	return cleaned
}

// Icon retrieves an application icon from the F-Droid repository, validating content-type.
func (c *Catalog) Icon(ctx context.Context, name string) ([]byte, string, error) {
	cleanName := path.Clean("/" + strings.TrimPrefix(name, "/"))
	if !strings.HasPrefix(cleanName, "/") || strings.Contains(cleanName, "..") {
		return nil, "", errors.New("invalid icon path")
	}

	c.mu.Lock()
	allowed := c.icons[cleanName]
	c.mu.Unlock()

	if !allowed {
		return nil, "", errors.New("icon path is not recognized or not allowed")
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	parsedBase, err := url.Parse(c.repoBaseURL)
	if err != nil {
		return nil, "", err
	}

	switch parsedBase.Hostname() {
	case "f-droid.org", "apt.izzysoft.de", "127.0.0.1", "localhost":
	default:
		return nil, "", errors.New("untrusted repository host for icon retrieval")
	}

	targetURL := &url.URL{
		Scheme: parsedBase.Scheme,
		Host:   parsedBase.Host,
		Path:   path.Join(parsedBase.Path, strings.TrimPrefix(cleanName, "/")),
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL.String(), nil)
	if err != nil {
		return nil, "", err
	}
	request.Header.Set("User-Agent", "Elene/1.0 (Android APK Installer)")

	response, err := c.client.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("icon download returned HTTP %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, (512<<10)+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) > (512 << 10) {
		return nil, "", errors.New("icon image is too large (maximum 512 KiB)")
	}

	contentType := http.DetectContentType(body)
	if contentType != "image/png" && contentType != "image/webp" {
		return nil, "", errors.New("unsupported icon image format")
	}

	return body, contentType, nil
}
