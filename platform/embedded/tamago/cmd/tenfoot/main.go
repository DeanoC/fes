//go:build tamago

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"runtime"
	"time"

	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/agentapi"
	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/board"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/modelbackend"
	"github.com/DeanoC/fes/platform/embedded/tamago/runtimeadmission"
	"github.com/DeanoC/fes/platform/embedded/tamago/serial"
)

type bootConfig struct {
	Model         bool   `json:"model"`
	APIURL        string `json:"api_url"`
	APIHost       string `json:"api_host"`
	Input         string `json:"input"`
	AgentPort     uint16 `json:"agent_port"`
	AgentTargetID string `json:"agent_target_id"`
	AgentToken    string `json:"agent_token"`
	AgentOpen     bool   `json:"agent_allow_unauthenticated"`
	AgentMDNS     bool   `json:"agent_mdns"`
	AgentInstance string `json:"agent_instance"`
	AgentHost     string `json:"agent_host"`
}

// This Tenfoot frontend runs games on its configured remote host. It never
// acquires a local execution lease, even while its own HTTP server is up.
type remoteFrontendLease struct{}

func (remoteFrontendLease) Status() kitlease.Status {
	return kitlease.Status{State: "blocked", Reason: "this frontend only runs remote sessions"}
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
	if err := p.volume.ReplaceFile(context.Background(), "FES/PREFS.JSON", data); err != nil {
		return err
	}
	fmt.Printf("FES_PREFERENCES_SAVED bytes=%d\n", len(data))
	return nil
}

type framePlatform struct {
	model         *modelbackend.Backend
	frame         *modelbackend.Frame
	oldFrame      *modelbackend.Frame
	oldGeneration uint64
	lastModel     string
	device        *gfx.Software
	display       board.Display
	network       board.Network
	uart          input.UART
	uartIRQ       bool
	keyboard      *input.USBKeyboard
	lastFrame     time.Time
	lastState     string
	ready         bool
	seq           uint64
}

