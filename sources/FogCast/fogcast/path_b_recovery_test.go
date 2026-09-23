package fogcast

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/internal/version"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

// Path B is a development session that is already reboot_required, a second
// LoadIdle that still fails, and only then a board reboot. These tests inject
// that protocol result and substitute WithRebootCommand. They do not call
// /sbin/reboot and they do not touch a kit.
func TestPathBSecondLoadIdleFailureStartsTestRebootCommand(t *testing.T) {
	harness := startPathB(t, "second-load-fails")
	if _, err := harness.service.LoadDevelopmentRBF(context.Background(), 3, strings.NewReader("rbf")); err != nil {
		t.Fatal(err)
	}

	status, err := harness.service.Stop(context.Background())
	if err != nil || status.State != protocol.StateIdle || harness.service.activeExecution != "" {
		t.Fatalf("path B stop = %#v, %v, execution %q", status, err, harness.service.activeExecution)
	}
	if harness.control.counts() != [2]int{1, 1} || harness.control.scriptSeenDuringRecover() {
		t.Fatalf("stop/recover calls = %v script already running at recover_idle = %t", harness.control.counts(), harness.control.scriptSeenDuringRecover())
	}
	if _, err := os.Stat(harness.marker); err != nil {
		t.Fatalf("test reboot command did not run: %v", err)
	}
}

func TestPathBIdleStopDoesNotArmDevelopmentReboot(t *testing.T) {
	harness := startPathB(t, "stop-idle")
	if _, err := harness.service.LoadDevelopmentRBF(context.Background(), 3, strings.NewReader("rbf")); err != nil {
		t.Fatal(err)
	}

	status, err := harness.service.Stop(context.Background())
	if err != nil || status.State != protocol.StateIdle {
		t.Fatalf("idle stop = %#v, %v", status, err)
	}
	if harness.control.counts() != [2]int{1, 0} {
		t.Fatalf("stop/recover calls = %v, want one stop and no recover_idle", harness.control.counts())
	}
	if _, err := os.Stat(harness.marker); !os.IsNotExist(err) {
		t.Fatalf("idle stop started the reboot command: %v", err)
	}
	code, body := postEmpty(t, harness.targetURL+"/v1/development/reboot", harness.token)
	if code != http.StatusBadRequest || !strings.Contains(body, "development reboot was not requested") {
		t.Fatalf("development reboot = %d %s", code, body)
	}
}

func TestPathBReconciledRebootRequiredStopOmitsRecovery(t *testing.T) {
	harness := startPathB(t, "reconciled")
	harness.service.activeExecution = ExecutionFPGADevelopment

	_, err := harness.service.Stop(context.Background())
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("reconciled stop = %v", err)
	}
	if harness.control.counts() != [2]int{0, 0} {
		t.Fatalf("reconciled stop/recover calls = %v", harness.control.counts())
	}
	if _, statErr := os.Stat(harness.marker); !os.IsNotExist(statErr) {
		t.Fatalf("reconciled stop started the reboot command: %v", statErr)
	}

	statusCode, statusBody := getAuthorized(t, harness.targetURL+"/v1/status", harness.token)
	if statusCode != http.StatusOK || !strings.Contains(statusBody, `"state":"failed"`) || !strings.Contains(statusBody, `"recovery":"reboot_required"`) || strings.Contains(statusBody, `"development"`) {
		t.Fatalf("stored status = %d %s", statusCode, statusBody)
	}
	stopCode, stopBody := postEmpty(t, harness.targetURL+"/v1/stop", harness.token)
	if stopCode != http.StatusServiceUnavailable || !strings.Contains(stopBody, string(protocol.CodeMiSTerUnavailable)) || strings.Contains(stopBody, `"recovery"`) {
		t.Fatalf("stop envelope = %d %s", stopCode, stopBody)
	}
	rebootCode, rebootBody := postEmpty(t, harness.targetURL+"/v1/development/reboot", harness.token)
	if rebootCode != http.StatusBadRequest || !strings.Contains(rebootBody, "development reboot was not requested") {
		t.Fatalf("development reboot = %d %s", rebootCode, rebootBody)
	}
}

