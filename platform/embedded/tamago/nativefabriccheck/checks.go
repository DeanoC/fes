// SPDX-License-Identifier: GPL-3.0-or-later
// Package nativefabriccheck exercises the native endpoint with synthetic hooks
// on both host Go and TamaGo/QEMU. It supplies no physical board service.
package nativefabriccheck

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
)

type simulator struct {
	regs                        [20]uint32
	contained, control          bool
	region                      *nativefabric.Region
	address                     uint64
	release, programs, disables int
	fault                       string
}

var identity = embeddedcontrol.Identity{ABI: 1, Build: [4]uint32{0x1234, 0x5678, 0x9abc, 0xdef0}}

func (s *simulator) init() {
	s.regs = [20]uint32{}
	s.regs[g.FesEmbeddedRegMagic/4] = g.FesEmbeddedMagic
	s.regs[g.FesEmbeddedRegVersion/4] = g.FesEmbeddedVersion
	s.regs[g.FesEmbeddedRegAbi/4] = identity.ABI
	for i, v := range identity.Build {
		s.regs[g.FesEmbeddedRegBuild0/4+uint32(i)] = v
	}
}
func (s *simulator) Resolve(i fabricmodel.Identity) (embeddedcontrol.Identity, error) {
	id := identity
	if s.fault == "identity" && i.Build == 2 {
		id.Build[3] ^= 1
	}
	return id, nil
}
func (s *simulator) Read32(o uint32) (uint32, error) {
	if !s.control || o%4 != 0 || o >= 80 {
		return 0, errors.New("invalid CSR read")
	}
	return s.regs[o/4], nil
}
func (s *simulator) Write32(o, v uint32) error {
	if !s.control || o%4 != 0 || o >= 80 {
		return errors.New("invalid CSR write")
	}
	if o != g.FesEmbeddedRegCommand {
		s.regs[o/4] = v
		return nil
	}
	switch v {
	case g.FesEmbeddedCommandDisable:
		if s.regs[g.FesEmbeddedRegStatus/4] != g.FesEmbeddedStatusEnabled {
			return errors.New("disable not enabled")
		}
		s.disables++
		s.regs[g.FesEmbeddedRegStatus/4] = g.FesEmbeddedStatusDraining
		if s.regs[g.FesEmbeddedRegPending/4] == 0 {
			s.drain()
		}
	case g.FesEmbeddedCommandEnable:
		if !s.contained || s.regs[g.FesEmbeddedRegStatus/4] != 0 {
			return errors.New("enable outside containment")
		}
		s.regs[g.FesEmbeddedRegGenerationLo/4] = s.regs[g.FesEmbeddedRegRequestGenerationLo/4]
		s.regs[g.FesEmbeddedRegGenerationHi/4] = s.regs[g.FesEmbeddedRegRequestGenerationHi/4]
		s.regs[g.FesEmbeddedRegStatus/4] = g.FesEmbeddedStatusEnabled
	default:
		return errors.New("invalid command")
	}
	return nil
}
func (s *simulator) drain() {
	s.regs[g.FesEmbeddedRegPending/4] = 0
	s.regs[g.FesEmbeddedRegStatus/4] = g.FesEmbeddedStatusDrained
	s.regs[g.FesEmbeddedRegAckGenerationLo/4] = s.regs[g.FesEmbeddedRegGenerationLo/4]
	s.regs[g.FesEmbeddedRegAckGenerationHi/4] = s.regs[g.FesEmbeddedRegGenerationHi/4]
	s.regs[g.FesEmbeddedRegAckCookieLo/4] = s.regs[g.FesEmbeddedRegRequestCookieLo/4]
	s.regs[g.FesEmbeddedRegAckCookieHi/4] = s.regs[g.FesEmbeddedRegRequestCookieHi/4]
}
func (s *simulator) Invoke(r [7]uint64) ([4]uint64, error) {
	switch r[0] {
	case uint64(g.FesEmbeddedFirmwareQuery):
		if s.fault == "firmware" {
			return [4]uint64{}, nil
		}
		return [4]uint64{0, uint64(g.FesEmbeddedFirmwareMagic), uint64(g.FesEmbeddedFirmwareVersion), uint64(g.FesEmbeddedFirmwareFeatures)}, nil
	case 0xc2000001:
		if !s.contained || s.control {
			return [4]uint64{}, errors.New("start outside containment")
		}
		s.programs++
		return [4]uint64{}, nil
	case 0x42000002:
		if s.region == nil || s.address != 0 {
			return [4]uint64{}, errors.New("invalid write")
		}
		s.address = r[1] - 0x80000000
		return [4]uint64{1, 0, 0, 0}, nil
	case 0xc2000003:
		if s.fault == "foreign" {
			return [4]uint64{0, 0x17000000, 0, 0}, nil
		}
		if s.address == 0 {
			return [4]uint64{}, errors.New("unexpected poll")
		}
		a := s.address
		s.address = 0
		return [4]uint64{0, a, 0, 0}, nil
	case 0xc2000004:
		if s.address != 0 {
			return [4]uint64{}, errors.New("early isdone")
		}
		s.init()
		return [4]uint64{}, nil
	}
	return [4]uint64{}, errors.New("unexpected SMC")
}
func (s *simulator) Reserve(data []byte) (*nativefabric.Region, error) {
	if s.region != nil {
		return nil, errors.New("region already held")
	}
	r := &nativefabric.Region{Address: 0x96000000, Bytes: bytes.Clone(data)}
	r.Release = func() error { s.region = nil; return nil }
	s.region = r
	return r, nil
}
func (s *simulator) Save(fabricmodel.Identity) (bool, error) { return true, nil }
func (s *simulator) Contain() (bool, error)                  { s.contained = true; s.control = false; return true, nil }
func (s *simulator) ControlOnly() (bool, error) {
	if !s.contained {
		return false, nil
	}
	s.control = true
	return true, nil
}
func (s *simulator) ReleaseData() (bool, error) {
	if !s.contained || !s.control || s.regs[g.FesEmbeddedRegStatus/4] != g.FesEmbeddedStatusEnabled {
		return false, nil
	}
	s.contained = false
	s.release++
	return true, nil
}
func image(value byte, id string, idle bool) *fabricmodel.Image {
	data := bytes.Repeat([]byte{value}, 128)
	i, err := fabricmodel.Prepare(id, data, sha256.Sum256(data), fabricmodel.Identity{ABI: 1, Core: 1, Build: uint64(value)}, idle)
	if err != nil {
		panic(err)
	}
	return i
}