func (p *framePlatform) Next(ctx context.Context) (tenfoot.PlatformInput, error) {
	if p.network != nil {
		if err := p.network.Err(); err != nil {
			return tenfoot.PlatformInput{}, fmt.Errorf("network stopped: %w", err)
		}
	}
	// Yield to the network stack and UI workers on this cooperative runtime.
	delay := 50*time.Millisecond - time.Since(p.lastFrame)
	if delay > 0 {
		if p.uartIRQ {
			select {
			case <-ctx.Done():
				return tenfoot.PlatformInput{}, ctx.Err()
			case <-serial.Ready():
			case <-time.After(delay):
			}
		} else {
			select {
			case <-ctx.Done():
				return tenfoot.PlatformInput{}, ctx.Err()
			case <-time.After(delay):
			}
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
		if p.model != nil && p.model.View().Status.State == fabricmodel.Running && event.Key != "escape" && event.Key != "s" && event.Key != "backspace" {
			if err := p.model.Input(ctx, p.model.View().Status.CoreGeneration, event.Key); err != nil && !errors.Is(err, fabricmodel.ErrStale) {
				return tenfoot.PlatformInput{}, err
			}
			continue
		}
		fmt.Printf("FES_INPUT key=%s\n", event.Key)
		if command := tenfoot.CommandFromKey(event.Key); command != tenfoot.CmdNone {
			frame.Commands = append(frame.Commands, command)
		}
		frame.Text += event.Text
	}
	for _, command := range frame.Commands {
		if command == tenfoot.CmdQuit {
			return frame, nil
		}
	}
	if p.model != nil {
		view := p.model.View()
		if p.oldGeneration != 0 && view.Status.CoreGeneration != p.oldGeneration {
			if err := p.model.Input(ctx, p.oldGeneration, "delayed-right"); !errors.Is(err, fabricmodel.ErrStale) {
				return tenfoot.PlatformInput{}, fmt.Errorf("stale input accepted: %v", err)
			}
			fmt.Println("FES_MODEL_STALE_INPUT PASS")
			p.oldGeneration = 0
		}
		if view.Status.CoreGeneration != 0 {
			p.oldGeneration = view.Status.CoreGeneration
		}
		if p.oldFrame != nil && view.Status.DisplayGeneration != 0 && p.oldFrame.Generation != view.Status.DisplayGeneration {
			if err := p.model.FinishFrame(ctx, *p.oldFrame); !errors.Is(err, fabricmodel.ErrStale) {
				return tenfoot.PlatformInput{}, fmt.Errorf("stale frame accepted: %v", err)
			}
			fmt.Println("FES_MODEL_STALE_FRAME PASS")
			p.oldFrame = nil
		}
		token, err := p.model.BeginFrame(ctx)
		if err == nil {
			p.frame = &token
		} else if !errors.Is(err, fabricmodel.ErrBusy) {
			return tenfoot.PlatformInput{}, err
		}
	}
	return frame, ctx.Err()
}

func (p *framePlatform) Present(ctx context.Context, snapshot tenfoot.Snapshot) error {
	if p.model == nil || p.frame != nil {
		err := p.display.Present(p.device.Framebuffer())
		if err != nil {
			// A failed publication cannot finish the renderer's model grant. Close
			// must refuse it; neither this path nor a CPU copy proves physical drain.
			return err
		}
		if p.frame != nil {
			if finish := p.model.FinishFrame(ctx, *p.frame); finish != nil {
				return finish
			}
			if p.oldFrame == nil {
				copy := *p.frame
				p.oldFrame = &copy
			}
			p.frame = nil
		}
	}
	if p.model != nil {
		data, _ := json.Marshal(map[string]any{"focus": snapshot.Grid.Focus, "columns": snapshot.Grid.Columns, "session": snapshot.Session.State, "launch": snapshot.Launch.Phase, "launch_game": snapshot.Launch.GameID, "retry": snapshot.Session.RetryStop, "parked": snapshot.GPUParked, "backend": p.model.View()})
		if string(data) != p.lastModel {
			fmt.Printf("FES_PLAY_STATE %s\n", data)
			p.lastModel = string(data)
		}
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
		if p.network != nil {
			fmt.Printf("FES_NETWORK_STATS %+v\n", p.network.Stats())
			if irqs := p.network.IRQCount(); irqs > 0 {
				fmt.Printf("FES_NET_IRQ_STATS irqs=%d\n", irqs)
			}
		}
		if g := board.GIC(); g != nil {
			if wakeups := g.IdleWakeups(); wakeups > 0 {
				fmt.Printf("FES_GIC_IDLE_READY wakeups=%d\n", wakeups)
			}
		}
		fmt.Println("FES_TENFOOT_READY")
	}
	return nil
}

func run() (result error) {
	fmt.Printf("FES tenfoot native Go os=%s arch=%s EL=%d\n", runtime.GOOS, runtime.GOARCH, board.ExceptionLevel())
	if runtime.GOOS != "tamago" || runtime.GOARCH != "arm64" || board.ExceptionLevel() != 1 {
		return fmt.Errorf("unexpected boot contract")
	}
	g := board.GIC()
	gicActive := false
	if g != nil {
		if err := g.Init(); err != nil {
			if !errors.Is(err, gic.ErrUnsupported) {
				return fmt.Errorf("init GIC: %w", err)
			}
			fmt.Println("FES_GIC_IDLE enabled=false reason=unsupported")
		} else {
			if err := g.EnableIdle(); err != nil {
				return fmt.Errorf("enable WFI idle: %w", err)
			}
			gicActive = true
			defer func() {
				wakeups := g.IdleWakeups()
				g.DisableIdle()
				fmt.Printf("FES_GIC_IDLE_EXIT wakeups=%d\n", wakeups)
			}()
			fmt.Printf("FES_GIC_IDLE enabled=true redistributor=%#x lines=%d\n", g.Redistributor(), g.Lines())
			stop := make(chan struct{})
			done := make(chan struct{})
			go func() {
				defer close(done)
				g.Serve(stop)
			}()
			defer func() {
				close(stop)
				<-done
			}()
		}
	}
	frameMemory, err := board.InitDevices()
	if err != nil {
		return err
	}
	disk, err := board.OpenStorage()
	if err != nil {
		return err
	}
	type irqStorage interface {
		EnableIRQ(*gic.Controller) error
		DisableIRQ(*gic.Controller)
		IRQCount() uint64
	}
	if gicActive {
		if irqDisk, ok := disk.(irqStorage); ok {
			if err := irqDisk.EnableIRQ(g); err != nil {
				if !errors.Is(err, gic.ErrUnsupported) {
					return fmt.Errorf("enable storage IRQ: %w", err)
				}
			} else {
				defer func() {
					irqs := irqDisk.IRQCount()
					irqDisk.DisableIRQ(g)
					fmt.Printf("FES_STORAGE_IRQ_EXIT irqs=%d\n", irqs)
				}()
				fmt.Println("FES_STORAGE_IRQ enabled=true")
			}
		}
	}
	cachedDisk, err := block.NewCache(disk, block.CacheConfig{Capacity: 128})
	if err != nil {
		return err
	}
	volume, err := fat.Mount(cachedDisk)
	if err != nil {
		return err
	}
	if _, ok := board.WallTime(); ok {
		volume.SetClock(func() time.Time {
			t, _ := board.WallTime()
			return t
		})
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
	ctx := context.Background()
	var iface board.Network
	var backend *modelbackend.Backend
	var client *tenfoot.Client
	if config.Model {
		policyData, err := volume.ReadFile("FES/RUNTIME.JSON")
		if err != nil {
			return err
		}
		configData, err := volume.ReadFile("FES/PLAY.JSON")
		if err != nil {
			return err
		}
		if len(policyData) > 1<<20 || len(configData) > 1<<20 {
			return fmt.Errorf("model configuration too large")
		}
		var policy runtimeadmission.Policy
		var modelConfig modelbackend.Config
		if err = json.Unmarshal(policyData, &policy); err != nil {
			return err
		}
		if err = json.Unmarshal(configData, &modelConfig); err != nil {
			return err
		}
		backend, err = modelbackend.New(policy, modelConfig, volume.ReadFile, func(event modelbackend.Event) {
			data, _ := json.Marshal(event)
			fmt.Printf("FES_MODEL_EVENT %s\n", data)
		})
		if err != nil {
			return err
		}
		defer func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := backend.Close(closeCtx); err != nil {
				result = err
			} else {
				fmt.Println("FES_MODEL_CLOSED PASS")
			}
		}()
		client = tenfoot.NewClient("http://qemu-model.invalid", &http.Client{Timeout: 30 * time.Second, Transport: backend})
		fmt.Println("FES_MODEL_BACKEND_READY physical_activation=unavailable")
	} else {
		api, err := url.Parse(config.APIURL)
		if err != nil {
			return err
		}
		address, err := netip.ParseAddr(api.Hostname())
		if err != nil || !address.Is4() || api.Scheme != "http" || api.User != nil || api.Path != "" || api.RawQuery != "" || api.Fragment != "" {
			return fmt.Errorf("prototype API must be a plain HTTP IPv4 origin")
		}
		fmt.Printf("FES_BOOT_CONFIG api_url=%s\n", config.APIURL)
		iface, err = board.OpenNetwork(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("FES_NETWORK_READY address=%s\n", iface.Address())
		client = tenfoot.NewClient(config.APIURL, &http.Client{Timeout: 5 * time.Second, Transport: observedTransport{base: &http.Transport{MaxIdleConnsPerHost: 4}}})
		client.Client = client.Client.WithAPIHost(config.APIHost)
	}
	if gicActive && iface != nil {
		if err := iface.EnableIRQ(g); err != nil {
			if !errors.Is(err, gic.ErrUnsupported) {
				return fmt.Errorf("enable network RX IRQ: %w", err)
			}
		} else {
			defer func() {
				irqs := iface.IRQCount()
				iface.DisableIRQ(g)
				fmt.Printf("FES_NET_IRQ_EXIT irqs=%d\n", irqs)
			}()
			fmt.Println("FES_NET_IRQ enabled=true")
		}
	}
	if iface != nil && config.AgentPort != 0 {
		agentCtrl := agentapi.NewStateController(agentapi.StateControllerConfig{
			Board:   board.Name,
			Address: iface.Address(),
		})
		agentSvc, err := agentapi.NewService(agentapi.ServiceConfig{
			Endpoint:             iface,
			Port:                 config.AgentPort,
			TargetID:             config.AgentTargetID,
			Token:                config.AgentToken,
			AllowUnauthenticated: config.AgentOpen,
			AdvertiseMDNS:        config.AgentMDNS,
			MDNSInstance:         config.AgentInstance,
			MDNSHost:             config.AgentHost,
			Controller:           agentCtrl,
			KitLease:             remoteFrontendLease{},
			ReadOnly:             true,
			OnError: func(err error) {
				fmt.Printf("FES_AGENT_ERROR err=%q\n", err)
			},
		})
		if err != nil {
			return fmt.Errorf("configure target agent: %w", err)
		}
		if err := agentSvc.Start(ctx); err != nil {
			return fmt.Errorf("start target agent: %w", err)
		}
		defer func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer stopCancel()
			_ = agentSvc.Stop(stopCtx)
		}()
		fmt.Printf("FES_AGENT_READY addr=%s target_id=%s mdns=%t\n", agentSvc.Addr(), config.AgentTargetID, config.AgentMDNS)
	}
	var keyboard *input.USBKeyboard
	uartIRQ := false
	switch config.Input {
	case "usb":
		keyboard, err = board.OpenKeyboard()
		if err != nil {
			return err
		}
	case "uart":
		fmt.Println("FES_UART_INPUT_READY")
		if gicActive {
			if err := serial.EnableIRQ(g); err != nil {
				return fmt.Errorf("enable UART RX IRQ: %w", err)
			}
			uartIRQ = true
			defer func() {
				irqs := serial.IRQCount()
				dropped := serial.Dropped()
				serial.DisableIRQ(g)
				fmt.Printf("FES_UART_IRQ_EXIT irqs=%d dropped=%d\n", irqs, dropped)
			}()
			fmt.Println("FES_UART_IRQ enabled=true")
		}
	default:
		return fmt.Errorf("boot input must explicitly select usb or uart")
	}
	const width, height = 1280, 720
	device, err := gfx.NewSoftware(width, height)
	if err != nil {
		return err
	}
	defer device.Close()
	screen, err := board.OpenDisplay(width, height, frameMemory)
	if err != nil {
		return err
	}
	app := tenfoot.NewApp(client, width, height, 400)
	if err := app.SetPreferenceStore(preferenceStore{volume}); err != nil {
		return fmt.Errorf("load preferences: %w", err)
	}
	fmt.Printf("FES_PREFERENCES_LOADED layout=%s\n", app.Snapshot().Grid.Mode)
	return tenfoot.RunPlatform(ctx, app, device, &framePlatform{device: device, display: screen, network: iface, uartIRQ: uartIRQ, keyboard: keyboard, model: backend})
}

func main() {
	if err := run(); err != nil {
		panic(err)
	}
	fmt.Println("FES_TENFOOT_EXIT PASS")
}
