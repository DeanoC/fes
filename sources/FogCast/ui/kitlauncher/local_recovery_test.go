package kitlauncher

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/remoteinput"
)

type recoveryCore struct {
	mu           sync.Mutex
	status       localcores.RunStatus
	statuses     []localcores.RunStatus
	reads        int
	launchStatus localcores.RunStatus
	statusErr    error
	launchErr    error
	stopErr      error
	launches     int
	stops        int
}

func (f *recoveryCore) List(context.Context) ([]localcores.Core, error) {
	return []localcores.Core{{CoreID: "fes.sms", PackageID: strings.Repeat("a", 64)}}, nil
}
func (f *recoveryCore) LaunchROM(context.Context, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.launches++
	if f.launchStatus.Phase != "" {
		f.status = f.launchStatus
	} else {
		f.status = localcores.RunStatus{Phase: "running", Running: true}
	}
	return f.launchErr
}
func (f *recoveryCore) Status(context.Context) (localcores.RunStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if len(f.statuses) > 0 {
		f.status = f.statuses[0]
		f.statuses = f.statuses[1:]
	}
	return f.status, f.statusErr
}
func (f *recoveryCore) Stop(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	f.status = localcores.RunStatus{Phase: "idle"}
	return f.stopErr
}
func (f *recoveryCore) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.launches, f.stops
}

func (f *recoveryCore) statusReads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

func recoveryTestClient(t *testing.T, f *recoveryCore) (*Client, *atomic.Int64) {
	t.Helper()
	hostStops := &atomic.Int64{}
	transport := recoveryRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/v1/session/stop" {
			hostStops.Add(1)
		}
		return &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader("host down")), Header: make(http.Header), Request: r}, nil
	})
	client := NewClient(Config{API: "http://host.invalid", MenuDisplay: true})
	client.HTTP = &http.Client{Transport: transport}
	client.Library = hostclient.NewClient(client.config.API, client.HTTP)
	client.SetLocalCores(f)
	client.localCore = func(context.Context) (bool, error) { return true, nil }
	client.localInputPath = filepath.Join(t.TempDir(), "missing-input.sock")
	client.localDial = func(string, string, time.Duration) (net.Conn, error) { return nil, errors.New("input unavailable") }
	return client, hostStops
}

type recoveryRoundTrip func(*http.Request) (*http.Response, error)