func packageImage() *fabricmodel.Image {
	data := bytes.Repeat([]byte{2}, 128)
	digest := sha256.Sum256(data)
	pkg := fabricmodel.PackageIdentity{PackageID: strings.Repeat("a", 64), CoreID: "test.core",
		Platform: "test-board", Device: "test-device", ProgrammingProfile: "test-profile", ABI: "test.abi", Major: 1,
		BuildID: "0000def000009abc0000567800001234", ProgrammedSHA256: fmt.Sprintf("%x", digest)}
	i, err := fabricmodel.Prepare("model.game", data, digest, fabricmodel.Identity{ABI: 1, Core: 1, Build: 2, Package: pkg}, false)
	if err != nil {
		panic(err)
	}
	return i
}
func runCase(fault string) error {
	idle := image(1, "model.idle", true)
	c, err := fabricmodel.New(idle, time.Second)
	if err != nil {
		return err
	}
	s := &simulator{control: true}
	s.init()
	options := nativefabric.Options{DRAMBase: 0x80000000, ChunkBytes: 32}
	if fault == "package" {
		options.IdentityProfiles = []nativefabric.IdentityProfile{{Platform: "test-board", Device: "test-device", ProgrammingProfile: "test-profile", ABI: "test.abi", Major: 1, ControlABI: 1}}
	}
	endpoint, err := nativefabric.New(s, options)
	if err != nil {
		return err
	}
	now := time.Duration(0)
	drive := func() error {
		var first error
		for range 500 {
			now += time.Millisecond
			e := endpoint.Step(c, now)
			if e != nil && first == nil {
				first = e
			}
			if c.Status().State != fabricmodel.Changing {
				return first
			}
		}
		return errors.New("step budget exceeded")
	}
	if err = c.Boot(now, time.Second); err != nil {
		return err
	}
	if err = drive(); err != nil {
		return err
	}
	s.fault = fault
	if fault == "" {
		s.regs[g.FesEmbeddedRegPending/4] = 1
	}
	target := image(2, "model.game", false)
	if fault == "package" || fault == "unmapped" {
		target = packageImage()
		before := c.Status()
		if err = endpoint.Preflight(target); fault == "unmapped" {
			if err == nil || c.Status() != before || s.programs != 1 || s.disables != 0 {
				return errors.New("unmapped package retired the old owner")
			}
			return nil
		} else if err != nil {
			return err
		}
	}
	if err = c.Load(target, now, now+time.Second); err != nil {
		return err
	}
	if fault == "" {
		for range 5 {
			now += time.Millisecond
			if err = endpoint.Step(c, now); err != nil {
				return err
			}
		}
		if s.programs != 1 || s.disables != 1 || c.Status().State != fabricmodel.Changing {
			return errors.New("reader was not held for drain")
		}
		s.drain()
	}
	err = drive()
	switch fault {
	case "firmware":
		if err == nil || s.disables != 0 || c.Status().State != fabricmodel.RebootRequired {
			return errors.New("unknown firmware changed reader")
		}
		return nil
	case "foreign":
		if err == nil || s.region == nil || s.release != 1 || c.Status().State != fabricmodel.RebootRequired {
			return errors.New("foreign completion lost ownership")
		}
		return nil
	case "identity":
		if !errors.Is(err, embeddedcontrol.ErrIdentity) || s.release != 2 || s.programs != 3 || c.Status().State != fabricmodel.Idle {
			return errors.New("identity recovery failed")
		}
		return nil
	}
	if err != nil || c.Status().State != fabricmodel.Running {
		return fmt.Errorf("launch %v", err)
	}
	if err = c.Stop(now, now+time.Second); err != nil {
		return err
	}
	if err = drive(); err != nil {
		return err
	}
	if c.Status().State != fabricmodel.Idle || s.release != 3 || s.programs != 3 {
		return errors.New("return did not restore idle")
	}
	return nil
}

// Run executes the same controlled software cases on host and TamaGo.
func Run(report func(string, error)) int {
	cases := []struct{ name, fault string }{{"launch-return-drain", ""}, {"unknown-firmware", "firmware"}, {"foreign-completion", "foreign"}, {"identity-recovery", "identity"}, {"bound-package", "package"}, {"unmapped-package", "unmapped"}}
	failures := 0
	for _, c := range cases {
		err := runCase(c.fault)
		if err != nil {
			failures++
		}
		if report != nil {
			report(c.name, err)
		}
	}
	return failures
}
