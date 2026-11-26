//go:build linux

package tenfoot

import (
	"context"
	"os"
	"time"

	"github.com/DeanoC/FogCast/internal/bluez"
)

type linuxBluetoothController struct {
	client *bluez.Client
}

func newLinuxBluetoothController(ctx context.Context, cfg bluez.Config) *linuxBluetoothController {
	return &linuxBluetoothController{client: bluez.Start(ctx, cfg)}
}

func startLinuxBluetooth(ctx context.Context, opts Options) *linuxBluetoothController {
	if opts.Smoke {
		return nil
	}
	if !opts.KitHost {
		if _, err := os.Stat(bluez.DefaultSystemSocket); err != nil {
			return nil
		}
	}
	return newLinuxBluetoothController(ctx, bluez.Config{SocketPath: bluez.DefaultSystemSocket})
}

func (c *linuxBluetoothController) BluetoothStatus() BluetoothStatus {
	if c == nil || c.client == nil {
		return BluetoothStatus{}
	}
	st := c.client.Status()
	out := BluetoothStatus{
		DongleConnected:  st.AdapterConnected,
		DongleName:       st.AdapterName,
		PairingActive:    st.PairingActive,
		PairingRemaining: st.PairingRemaining,
		ConnectedCount:   st.ConnectedCount,
	}
	if len(st.Devices) > 0 {
		out.Devices = make([]BluetoothDevice, len(st.Devices))
		for i, d := range st.Devices {
			out.Devices[i] = BluetoothDevice{
				Address:   d.Address,
				Name:      d.Name,
				Kind:      d.Kind,
				Connected: d.Connected,
			}
		}
	}
	return out
}

func (c *linuxBluetoothController) StartBluetoothPairing(duration time.Duration) error {
	return c.client.StartPairing(duration)
}

func (c *linuxBluetoothController) StopBluetoothPairing() error {
	return c.client.StopPairing()
}

func (c *linuxBluetoothController) ForgetBluetoothDevice(address string) error {
	return c.client.ForgetDevice(address)
}

func (c *linuxBluetoothController) Close() {
	if c != nil && c.client != nil {
		c.client.Close()
	}
}
