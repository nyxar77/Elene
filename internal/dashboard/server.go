// Package dashboard serves Elene's embedded, local-only interface.
package dashboard

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nyxar77/elene/internal/adb"
	"github.com/nyxar77/elene/internal/fdroid"
)

//go:embed assets/*
var assets embed.FS

const maxAPK = 512 << 20

// Backend defines the ADB operations required by the dashboard.
type Backend interface {
	Devices(context.Context) ([]adb.Device, error)
	InspectDevice(context.Context, string) (adb.Device, error)
	Install(context.Context, string, string) error
	Packages(context.Context, string) ([]adb.Package, error)
}

// Job represents an active, queued, or completed installation task.
type Job struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	PackageID       string    `json:"packageId,omitempty"`
	Serial          string    `json:"serial"`
	State           string    `json:"state"` // "queued", "downloading", "installing", "succeeded", "failed", "cancelled"
	Progress        int       `json:"progress"`
	DownloadedBytes int64     `json:"downloadedBytes,omitempty"`
	TotalBytes      int64     `json:"totalBytes,omitempty"`
	Source          string    `json:"source"`
	Error           string    `json:"error,omitempty"`
	Created         time.Time `json:"created"`
	DownloadOnly    bool      `json:"downloadOnly,omitempty"`
}

// LogEntry records a timestamped operational event for user inspection.
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"` // "INFO", "WARN", "ERROR", "SUCCESS"
	Source    string    `json:"source"`
	Message   string    `json:"message"`
}

// CachedAPK describes an APK stored on the local computer.
type CachedAPK struct {
	SHA256      string `json:"sha256"`
	PackageID   string `json:"packageId"`
	Name        string `json:"name"`
	VersionName string `json:"versionName"`
	Size        int64  `json:"size"`
	Downloaded  string `json:"downloaded"`
}

// Server provides the loopback HTTP API and server-rendered views.
type Server struct {
	catalog     *fdroid.Catalog
	izzyCatalog *fdroid.Catalog
	cancel      context.CancelFunc
	workers     sync.WaitGroup
	preferences Preferences
	configPath  string
	cacheDir    string

	backend    Backend
	ctx        context.Context
	token      string
	mu         sync.Mutex
	jobs       []Job
	jobCancels map[string]context.CancelFunc
	busy       bool
	logs       []LogEntry
}

// New creates a new dashboard server scoped to the daemon lifetime.
func New(ctx context.Context, backend Backend, configPath string) (*Server, error) {
	preferences, err := loadPreferences(configPath)
	if err != nil {
		return nil, err
	}

	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, fmt.Errorf("generate session token: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)

	cacheDir := ""
	if userCache, err := os.UserCacheDir(); err == nil {
		cacheDir = filepath.Join(userCache, "elene")
	} else {
		cacheDir = filepath.Join(os.TempDir(), "elene-cache")
	}

	apkDir := filepath.Join(cacheDir, "apks")
	_ = os.MkdirAll(apkDir, 0700)
	go fdroid.PruneAPKs(apkDir, time.Duration(preferences.CacheRetentionDays)*24*time.Hour, int(preferences.MaxCachedAPKs))

	return &Server{
		catalog:     fdroid.New(),
		izzyCatalog: fdroid.NewIzzy(),
		cancel:      cancel,
		backend:     backend,
		ctx:         ctx,
		token:       hex.EncodeToString(token),
		jobs:        []Job{},
		jobCancels:  make(map[string]context.CancelFunc),
		preferences: preferences,
		configPath:  configPath,
		cacheDir:    cacheDir,
		logs:        []LogEntry{},
	}, nil
}

// StartBackgroundSync starts background warm-up of the catalog without blocking server startup.
func (s *Server) StartBackgroundSync() {
	go func() {
		bgCtx, bgCancel := context.WithTimeout(s.ctx, 5*time.Minute)
		defer bgCancel()
		cat := s.catalogForSource("fdroid")
		_ = cat.Refresh(bgCtx, false)
	}()
}

// SetCacheDir overrides the cache directory root for APKs and catalogs (used in testing).
func (s *Server) SetCacheDir(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cacheDir = dir
	_ = os.MkdirAll(filepath.Join(dir, "apks"), 0700)
	s.catalog.SetCacheDir(dir)
	s.izzyCatalog.SetCacheDir(dir)
}

// SetCatalog allows testing harnesses or configurations to swap the F-Droid catalog implementation safely.
func (s *Server) SetCatalog(c *fdroid.Catalog) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.catalog = c
}

func (s *Server) log(level, source, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := LogEntry{
		Timestamp: time.Now().UTC(),
		Level:     level,
		Source:    source,
		Message:   message,
	}
	if len(s.logs) >= 200 {
		s.logs = s.logs[1:]
	}
	s.logs = append(s.logs, entry)
}

func (s *Server) catalogForSource(source string) *fdroid.Catalog {
	s.mu.Lock()
	defer s.mu.Unlock()
	if source == "aurora" || source == "izzy" {
		return s.izzyCatalog
	}
	return s.catalog
}

func isAllowedHost(reqHost, configuredHost string) bool {
	if reqHost == configuredHost {
		return true
	}
	_, port, err := net.SplitHostPort(configuredHost)
	if err != nil {
		port = "18420"
	}
	allowed := []string{
		configuredHost,
		"127.0.0.1:" + port,
		"localhost:" + port,
		"[::1]:" + port,
	}
	return slices.Contains(allowed, reqHost)
}

func isAllowedOrigin(origin, configuredHost string) bool {
	if origin == "" {
		return true
	}
	_, port, err := net.SplitHostPort(configuredHost)
	if err != nil {
		port = "18420"
	}
	allowed := []string{
		"http://" + configuredHost,
		"http://127.0.0.1:" + port,
		"http://localhost:" + port,
		"http://[::1]:" + port,
	}
	return slices.Contains(allowed, origin)
}

