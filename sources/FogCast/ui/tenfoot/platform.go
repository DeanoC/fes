package tenfoot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
)

// Presenter retains the existing tenfoot renderer's textures and text caches
// across frames. The caller owns the Device and its physical presentation.
// This adapter does not open a window, device, socket, or framebuffer.
type Presenter struct {
	device   gfx.Device
	textures map[string]gpuTexture
	labels   map[string]gpuTexture
	parked   bool
}

func NewPresenter(device gfx.Device) (*Presenter, error) {
	if device == nil {
		return nil, errors.New("tenfoot presenter needs a graphics device")
	}
	return &Presenter{
		device: device, textures: map[string]gpuTexture{}, labels: map[string]gpuTexture{},
	}, nil
}

// Draw uses the same render path as the native launcher. Device.Present is
// called by that path; a software device leaves its pixels ready for the
// platform's separate scanout or transport operation.
func (p *Presenter) Draw(snapshot Snapshot) error {
	if p == nil || p.device == nil {
		return errors.New("tenfoot presenter is closed")
	}
	p.parked = presentFrame(p.device, snapshot, p.textures, p.labels, p.parked)
	return nil
}

// Close releases resources this presenter created, without closing the
// caller's device. It is safe to call more than once.
func (p *Presenter) Close() {
	if p == nil || p.device == nil {
		return
	}
	destroyTextures(p.device, p.textures)
	destroyTextures(p.device, p.labels)
	p.device = nil
}

// PlatformInput is one frame's logical input and time. Commands are already
// mapped keyboard/gamepad edge actions, not raw USB reports. Platforms which
// need held-button repeat can call App.Press/Release before returning Next.
type PlatformInput struct {
	At       time.Time
	Commands []Command
	Text     string
}

// Platform is the scheduling and physical-output boundary of the launcher.
// Next owns frame pacing and returns io.EOF to finish. Present consumes the
// completed device frame without changing the supplied snapshot. Both methods
// must honor cancellation; drivers and the transport live outside tenfoot.
type Platform interface {
	Next(context.Context) (PlatformInput, error)
	Present(context.Context, Snapshot) error
}

// RunPlatform runs the real App with an injected platform and graphics device.
// The App's Client supplies host/library services, including on embedded
// targets where an http.RoundTripper bridges to an in-process service or a
// network stack. This function never constructs the native kit's local server.
// It starts and stops the App, but the caller owns the graphics device.
func RunPlatform(ctx context.Context, app *App, device gfx.Device, platform Platform) error {
	if app == nil || platform == nil {
		return errors.New("tenfoot platform needs an app and platform")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	presenter, err := NewPresenter(device)
	if err != nil {
		return err
	}
	defer presenter.Close()
	app.Start(ctx)
	defer app.Stop()
	var previous time.Time
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		input, err := platform.Next(ctx)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if input.At.IsZero() || (!previous.IsZero() && input.At.Before(previous)) {
			return errors.New("tenfoot platform time must be nonzero and monotonic")
		}
		previous = input.At
		for _, command := range input.Commands {
			if command == CmdQuit {
				return nil
			}
			if command < CmdNone || command > CmdDetails {
				return fmt.Errorf("invalid tenfoot platform command %d", command)
			}
			app.HandleCommand(command, input.At)
		}
		if input.Text != "" {
			app.TypeText(input.Text, input.At)
		}
		if app.Tick(input.At) == CmdQuit {
			return nil
		}
		snapshot := app.Snapshot()
		if err := presenter.Draw(snapshot); err != nil {
			return err
		}
		if err := platform.Present(ctx, snapshot); err != nil {
			return err
		}
	}
}
