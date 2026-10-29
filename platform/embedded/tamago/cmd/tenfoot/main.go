//go:build tamago

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"runtime"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/qemuvirt"
)

// The base-system experiment has no network/FPGA backend yet. This explicit
// read-only service fixture exercises the real App through its host API client.
type fixtureTransport struct{}

func (fixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	status := http.StatusOK
	var value any
	switch request.URL.Path {
	case "/api/v1/games":
		value = map[string]any{"games": []hostclient.Game{
			{ID: "pong", Title: "FES Pong", System: "pong", RootOnline: true, State: "available"},
			{ID: "zx81", Title: "Sinclair ZX81", System: "zx81", RootOnline: true, State: "available"},
			{ID: "coleco", Title: "ColecoVision", System: "coleco", RootOnline: true, State: "available"},
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
		value = map[string]any{"error": "unimplemented read-only fixture endpoint"}
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
}

type framePlatform struct {
	app      *tenfoot.App
	device   *gfx.Software
	stage    int
	captured int
	focus    []int
	hashes   [][32]byte
	started  time.Time
}

func (p *framePlatform) Next(ctx context.Context) (tenfoot.PlatformInput, error) {
	select {
	case <-ctx.Done():
		return tenfoot.PlatformInput{}, ctx.Err()
	case <-time.After(time.Millisecond):
	}
	if time.Since(p.started) > 15*time.Second {
		return tenfoot.PlatformInput{}, fmt.Errorf("tenfoot fixture did not complete")
	}
	input := tenfoot.PlatformInput{At: time.Now()}
	snapshot := p.app.Snapshot()
	if snapshot.Loading || len(snapshot.Games) != 3 {
		return input, nil
	}
	switch p.stage {
	case 0:
		p.stage = 1
	case 1:
		input.Commands = []tenfoot.Command{tenfoot.CmdRight}
		p.stage = 2
	case 2:
		input.Commands = []tenfoot.Command{tenfoot.CmdLeft}
		p.stage = 3
	default:
		return tenfoot.PlatformInput{}, io.EOF
	}
	return input, nil
}

func (p *framePlatform) Present(_ context.Context, snapshot tenfoot.Snapshot) error {
	if p.stage == 0 || snapshot.Loading || len(snapshot.Games) != 3 || p.captured >= p.stage {
		return nil
	}
	p.focus = append(p.focus, snapshot.Grid.Focus)
	p.hashes = append(p.hashes, sha256.Sum256(p.device.Framebuffer().Pix))
	var frame bytes.Buffer
	if err := png.Encode(&frame, p.device.Framebuffer()); err != nil {
		return err
	}
	data := frame.Bytes()
	fmt.Printf("FES_FRAME_BEGIN stage=%d focus=%d bytes=%d sha256=%x\n", p.stage, snapshot.Grid.Focus, len(data), sha256.Sum256(data))
	fmt.Println(base64.StdEncoding.EncodeToString(data))
	fmt.Println("FES_FRAME_END")
	p.captured++
	return nil
}

func main() {
	fmt.Printf("FES tenfoot native Go os=%s arch=%s EL=%d fixture=read-only network=absent\n",
		runtime.GOOS, runtime.GOARCH, qemuvirt.ExceptionLevel())
	if runtime.GOOS != "tamago" || runtime.GOARCH != "arm64" || qemuvirt.ExceptionLevel() != 1 {
		panic("unexpected boot contract")
	}
	const width, height = 1280, 720
	device, err := gfx.NewSoftware(width, height)
	if err != nil {
		panic(err)
	}
	defer device.Close()
	client := tenfoot.NewClient("http://embedded.invalid", &http.Client{Transport: fixtureTransport{}})
	app := tenfoot.NewApp(client, width, height, 10)
	app.SetAttractDisabled(true)
	app.SetPrefsPath("/tmp/tenfoot.json")
	platform := &framePlatform{app: app, device: device, started: time.Now()}
	if err := tenfoot.RunPlatform(context.Background(), app, device, platform); err != nil {
		panic(err)
	}
	if len(platform.focus) != 3 || platform.focus[0] != 0 || platform.focus[1] != 1 || platform.focus[2] != 0 ||
		platform.hashes[0] == platform.hashes[1] {
		panic("native tenfoot navigation did not change the rendered frame")
	}
	fmt.Println("FES_NATIVE_TENFOOT PASS frames=3 focus=0,1,0 services=fixture input=scripted")
}