// TestPathBUnclassifiedStopDoesNotArmRecovery matches the B3/B4 agent result:
// a development Stop that does not come back as reboot_required. A zero-filled
// regular idle file is not what this injects; the protocol reply is.
func TestPathBUnclassifiedStopDoesNotArmRecovery(t *testing.T) {
	for _, scenario := range []string{"stop-unclassified", "lost-stop"} {
		t.Run(scenario, func(t *testing.T) {
			harness := startPathB(t, scenario)
			if _, err := harness.service.LoadDevelopmentRBF(context.Background(), 3, strings.NewReader("rbf")); err != nil {
				t.Fatal(err)
			}

			started := time.Now()
			_, err := harness.service.Stop(context.Background())
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Fatalf("unclassified stop waited %s", elapsed)
			}
			var apiErr *protocol.APIError
			if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeMiSTerUnavailable || strings.Contains(apiErr.Message, "private") {
				t.Fatalf("unclassified stop = %v", err)
			}
			if harness.control.counts() != [2]int{1, 0} {
				t.Fatalf("stop/recover calls = %v", harness.control.counts())
			}
			if _, statErr := os.Stat(harness.marker); !os.IsNotExist(statErr) {
				t.Fatalf("unclassified stop started the reboot command: %v", statErr)
			}

			statusCode, statusBody := getAuthorized(t, harness.targetURL+"/v1/status", harness.token)
			if statusCode != http.StatusOK || !strings.Contains(statusBody, `"state":"failed"`) || !strings.Contains(statusBody, `"development":true`) || !strings.Contains(statusBody, string(protocol.CodeMiSTerUnavailable)) || strings.Contains(statusBody, `"recovery"`) || strings.Contains(statusBody, "private") {
				t.Fatalf("stored status = %d %s", statusCode, statusBody)
			}
			rebootCode, rebootBody := postEmpty(t, harness.targetURL+"/v1/development/reboot", harness.token)
			if rebootCode != http.StatusBadRequest || !strings.Contains(rebootBody, "development reboot was not requested") {
				t.Fatalf("development reboot = %d %s", rebootCode, rebootBody)
			}
		})
	}
}

func TestPathBRecoverIdleWithoutRecoveryPhaseDoesNotReboot(t *testing.T) {
	harness := startPathB(t, "recover-wrong-phase")
	if _, err := harness.service.LoadDevelopmentRBF(context.Background(), 3, strings.NewReader("rbf")); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	_, err := harness.service.Stop(context.Background())
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("wrong-phase recovery waited %s", elapsed)
	}
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("wrong-phase stop = %v", err)
	}
	if harness.control.counts() != [2]int{1, 1} {
		t.Fatalf("stop/recover calls = %v", harness.control.counts())
	}
	if _, statErr := os.Stat(harness.marker); !os.IsNotExist(statErr) {
		t.Fatalf("wrong-phase recover_idle started the reboot command: %v", statErr)
	}
}

type pathBHarness struct {
	service   *Service
	control   *pathBControl
	marker    string
	targetURL string
	token     string
}