// Handler configures HTTP routes, security middleware, and CSRF protection.
func (s *Server) Handler(host string) http.Handler {
	mux := http.NewServeMux()

	// Authentication session token
	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, map[string]string{"token": s.token})
	})

	// Device and package listings
	mux.HandleFunc("GET /api/devices", s.devices)
	mux.HandleFunc("GET /api/packages", s.packages)

	// Catalog metadata and icons
	mux.HandleFunc("GET /api/catalog", s.catalogPage)
	mux.HandleFunc("GET /api/catalog/status", s.catalogStatus)
	mux.HandleFunc("POST /api/catalog/refresh", s.catalogRefresh)
	mux.HandleFunc("GET /api/catalog/app", s.catalogApp)
	mux.HandleFunc("POST /api/catalog/resolve", s.catalogResolve)
	mux.HandleFunc("GET /api/catalog/icon", s.catalogIcon)

	// Downloaded / cached APK management
	mux.HandleFunc("GET /api/cached-apks", s.listCachedAPKs)
	mux.HandleFunc("POST /api/cached-apks/install", s.installCachedAPK)
	mux.HandleFunc("POST /cached-apks/install", func(w http.ResponseWriter, r *http.Request) {
		s.installCachedAPKForm(w, r)
	})
	mux.HandleFunc("POST /cached-apks/delete", func(w http.ResponseWriter, r *http.Request) {
		s.deleteCachedAPKForm(w, r)
	})
	mux.HandleFunc("DELETE /api/cached-apks", s.deleteCachedAPK)

	// User preferences
	mux.HandleFunc("GET /api/preferences", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		respond(w, http.StatusOK, s.preferences)
	})
	mux.HandleFunc("PUT /api/preferences", s.updatePreferences)
	mux.HandleFunc("POST /preferences", s.preferencesForm)

	// Activity and operational logs
	mux.HandleFunc("GET /api/logs", s.getLogs)
	mux.HandleFunc("POST /api/logs/clear", s.clearLogs)
	mux.HandleFunc("POST /logs/clear", func(w http.ResponseWriter, r *http.Request) {
		s.clearLogsForm(w, r)
	})

	// Job tracking and cancellation
	mux.HandleFunc("GET /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		respond(w, http.StatusOK, s.jobs)
	})
	mux.HandleFunc("POST /api/jobs/cancel", s.cancelJob)

	// Installation handlers (local file upload)
	mux.HandleFunc("POST /api/install", s.install)
	mux.HandleFunc("POST /install", s.formInstall)

	// Installation handlers (remote sources like F-Droid)
	mux.HandleFunc("POST /api/install-remote", func(w http.ResponseWriter, r *http.Request) {
		s.installRemote(w, r, false)
	})
	mux.HandleFunc("POST /install-remote", func(w http.ResponseWriter, r *http.Request) {
		s.installRemote(w, r, true)
	})

	// Favicon and icon handlers
	serveIcon := func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(assets, "assets/logo.svg")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(data)
	}
	mux.HandleFunc("GET /favicon.ico", serveIcon)
	mux.HandleFunc("GET /logo.svg", serveIcon)

	// Server-rendered pages (No-JS / direct URL access)
	for _, route := range []string{"/installed", "/updates", "/activity", "/settings"} {
		mux.HandleFunc("GET "+route, s.page)
	}
	mux.HandleFunc("GET /{$}", s.page)

	// Static embedded assets
	files, _ := fs.Sub(assets, "assets")
	mux.Handle("GET /", http.FileServer(http.FS(files)))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; frame-src https://github.com/sponsors/nyxar77/; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")

		// Reject DNS rebinding and cross-origin access
		if !isAllowedHost(r.Host, host) || !isAllowedOrigin(r.Header.Get("Origin"), host) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			fail(w, http.StatusForbidden, "This dashboard is only available from its local address.")
			return
		}

		// Require CSRF token for mutating JSON API endpoints (read-only resolve is exempted)
		if strings.HasPrefix(r.URL.Path, "/api/") &&
			r.Method != http.MethodGet &&
			r.Method != http.MethodHead &&
			r.URL.Path != "/api/catalog/resolve" &&
			r.Header.Get("X-Elene-Token") != s.token {
			fail(w, http.StatusForbidden, "Refresh the dashboard before performing this operation.")
			return
		}

		s.mu.Lock()
		basicInterface := s.preferences.Interface == "basic"
		s.mu.Unlock()

		if basicInterface {
			w.Header().Set("Content-Security-Policy",
				"default-src 'self'; script-src 'none'; style-src 'self'; frame-src https://github.com/sponsors/nyxar77/; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		}

		mux.ServeHTTP(w, r)
	})
}

func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	devices, err := s.backend.Devices(ctx)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if devices == nil {
		devices = []adb.Device{}
	}
	respond(w, http.StatusOK, devices)
}

func (s *Server) install(w http.ResponseWriter, r *http.Request) {
	s.installAPK(w, r, false)
}

