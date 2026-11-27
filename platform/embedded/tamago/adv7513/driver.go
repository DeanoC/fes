// SPDX-License-Identifier: GPL-3.0-or-later

package adv7513

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"

	"github.com/DeanoC/fes/platform/embedded/tamago/i2c"
)

const defaultMaxEDIDPolls = 10_000

// ErrInvalidConfig indicates a missing i2c.Bus or invalid ADV7513 address/timeout setting.
var ErrInvalidConfig = errors.New("adv7513: invalid configuration")

// Config configures an ADV7513 transmitter instance.
type Config struct {
	MainAddr     uint8 // defaults to MainMapAddr7Bit (0x39)
	EDIDAddr     uint8 // defaults to DefaultEDIDMapAddr (0x3f)
	MaxEDIDPolls int   // defaults to 10_000
}

// LinkStatus summarizes live ADV7513 HPD, monitor sense, PLL lock, and VIC status.
type LinkStatus struct {
	HPD          bool
	MonitorSense bool
	PoweredUp    bool
	PLLLocked    bool
	EDIDReady    bool
	DetectedVIC  uint8
	RawStatus    uint8
	RawPower     uint8
	RawPLL       uint8
	RawInt0      uint8
}

// Device manages an ADV7513 HDMI TX chip over an i2c.Bus.
type Device struct {
	mu           sync.Mutex
	bus          i2c.Bus
	mainAddr     uint8
	edidAddr     uint8
	maxEDIDPolls int
}

// New validates cfg and wraps an ADV7513 attached to bus.
func New(bus i2c.Bus, cfg Config) (*Device, error) {
	if bus == nil {
		return nil, fmt.Errorf("%w: nil I2C bus", ErrInvalidConfig)
	}
	if cfg.MainAddr == 0 {
		cfg.MainAddr = MainMapAddr7Bit
	}
	if cfg.EDIDAddr == 0 {
		cfg.EDIDAddr = DefaultEDIDMapAddr
	}
	if cfg.MaxEDIDPolls == 0 {
		cfg.MaxEDIDPolls = defaultMaxEDIDPolls
	}
	if cfg.MaxEDIDPolls < 1 {
		return nil, fmt.Errorf("%w: MaxEDIDPolls %d", ErrInvalidConfig, cfg.MaxEDIDPolls)
	}
	return &Device{
		bus:          bus,
		mainAddr:     cfg.MainAddr,
		edidAddr:     cfg.EDIDAddr,
		maxEDIDPolls: cfg.MaxEDIDPolls,
	}, nil
}

// ChipRevision reads ADV7513 register 0x00 (expected 0x13).
func (d *Device) ChipRevision(ctx context.Context) (uint8, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.bus.ReadRegByte(ctx, d.mainAddr, RegChipRevision)
}

// Status reads power, HPD/sense, PLL lock, interrupt, and detected VIC registers.
func (d *Device) Status(ctx context.Context) (LinkStatus, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	rawPower, err := d.bus.ReadRegByte(ctx, d.mainAddr, RegPower)
	if err != nil {
		return LinkStatus{}, err
	}
	rawStatus, err := d.bus.ReadRegByte(ctx, d.mainAddr, RegStatus)
	if err != nil {
		return LinkStatus{}, err
	}
	rawPLL, err := d.bus.ReadRegByte(ctx, d.mainAddr, RegPllStatus)
	if err != nil {
		return LinkStatus{}, err
	}
	rawInt0, err := d.bus.ReadRegByte(ctx, d.mainAddr, RegInt0)
	if err != nil {
		return LinkStatus{}, err
	}
	rawVIC, err := d.bus.ReadRegByte(ctx, d.mainAddr, RegVicDetected)
	if err != nil {
		return LinkStatus{}, err
	}

	return LinkStatus{
		HPD:          rawStatus&StatusHPD != 0,
		MonitorSense: rawStatus&StatusMonitorSense != 0,
		PoweredUp:    rawPower&PowerDown == 0,
		PLLLocked:    rawPLL&PllLocked != 0,
		EDIDReady:    rawInt0&Int0EDIDReady != 0,
		DetectedVIC:  rawVIC >> VicShift,
		RawStatus:    rawStatus,
		RawPower:     rawPower,
		RawPLL:       rawPLL,
		RawInt0:      rawInt0,
	}, nil
}

