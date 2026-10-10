package kitlauncher

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/remoteinput"
)

type fakeLocalCore struct {
	cores                []localcores.Core
	launchID, launchPath string
	launchErr            error
	stops                int
}

func (f *fakeLocalCore) List(context.Context) ([]localcores.Core, error) { return f.cores, nil }
func (f *fakeLocalCore) LaunchROM(_ context.Context, id, path string) error {
	f.launchID, f.launchPath = id, path
	return f.launchErr
}
func (f *fakeLocalCore) Status(context.Context) (localcores.RunStatus, error) {
	return localcores.RunStatus{Phase: "running", Running: true}, nil
}
func (f *fakeLocalCore) Stop(context.Context) error { f.stops++; return nil }

func smsRow() hostclient.Game {
	return hostclient.Game{ID: "data", Title: "Data Storm", System: "sms", State: "available", RootOnline: true}
}
func pressA() remoteinput.Event { e, _ := remoteinput.NormalizeGamepad("a", true); return e }

func TestHostlessAttractArmsLocalSMS(t *testing.T) {
	row := smsRow()
	m := Model{Games: []hostclient.Game{row}, Catalog: []hostclient.Game{row}, LocalPlayEnabled: true}
	m.setLocalAttract([]hostclient.Game{row})
	m.SetAttractIdle(time.Millisecond)
	t0 := time.Unix(0, 0)
	m.Tick(t0)
	m.Tick(t0.Add(time.Second))
	if !m.AttractActive {
		t.Fatal("local attract stayed blocked while the host was down")
	}
	if action := m.Input(pressA(), t0.Add(2*time.Second)); action != "local-launch" {
		t.Fatalf("attract launch %q message %q", action, m.Message)
	}
}

func TestHostlessLocalSMSPaths(t *testing.T) {
	row := smsRow()
	for _, screen := range []string{"browse", "detail", "attract"} {
		m := Model{Games: []hostclient.Game{row}, Catalog: []hostclient.Game{row}, LocalPlayEnabled: true}
		now := time.Now()
		switch screen {
		case "detail":
			m.DetailOpen = true
		case "attract":
			m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{{GameID: row.ID, Backdrop: strings.Repeat("a", 64), Launchable: false}}})
			m.AttractActive = true
		}
		if action := m.Input(pressA(), now); action != "local-launch" {
			t.Fatalf("%s: action %q", screen, action)
		}
	}
	f := &fakeLocalCore{cores: []localcores.Core{{CoreID: "fes.sms", PackageID: strings.Repeat("a", 64)}}}
	resolve := func(context.Context, string) (string, error) { return "/media/fat/games/sms/datastorm.sms", nil }
	if err := dispatchLocalAction(context.Background(), f, resolve, []hostclient.Game{row}, "local-launch", row.ID); err != nil {
		t.Fatal(err)
	}
	if f.launchID != strings.Repeat("a", 64) || f.launchPath != "/media/fat/games/sms/datastorm.sms" {
		t.Fatalf("launch %q %q", f.launchID, f.launchPath)
	}
	if err := dispatchLocalAction(context.Background(), f, resolve, nil, "stop", row.ID); err != nil || f.stops != 1 {
		t.Fatalf("stop %d %v", f.stops, err)
	}
}

func TestLocalLaunchFailuresAndHostRoute(t *testing.T) {
	row := smsRow()
	m := Model{Games: []hostclient.Game{row}, LocalPlayEnabled: true}
	m.Games[0].System = "snes"
	if action := m.Input(pressA(), time.Now()); action != "" || m.Message == "" {
		t.Fatalf("action %q message %q", action, m.Message)
	}
	f := &fakeLocalCore{}
	resolve := func(context.Context, string) (string, error) { return "/media/fat/games/sms/datastorm.sms", nil }
	if msg := localCoreMessage(launchLocalGame(context.Background(), f, resolve, []hostclient.Game{row}, row.ID)); msg != "The Master System core is not installed." {
		t.Fatal(msg)
	}
	f.cores = []localcores.Core{{CoreID: "fes.sms", PackageID: strings.Repeat("a", 64)}}
	f.launchErr = localcores.ErrInUse
	if msg := localCoreMessage(launchLocalGame(context.Background(), f, resolve, []hostclient.Game{row}, row.ID)); msg != "Kit is in use" {
		t.Fatal(msg)
	}
	if msg := localCoreMessage(localcores.ErrUnavailable); msg != "Kit local control is unavailable" {
		t.Fatal(msg)
	}
	if msg := localCoreMessage(launchLocalGame(context.Background(), f, func(context.Context, string) (string, error) { return "", errors.New("missing") }, []hostclient.Game{row}, row.ID)); msg != "Cartridge is missing" {
		t.Fatal(msg)
	}
	if !errors.Is(f.launchErr, localcores.ErrInUse) {
		t.Fatal("lost typed error")
	}
	host := Model{Games: []hostclient.Game{{ID: "pong", State: "available", RootOnline: true, Launchable: true}}, Connected: true, TargetReady: true, LocalPlayEnabled: true}
	if action := host.Input(pressA(), time.Now()); action != "launch" {
		t.Fatal(action)
	}
}