func (s *Server) installAPK(w http.ResponseWriter, r *http.Request, form bool) {
	reject := func(status int, message string) {
		if form {
			s.renderPage(w, r, status, message)
		} else {
			fail(w, status, message)
		}
	}

	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		reject(http.StatusConflict, "An installation is already in progress.")
		return
	}
	s.busy = true
	s.mu.Unlock()

	handedOff := false
	defer func() {
		if !handedOff {
			s.mu.Lock()
			s.busy = false
			s.mu.Unlock()
		}
	}()

	serial := strings.TrimSpace(r.URL.Query().Get("serial"))
	name := filepath.Base(r.URL.Query().Get("name"))

	if serial == "" || len(serial) > 256 || len(name) > 255 || strings.ToLower(filepath.Ext(name)) != ".apk" {
		reject(http.StatusBadRequest, "Select a device and a single .apk file.")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAPK)
	dir, err := os.MkdirTemp("", "elene-install-")
	if err != nil {
		reject(http.StatusInternalServerError, "Could not create temporary storage.")
		return
	}
	defer func() {
		if !handedOff {
			_ = os.RemoveAll(dir)
		}
	}()

	apkPath := filepath.Join(dir, "package.apk")
	file, err := os.OpenFile(apkPath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		reject(http.StatusInternalServerError, "Could not store the APK.")
		return
	}

	n, copyErr := io.Copy(file, r.Body)
	closeErr := file.Close()
	if copyErr != nil {
		var sizeErr *http.MaxBytesError
		if errors.As(copyErr, &sizeErr) {
			reject(http.StatusRequestEntityTooLarge, "APKs must be smaller than 512 MiB.")
		} else {
			reject(http.StatusBadRequest, "The upload was interrupted.")
		}
		return
	}

	if closeErr != nil || n == 0 {
		reject(http.StatusBadRequest, "The uploaded APK is empty or could not be saved.")
		return
	}

	job := Job{
		ID:       time.Now().UTC().Format("20060102T150405.000000000"),
		Name:     name,
		Serial:   serial,
		State:    "installing",
		Progress: 100,
		Source:   "local",
		Created:  time.Now().UTC(),
	}

	s.mu.Lock()
	if len(s.jobs) >= 20 {
		s.jobs = s.jobs[:19]
	}
	s.jobs = append([]Job{job}, s.jobs...)
	s.mu.Unlock()

	handedOff = true
	s.workers.Add(1)

	go func() {
		defer s.workers.Done()
		defer func() {
			_ = os.RemoveAll(dir)
		}()

		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
		s.mu.Lock()
		s.jobCancels[job.ID] = cancel
		s.mu.Unlock()
		defer func() {
			cancel()
			s.mu.Lock()
			delete(s.jobCancels, job.ID)
			s.mu.Unlock()
			s.startNextQueuedJob()
		}()

		err := s.backend.Install(ctx, serial, apkPath)

		s.mu.Lock()
		defer s.mu.Unlock()
		for i := range s.jobs {
			if s.jobs[i].ID == job.ID {
				if s.jobs[i].State == "cancelled" {
					break
				}
				if err != nil {
					if errors.Is(err, context.Canceled) && s.ctx.Err() == nil {
						s.jobs[i].State = "cancelled"
						s.jobs[i].Error = "Cancelled by user."
					} else {
						s.jobs[i].State = "failed"
						s.jobs[i].Error = err.Error()
					}
				} else {
					s.jobs[i].State = "succeeded"
				}
				break
			}
		}
	}()

	if form {
		http.Redirect(w, r, "/activity", http.StatusSeeOther)
	} else {
		respond(w, http.StatusAccepted, job)
	}
}

