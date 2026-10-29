//go:build tamago

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"runtime"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/qemuvirt"
	"github.com/DeanoC/fes/platform/embedded/tamago/display"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

type bootConfig struct {
	APIURL  string `json:"api_url"`
	APIHost string `json:"api_host"`
	Input   string `json:"input"`
}

type observedTransport struct{ base http.RoundTripper }

func (t observedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err != nil {
		fmt.Printf("FES_HTTP method=%s path=%s error=%q\n", request.Method, request.URL.Path, err)
	} else {
		fmt.Printf("FES_HTTP method=%s path=%s status=%d\n", request.Method, request.URL.Path, response.StatusCode)
	}
	return response, err
}

type preferenceStore struct{ volume *fat.Volume }

func (p preferenceStore) ReadPreferences() ([]byte, error) {
	return p.volume.ReadFile("FES/PREFS.JSON")
}

func (p preferenceStore) WritePreferences(data []byte) error {
	if err := p.volume.WriteFile("FES/PREFS.JSON", data); err != nil {
		return err
	}
	fmt.Printf("FES_PREFERENCES_SAVED bytes=%d\n", len(data))
	return nil
}

type framePlatform struct {
	device    *gfx.Software
	display   *display.RAMFB
	network   *network.Interface
	uart      input.UART
	keyboard  *input.USBKeyboard
	lastFrame time.Time
	lastState string
	ready     bool
	seq       uint64
}

func (p *framePlatform) Next(ctx context.Context) (tenfoot.PlatformInput, error) {
	if err := p.network.Err(); err != nil {
		return tenfoot.PlatformInput{}, fmt.Errorf("network stopped: %w", err)
	}
	// Yield to the network stack and UI workers on this cooperative runtime.
	delay := 50*time.Millisecond - time.Since(p.lastFrame)
	if delay > 0 {
		select {
		case <-ctx.Done():
			return tenfoot.PlatformInput{}, ctx.Err()
		case <-time.After(delay):
		}
	} else {
		runtime.Gosched()
	}
	now := time.Now()
	p.lastFrame = now
	frame := tenfoot.PlatformInput{At: now}
	var events []input.Event
	if p.keyboard != nil {
		var err error
		events, err = p.keyboard.Poll()
		if err != nil {
			return tenfoot.PlatformInput{}, fmt.Errorf("USB keyboard: %w", err)
		}
	} else {
		events = p.uart.Poll(now)
	}
	for _, event := range events {
		fmt.Printf("FES_INPUT key=%s\n", event.Key)
		if command := tenfoot.CommandFromKey(event.Key); command != tenfoot.CmdNone {
			frame.Commands = append(frame.Commands, command)
		}
		frame.Text += event.Text
	}
	return frame, ctx.Err()
}

func (p *framePlatform) Present(_ context.Context, snapshot tenfoot.Snapshot) error {
	if err := p.display.Present(p.device.Framebuffer()); err != nil {
		return err
	}
	p.seq++
	state := fmt.Sprintf("titles=%d focus=%d layout=%s loading=%t error=%q", len(snapshot.Games), snapshot.Grid.Focus, snapshot.Grid.Mode, snapshot.Loading, snapshot.LoadErr)
	if state != p.lastState {
		fmt.Printf("FES_FRAME seq=%d %s\n", p.seq, state)
		p.lastState = state
	}
	if !p.ready && !snapshot.Loading && snapshot.LoadErr == "" && len(snapshot.Games) > 0 {
		p.ready = true
		ids := make([]string, len(snapshot.Games))
		for i, game := range snapshot.Games {
			ids[i] = game.ID
		}
		catalog, _ := json.Marshal(ids)
		fmt.Printf("FES_CATALOG ids=%s\n", catalog)
		fmt.Printf("FES_NETWORK_STATS %+v\n", p.network.Statistics())
		fmt.Println("FES_TENFOOT_READY")
	}
	return nil
}

func run() error {
	fmt.Printf("FES tenfoot native Go os=%s arch=%s EL=%d\n", runtime.GOOS, runtime.GOARCH, qemuvirt.ExceptionLevel())
	if runtime.GOOS != "tamago" || runtime.GOARCH != "arm64" || qemuvirt.ExceptionLevel() != 1 {
		return fmt.Errorf("unexpected boot contract")
	}
	frameMemory, err := qemuvirt.InitDevices()
	if err != nil {
		return err
	}
	disk, err := block.ScanVirtIO()
	if err != nil {
		return err
	}
	volume, err := fat.Mount(disk)
	if err != nil {
		return err
	}
	fmt.Printf("FES_STORAGE_READY sectors=%d\n", disk.SectorCount())
	data, err := volume.ReadFile("FES/BOOT.JSON")
	if err != nil {
		return err
	}
	var config bootConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}
	api, err := url.Parse(config.APIURL)
	if err != nil {
		return err
	}
	address, err := netip.ParseAddr(api.Hostname())
	if err != nil || !address.Is4() || api.Scheme != "http" || api.User != nil || api.Path != "" || api.RawQuery != "" || api.Fragment != "" {
		return fmt.Errorf("prototype API must be a plain HTTP IPv4 origin")
	}
	fmt.Printf("FES_BOOT_CONFIG api_url=%s\n", config.APIURL)
	ctx := context.Background()
	iface, err := network.Open(ctx, network.Config{})
	if err != nil {
		return err
	}
	fmt.Printf("FES_NETWORK_READY address=%s\n", iface.Address())
	var keyboard *input.USBKeyboard
	switch config.Input {
	case "usb":
		base, err := qemuvirt.OpenUSBPCI()
		if err != nil {
			return err
		}
		host, err := usb.OpenXHCI(base)
		if err != nil {
			return err
		}
		fmt.Printf("FES_USB_READY device=%+v configuration=%+v\n", host.DeviceInfo(), host.Configuration())
		keyboard, err = input.NewUSBKeyboard(host)
		if err != nil {
			return err
		}
	case "uart":
		fmt.Println("FES_UART_INPUT_READY")
	default:
		return fmt.Errorf("boot input must explicitly select usb or uart")
	}
	const width, height = 1280, 720
	device, err := gfx.NewSoftware(width, height)
	if err != nil {
		return err
	}
	defer device.Close()
	screen, err := display.Open(width, height, frameMemory)
	if err != nil {
		return err
	}
	client := tenfoot.NewClient(config.APIURL, &http.Client{Timeout: 5 * time.Second, Transport: observedTransport{base: &http.Transport{MaxIdleConnsPerHost: 4}}})
	client.Client = client.Client.WithAPIHost(config.APIHost)
	app := tenfoot.NewApp(client, width, height, 400)
	if err := app.SetPreferenceStore(preferenceStore{volume}); err != nil {
		return fmt.Errorf("load preferences: %w", err)
	}
	fmt.Printf("FES_PREFERENCES_LOADED layout=%s\n", app.Snapshot().Grid.Mode)
	return tenfoot.RunPlatform(ctx, app, device, &framePlatform{device: device, display: screen, network: iface, keyboard: keyboard})
}

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}
