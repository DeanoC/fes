// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
)

type fake struct {
	identity, target                                          embeddedcontrol.Identity
	status, pending                                           uint32
	gen, reqGen, reqCookie, ackGen, ackCookie                 uint64
	contained, control                                        bool
	region                                                    *Region
	expected                                                  map[byte]embeddedcontrol.Identity
	received                                                  []byte
	pendingAddress                                            uint64
	trace                                                     []string
	writes, released, programs, dataReleases, disableCommands int
	fault                                                     string
	once                                                      bool
	enableHook                                                func()
}

func fpgaID(n uint64) embeddedcontrol.Identity {
	return embeddedcontrol.Identity{ABI: 1, Build: [4]uint32{uint32(n), uint32(n >> 32), 0x11223344, 0x55667788}}
}
func (f *fake) Resolve(i fabricmodel.Identity) (embeddedcontrol.Identity, error) {
	return fpgaID(i.Build), nil
}
func (f *fake) Read32(o uint32) (uint32, error) {
	if !f.control {
		return 0, errors.New("control aperture closed")
	}
	switch o {
	case g.FesEmbeddedRegMagic:
		return g.FesEmbeddedMagic, nil
	case g.FesEmbeddedRegVersion:
		return g.FesEmbeddedVersion, nil
	case g.FesEmbeddedRegAbi:
		return f.identity.ABI, nil
	case g.FesEmbeddedRegBuild0, g.FesEmbeddedRegBuild1, g.FesEmbeddedRegBuild2, g.FesEmbeddedRegBuild3:
		return f.identity.Build[(o-g.FesEmbeddedRegBuild0)/4], nil
	case g.FesEmbeddedRegGenerationLo:
		return uint32(f.gen), nil
	case g.FesEmbeddedRegGenerationHi:
		return uint32(f.gen >> 32), nil
	case g.FesEmbeddedRegAckGenerationLo:
		return uint32(f.ackGen), nil
	case g.FesEmbeddedRegAckGenerationHi:
		return uint32(f.ackGen >> 32), nil
	case g.FesEmbeddedRegAckCookieLo:
		return uint32(f.ackCookie), nil
	case g.FesEmbeddedRegAckCookieHi:
		return uint32(f.ackCookie >> 32), nil
	case g.FesEmbeddedRegStatus:
		return f.status, nil
	case g.FesEmbeddedRegPending:
		return f.pending, nil
	}
	return 0, fmt.Errorf("unknown read %x", o)
}
func (f *fake) drain() {
	f.pending = 0
	f.status = g.FesEmbeddedStatusDrained
	f.ackGen = f.gen
	f.ackCookie = f.reqCookie
	if f.fault == "stale" {
		f.ackCookie++
	}
}
func (f *fake) Write32(o, v uint32) error {
	if !f.control {
		return errors.New("control aperture closed")
	}
	switch o {
	case g.FesEmbeddedRegRequestGenerationLo:
		f.reqGen = (f.reqGen & 0xffffffff00000000) | uint64(v)
	case g.FesEmbeddedRegRequestGenerationHi:
		f.reqGen = (f.reqGen & 0xffffffff) | uint64(v)<<32
	case g.FesEmbeddedRegRequestCookieLo:
		f.reqCookie = (f.reqCookie & 0xffffffff00000000) | uint64(v)
	case g.FesEmbeddedRegRequestCookieHi:
		f.reqCookie = (f.reqCookie & 0xffffffff) | uint64(v)<<32
	case g.FesEmbeddedRegCommand:
		switch v {
		case g.FesEmbeddedCommandDisable:
			if f.status != g.FesEmbeddedStatusEnabled || f.reqGen != f.gen || f.reqCookie == 0 {
				return errors.New("invalid disable")
			}
			f.disableCommands++
			f.trace = append(f.trace, "disable")
			f.status = g.FesEmbeddedStatusDraining
			if f.pending == 0 {
				f.drain()
			}
		case g.FesEmbeddedCommandEnable:
			if f.status != 0 || f.reqGen == 0 || !f.contained {
				return errors.New("invalid enable")
			}
			f.gen = f.reqGen
			f.status = g.FesEmbeddedStatusEnabled
			f.trace = append(f.trace, "enable")
			if f.enableHook != nil {
				f.enableHook()
			}
		default:
			return errors.New("unknown command")
		}
	default:
		return errors.New("invalid write")
	}
	return nil
}
func (f *fake) Invoke(r [7]uint64) ([4]uint64, error) {
	switch r[0] {
	case uint64(g.FesEmbeddedFirmwareQuery):
		f.trace = append(f.trace, "query")
		if f.fault == "firmware" {
			return [4]uint64{}, nil
		}
		return [4]uint64{0, uint64(g.FesEmbeddedFirmwareMagic), uint64(g.FesEmbeddedFirmwareVersion), uint64(g.FesEmbeddedFirmwareFeatures)}, nil
	case start:
		f.trace = append(f.trace, "start")
		f.programs++
		if !f.contained || f.control || r[1] != 0 {
			return [4]uint64{}, errors.New("start outside containment")
		}
		if f.fault == "start" {
			return [4]uint64{}, errors.New("unknown start result")
		}
		f.received = nil
		f.pendingAddress = 0
		return [4]uint64{}, nil
	case write:
		f.trace = append(f.trace, "write")
		f.writes++
		if !f.contained || f.pendingAddress != 0 || f.region == nil || r[1] != f.region.Address+uint64(len(f.received)) || r[2] == 0 || r[2] > uint64(len(f.region.Bytes)-len(f.received)) {
			return [4]uint64{}, errors.New("invalid write")
		}
		off := len(f.received)
		f.received = append(f.received, f.region.Bytes[off:off+int(r[2])]...)
		f.pendingAddress = r[1] - 0x80000000
		return [4]uint64{1, 0, 0, 0}, nil // accepted BUSY
	case completed:
		f.trace = append(f.trace, "completed")
		if f.fault == "lost" {
			return [4]uint64{1, 0, 0, 0}, nil
		}
		if f.fault == "foreign" {
			return [4]uint64{0, 0x17000000, 0, 0}, nil
		}
		if f.pendingAddress == 0 {
			return [4]uint64{}, errors.New("poll without write")
		}
		a := f.pendingAddress
		f.pendingAddress = 0
		return [4]uint64{0, a, 0, 0}, nil
	case isdone:
		f.trace = append(f.trace, "isdone")
		if f.pendingAddress != 0 || f.region == nil || !bytes.Equal(f.received, f.region.Bytes) || !f.contained {
			return [4]uint64{}, errors.New("early isdone")
		}
		f.identity = f.target
		if f.fault == "identity" && !f.once {
			f.identity.Build[3] ^= 1
			f.once = true
		}
		f.status = 0
		f.pending = 0
		f.gen = 0
		f.ackGen = 0
		f.ackCookie = 0
		f.reqGen = 0
		f.reqCookie = 0
		return [4]uint64{}, nil
	}
	return [4]uint64{}, errors.New("unknown SMC")
}
func (f *fake) Reserve(b []byte) (*Region, error) {
	f.trace = append(f.trace, "reserve")
	if f.region != nil {
		return nil, errors.New("duplicate reserve")
	}
	f.target = f.expected[b[0]]
	r := &Region{Address: 0x96000000, Bytes: bytes.Clone(b)}
	r.Release = func() error { f.released++; f.region = nil; return nil }
	f.region = r
	return r, nil
}
func (f *fake) Save(fabricmodel.Identity) (bool, error) {
	f.trace = append(f.trace, "save")
	if f.fault == "save" {
		return true, errors.New("save failed")
	}
	if f.fault == "save-unknown" {
		return false, errors.New("save unknown")
	}
	return true, nil
}
func (f *fake) Contain() (bool, error) {
	f.trace = append(f.trace, "contain")
	if f.fault == "contain" {
		return false, errors.New("contain unknown")
	}
	f.contained = true
	f.control = false
	return true, nil
}
func (f *fake) ControlOnly() (bool, error) {
	f.trace = append(f.trace, "control")
	if !f.contained || f.fault == "control" {
		return false, nil
	}
	f.control = true
	return true, nil
}
func (f *fake) ReleaseData() (bool, error) {
	f.trace = append(f.trace, "release")
	if !f.contained || !f.control || f.status != g.FesEmbeddedStatusEnabled || f.fault == "release" {
		return false, nil
	}
	f.contained = false
	f.dataReleases++
	return true, nil
}