// installRemote handles downloading an APK from a remote repository (such as F-Droid or IzzyOnDroid)
// and installing it to the connected Android device. If an installation is already running, the task is queued.
func (s *Server) installRemote(w http.ResponseWriter, r *http.Request, form bool) {
	reject := func(status int, message string) {
		if form {
			s.renderPage(w, r, status, message)
		} else {
			fail(w, status, message)
		}
	}

	var serial, source, packageID string
	var downloadOnly bool

	if form {
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if err := r.ParseForm(); err != nil || r.PostForm.Get("token") != s.token {
			reject(http.StatusForbidden, "This form expired. Please try again.")
			return
		}
		serial = strings.TrimSpace(r.PostForm.Get("serial"))
		source = strings.TrimSpace(r.PostForm.Get("source"))
		packageID = strings.TrimSpace(r.PostForm.Get("package"))
		downloadOnly = r.PostForm.Get("download_only") == "true" || r.PostForm.Get("download_only") == "1"
	} else {
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			r.Body = http.MaxBytesReader(w, r.Body, 8192)
			var payload struct {
				Serial       string `json:"serial"`
				Source       string `json:"source"`
				Package      string `json:"package"`
				DownloadOnly bool   `json:"downloadOnly"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
				serial = payload.Serial
				source = payload.Source
				packageID = payload.Package
				downloadOnly = payload.DownloadOnly
			}
		}
		if serial == "" {
			serial = strings.TrimSpace(r.URL.Query().Get("serial"))
		}
		if source == "" {
			source = strings.TrimSpace(r.URL.Query().Get("source"))
		}
		if packageID == "" {
			packageID = strings.TrimSpace(r.URL.Query().Get("package"))
		}
		if !downloadOnly && (r.URL.Query().Get("download_only") == "true" || r.URL.Query().Get("download_only") == "1") {
			downloadOnly = true
		}
	}

	s.mu.Lock()
	prefStrategy := s.preferences.InstallStrategy
	s.mu.Unlock()

	if prefStrategy == "download_only" {
		downloadOnly = true
	} else if prefStrategy == "auto" && serial == "" {
		downloadOnly = true
	}

	if !downloadOnly && (serial == "" || len(serial) > 256) {
		reject(http.StatusBadRequest, "Please choose a connected device.")
		return
	}
	if source != "fdroid" && source != "aurora" && source != "izzy" {
		reject(http.StatusBadRequest, "Unsupported application source: "+source)
		return
	}
	if packageID == "" || len(packageID) > 256 {
		reject(http.StatusBadRequest, "Please choose an application to install.")
		return
	}

	s.mu.Lock()
	// Avoid duplicating a package that is already queued or actively installing on the same device
	for _, existing := range s.jobs {
		if existing.Serial == serial && existing.PackageID == packageID &&
			(existing.State == "queued" || existing.State == "downloading" || existing.State == "installing") {
			s.mu.Unlock()
			if form {
				http.Redirect(w, r, "/activity", http.StatusSeeOther)
			} else {
				respond(w, http.StatusAccepted, existing)
			}
			return
		}
	}

	initialState := "downloading"
	shouldStartNow := true
	if s.busy {
		initialState = "queued"
		shouldStartNow = false
	} else {
		s.busy = true
	}

	job := Job{
		ID:           fmt.Sprintf("%s-%d", time.Now().UTC().Format("20060102T150405.000000000"), len(s.jobs)),
		Name:         packageID,
		PackageID:    packageID,
		Serial:       serial,
		State:        initialState,
		Progress:     0,
		Source:       source,
		Created:      time.Now().UTC(),
		DownloadOnly: downloadOnly,
	}

	if len(s.jobs) >= 30 {
		s.jobs = s.jobs[:29]
	}
	s.jobs = append([]Job{job}, s.jobs...)
	s.mu.Unlock()

	if shouldStartNow {
		s.workers.Add(1)
		go s.runRemoteJob(job)
	}

	if form {
		http.Redirect(w, r, "/activity", http.StatusSeeOther)
	} else {
		respond(w, http.StatusAccepted, job)
	}
}

func (s *Server) startNextQueuedJob() {
	s.mu.Lock()
	// Find the oldest queued job (FIFO order)
	var nextJob *Job
	for i := len(s.jobs) - 1; i >= 0; i-- {
		if s.jobs[i].State == "queued" {
			s.jobs[i].State = "downloading"
			cp := s.jobs[i]
			nextJob = &cp
			break
		}
	}

	if nextJob == nil {
		s.busy = false
		s.mu.Unlock()
		return
	}
	s.busy = true
	s.mu.Unlock()

	s.workers.Add(1)
	go s.runRemoteJob(*nextJob)
}

func (s *Server) runRemoteJob(job Job) {
	defer s.workers.Done()

	workerCtx, cancel := context.WithTimeout(s.ctx, 10*time.Minute)
	s.mu.Lock()
	// Check if job was cancelled before it even started
	for i := range s.jobs {
		if s.jobs[i].ID == job.ID && s.jobs[i].State == "cancelled" {
			s.mu.Unlock()
			cancel()
			s.startNextQueuedJob()
			return
		}
	}
	s.jobCancels[job.ID] = cancel
	s.mu.Unlock()

	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.jobCancels, job.ID)
		s.mu.Unlock()
		s.startNextQueuedJob()
	}()

	setJobState := func(state, errMsg string, progress int, downloaded, total int64) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i := range s.jobs {
			if s.jobs[i].ID == job.ID {
				if s.jobs[i].State == "cancelled" {
					break
				}
				s.jobs[i].State = state
				s.jobs[i].Error = errMsg
				if progress >= 0 {
					s.jobs[i].Progress = progress
				}
				if downloaded > 0 {
					s.jobs[i].DownloadedBytes = downloaded
				}
				if total > 0 {
					s.jobs[i].TotalBytes = total
				}
				break
			}
		}
	}

	targetCatalog := s.catalogForSource(job.Source)

	// 1. Fetch app entry from catalog
	app, err := targetCatalog.FindApp(workerCtx, job.PackageID)
	if err != nil {
		// Fallback to the other catalog if not found in primary
		if job.Source == "fdroid" {
			app, err = s.izzyCatalog.FindApp(workerCtx, job.PackageID)
			if err == nil {
				targetCatalog = s.izzyCatalog
			}
		}
	}
	if err != nil {
		if errors.Is(err, context.Canceled) && s.ctx.Err() == nil {
			setJobState("cancelled", "Cancelled by user.", -1, 0, 0)
			return
		}
		setJobState("failed", "Could not locate app in catalog: "+err.Error(), -1, 0, 0)
		return
	}

	// 2. Inspect device for ABI architecture and Android SDK if installing to device
	var deviceABIs []string
	var deviceSDK int
	if job.Serial != "" {
		device, _ := s.backend.InspectDevice(workerCtx, job.Serial)
		deviceABIs = device.ABIs
		deviceSDK = device.SDK
	}

	// 3. Find compatible release
	release, err := app.CompatibleRelease(deviceABIs, deviceSDK)
	if err != nil {
		s.log("ERROR", "Catalog", fmt.Sprintf("No compatible release for %s: %v", app.Name, err))
		setJobState("failed", err.Error(), -1, 0, 0)
		return
	}

	// 4. Download / retrieve cached APK with live progress reporting
	s.log("INFO", "Download", fmt.Sprintf("Starting download of %s (%s)...", app.Name, release.VersionName))
	setJobState("downloading", "", 0, 0, release.Size)
	onProgress := func(downloaded, total int64) {
		pct := 0
		if total > 0 {
			pct = int((downloaded * 100) / total)
			if pct > 100 {
				pct = 100
			}
		}
		setJobState("downloading", "", pct, downloaded, total)
	}

	apkPath, err := targetCatalog.GetOrDownloadAPK(workerCtx, release.APKPath, release.SHA256, onProgress)
	if err != nil {
		if errors.Is(err, context.Canceled) && s.ctx.Err() == nil {
			s.log("WARN", "Download", fmt.Sprintf("Download of %s cancelled", app.Name))
			setJobState("cancelled", "Cancelled by user.", -1, 0, 0)
			return
		}
		s.log("ERROR", "Download", fmt.Sprintf("Download verification failed for %s: %v", app.Name, err))
		setJobState("failed", "Download verification failed: "+err.Error(), -1, 0, 0)
		return
	}

	if job.DownloadOnly || job.Serial == "" {
		s.log("SUCCESS", "Download", fmt.Sprintf("Downloaded %s (%s) to local storage", app.Name, release.VersionName))
		setJobState("succeeded", "", 100, release.Size, release.Size)
		return
	}

	// 5. Install APK via ADB
	s.log("INFO", "Installer", fmt.Sprintf("Installing %s to device %s via ADB...", app.Name, job.Serial))
	setJobState("installing", "", 100, release.Size, release.Size)
	if err := s.backend.Install(workerCtx, job.Serial, apkPath); err != nil {
		if errors.Is(err, context.Canceled) && s.ctx.Err() == nil {
			s.log("WARN", "Installer", fmt.Sprintf("Installation of %s cancelled by user", app.Name))
			setJobState("cancelled", "Cancelled by user.", -1, 0, 0)
			return
		}
		s.log("ERROR", "ADB", fmt.Sprintf("ADB installation failed for %s on %s: %v", app.Name, job.Serial, err))
		setJobState("failed", "ADB installation failed: "+err.Error(), -1, 0, 0)
		return
	}

	s.log("SUCCESS", "Installer", fmt.Sprintf("Successfully installed %s to %s", app.Name, job.Serial))
	setJobState("succeeded", "", 100, release.Size, release.Size)
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || strings.TrimSpace(payload.ID) == "" {
		fail(w, http.StatusBadRequest, "Missing job ID.")
		return
	}

	s.mu.Lock()
	var updated *Job
	var cancelFn context.CancelFunc
	for i := range s.jobs {
		if s.jobs[i].ID == payload.ID {
			if s.jobs[i].State == "queued" || s.jobs[i].State == "downloading" || s.jobs[i].State == "installing" {
				s.jobs[i].State = "cancelled"
				s.jobs[i].Error = "Cancelled by user."
				cancelFn = s.jobCancels[payload.ID]
			}
			cp := s.jobs[i]
			updated = &cp
			break
		}
	}
	s.mu.Unlock()

	if cancelFn != nil {
		cancelFn()
	}

	if updated == nil {
		fail(w, http.StatusNotFound, "Job not found.")
		return
	}

	respond(w, http.StatusOK, updated)
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func fail(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}

var pageTemplate = template.Must(template.New("index.html").Funcs(template.FuncMap{
	"lower": strings.ToLower,
}).ParseFS(assets, "assets/index.html"))

type CategoryChip struct {
	ID    string
	Label string
}

var popularCategories = []CategoryChip{
	{ID: "", Label: "All Apps"},
	{ID: "Internet", Label: "Internet"},
	{ID: "Multimedia", Label: "Multimedia"},
	{ID: "Security", Label: "Security & Privacy"},
	{ID: "Development", Label: "Development"},
	{ID: "System", Label: "System & Tools"},
	{ID: "Navigation", Label: "Navigation & Maps"},
	{ID: "Games", Label: "Games"},
	{ID: "Reading", Label: "Reading & Docs"},
	{ID: "Science & Education", Label: "Science & Education"},
	{ID: "Graphics", Label: "Graphics & Photos"},
	{ID: "Time", Label: "Productivity & Time"},
}

type pageData struct {
	Catalog    *fdroid.Page
	Query      string
	Category   string
	Categories []CategoryChip

	Path       string
	Source     string
	Packages   []adb.Package
	Selected   string
	CachedAPKs []CachedAPK
	Logs       []LogEntry
	SubTab     string

	Token       string
	Preferences Preferences
	Themes      []Choice
	Accents     []string
	Devices     []adb.Device
	Jobs        []Job
	Error       string
	Busy        bool
}

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, http.StatusOK, "")
}

func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, status int, message string) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	devices, err := s.backend.Devices(ctx)

	s.mu.Lock()
	data := pageData{
		Path:        r.URL.Path,
		Selected:    r.URL.Query().Get("device"),
		SubTab:      r.URL.Query().Get("tab"),
		Token:       s.token,
		Preferences: s.preferences,
		Themes:      themes,
		Accents:     accents,
		Categories:  popularCategories,
		Devices:     devices,
		Jobs:        append([]Job(nil), s.jobs...),
		Logs:        append([]LogEntry(nil), s.logs...),
		Busy:        s.busy,
	}
	s.mu.Unlock()

	if data.SubTab == "" {
		data.SubTab = "tasks"
	}

	if data.Path == "/install" || data.Path == "/install-remote" {
		data.Path = "/"
	}
	if data.Path == "/preferences" {
		data.Path = "/settings"
	}

	data.Source = r.URL.Query().Get("source")
	if data.Source != "fdroid" && data.Source != "aurora" {
		data.Source = "local"
	}

	if err != nil {
		data.Error = err.Error()
	}

	data.Query = r.URL.Query().Get("q")
	data.Category = r.URL.Query().Get("category")

	// Pre-populate cached APKs for the Activity page or when needed
	if data.Path == "/activity" {
		data.CachedAPKs = s.getCachedAPKsList()
	}

	if data.Path == "/" && (data.Source == "fdroid" || data.Source == "aurora") {
		// Only fetch catalog synchronously if the user is in basic (No-JS) interface mode.
		// In enhanced mode, the Svelte application loads instantly and streams catalog data asynchronously.
		if data.Preferences.Interface == "basic" {
			catalogCtx, catalogCancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer catalogCancel()

			pageNumber, _ := strconv.Atoi(r.URL.Query().Get("page"))
			targetCatalog := s.catalogForSource(data.Source)
			result, catalogErr := targetCatalog.List(catalogCtx, data.Query, data.Category, pageNumber)
			if catalogErr != nil {
				data.Error = catalogErr.Error()
			} else {
				data.Catalog = &result
			}
		}
	}

	if r.URL.Path == "/installed" && data.Selected != "" {
		data.Packages, err = s.backend.Packages(ctx, data.Selected)
		if err != nil {
			data.Error = err.Error()
		}
	}

	if message != "" {
		data.Error = message
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = pageTemplate.Execute(w, data)
}

func (s *Server) preferencesForm(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil || r.PostForm.Get("token") != s.token {
		s.renderPage(w, r, http.StatusForbidden, "This form expired. Please try again.")
		return
	}

	maxAPKs, _ := strconv.Atoi(r.PostForm.Get("max_cached_apks"))
	retentionDays, _ := strconv.Atoi(r.PostForm.Get("cache_retention_days"))

	strategy := r.PostForm.Get("install_strategy")
	if strategy == "" {
		strategy = "auto"
	}
	if maxAPKs == 0 {
		maxAPKs = 10
	}
	if retentionDays == 0 {
		retentionDays = 14
	}

	p := Preferences{
		Theme:              r.PostForm.Get("theme"),
		Accent:             r.PostForm.Get("accent"),
		Contrast:           r.PostForm.Get("contrast"),
		Interface:          r.PostForm.Get("interface"),
		InstallStrategy:    strategy,
		MaxCachedAPKs:      StrictInt(maxAPKs),
		CacheRetentionDays: StrictInt(retentionDays),
	}

	s.mu.Lock()
	err := savePreferences(s.configPath, p)
	if err == nil {
		s.preferences = p
	}
	s.mu.Unlock()

	if err != nil {
		s.renderPage(w, r, http.StatusBadRequest, "Could not save settings: "+err.Error())
		return
	}

	apkDir := filepath.Join(s.cacheDir, "apks")
	go fdroid.PruneAPKs(apkDir, time.Duration(p.CacheRetentionDays)*24*time.Hour, int(p.MaxCachedAPKs))

	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) formInstall(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAPK+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		s.renderPage(w, r, http.StatusBadRequest, "Could not read APK upload. Maximum size: 512 MiB.")
		return
	}
	defer func() {
		_ = r.MultipartForm.RemoveAll()
	}()

	if r.FormValue("token") != s.token {
		s.renderPage(w, r, http.StatusForbidden, "This form expired. Please try again.")
		return
	}

	file, header, err := r.FormFile("apk")
	if err != nil {
		s.renderPage(w, r, http.StatusBadRequest, "Choose an APK file and try again.")
		return
	}
	defer func() {
		_ = file.Close()
	}()

	query := r.URL.Query()
	query.Set("serial", r.FormValue("serial"))
	query.Set("name", header.Filename)
	r.URL.RawQuery = query.Encode()
	r.Body = io.NopCloser(file)

	// Reuse the install path; form submissions redirect to the activity list.
	s.installAPK(w, r, true)
}

func (s *Server) packages(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	packages, err := s.backend.Packages(ctx, r.URL.Query().Get("device"))
	if err != nil {
		fail(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if packages == nil {
		packages = []adb.Package{}
	}
	respond(w, http.StatusOK, packages)
}

func (s *Server) updatePreferences(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)

	var p Preferences
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&p); err != nil {
		fail(w, http.StatusBadRequest, "Invalid settings: "+err.Error())
		return
	}

	if err := p.validate(); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.Lock()
	err := savePreferences(s.configPath, p)
	if err == nil {
		s.preferences = p
	}
	s.mu.Unlock()

	if err != nil {
		fail(w, http.StatusBadRequest, "Could not save settings: "+err.Error())
		return
	}

	apkDir := filepath.Join(s.cacheDir, "apks")
	go fdroid.PruneAPKs(apkDir, time.Duration(p.CacheRetentionDays)*24*time.Hour, int(p.MaxCachedAPKs))

	respond(w, http.StatusOK, p)
}

func (s *Server) catalogStatus(w http.ResponseWriter, r *http.Request) {
	target := s.catalogForSource(r.URL.Query().Get("source"))
	respond(w, http.StatusOK, target.Status())
}

func (s *Server) catalogRefresh(w http.ResponseWriter, r *http.Request) {
	target := s.catalogForSource(r.URL.Query().Get("source"))
	force := r.URL.Query().Get("force") == "true" || r.URL.Query().Get("force") == "1"

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		_ = target.Refresh(ctx, force)
	}()

	respond(w, http.StatusOK, target.Status())
}

func (s *Server) getLogs(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	respond(w, http.StatusOK, s.logs)
}

func (s *Server) clearLogs(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.logs = []LogEntry{}
	s.mu.Unlock()
	respond(w, http.StatusOK, map[string]bool{"cleared": true})
}

func (s *Server) getCachedAPKsList() []CachedAPK {
	apkDir := filepath.Join(s.cacheDir, "apks")
	entries, err := os.ReadDir(apkDir)
	if err != nil {
		return []CachedAPK{}
	}

	var results []CachedAPK
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".apk") {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Size() == 0 {
			continue
		}

		hash := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		name := hash
		if len(hash) > 12 {
			name = hash[:12] + ".apk"
		}
		pkgID := ""
		ver := ""

		app, rel, found := s.catalog.LookupReleaseBySHA256(hash)
		if !found {
			app, rel, found = s.izzyCatalog.LookupReleaseBySHA256(hash)
		}
		if found {
			name = app.Name
			pkgID = app.ID
			ver = rel.VersionName
		}

		results = append(results, CachedAPK{
			SHA256:      hash,
			PackageID:   pkgID,
			Name:        name,
			VersionName: ver,
			Size:        info.Size(),
			Downloaded:  info.ModTime().UTC().Format(time.RFC3339),
		})
	}

	slices.SortFunc(results, func(a, b CachedAPK) int {
		return strings.Compare(b.Downloaded, a.Downloaded)
	})

	return results
}

func (s *Server) listCachedAPKs(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, s.getCachedAPKsList())
}

func isValidSHA256(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for i := 0; i < 64; i++ {
		c := hash[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func (s *Server) installCachedAPK(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var payload struct {
		SHA256 string `json:"sha256"`
		Serial string `json:"serial"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.SHA256 == "" || payload.Serial == "" {
		fail(w, http.StatusBadRequest, "Missing sha256 or serial parameter.")
		return
	}

	payload.SHA256 = strings.ToLower(strings.TrimSpace(payload.SHA256))
	if strings.Contains(payload.SHA256, "..") || strings.ContainsAny(payload.SHA256, "/\\") || filepath.Base(payload.SHA256) != payload.SHA256 || !isValidSHA256(payload.SHA256) {
		fail(w, http.StatusBadRequest, "Invalid sha256 parameter.")
		return
	}
	if strings.ContainsAny(payload.Serial, "\r\n\t") || len(payload.Serial) > 128 {
		fail(w, http.StatusBadRequest, "Invalid serial parameter.")
		return
	}

	baseDir := filepath.Clean(filepath.Join(s.cacheDir, "apks"))
	apkPath := filepath.Clean(filepath.Join(baseDir, payload.SHA256+".apk"))
	relPath, err := filepath.Rel(baseDir, apkPath)
	if err != nil || strings.HasPrefix(relPath, "..") || filepath.Dir(apkPath) != baseDir {
		fail(w, http.StatusBadRequest, "Invalid APK path traversal.")
		return
	}

	if info, err := os.Stat(apkPath); err != nil || info.Size() == 0 {
		fail(w, http.StatusNotFound, "Cached APK not found on disk.")
		return
	}

	name := payload.SHA256[:12] + ".apk"
	var pkgID string
	app, rel, found := s.catalog.LookupReleaseBySHA256(payload.SHA256)
	if !found {
		app, rel, found = s.izzyCatalog.LookupReleaseBySHA256(payload.SHA256)
	}
	if found {
		name = fmt.Sprintf("%s (%s)", app.Name, rel.VersionName)
		pkgID = app.ID
	}

	s.mu.Lock()
	initialState := "installing"
	shouldStartNow := true
	if s.busy {
		initialState = "queued"
		shouldStartNow = false
	} else {
		s.busy = true
	}

	job := Job{
		ID:        fmt.Sprintf("%s-%d", time.Now().UTC().Format("20060102T150405.000000000"), len(s.jobs)),
		Name:      name,
		PackageID: pkgID,
		Serial:    payload.Serial,
		State:     initialState,
		Progress:  0,
		Source:    "local-cache",
		Created:   time.Now().UTC(),
	}

	if len(s.jobs) >= 30 {
		s.jobs = s.jobs[:29]
	}
	s.jobs = append([]Job{job}, s.jobs...)
	s.mu.Unlock()

	if shouldStartNow {
		s.workers.Add(1)
		go s.runLocalInstallJob(job, apkPath)
	}

	respond(w, http.StatusAccepted, job)
}

func (s *Server) installCachedAPKForm(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil || r.PostForm.Get("token") != s.token {
		s.renderPage(w, r, http.StatusForbidden, "This form expired. Please try again.")
		return
	}

	sha256Hash := strings.ToLower(strings.TrimSpace(r.PostForm.Get("sha256")))
	serial := strings.TrimSpace(r.PostForm.Get("serial"))

	if sha256Hash == "" || serial == "" {
		s.renderPage(w, r, http.StatusBadRequest, "Missing sha256 or device serial.")
		return
	}
	if strings.Contains(sha256Hash, "..") || strings.ContainsAny(sha256Hash, "/\\") || filepath.Base(sha256Hash) != sha256Hash || !isValidSHA256(sha256Hash) {
		s.renderPage(w, r, http.StatusBadRequest, "Invalid sha256 hash.")
		return
	}
	if strings.ContainsAny(serial, "\r\n\t") || len(serial) > 128 {
		s.renderPage(w, r, http.StatusBadRequest, "Invalid serial parameter.")
		return
	}

	baseDir := filepath.Clean(filepath.Join(s.cacheDir, "apks"))
	apkPath := filepath.Clean(filepath.Join(baseDir, sha256Hash+".apk"))
	relPath, err := filepath.Rel(baseDir, apkPath)
	if err != nil || strings.HasPrefix(relPath, "..") || filepath.Dir(apkPath) != baseDir {
		s.renderPage(w, r, http.StatusBadRequest, "Invalid APK path traversal.")
		return
	}

	if info, err := os.Stat(apkPath); err != nil || info.Size() == 0 {
		s.renderPage(w, r, http.StatusNotFound, "Cached APK not found on disk.")
		return
	}

	name := sha256Hash[:12] + ".apk"
	var pkgID string
	app, rel, found := s.catalog.LookupReleaseBySHA256(sha256Hash)
	if !found {
		app, rel, found = s.izzyCatalog.LookupReleaseBySHA256(sha256Hash)
	}
	if found {
		name = fmt.Sprintf("%s (%s)", app.Name, rel.VersionName)
		pkgID = app.ID
	}

	s.mu.Lock()
	initialState := "installing"
	shouldStartNow := true
	if s.busy {
		initialState = "queued"
		shouldStartNow = false
	} else {
		s.busy = true
	}

	job := Job{
		ID:        fmt.Sprintf("%s-%d", time.Now().UTC().Format("20060102T150405.000000000"), len(s.jobs)),
		Name:      name,
		PackageID: pkgID,
		Serial:    serial,
		State:     initialState,
		Progress:  0,
		Source:    "local-cache",
		Created:   time.Now().UTC(),
	}

	if len(s.jobs) >= 30 {
		s.jobs = s.jobs[:29]
	}
	s.jobs = append([]Job{job}, s.jobs...)
	s.mu.Unlock()

	if shouldStartNow {
		s.workers.Add(1)
		go s.runLocalInstallJob(job, apkPath)
	}

	http.Redirect(w, r, "/activity?device="+serial, http.StatusSeeOther)
}

func (s *Server) deleteCachedAPKForm(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil || r.PostForm.Get("token") != s.token {
		s.renderPage(w, r, http.StatusForbidden, "This form expired. Please try again.")
		return
	}

	sha256Hash := strings.ToLower(strings.TrimSpace(r.PostForm.Get("sha256")))
	if sha256Hash == "" || strings.Contains(sha256Hash, "..") || strings.ContainsAny(sha256Hash, "/\\") || filepath.Base(sha256Hash) != sha256Hash || !isValidSHA256(sha256Hash) {
		s.renderPage(w, r, http.StatusBadRequest, "Invalid SHA-256 hash.")
		return
	}

	baseDir := filepath.Clean(filepath.Join(s.cacheDir, "apks"))
	apkPath := filepath.Clean(filepath.Join(baseDir, sha256Hash+".apk"))
	relPath, err := filepath.Rel(baseDir, apkPath)
	if err != nil || strings.HasPrefix(relPath, "..") || filepath.Dir(apkPath) != baseDir {
		s.renderPage(w, r, http.StatusBadRequest, "Invalid file path.")
		return
	}

	_ = os.Remove(apkPath)
	s.log("INFO", "Cache", "Deleted cached APK: "+sha256Hash[:12])
	http.Redirect(w, r, "/activity", http.StatusSeeOther)
}

func (s *Server) clearLogsForm(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil || r.PostForm.Get("token") != s.token {
		s.renderPage(w, r, http.StatusForbidden, "This form expired. Please try again.")
		return
	}

	s.mu.Lock()
	s.logs = []LogEntry{}
	s.mu.Unlock()
	http.Redirect(w, r, "/activity?tab=logs", http.StatusSeeOther)
}

func (s *Server) runLocalInstallJob(job Job, apkPath string) {
	defer s.workers.Done()

	workerCtx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	s.mu.Lock()
	s.jobCancels[job.ID] = cancel
	s.mu.Unlock()

	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.jobCancels, job.ID)
		s.mu.Unlock()
		s.startNextQueuedJob()
	}()

	s.log("INFO", "ADB", fmt.Sprintf("Installing cached APK %s on %s...", job.Name, job.Serial))
	err := s.backend.Install(workerCtx, job.Serial, apkPath)

	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.jobs {
		if s.jobs[i].ID == job.ID {
			if s.jobs[i].State == "cancelled" {
				break
			}
			if err != nil {
				if errors.Is(err, context.Canceled) && s.ctx.Err() == nil {
					s.jobs[i].State = "cancelled"
					s.jobs[i].Error = "Cancelled by user."
					s.log("WARN", "ADB", fmt.Sprintf("Install of %s cancelled", job.Name))
				} else {
					s.jobs[i].State = "failed"
					s.jobs[i].Error = err.Error()
					s.log("ERROR", "ADB", fmt.Sprintf("Install failed for %s: %v", job.Name, err))
				}
			} else {
				s.jobs[i].State = "succeeded"
				s.jobs[i].Progress = 100
				s.log("SUCCESS", "ADB", fmt.Sprintf("Successfully installed %s to %s", job.Name, job.Serial))
			}
			break
		}
	}
}