func startPathB(t *testing.T, scenario string) pathBHarness {
	t.Helper()
	dir := t.TempDir()
	bootIDPath := filepath.Join(dir, "boot-id")
	marker := filepath.Join(dir, "reboot-marker")
	if err := os.WriteFile(bootIDPath, []byte("boot-before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rebootPath := filepath.Join(dir, "reboot")
	script := "#!/bin/sh\nprintf 'ran\\n' > " + shellQuote(marker) + "\nprintf 'boot-after\\n' > " + shellQuote(bootIDPath) + "\n"
	if err := os.WriteFile(rebootPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	control := &pathBControl{scenario: scenario, bootIDPath: bootIDPath, markerPath: marker}
	runtime := misterruntime.NewRuntime(control, bootIDPath, time.Millisecond, 20*time.Millisecond,
		misterruntime.WithDevelopmentRBFPath(filepath.Join(dir, "core.rbf")),
		misterruntime.WithRebootCommand(rebootPath))
	coordinator := agent.New(runtime, 2*time.Second, 2*time.Second)
	coordinator.Initialize(context.Background())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	const token = "test-token"
	targetHandler := httpapi.New(coordinator, token, version.Version, logger, httpapi.WithDevelopment(coordinator))
	targetServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if (request.URL.Path == "/v1/health" || request.URL.Path == "/v1/status") && control.bootChanged() {
			coordinator.Initialize(request.Context())
		}
		targetHandler.ServeHTTP(response, request)
	}))
	t.Cleanup(targetServer.Close)
	baseURL, err := url.Parse(targetServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := targetclient.NewClient(baseURL, token, targetServer.Client())
	service := newService(
		Config{
			Targets:        []TargetConfig{{Name: "dev", Enabled: true, Address: targetServer.URL, Agent: token}},
			SelectedTarget: "dev", RequestTimeout: 2 * time.Second, UploadTimeout: 2 * time.Second,
		},
		Paths{Staging: dir}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
	t.Cleanup(func() { _ = service.Close() })
	return pathBHarness{service: service, control: control, marker: marker, targetURL: targetServer.URL, token: token}
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

type pathBControl struct {
	mu                     sync.Mutex
	scenario               string
	loaded                 bool
	armed                  bool
	bootIDPath             string
	markerPath             string
	stopN                  int
	recoverN               int
	sawScriptDuringRecover bool
}

func (c *pathBControl) Protocol2Status(context.Context) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.statusLocked(), nil
}

func (c *pathBControl) Protocol2LoadDevelopmentRBF(context.Context, string) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	c.loaded = true
	c.mu.Unlock()
	return pathBRunning(), nil
}

func (c *pathBControl) Protocol2Stop(context.Context) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	c.stopN++
	scenario := c.scenario
	if scenario == "lost-stop" {
		c.mu.Unlock()
		return misterruntime.Protocol2Response{}, errors.New("runtime socket closed during stop")
	}
	if scenario == "stop-unclassified" {
		c.mu.Unlock()
		return pathBUnclassifiedStop(), nil
	}
	c.loaded = false
	if scenario != "stop-idle" {
		c.armed = true
	}
	response := c.statusLocked()
	c.mu.Unlock()
	return response, nil
}

func (c *pathBControl) Protocol2RecoverIdle(context.Context) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	c.recoverN++
	if _, err := os.Stat(c.markerPath); err == nil {
		c.sawScriptDuringRecover = true
	}
	phase := "recovery"
	if c.scenario == "recover-wrong-phase" {
		phase = "lifecycle"
	}
	c.mu.Unlock()
	return pathBReboot(phase), nil
}

func (c *pathBControl) statusLocked() misterruntime.Protocol2Response {
	if c.bootChangedLocked() {
		return pathBIdle()
	}
	if c.scenario == "reconciled" || c.armed {
		return pathBReboot("recovery")
	}
	if c.loaded {
		return pathBRunning()
	}
	return pathBIdle()
}

func (c *pathBControl) bootChanged() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bootChangedLocked()
}

func (c *pathBControl) bootChangedLocked() bool {
	bootID, err := os.ReadFile(c.bootIDPath)
	return err == nil && strings.TrimSpace(string(bootID)) == "boot-after"
}

func (c *pathBControl) counts() [2]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return [2]int{c.stopN, c.recoverN}
}

func (c *pathBControl) scriptSeenDuringRecover() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sawScriptDuringRecover
}

func pathBIdle() misterruntime.Protocol2Response {
	return misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test"}
}

func pathBRunning() misterruntime.Protocol2Response {
	generation := uint64(1)
	return misterruntime.Protocol2Response{
		Protocol: 2, OK: true, State: "running_development", Execution: "development",
		Generation: &generation, Version: "test",
	}
}

func pathBUnclassifiedStop() misterruntime.Protocol2Response {
	return misterruntime.Protocol2Response{
		Protocol: 2, OK: false, State: "starting", Execution: "none", Version: "test",
		Error: &misterruntime.Protocol2Error{Code: "program_failed", Message: "private program detail", Phase: "programming"},
	}
}

func pathBReboot(phase string) misterruntime.Protocol2Response {
	return misterruntime.Protocol2Response{
		Protocol: 2, OK: false, State: "reboot_required", Execution: "none", Version: "test",
		Error: &misterruntime.Protocol2Error{Code: "idle_failed", Message: "private idle detail", Phase: phase},
	}
}

func postEmpty(t *testing.T, endpoint, token string) (int, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	return doPathB(t, request)
}

func getAuthorized(t *testing.T, endpoint, token string) (int, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	return doPathB(t, request)
}

func doPathB(t *testing.T, request *http.Request) (int, string) {
	t.Helper()
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(body)
}
