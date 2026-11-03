package dashboard

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nyxar77/elene/internal/adb"
	"github.com/nyxar77/elene/internal/fdroid"
)

type fakeBackend struct {
	devices   []adb.Device
	deviceErr error
	install   func(context.Context, string, string) error
}

func (f *fakeBackend) Devices(context.Context) ([]adb.Device, error) {
	return f.devices, f.deviceErr
}

func (f *fakeBackend) InspectDevice(ctx context.Context, serial string) (adb.Device, error) {
	return adb.Device{Serial: serial, ABIs: []string{"arm64-v8a"}, SDK: 33}, nil
}

func (f *fakeBackend) Packages(context.Context, string) ([]adb.Package, error) {
	return []adb.Package{{ID: "org.example.app"}}, nil
}

func (f *fakeBackend) Install(ctx context.Context, serial, path string) error {
	if f.install != nil {
		return f.install(ctx, serial, path)
	}
	return nil
}

func testServer(t *testing.T, b *fakeBackend) *Server {
	t.Helper()
	temp := t.TempDir()
	s, err := New(t.Context(), b, filepath.Join(temp, "eden", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.SetCacheDir(filepath.Join(temp, "cache"))
	t.Cleanup(s.Close)
	return s
}

func request(s *Server, method, path string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:18420"+path, body)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.Handler("127.0.0.1:18420").ServeHTTP(w, r)
	return w
}

func TestDashboardRendersWithoutJavaScript(t *testing.T) {
	s := testServer(t, &fakeBackend{devices: []adb.Device{
		{Serial: "phone-1", Model: "<script>bad</script>", State: adb.DeviceOnline},
		{Serial: "phone-2", State: adb.DeviceUnauthorized},
	}})
	s.preferences.Interface = "basic"
	w := request(s, "GET", "/", nil, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, want := range []string{`action="/install"`, `name="serial"`, `value="phone-1"`, `name="apk"`, `&lt;script&gt;bad&lt;/script&gt;`, `Accept the debugging prompt`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(w.Body.String(), `<script`) || !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'none'") {
		t.Fatal("basic mode permits scripts")
	}
}

func TestLocalRequestProtection(t *testing.T) {
	s := testServer(t, &fakeBackend{})
	for _, headers := range []map[string]string{{"Origin": "https://evil.example"}, {"Sec-Fetch-Site": "cross-site"}} {
		if w := request(s, "GET", "/api/session", nil, headers); w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	r := httptest.NewRequest("GET", "http://evil.example/api/devices", nil)
	w := httptest.NewRecorder()
	s.Handler("127.0.0.1:18420").ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := request(s, "POST", "/api/install?serial=a&name=a.apk", strings.NewReader("apk"), nil); w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func TestInstallLifecycleAndBusy(t *testing.T) {
	release := make(chan struct{})
	started := make(chan string, 1)
	s := testServer(t, &fakeBackend{install: func(ctx context.Context, serial, path string) error {
		if serial != "phone" {
			return errors.New("wrong serial")
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "apk contents" {
			return errors.New("wrong upload")
		}
		started <- path
		select {
		case <-release:
			return errors.New("device disconnected")
		case <-ctx.Done():
			return ctx.Err()
		}
	}})

	headers := map[string]string{"X-Elene-Token": s.token}
	w := request(s, "POST", "/api/install?serial=phone&name=app.apk", strings.NewReader("apk contents"), headers)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}

	var path string
	select {
	case path = <-started:
	case <-time.After(time.Second):
		t.Fatal("install never started")
	}

	if w = request(s, "POST", "/api/install?serial=phone&name=app.apk", strings.NewReader("apk"), headers); w.Code != 409 {
		t.Fatal(w.Code)
	}

	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		job := s.jobs[0]
		busy := s.busy
		s.mu.Unlock()
		_, err := os.Stat(filepath.Dir(path))
		if !busy && os.IsNotExist(err) {
			if job.State != "failed" || job.Error != "device disconnected" {
				t.Fatal(job)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job or cleanup did not finish")
}

func TestInstallRemoteLifecycle(t *testing.T) {
	fakeAPK := []byte("f-droid fake app apk content")
	hasher := sha256.New()
	hasher.Write(fakeAPK)
	apkSHA256 := hex.EncodeToString(hasher.Sum(nil))

	catalogServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repo/index-v2.json" {
			_, _ = w.Write([]byte(`{
				"packages": {
					"org.fdroid.testapp": {
						"metadata": {
							"name": {"en-US": "TestApp"},
							"summary": {"en-US": "Testing App"},
							"categories": ["Utility"],
							"lastUpdated": 500
						},
						"versions": {
							"v1": {
								"added": 500,
								"file": {
									"name": "/testapp_1.apk",
									"sha256": "` + apkSHA256 + `",
									"size": 100
								},
								"manifest": {
									"versionName": "1.0",
									"versionCode": 1,
									"nativecode": ["arm64-v8a"]
								}
							}
						}
					}
				}
			}`))
			return
		}
		if r.URL.Path == "/repo/testapp_1.apk" {
			_, _ = w.Write(fakeAPK)
			return
		}
		http.NotFound(w, r)
	}))
	defer catalogServer.Close()

	installedChan := make(chan string, 1)
	s := testServer(t, &fakeBackend{install: func(ctx context.Context, serial, path string) error {
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		installedChan <- string(content)
		return nil
	}})

	// Direct catalog to test server
	testCat := fdroid.New()
	testCat.SetCacheDir(t.TempDir())
	testCat.SetTestEndpoints(catalogServer.URL+"/repo/index-v2.json", catalogServer.URL+"/repo")
	s.SetCatalog(testCat)

	headers := map[string]string{
		"X-Elene-Token": s.token,
		"Content-Type":  "application/json",
	}

	w := request(s, "POST", "/api/install-remote", strings.NewReader(`{"serial":"phone","source":"fdroid","package":"org.fdroid.testapp"}`), headers)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case content := <-installedChan:
		if content != string(fakeAPK) {
			t.Fatalf("installed content does not match expected APK: got %q", content)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("remote install timed out")
	}

	// Wait for the background worker to finish updating job state
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		job := s.jobs[0]
		busy := s.busy
		s.mu.Unlock()
		if !busy && job.State == "succeeded" {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// Verify job recorded as succeeded
	s.mu.Lock()
	job := s.jobs[0]
	s.mu.Unlock()
	if job.State != "succeeded" || job.Name != "org.fdroid.testapp" {
		t.Fatalf("unexpected job status: %+v", job)
	}

	// Test form submission for remote install (No-JS mode)
	formValues := url.Values{
		"token":   {s.token},
		"serial":  {"phone"},
		"source":  {"fdroid"},
		"package": {"org.fdroid.testapp"},
	}
	formHeaders := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	wForm := request(s, "POST", "/install-remote", strings.NewReader(formValues.Encode()), formHeaders)
	if wForm.Code != http.StatusSeeOther || wForm.Header().Get("Location") != "/activity" {
		t.Fatalf("expected redirect to /activity, got %d (loc: %s)", wForm.Code, wForm.Header().Get("Location"))
	}
}

func TestFormInstall(t *testing.T) {
	s := testServer(t, &fakeBackend{})
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("token", s.token)
	_ = form.WriteField("serial", "phone")
	file, err := form.CreateFormFile("apk", "hello.apk")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(file, "apk")
	_ = form.Close()
	w := request(s, "POST", "/install", &body, map[string]string{"Content-Type": form.FormDataContentType()})
	if w.Code != 303 || w.Header().Get("Location") != "/activity" {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestInvalidUploadAndDevicesFailure(t *testing.T) {
	s := testServer(t, &fakeBackend{deviceErr: errors.New("ADB unavailable")})
	for _, tc := range []struct{ query, body string }{
		{"serial=p&name=a.zip", "x"},
		{"serial=&name=a.apk", "x"},
		{"serial=p&name=a.apk", ""},
	} {
		w := request(s, "POST", "/api/install?"+tc.query, strings.NewReader(tc.body), map[string]string{"X-Elene-Token": s.token})
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
		s.mu.Lock()
		busy := s.busy
		s.mu.Unlock()
		if busy {
			t.Fatal("invalid upload blocked future jobs")
		}
	}
	if w := request(s, "GET", "/api/devices", nil, nil); w.Code != 503 {
		t.Fatal(w.Code)
	}
	if w := request(s, "GET", "/", nil, nil); !strings.Contains(w.Body.String(), "ADB unavailable") {
		t.Fatal(w.Body.String())
	}
}

func TestPreferencesPersistAndValidate(t *testing.T) {
	s := testServer(t, &fakeBackend{})
	fields := url.Values{
		"token":     {s.token},
		"theme":     {"mocha"},
		"accent":    {"peach"},
		"contrast":  {"hard"},
		"interface": {"basic"},
	}
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	w := request(s, "POST", "/preferences", strings.NewReader(fields.Encode()), headers)
	if w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	restarted, err := New(t.Context(), &fakeBackend{}, s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.preferences != s.preferences {
		t.Fatal(restarted.preferences)
	}
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved Preferences
	if err = json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	for _, theme := range themes {
		for _, accent := range accents {
			p := saved
			p.Theme = theme.Value
			p.Accent = accent
			if err = p.validate(); err != nil {
				t.Fatal(err)
			}
		}
	}
	fields.Set("theme", "unknown")
	w = request(s, "POST", "/preferences", strings.NewReader(fields.Encode()), headers)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	fields.Set("token", "wrong")
	w = request(s, "POST", "/preferences", strings.NewReader(fields.Encode()), headers)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	after, err := loadPreferences(s.configPath)
	if err != nil || after != saved {
		t.Fatal("bad request changed settings", err)
	}
	info, err := os.Stat(s.configPath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("settings permissions", err)
	}
}

func TestAssets(t *testing.T) {
	s := testServer(t, &fakeBackend{})
	for _, path := range []string{"/themes.css", "/dist/app.css", "/dist/app.js", "/favicon.ico", "/logo.svg"} {
		w := request(s, http.MethodGet, path, nil, nil)
		if w.Code != 200 {
			t.Fatal(path, w.Code)
		}
	}
}

func TestJSONPreferencesAndInstalledRoutes(t *testing.T) {
	s := testServer(t, &fakeBackend{})
	w := request(s, "PUT", "/api/preferences", strings.NewReader(`{"theme":"dracula","accent":"mauve","contrast":"medium","interface":"basic"}`), map[string]string{"X-Elene-Token": s.token})
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(s, "GET", "/api/preferences", nil, nil)
	if !strings.Contains(w.Body.String(), "dracula") {
		t.Fatal(w.Body.String())
	}
	for _, path := range []string{"/installed?device=phone", "/updates", "/activity", "/settings"} {
		w := request(s, "GET", path, nil, nil)
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "<script") {
			t.Fatal(path, w.Code)
		}
	}
	w = request(s, "GET", "/api/packages?device=phone", nil, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "org.example.app") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestShutdownCancelsInstallAndCleansUpload(t *testing.T) {
	started := make(chan string, 1)
	s := testServer(t, &fakeBackend{install: func(ctx context.Context, serial, path string) error {
		started <- path
		<-ctx.Done()
		return ctx.Err()
	}})
	w := request(s, "POST", "/api/install?serial=phone&name=app.apk", strings.NewReader("apk"), map[string]string{"X-Elene-Token": s.token})
	if w.Code != http.StatusAccepted {
		t.Fatal(w.Code)
	}
	var path string
	select {
	case path = <-started:
	case <-time.After(time.Second):
		t.Fatal("job never started")
	}
	s.Close()
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("upload remains after shutdown", err)
	}
}

func TestRemoteInstallQueueAndCancel(t *testing.T) {
	fakeAPK := []byte("queued app apk content")
	hasher := sha256.New()
	hasher.Write(fakeAPK)
	apkSHA256 := hex.EncodeToString(hasher.Sum(nil))

	catalogServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repo/index-v2.json" {
			_, _ = w.Write([]byte(`{
				"packages": {
					"org.example.app1": {
						"metadata": {"name": {"en-US": "App1"}, "summary": {"en-US": "First"}},
						"versions": {"v1": {"added": 100, "file": {"name": "/app.apk", "sha256": "` + apkSHA256 + `", "size": 50}, "manifest": {"versionName": "1.0", "versionCode": 1}}}
					},
					"org.example.app2": {
						"metadata": {"name": {"en-US": "App2"}, "summary": {"en-US": "Second"}},
						"versions": {"v1": {"added": 100, "file": {"name": "/app.apk", "sha256": "` + apkSHA256 + `", "size": 50}, "manifest": {"versionName": "1.0", "versionCode": 1}}}
					},
					"org.example.app3": {
						"metadata": {"name": {"en-US": "App3"}, "summary": {"en-US": "Third"}},
						"versions": {"v1": {"added": 100, "file": {"name": "/app.apk", "sha256": "` + apkSHA256 + `", "size": 50}, "manifest": {"versionName": "1.0", "versionCode": 1}}}
					}
				}
			}`))
			return
		}
		if r.URL.Path == "/repo/app.apk" {
			_, _ = w.Write(fakeAPK)
			return
		}
		http.NotFound(w, r)
	}))
	defer catalogServer.Close()

	blockFirst := make(chan struct{})
	firstStarted := make(chan struct{}, 1)
	s := testServer(t, &fakeBackend{install: func(ctx context.Context, serial, path string) error {
		select {
		case firstStarted <- struct{}{}:
			<-blockFirst
		default:
		}
		return nil
	}})

	testCat := fdroid.New()
	testCat.SetCacheDir(t.TempDir())
	testCat.SetTestEndpoints(catalogServer.URL+"/repo/index-v2.json", catalogServer.URL+"/repo")
	s.SetCatalog(testCat)

	headers := map[string]string{
		"X-Elene-Token": s.token,
		"Content-Type":  "application/json",
	}

	// 1. Start first remote install
	w1 := request(s, "POST", "/api/install-remote", strings.NewReader(`{"serial":"phone","source":"fdroid","package":"org.example.app1"}`), headers)
	if w1.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for app1, got %d", w1.Code)
	}

	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first install did not start")
	}

	// 2. Enqueue second and third remote installs while first is busy
	w2 := request(s, "POST", "/api/install-remote", strings.NewReader(`{"serial":"phone","source":"fdroid","package":"org.example.app2"}`), headers)
	if w2.Code != http.StatusAccepted {
		t.Fatalf("expected 202 queued for app2, got %d: %s", w2.Code, w2.Body.String())
	}
	var job2 Job
	_ = json.NewDecoder(w2.Body).Decode(&job2)
	if job2.State != "queued" {
		t.Fatalf("expected job2 state 'queued', got %q", job2.State)
	}

	w3 := request(s, "POST", "/api/install-remote", strings.NewReader(`{"serial":"phone","source":"fdroid","package":"org.example.app3"}`), headers)
	if w3.Code != http.StatusAccepted {
		t.Fatalf("expected 202 queued for app3, got %d", w3.Code)
	}
	var job3 Job
	_ = json.NewDecoder(w3.Body).Decode(&job3)
	if job3.State != "queued" {
		t.Fatalf("expected job3 state 'queued', got %q", job3.State)
	}

	// 3. Cancel job2 while it is queued
	wCancel := request(s, "POST", "/api/jobs/cancel", strings.NewReader(`{"id":"`+job2.ID+`"}`), headers)
	if wCancel.Code != http.StatusOK {
		t.Fatalf("expected 200 on cancel, got %d: %s", wCancel.Code, wCancel.Body.String())
	}

	// 4. Unblock first job and verify job1 & job3 succeed while job2 stays cancelled
	close(blockFirst)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		busy := s.busy
		s.mu.Unlock()
		if !busy {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	states := make(map[string]string)
	for _, j := range s.jobs {
		states[j.Name] = j.State
	}
	if states["org.example.app1"] != "succeeded" || states["org.example.app2"] != "cancelled" || states["org.example.app3"] != "succeeded" {
		t.Fatalf("unexpected final job states: %+v", states)
	}
}

func TestLocalhostAnd127Protection(t *testing.T) {
	s, err := New(t.Context(), &fakeBackend{}, filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}

	handler := s.Handler("127.0.0.1:18420")

	// 127.0.0.1:18420 allowed
	req1 := httptest.NewRequest("GET", "/api/session", nil)
	req1.Host = "127.0.0.1:18420"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200 for 127.0.0.1, got %d", rec1.Code)
	}

	// localhost:18420 allowed
	req2 := httptest.NewRequest("GET", "/api/session", nil)
	req2.Host = "localhost:18420"
	req2.Header.Set("Origin", "http://localhost:18420")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 for localhost, got %d", rec2.Code)
	}

	// [::1]:18420 allowed
	req3 := httptest.NewRequest("GET", "/api/session", nil)
	req3.Host = "[::1]:18420"
	req3.Header.Set("Origin", "http://[::1]:18420")
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("expected 200 for [::1], got %d", rec3.Code)
	}

	// External domain rejected with 403
	req4 := httptest.NewRequest("GET", "/api/session", nil)
	req4.Host = "attacker.example.com:18420"
	rec4 := httptest.NewRecorder()
	handler.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for attacker host, got %d", rec4.Code)
	}
}

