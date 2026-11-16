// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"context"
	"testing"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/usbcontrol"
)

type closeFirmware struct {
	*ownedFirmware
	beforeStop func()
}

func (f closeFirmware) Invoke(a [7]uint64) ([4]uint64, error) {
	if a[1] == uint64(g.FesEmbeddedUsbStop) {
		f.beforeStop()
	}
	return f.ownedFirmware.Invoke(a)
}

type closingSource interface {
	Close(context.Context) error
	PausePolling(context.Context) error
}

func TestOwnedCloseDrainsBeforeFirmwareStop(t *testing.T) {
	for _, gamepad := range []bool{false, true} {
		for _, state := range []string{"idle", "pending", "already-paused"} {
			name := "keyboard/" + state
			if gamepad {
				name = "gamepad/" + state
			}
			t.Run(name, func(t *testing.T) {
				hw := newController()
				if gamepad {
					hw = gamepadController(t)
				}
				hw.reports = [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}}
				if gamepad {
					hw.reports = [][]byte{{128, 128, 1, 0}}
				}
				var host *Host
				fw := closeFirmware{ownedFirmware: &ownedFirmware{t: t, controller: hw, gamepad: gamepad}, beforeStop: func() {
					if host == nil || !host.pollingPaused || host.pending.active || host.healthStage != 0 || hw.regs[hcchar]&channelEnable != 0 || len(hw.rx) != 0 || len(hw.words) != 0 {
						t.Fatal("firmware Stop preceded report/channel drain")
					}
				}}
				ctx := context.Background()
				client, err := usbcontrol.New(ctx, fw, 17)
				if err != nil {
					t.Fatal(err)
				}
				var source closingSource
				var poll func() ([]byte, error)
				if gamepad {
					s, e := usbcontrol.OpenGamepad(ctx, client, func() (input.USBGamepadReports, error) {
						h, e := OpenGamepad(hw, Config{PHYClockHz: 60000000})
						if h != nil {
							host = h.host
						}
						return h, e
					})
					source, poll, err = s, s.PollGamepad, e
				} else {
					s, e := usbcontrol.Open(ctx, client, func() (input.USBReports, error) { host, err = Open(hw, Config{PHYClockHz: 60000000}); return host, err })
					source, poll, err = s, s.PollKeyboard, e
				}
				if err != nil {
					t.Fatal(err)
				}
				if state != "idle" {
					if r, e := poll(); e != nil || r != nil || !host.pending.active {
						t.Fatal("missing pending packet", r, e)
					}
				}
				if state == "already-paused" {
					if e := source.PausePolling(ctx); e != nil {
						t.Fatal(e)
					}
				}
				if err := source.Close(ctx); err != nil || fw.stops != 1 {
					t.Fatal(err, fw.stops)
				}
				accesses := hw.accesses
				if err := source.Close(ctx); err != nil || fw.stops != 1 || hw.accesses != accesses {
					t.Fatal("close replayed drain/stop", err)
				}
				if _, err := poll(); err == nil || hw.accesses != accesses {
					t.Fatal("closed source touched hardware", err)
				}
			})
		}
	}
}

func TestOwnedCloseDrainFailureNeverRestartsDriver(t *testing.T) {
	for _, mode := range []string{"stuck", "halt", "dma"} {
		t.Run(mode, func(t *testing.T) {
			hw := newController()
			fw := &ownedFirmware{t: t, controller: hw}
			ctx := context.Background()
			c, err := usbcontrol.New(ctx, fw, 17)
			if err != nil {
				t.Fatal(err)
			}
			var host *Host
			s, err := usbcontrol.Open(ctx, c, func() (input.USBReports, error) { host, err = Open(hw, Config{PHYClockHz: 60000000}); return host, err })
			if err != nil {
				t.Fatal(err)
			}
			hw.mode = "stuck"
			if _, err = s.PollKeyboard(); err != nil || !host.pending.active {
				t.Fatal(err)
			}
			if mode == "dma" {
				hw.regs[gahb] |= 1 << 5
			} else {
				hw.mode = mode
				hw.frozen = true
			}
			if err = s.Close(ctx); err == nil || host.fault == nil || fw.stops != 1 {
				t.Fatal("failed drain supplied a successful close", err)
			}
			accesses, starts := hw.accesses, len(hw.starts)
			if _, valid := c.LastReceipt(); valid {
				t.Fatal("failed drain revived lease")
			}
			if err = s.Close(ctx); err == nil || fw.stops != 1 || hw.accesses != accesses {
				t.Fatal("failed close replayed", err)
			}
			if _, err = s.PollKeyboard(); err == nil || hw.accesses != accesses || len(hw.starts) != starts {
				t.Fatal("failed source polled/restarted", err)
			}
		})
	}
}
