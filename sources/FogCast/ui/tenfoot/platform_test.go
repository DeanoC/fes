package tenfoot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/gfx"
)

type platformTransport func(*http.Request) (*http.Response, error)

func (f platformTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func platformFixtureClient() *Client {
	return NewClient("http://embedded.invalid", &http.Client{Transport: platformTransport(func(request *http.Request) (*http.Response, error) {
		status := http.StatusOK
		var value any
		switch request.URL.Path {
		case "/api/v1/games":
			value = map[string]any{"games": []hostclient.Game{
				{ID: "one", Title: "First game", System: "pong", RootOnline: true, Launchable: false, State: "available"},
				{ID: "two", Title: "Second game", System: "pong", RootOnline: true, Launchable: false, State: "available"},
			}}
		case "/api/v1/platforms":
			value = map[string]any{"platforms": []any{}}
		case "/api/v1/library/collections":
			value = map[string]any{"collections": []any{}}
		case "/api/v1/library/edition-preferences":
			value = map[string]any{"preferences": []any{}}
		case "/api/v1/library/facets":
			value = map[string]any{"genres": []any{}, "years": []any{}}
		case "/api/v1/session":
			value = map[string]any{"state": "idle"}
		default:
			status = http.StatusNotImplemented
			value = map[string]any{"error": "not implemented by read-only platform fixture"}
		}
		body, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
	})})
}

type navigationPlatform struct {
	app       *App
	device    *gfx.Software
	stage     int
	focused   []int
	frames    int
	sawPixels bool
}

func (p *navigationPlatform) Next(ctx context.Context) (PlatformInput, error) {
	select {
	case <-ctx.Done():
		return PlatformInput{}, ctx.Err()
	case <-time.After(time.Millisecond):
	}
	in := PlatformInput{At: time.Now()}
	snapshot := p.app.Snapshot()
	if snapshot.Loading || len(snapshot.Games) != 2 {
		return in, nil
	}
	switch p.stage {
	case 0:
		p.stage++
	case 1:
		in.Commands = []Command{CmdRight}
		p.stage++
	case 2:
		in.Commands = []Command{CmdLeft}
		p.stage++
	default:
		return PlatformInput{}, io.EOF
	}
	return in, nil
}

func (p *navigationPlatform) Present(_ context.Context, snapshot Snapshot) error {
	p.frames++
	if len(snapshot.Games) == 2 {
		p.focused = append(p.focused, snapshot.Grid.Focus)
	}
	for _, value := range p.device.Framebuffer().Pix {
		if value != 0 {
			p.sawPixels = true
			break
		}
	}
	return nil
}

func TestRunPlatformRendersAndNavigatesRealApp(t *testing.T) {
	device, err := gfx.NewSoftware(640, 480)
	if err != nil {
		t.Fatal(err)
	}
	defer device.Close()
	app := NewApp(platformFixtureClient(), 640, 480, 10)
	app.SetAttractDisabled(true)
	app.SetPrefsPath(t.TempDir() + "/preferences.json")
	platform := &navigationPlatform{app: app, device: device}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := RunPlatform(ctx, app, device, platform); err != nil {
		t.Fatal(err)
	}
	if platform.frames < 3 || !platform.sawPixels {
		t.Fatalf("presentation frames=%d, pixels=%v", platform.frames, platform.sawPixels)
	}
	if got := platform.focused; len(got) < 3 || got[len(got)-3] != 0 || got[len(got)-2] != 1 || got[len(got)-1] != 0 {
		t.Fatalf("focus sequence = %v", got)
	}
}

func TestPresenterMatchesNativeRenderAndRetainsDevice(t *testing.T) {
	app := libraryRenderApp(t, 640, 480, true)
	native, _ := gfx.NewSoftware(640, 480)
	defer native.Close()
	embedded, _ := gfx.NewSoftware(640, 480)
	defer embedded.Close()
	presenter, err := NewPresenter(embedded)
	if err != nil {
		t.Fatal(err)
	}
	defer presenter.Close()
	textures, labels := map[string]gpuTexture{}, map[string]gpuTexture{}
	defer destroyTextures(native, textures)
	defer destroyTextures(native, labels)
	for _, command := range []Command{CmdNone, CmdRight, CmdDown, CmdLeft} {
		app.HandleCommand(command, time.Now())
		snapshot := app.Snapshot()
		presentFrame(native, snapshot, textures, labels, false)
		if err := presenter.Draw(snapshot); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(native.Framebuffer().Pix, embedded.Framebuffer().Pix) {
			t.Fatalf("render differs after command %v", command)
		}
	}
	presenter.Close()
	presenter.Close()
	if err := presenter.Draw(app.Snapshot()); err == nil {
		t.Fatal("closed presenter accepted a frame")
	}
	embedded.Clear(gfx.RGB(1, 2, 3))
	if embedded.Framebuffer().Pix[0] != 1 {
		t.Fatal("presenter closed caller-owned device")
	}
}

type invalidTimePlatform struct{}

func (invalidTimePlatform) Next(context.Context) (PlatformInput, error) {
	return PlatformInput{}, nil
}
func (invalidTimePlatform) Present(context.Context, Snapshot) error {
	return errors.New("invalid time reached presentation")
}

func TestRunPlatformRejectsMissingTime(t *testing.T) {
	device, _ := gfx.NewSoftware(640, 480)
	defer device.Close()
	app := NewApp(platformFixtureClient(), 640, 480, 10)
	if err := RunPlatform(t.Context(), app, device, invalidTimePlatform{}); err == nil {
		t.Fatal("zero platform time was accepted")
	}
}

type scriptedPlatform struct {
	inputs      []PlatformInput
	nextError   error
	submitError error
	presents    int
}

func (p *scriptedPlatform) Next(context.Context) (PlatformInput, error) {
	if len(p.inputs) == 0 {
		if p.nextError != nil {
			return PlatformInput{}, p.nextError
		}
		return PlatformInput{}, io.EOF
	}
	next := p.inputs[0]
	p.inputs = p.inputs[1:]
	return next, nil
}

func (p *scriptedPlatform) Present(context.Context, Snapshot) error {
	p.presents++
	return p.submitError
}

func TestRunPlatformExitPathsStopAppAndKeepDevice(t *testing.T) {
	stamp := time.Unix(10000, 0)
	failure := errors.New("platform failure")
	for _, tc := range []struct {
		name     string
		platform scriptedPlatform
		cancel   bool
		want     error
		presents int
	}{
		{name: "next failure", platform: scriptedPlatform{nextError: failure}, want: failure},
		{name: "present failure", platform: scriptedPlatform{inputs: []PlatformInput{{At: stamp}}, submitError: failure}, want: failure, presents: 1},
		{name: "quit", platform: scriptedPlatform{inputs: []PlatformInput{{At: stamp, Commands: []Command{CmdQuit}}}}},
		{name: "cancelled", cancel: true, want: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			device, _ := gfx.NewSoftware(640, 480)
			defer device.Close()
			app := NewApp(platformFixtureClient(), 640, 480, 10)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			err := RunPlatform(ctx, app, device, &tc.platform)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want %v", err, tc.want)
			}
			if tc.platform.presents != tc.presents {
				t.Fatalf("presented=%d, want %d", tc.platform.presents, tc.presents)
			}
			if app.ctx == nil || app.ctx.Err() == nil {
				t.Fatal("platform exit did not cancel app workers")
			}
			device.Clear(gfx.RGB(3, 2, 1))
			if device.Framebuffer().Pix[0] != 3 {
				t.Fatal("platform closed the caller's device")
			}
		})
	}
}

func TestRunPlatformRejectsRegressingTimeBeforeSecondPresent(t *testing.T) {
	device, _ := gfx.NewSoftware(640, 480)
	defer device.Close()
	app := NewApp(platformFixtureClient(), 640, 480, 10)
	stamp := time.Unix(10000, 0)
	platform := &scriptedPlatform{inputs: []PlatformInput{{At: stamp}, {At: stamp.Add(-time.Millisecond)}}}
	if err := RunPlatform(t.Context(), app, device, platform); err == nil {
		t.Fatal("regressing time was accepted")
	}
	if platform.presents != 1 {
		t.Fatalf("presented=%d, want 1", platform.presents)
	}
}