func (s *Server) deleteCachedAPK(w http.ResponseWriter, r *http.Request) {
	sha256Hash := strings.TrimSpace(r.URL.Query().Get("sha256"))
	if sha256Hash == "" {
		var payload struct {
			SHA256 string `json:"sha256"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		sha256Hash = strings.TrimSpace(payload.SHA256)
	}

	sha256Hash = strings.ToLower(sha256Hash)
	if strings.Contains(sha256Hash, "..") || strings.ContainsAny(sha256Hash, "/\\") || filepath.Base(sha256Hash) != sha256Hash || !isValidSHA256(sha256Hash) {
		fail(w, http.StatusBadRequest, "Invalid SHA-256 hash.")
		return
	}

	baseDir := filepath.Clean(filepath.Join(s.cacheDir, "apks"))
	apkPath := filepath.Clean(filepath.Join(baseDir, sha256Hash+".apk"))
	relPath, err := filepath.Rel(baseDir, apkPath)
	if err != nil || strings.HasPrefix(relPath, "..") || filepath.Dir(apkPath) != baseDir {
		fail(w, http.StatusBadRequest, "Invalid file path.")
		return
	}
	if err := os.Remove(apkPath); err != nil && !os.IsNotExist(err) {
		fail(w, http.StatusInternalServerError, "Could not delete APK: "+err.Error())
		return
	}

	s.log("INFO", "Cache", "Deleted cached APK: "+sha256Hash[:12])
	respond(w, http.StatusOK, map[string]bool{"deleted": true})
}

// Close cancels active ADB work and waits for worker goroutines to finish.
func (s *Server) Close() {
	s.cancel()
	s.workers.Wait()
}

func (s *Server) catalogPage(w http.ResponseWriter, r *http.Request) {
	pageNumber, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil && r.URL.Query().Get("page") != "" {
		fail(w, http.StatusBadRequest, "Invalid catalog page.")
		return
	}

	targetCatalog := s.catalogForSource(r.URL.Query().Get("source"))
	st := targetCatalog.Status()

	// If the catalog is actively downloading its initial index over the network,
	// return an empty page immediately so the client can display the live orbital progress banner
	// instead of freezing the HTTP request connection.
	if st.State == "loading" && st.AppCount == 0 {
		respond(w, http.StatusOK, fdroid.Page{
			Apps:       []fdroid.App{},
			Total:      0,
			Page:       1,
			Pages:      1,
			Categories: []string{},
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	result, err := targetCatalog.List(ctx, r.URL.Query().Get("q"), r.URL.Query().Get("category"), pageNumber)
	if err != nil {
		st = targetCatalog.Status()
		if st.State == "loading" {
			respond(w, http.StatusOK, fdroid.Page{
				Apps:       []fdroid.App{},
				Total:      0,
				Page:       1,
				Pages:      1,
				Categories: []string{},
			})
			return
		}
		fail(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	respond(w, http.StatusOK, result)
}

func (s *Server) catalogIcon(w http.ResponseWriter, r *http.Request) {
	iconPath := r.URL.Query().Get("path")
	if iconPath == "" || len(iconPath) > 512 || !strings.HasPrefix(iconPath, "/") || strings.HasPrefix(iconPath, "//") || strings.Contains(iconPath, "\\") || strings.Contains(iconPath, "..") {
		http.Error(w, "Invalid icon path.", http.StatusBadRequest)
		return
	}
	targetCatalog := s.catalogForSource(r.URL.Query().Get("source"))

	body, kind, err := targetCatalog.Icon(r.Context(), iconPath)
	if err != nil {
		// Fallback to the other catalog if source wasn't explicitly specified
		other := s.izzyCatalog
		if targetCatalog == s.izzyCatalog {
			other = s.catalog
		}
		body, kind, err = other.Icon(r.Context(), iconPath)
		if err != nil {
			http.Error(w, "Icon is unavailable.", http.StatusNotFound)
			return
		}
	}

	w.Header().Set("Content-Type", kind)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(body)
}

func (s *Server) catalogApp(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		fail(w, http.StatusBadRequest, "Missing app id parameter.")
		return
	}

	targetCatalog := s.catalogForSource(r.URL.Query().Get("source"))
	app, err := targetCatalog.FindApp(r.Context(), id)
	if err != nil {
		other := s.izzyCatalog
		if targetCatalog == s.izzyCatalog {
			other = s.catalog
		}
		app, err = other.FindApp(r.Context(), id)
		if err != nil {
			fail(w, http.StatusNotFound, err.Error())
			return
		}
	}

	respond(w, http.StatusOK, app)
}

func (s *Server) catalogResolve(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var packageIDs []string
	if err := json.NewDecoder(r.Body).Decode(&packageIDs); err != nil {
		fail(w, http.StatusBadRequest, "Invalid package ID list JSON.")
		return
	}

	matches, err := s.catalog.ResolvePackages(r.Context(), packageIDs)
	if err != nil {
		matches = make(map[string]fdroid.App)
	}

	// Also check IzzyOnDroid if already loaded or if any packages remain unmatched
	if izzyMatches, izzyErr := s.izzyCatalog.ResolvePackages(r.Context(), packageIDs); izzyErr == nil {
		for id, app := range izzyMatches {
			if _, exists := matches[id]; !exists {
				matches[id] = app
			}
		}
	}

	respond(w, http.StatusOK, matches)
}