// PowerUp clears PD (0x41) and optionally forces HPD override (0xd6 = 0xc0).
func (d *Device) PowerUp(ctx context.Context, forceHPD bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if forceHPD {
		if err := d.bus.WriteRegByte(ctx, d.mainAddr, RegPower2, Power2HpdAlwaysHigh); err != nil {
			return err
		}
	}
	return d.bus.WriteRegByte(ctx, d.mainAddr, RegPower, PowerUp)
}

// PowerDown puts the ADV7513 transmitter into standby power-down mode.
func (d *Device) PowerDown(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.bus.WriteRegByte(ctx, d.mainAddr, RegPower, PowerDown|PowerUp)
}

// ApplyWrites executes a slice of RegisterWrite pairs sequentially on the main map.
func (d *Device) ApplyWrites(ctx context.Context, writes []RegisterWrite) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.applyWritesLocked(ctx, writes)
}

func (d *Device) applyWritesLocked(ctx context.Context, writes []RegisterWrite) error {
	for _, w := range writes {
		if err := d.bus.WriteRegByte(ctx, d.mainAddr, w.Address, w.Value); err != nil {
			return err
		}
	}
	return nil
}

// InitMenu720p60 runs Menu720p60Initialization followed by Menu720p60Mode.
func (d *Device) InitMenu720p60(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.applyWritesLocked(ctx, Menu720p60Initialization()); err != nil {
		return err
	}
	return d.applyWritesLocked(ctx, Menu720p60Mode())
}

// ConfigureAudio48k programs 48 kHz I2S PCM + Audio InfoFrame with audio sample
// packets disabled during configuration, optionally re-enabling packets afterward.
func (d *Device) ConfigureAudio48k(ctx context.Context, enablePackets bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.bus.WriteRegByte(ctx, d.mainAddr, RegPacketEnable1, PacketsVideoOnly); err != nil {
		return err
	}
	if err := d.applyWritesLocked(ctx, ApplicationAudio48k()); err != nil {
		return err
	}
	if enablePackets {
		return d.bus.WriteRegByte(ctx, d.mainAddr, RegPacketEnable1, PacketsVideoAudio)
	}
	return nil
}

// ReadEDID triggers an ADV7513 DDC/EDID fetch, waits for INT0 EDID_READY, reads
// block 0 (plus CEA-861 block 1 if ExtensionCount >= 1), and returns parsed EDID + raw bytes.
func (d *Device) ReadEDID(ctx context.Context) (*EDID, []byte, error) {
	if ctx == nil {
		return nil, nil, fmt.Errorf("%w: context required", ErrInvalidConfig)
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.bus.WriteRegByte(ctx, d.mainAddr, RegEDIDAddr, d.edidAddr<<1); err != nil {
		return nil, nil, err
	}
	if err := d.applyWritesLocked(ctx, HDMIWake()); err != nil {
		return nil, nil, err
	}

	ready := false
	for i := 0; i < d.maxEDIDPolls; i++ {
		val, err := d.bus.ReadRegByte(ctx, d.mainAddr, RegInt0)
		if err != nil {
			return nil, nil, err
		}
		if val&Int0EDIDReady != 0 {
			ready = true
			break
		}
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		runtime.Gosched()
	}
	if !ready {
		return nil, nil, fmt.Errorf("%w: EDID ready bit (0x96[2]) not set", i2c.ErrTimeout)
	}

	raw := make([]byte, 128)
	if err := d.bus.ReadReg(ctx, d.edidAddr, 0x00, raw); err != nil {
		return nil, nil, err
	}
	if raw[126] >= 1 {
		ext := make([]byte, 128)
		if err := d.bus.ReadReg(ctx, d.edidAddr, 0x80, ext); err != nil {
			return nil, nil, err
		}
		raw = append(raw, ext...)
	}
	parsed, err := ParseEDID(raw)
	if err != nil {
		return nil, nil, err
	}
	return parsed, raw, nil
}