func TestStrictIntegerPreferenceValidation(t *testing.T) {
	s, err := New(t.Context(), &fakeBackend{}, filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}

	headers := map[string]string{
		"Content-Type":  "application/json",
		"X-Elene-Token": s.token,
	}

	// Reject decimal 19.5
	w1 := request(s, "PUT", "/api/preferences", strings.NewReader(`{"theme":"light","accent":"mauve","contrast":"medium","interface":"enhanced","installStrategy":"auto","maxCachedAPKs":19.5,"cacheRetentionDays":14}`), headers)
	if w1.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for decimal 19.5, got %d", w1.Code)
	}

	// Reject decimal 18.0
	w2 := request(s, "PUT", "/api/preferences", strings.NewReader(`{"theme":"light","accent":"mauve","contrast":"medium","interface":"enhanced","installStrategy":"auto","maxCachedAPKs":18.0,"cacheRetentionDays":14}`), headers)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for decimal 18.0, got %d", w2.Code)
	}

	// Reject out of range (< 5)
	w3 := request(s, "PUT", "/api/preferences", strings.NewReader(`{"theme":"light","accent":"mauve","contrast":"medium","interface":"enhanced","installStrategy":"auto","maxCachedAPKs":4,"cacheRetentionDays":14}`), headers)
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for maxCachedAPKs 4, got %d", w3.Code)
	}

	// Reject out of range (> 20)
	w4 := request(s, "PUT", "/api/preferences", strings.NewReader(`{"theme":"light","accent":"mauve","contrast":"medium","interface":"enhanced","installStrategy":"auto","maxCachedAPKs":21,"cacheRetentionDays":14}`), headers)
	if w4.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for maxCachedAPKs 21, got %d", w4.Code)
	}

	// Reject retention days > 30
	w5 := request(s, "PUT", "/api/preferences", strings.NewReader(`{"theme":"light","accent":"mauve","contrast":"medium","interface":"enhanced","installStrategy":"auto","maxCachedAPKs":10,"cacheRetentionDays":31}`), headers)
	if w5.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cacheRetentionDays 31, got %d", w5.Code)
	}

	// Accept valid integer values (max 20, retention 30)
	w6 := request(s, "PUT", "/api/preferences", strings.NewReader(`{"theme":"light","accent":"mauve","contrast":"medium","interface":"enhanced","installStrategy":"auto","maxCachedAPKs":20,"cacheRetentionDays":30}`), headers)
	if w6.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid settings, got %d: %s", w6.Code, w6.Body.String())
	}
}

func TestCachedAPKsAndLogsEndpoints(t *testing.T) {
	tempDir := t.TempDir()
	s, err := New(t.Context(), &fakeBackend{}, filepath.Join(tempDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.SetCacheDir(filepath.Join(tempDir, "cache"))

	headers := map[string]string{
		"Content-Type":  "application/json",
		"X-Elene-Token": s.token,
	}

	// 1. Initial cached APK list is empty
	wList := request(s, "GET", "/api/cached-apks", nil, nil)
	if wList.Code != http.StatusOK {
		t.Fatalf("expected 200 for cached-apks, got %d", wList.Code)
	}

	// 2. Logs endpoint returns entries
	s.log("INFO", "Test", "Test log message")
	wLogs := request(s, "GET", "/api/logs", nil, nil)
	if wLogs.Code != http.StatusOK || !strings.Contains(wLogs.Body.String(), "Test log message") {
		t.Fatalf("expected log entry in response: %s", wLogs.Body.String())
	}

	// 3. Clear logs
	wClear := request(s, "POST", "/api/logs/clear", nil, headers)
	if wClear.Code != http.StatusOK {
		t.Fatalf("expected 200 on logs clear, got %d", wClear.Code)
	}
}