func image(b byte, id string, idle bool) *fabricmodel.Image {
	data := bytes.Repeat([]byte{b}, 128)
	i, err := fabricmodel.Prepare(id, data, sha256.Sum256(data), fabricmodel.Identity{ABI: 1, Core: 1, Build: uint64(b)}, idle)
	if err != nil {
		panic(err)
	}
	return i
}
func setup(t *testing.T) (*fabricmodel.Controller, *Endpoint, *fake, *time.Duration) {
	t.Helper()
	now := time.Duration(0)
	idle := image(1, "model.idle", true)
	c, err := fabricmodel.New(idle, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	f := &fake{control: true, expected: map[byte]embeddedcontrol.Identity{1: fpgaID(1), 2: fpgaID(2)}}
	e, err := New(f, Options{DRAMBase: 0x80000000, ChunkBytes: 32})
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Boot(now, time.Second); err != nil {
		t.Fatal(err)
	}
	if err = drive(c, e, &now, 600); err != nil || c.Status().State != fabricmodel.Idle {
		t.Fatalf("boot: %v %+v", err, c.Status())
	}
	return c, e, f, &now
}
func drive(c *fabricmodel.Controller, e *Endpoint, now *time.Duration, steps int) error {
	var first error
	for range steps {
		*now += time.Millisecond
		err := e.Step(c, *now)
		if err != nil && first == nil {
			first = err
		}
		if c.Status().State != fabricmodel.Changing {
			return first
		}
	}
	return errors.New("step budget exceeded")
}
func TestLaunchReturnUsesFirmwareAndGenerationBoundDrain(t *testing.T) {
	c, e, f, now := setup(t)
	old := c.Status().DisplayGeneration
	f.pending = 1
	if err := c.Load(image(2, "model.game", false), *now, *now+time.Second); err != nil {
		t.Fatal(err)
	}
	for range 8 {
		*now += time.Millisecond
		if err := e.Step(c, *now); err != nil {
			t.Fatal(err)
		}
	}
	if f.disableCommands != 1 || f.programs != 1 || c.Status().State != fabricmodel.Changing {
		t.Fatalf("advanced before old read drained: %+v %v", c.Status(), f.trace)
	}
	f.drain()
	if err := drive(c, e, now, 500); err != nil {
		t.Fatal(err)
	}
	if c.Status().State != fabricmodel.Running || c.Status().DisplayGeneration <= old || f.identity != fpgaID(2) || f.released != 2 || f.writes != 8 {
		t.Fatalf("launch: %+v releases=%d writes=%d", c.Status(), f.released, f.writes)
	}
	if err := c.Stop(*now, *now+time.Second); err != nil {
		t.Fatal(err)
	}
	if err := drive(c, e, now, 500); err != nil {
		t.Fatal(err)
	}
	if c.Status().State != fabricmodel.Idle || f.identity != fpgaID(1) || f.programs != 3 || f.released != 3 || f.dataReleases != 3 {
		t.Fatalf("return: %+v trace=%v", c.Status(), f.trace)
	}
}
func TestFailuresKeepOwnershipAndNeverReleaseTarget(t *testing.T) {
	for _, fault := range []string{"firmware", "stale", "contain", "start", "foreign", "control", "release"} {
		t.Run(fault, func(t *testing.T) {
			c, e, f, now := setup(t)
			f.fault = fault
			before := f.dataReleases
			if err := c.Load(image(2, "model.game", false), *now, *now+time.Second); err != nil {
				t.Fatal(err)
			}
			err := drive(c, e, now, 500)
			if err == nil || c.Status().State != fabricmodel.RebootRequired || f.dataReleases != before {
				t.Fatalf("fault %s: %v %+v %v", fault, err, c.Status(), f.trace)
			}
			writes := f.writes
			programs := f.programs
			if err = e.Step(c, *now); err == nil || f.writes != writes || f.programs != programs {
				t.Fatal("failed command replayed")
			}
			if fault == "firmware" && f.disableCommands != 0 {
				t.Fatal("unknown firmware disabled reader")
			}
			if (fault == "start" || fault == "foreign") && (f.region == nil || len(e.retained) != 1 || f.released != 1) {
				t.Fatal("unresolved DMA region lost")
			}
		})
	}
}
func TestIdentityMismatchPerformsOneContainedIdleRecovery(t *testing.T) {
	c, e, f, now := setup(t)
	f.fault = "identity"
	before := f.dataReleases
	if err := c.Load(image(2, "model.game", false), *now, *now+time.Second); err != nil {
		t.Fatal(err)
	}
	err := drive(c, e, now, 500)
	if !errors.Is(err, embeddedcontrol.ErrIdentity) || c.Status().State != fabricmodel.Idle || f.programs != 3 || f.dataReleases != before+1 || f.identity != fpgaID(1) {
		t.Fatalf("recovery: %v %+v %v", err, c.Status(), f.trace)
	}
}
func TestDeadlineAndMutationRetainPendingRegion(t *testing.T) {
	for _, fault := range []string{"lost", "mutation"} {
		t.Run(fault, func(t *testing.T) {
			c, e, f, now := setup(t)
			f.fault = fault
			if err := c.Load(image(2, "model.game", false), *now, *now+100*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			for f.writes < 5 {
				*now += time.Millisecond
				if err := e.Step(c, *now); err != nil {
					t.Fatal(err)
				}
			}
			if fault == "mutation" {
				f.region.Bytes[0] ^= 1
			}
			err := drive(c, e, now, 200)
			if err == nil || c.Status().State != fabricmodel.RebootRequired || f.region == nil || len(e.retained) != 1 || f.dataReleases != 1 {
				t.Fatalf("lost ownership: %v %+v %v", err, c.Status(), f.trace)
			}
		})
	}
}
func TestSaveFailureRetainsRunningOwner(t *testing.T) {
	c, e, f, now := setup(t)
	if err := c.Load(image(2, "model.game", false), *now, *now+time.Second); err != nil {
		t.Fatal(err)
	}
	if err := drive(c, e, now, 300); err != nil {
		t.Fatal(err)
	}
	before := c.Status()
	f.fault = "save"
	if err := c.Stop(*now, *now+time.Second); err != nil {
		t.Fatal(err)
	}
	if err := drive(c, e, now, 300); err == nil {
		t.Fatal("save failure was ignored")
	}
	got := c.Status()
	if got.State != fabricmodel.Running || got.CoreGeneration != before.CoreGeneration || got.InputEnabled || f.programs != 2 {
		t.Fatalf("owner lost: %+v", got)
	}
}

func TestCancellationAndRegressingClockRetainUnresolvedTransfer(t *testing.T) {
	for _, method := range []string{"cancel", "clock"} {
		t.Run(method, func(t *testing.T) {
			c, e, f, now := setup(t)
			f.fault = "lost"
			if err := c.Load(image(2, "model.game", false), *now, *now+time.Second); err != nil {
				t.Fatal(err)
			}
			for f.writes < 5 {
				*now += time.Millisecond
				if err := e.Step(c, *now); err != nil {
					t.Fatal(err)
				}
			}
			var err error
			if method == "cancel" {
				err = e.Cancel(c, *now, errors.New("canceled"))
			} else {
				err = e.Step(c, *now-time.Millisecond)
			}
			if err == nil || c.Status().State != fabricmodel.RebootRequired || f.region == nil || len(e.retained) != 1 || f.dataReleases != 1 {
				t.Fatalf("%s lost uncertain ownership: %v %+v", method, err, c.Status())
			}
			if err = c.RecoverIdle(*now, *now+time.Second); !errors.Is(err, fabricmodel.ErrReboot) {
				t.Fatalf("unsafe recovery: %v", err)
			}
		})
	}
}

func TestEndpointCannotSwitchControllers(t *testing.T) {
	c, e, _, now := setup(t)
	other, err := fabricmodel.New(image(1, "model.idle", true), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = other.Boot(*now, *now+time.Second); err != nil {
		t.Fatal(err)
	}
	if err = e.Step(other, *now); err == nil || c.Status().State != fabricmodel.Idle || other.Status().State != fabricmodel.Changing {
		t.Fatalf("controller ownership changed: %v", err)
	}
}

func TestDeadlineAfterSlowEnablePreventsDataRelease(t *testing.T) {
	now := time.Duration(0)
	c, err := fabricmodel.New(image(1, "model.idle", true), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	f := &fake{control: true, expected: map[byte]embeddedcontrol.Identity{1: fpgaID(1), 2: fpgaID(2)}}
	e, err := New(f, Options{DRAMBase: 0x80000000, ChunkBytes: 32})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.SetClock(func() time.Duration { return now }); err != nil {
		t.Fatal(err)
	}
	if err = c.Boot(now, time.Second); err != nil {
		t.Fatal(err)
	}
	if err = drive(c, e, &now, 500); err != nil {
		t.Fatal(err)
	}
	deadline := now + time.Second
	f.enableHook = func() { now = deadline }
	if err = c.Load(image(2, "model.game", false), now, deadline); err != nil {
		t.Fatal(err)
	}
	if err = drive(c, e, &now, 500); !errors.Is(err, fabricmodel.ErrDeadline) || c.Status().State != fabricmodel.RebootRequired || f.dataReleases != 1 {
		t.Fatalf("late enable released data: %v %+v", err, c.Status())
	}
}