func (f recoveryRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func recoveryChordPad() *scriptPad {
	selectPress, _ := remoteinput.NormalizeGamepad("select", true)
	startPress, _ := remoteinput.NormalizeGamepad("start", true)
	return &scriptPad{events: []remoteinput.Event{selectPress, startPress}}
}

type recoveryStatusPad struct {
	*scriptPad
	core  *recoveryCore
	reads int
}

func (p *recoveryStatusPad) Poll() ([]remoteinput.Event, error) {
	if p.core.statusReads() >= p.reads {
		p.arm.Store(true)
	}
	return p.scriptPad.Poll()
}

type recoveryLaunchPad struct {
	ready          atomic.Bool
	core           *recoveryCore
	launched       bool
	presses        int
	chordSent      bool
	launchAt       time.Time
	chordOnRunning bool
}

func (p *recoveryLaunchPad) Poll() ([]remoteinput.Event, error) {
	if !p.ready.Load() {
		return nil, nil
	}
	if !p.launched {
		// The shell boots on the platform wheel: the first A opens the
		// shelf, the second launches the focused title.
		p.presses++
		p.launched = p.presses >= 2
		a, _ := remoteinput.NormalizeGamepad("a", true)
		up, _ := remoteinput.NormalizeGamepad("a", false)
		return []remoteinput.Event{a, up}, nil
	}
	launches, _ := p.core.counts()
	if launches == 0 {
		return nil, nil
	}
	if p.chordOnRunning {
		p.core.mu.Lock()
		running := p.core.status.Phase == "running"
		p.core.mu.Unlock()
		if !running {
			return nil, nil
		}
	}
	if p.launchAt.IsZero() {
		p.launchAt = time.Now()
	}
	if p.chordSent || time.Since(p.launchAt) < 200*time.Millisecond {
		return nil, nil
	}
	p.chordSent = true
	selectPress, _ := remoteinput.NormalizeGamepad("select", true)
	startPress, _ := remoteinput.NormalizeGamepad("start", true)
	return []remoteinput.Event{selectPress, startPress}, nil
}
func (*recoveryLaunchPad) Close() error { return nil }

func TestRunAdoptsLocalRunAtStartupAndStopsIt(t *testing.T) {
	f := &recoveryCore{status: localcores.RunStatus{Phase: "running", Running: true}, stopErr: errors.New("response lost")}
	client, hostStops := recoveryTestClient(t, f)
	pad := recoveryChordPad()
	var firstPaint, paused, resumed atomic.Bool
	client.SetMenuDisplayHandoff(func(context.Context) error {
		if !firstPaint.Load() {
			t.Error("status blocked first paint")
		}
		paused.Store(true)
		pad.arm.Store(true)
		return nil
	}, func() { resumed.Store(true) })
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := Run(ctx, client, func(Model) {
		firstPaint.Store(true)
		if _, stops := f.counts(); stops > 0 && resumed.Load() {
			cancel()
		}
	}, func() (Pad, error) { return pad, nil }); err != nil {
		t.Fatal(err)
	}
	_, stops := f.counts()
	if !firstPaint.Load() || !paused.Load() || stops != 1 || !resumed.Load() || hostStops.Load() != 0 {
		t.Fatalf("paint=%t paused=%t local stops=%d resumed=%t host stops=%d", firstPaint.Load(), paused.Load(), stops, resumed.Load(), hostStops.Load())
	}
}

func TestRunAdoptsLaunchingAtStartupAndStopsAfterRunning(t *testing.T) {
	f := &recoveryCore{statuses: []localcores.RunStatus{{Phase: "launching"}, {Phase: "running", Running: true}}}
	client, hostStops := recoveryTestClient(t, f)
	pad := &recoveryStatusPad{scriptPad: recoveryChordPad(), core: f, reads: 2}
	var pauses atomic.Int64
	var resumed atomic.Bool
	client.SetMenuDisplayHandoff(func(context.Context) error { pauses.Add(1); return nil }, func() { resumed.Store(true) })
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := Run(ctx, client, func(Model) {
		_, stops := f.counts()
		if stops > 0 && resumed.Load() {
			cancel()
		}
	}, func() (Pad, error) { return pad, nil }); err != nil {
		t.Fatal(err)
	}
	_, stops := f.counts()
	if f.statusReads() < 2 || pauses.Load() == 0 || stops != 1 || hostStops.Load() != 0 {
		t.Fatalf("status reads=%d pauses=%d local stops=%d host stops=%d", f.statusReads(), pauses.Load(), stops, hostStops.Load())
	}
}

func TestRunStopsAdoptedLaunchWhileStillLaunching(t *testing.T) {
	f := &recoveryCore{status: localcores.RunStatus{Phase: "launching"}}
	client, hostStops := recoveryTestClient(t, f)
	pad := recoveryChordPad()
	var resumed atomic.Bool
	client.SetMenuDisplayHandoff(func(context.Context) error { pad.arm.Store(true); return nil }, func() { resumed.Store(true) })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := Run(ctx, client, func(Model) {
		_, stops := f.counts()
		if stops > 0 && resumed.Load() {
			cancel()
		}
	}, func() (Pad, error) { return pad, nil }); err != nil {
		t.Fatal(err)
	}
	_, stops := f.counts()
	if stops != 1 || hostStops.Load() != 0 {
		t.Fatalf("local stops=%d host stops=%d", stops, hostStops.Load())
	}
}

func TestRunLaunchingReturnsToMenuOnlyAtIdle(t *testing.T) {
	for _, phases := range [][]string{{"launching", "idle"}, {"launching", "stopping", "idle"}} {
		t.Run(strings.Join(phases, "-"), func(t *testing.T) {
			f := &recoveryCore{}
			for _, phase := range phases {
				f.statuses = append(f.statuses, localcores.RunStatus{Phase: phase})
			}
			client, hostStops := recoveryTestClient(t, f)
			var pauses, resumes atomic.Int64
			client.SetMenuDisplayHandoff(func(context.Context) error { pauses.Add(1); return nil }, func() { resumes.Add(1) })
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			if err := Run(ctx, client, func(m Model) {
				if resumes.Load() > 0 {
					if m.Session.State != "idle" {
						t.Error("menu resumed without idle session")
					}
					cancel()
				}
			}, func() (Pad, error) { return recoveryChordPad(), nil }); err != nil {
				t.Fatal(err)
			}
			_, stops := f.counts()
			if f.statusReads() < len(phases) || pauses.Load() != 1 || resumes.Load() != 1 || stops != 0 || hostStops.Load() != 0 {
				t.Fatalf("reads=%d pauses=%d resumes=%d local stops=%d host stops=%d", f.statusReads(), pauses.Load(), resumes.Load(), stops, hostStops.Load())
			}
		})
	}
}

func TestRunAdoptsFailedLocalLaunchAfterStatus(t *testing.T) {
	probe, err := net.Listen("unix", filepath.Join(t.TempDir(), "probe.sock"))
	if errors.Is(err, syscall.EPERM) {
		t.Skip("sandbox denies local catalog socket binding")
	}
	if err != nil {
		t.Fatal(err)
	}
	_ = probe.Close()
	f := &recoveryCore{
		status:       localcores.RunStatus{Phase: "idle"},
		statuses:     []localcores.RunStatus{{Phase: "idle"}, {Phase: "launching"}, {Phase: "running", Running: true}},
		launchStatus: localcores.RunStatus{Phase: "launching"},
		launchErr:    errors.New("response lost"),
	}
	client, hostStops := recoveryTestClient(t, f)
	dir := t.TempDir()
	root := filepath.Join(dir, "sms")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Data Storm 1.00.sms"), []byte("data-storm-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(config, []byte("base_url = \"http://127.0.0.1:1\"\ntoken = \"synthetic-token\"\nrequest_timeout_seconds = 1\nupload_timeout_seconds = 2\n\n[[libraries]]\nid = \"sms-main\"\nsystem = \"sms\"\nroot = \""+root+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client.SetCatalogConfig(config)
	// No core is bound until the (ambiguous) launch loads one, so the
	// first presses browse instead of feeding the local input socket.
	client.localCore = func(context.Context) (bool, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.status.Phase == "launching" || f.status.Running, nil
	}
	pad := &recoveryLaunchPad{core: f, chordOnRunning: true}
	var pauses atomic.Int64
	var resumed atomic.Bool
	client.SetMenuDisplayHandoff(func(context.Context) error { pauses.Add(1); return nil }, func() { resumed.Store(true) })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := Run(ctx, client, func(m Model) {
		if len(m.Games) > 0 {
			pad.ready.Store(true)
		}
		_, stops := f.counts()
		if stops > 0 && resumed.Load() {
			cancel()
		}
	}, func() (Pad, error) { return pad, nil }); err != nil {
		t.Fatal(err)
	}
	launches, stops := f.counts()
	if launches != 1 || stops != 1 || pauses.Load() == 0 || hostStops.Load() != 0 {
		t.Fatalf("launches=%d local stops=%d pauses=%d host stops=%d", launches, stops, pauses.Load(), hostStops.Load())
	}
}

func TestRunStartupStatusErrorDoesNotArmLocalStop(t *testing.T) {
	f := &recoveryCore{status: localcores.RunStatus{Phase: "running", Running: true}, statusErr: errors.New("unavailable")}
	client, hostStops := recoveryTestClient(t, f)
	pad := recoveryChordPad()
	pad.arm.Store(true)
	var pauses atomic.Int64
	client.SetMenuDisplayHandoff(func(context.Context) error { pauses.Add(1); return nil }, func() {})
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if err := Run(ctx, client, func(Model) {}, func() (Pad, error) { return pad, nil }); err != nil {
		t.Fatal(err)
	}
	_, stops := f.counts()
	if stops != 0 || pauses.Load() != 0 || hostStops.Load() != 0 {
		t.Fatalf("local stops=%d pauses=%d host stops=%d", stops, pauses.Load(), hostStops.Load())
	}
}

// A local run started outside the shell after its startup check (for example
// over the local control socket) is adopted once the core binds, so
// Select+Start still stops it and the menu resumes.
func TestRunAdoptsExternallyStartedLocalRunAndStopsIt(t *testing.T) {
	f := &recoveryCore{statuses: []localcores.RunStatus{{Phase: "idle"}, {Phase: "running", Running: true}}}
	client, hostStops := recoveryTestClient(t, f)
	pad := recoveryChordPad()
	var paused, resumed atomic.Bool
	client.SetMenuDisplayHandoff(func(context.Context) error {
		paused.Store(true)
		// Arm after adoption pauses the menu, so the asynchronous status read
		// cannot emit the one-shot chord while the session is still idle.
		pad.arm.Store(true)
		return nil
	}, func() { resumed.Store(true) })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := Run(ctx, client, func(Model) {
		if _, stops := f.counts(); stops > 0 && resumed.Load() {
			cancel()
		}
	}, func() (Pad, error) { return pad, nil }); err != nil {
		t.Fatal(err)
	}
	launches, stops := f.counts()
	if launches != 0 || !paused.Load() || stops != 1 || !resumed.Load() || hostStops.Load() != 0 {
		t.Fatalf("launches=%d paused=%t local stops=%d resumed=%t host stops=%d", launches, paused.Load(), stops, resumed.Load(), hostStops.Load())
	}
}

func TestRunRetriesExternalAdoptionAfterMenuPauseFailure(t *testing.T) {
	f := &recoveryCore{statuses: []localcores.RunStatus{{Phase: "idle"}, {Phase: "running", Running: true}}}
	client, _ := recoveryTestClient(t, f)
	var pauses, presents atomic.Int64
	var presentsAtAdoption atomic.Int64
	var activeBeforePauseSuccess atomic.Bool
	var presentedAfterPauseFailure atomic.Bool
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	client.SetMenuDisplayHandoff(func(context.Context) error {
		if pauses.Add(1) == 1 {
			return errors.New("pause timed out")
		}
		presentsAtAdoption.Store(presents.Load())
		return nil
	}, func() {})
	if err := Run(ctx, client, func(m Model) {
		presents.Add(1)
		if pauses.Load() == 1 && m.Session.State != "active" {
			presentedAfterPauseFailure.Store(true)
		}
		if m.Session.State == "active" {
			if pauses.Load() < 2 {
				activeBeforePauseSuccess.Store(true)
			}
			cancel()
		}
	}, func() (Pad, error) { return &scriptPad{}, nil }); err != nil {
		t.Fatal(err)
	}
	if pauses.Load() != 2 || activeBeforePauseSuccess.Load() || !presentedAfterPauseFailure.Load() {
		t.Fatalf("pauses=%d active before pause succeeded=%t presented after failed pause=%t", pauses.Load(), activeBeforePauseSuccess.Load(), presentedAfterPauseFailure.Load())
	}
	if presents.Load() != presentsAtAdoption.Load() {
		t.Fatalf("menu presented after adoption: at pause=%d after=%d", presentsAtAdoption.Load(), presents.Load())
	}
}
