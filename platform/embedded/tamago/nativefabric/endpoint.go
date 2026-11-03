// SPDX-License-Identifier: GPL-3.0-or-later
// Package nativefabric connects the exploratory native lifecycle to injectable
// contained firmware and FPGA control operations. It has no board binding.
package nativefabric

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
)

const (
	start     = 0xc2000001
	write     = 0x42000002
	completed = 0xc2000003
	isdone    = 0xc2000004
)

// Region is exclusively reserved until Release returns successfully. Bytes must
// be the actual DMA-visible storage, kept immutable and coherent during SMC
// calls and asynchronous SDM reads. The platform owns cache/mapping proof.
type Region struct {
	Address uint64
	Bytes   []byte
	Release func() error
	// Orders CPU frame stores in this exact boot-pinned, Normal uncached region.
	// It is synchronous and bounded; it supplies no reader or bridge drain.
	OrderFrameStores func() error
	// The cold platform publishes one per-load descriptor in its separately
	// reserved metadata page. Generic firmware transfers do not use this hook.
	PublishConfiguration func([]byte) error
}

// Hooks are trusted, serialized, synchronous and bounded-return operations.
// Contain and ControlOnly must check the bridge masks and all other masters;
// ControlOnly admits only HPS-to-FPGA CSR while data masters remain contained.
// Save reports whether an error is a known, completed result. Resolve applies
// only to unpackaged model/idle images; admitted packages use explicit profiles.
type Hooks interface {
	embeddedcontrol.IO
	Invoke([7]uint64) ([4]uint64, error)
	Resolve(fabricmodel.Identity) (embeddedcontrol.Identity, error)
	Reserve([]byte) (*Region, error)
	Save(fabricmodel.Identity) (known bool, err error)
	Contain() (bool, error)
	ControlOnly() (bool, error)
	ReleaseData() (bool, error)
}

type Options struct {
	DRAMBase         uint64
	ChunkBytes       int
	IdentityProfiles []IdentityProfile
}

type Endpoint struct {
	hooks      Hooks
	owner      *fabricmodel.Controller
	clock      func() time.Duration
	ctrl       embeddedcontrol.Control
	options    Options
	command    *fabricmodel.Command
	phase      string
	cookie     uint64
	target     embeddedcontrol.Identity
	imageHash  [32]byte
	transfer   *ConfigurationTransfer
	retained   []*Region
	retired    bool
	issued     fabricmodel.Operation
	progressed bool
	pollBusy   bool
}

func New(hooks Hooks, options Options) (*Endpoint, error) {
	if hooks == nil || options.DRAMBase == 0 || options.ChunkBytes <= 0 || options.ChunkBytes > 0xffff0 {
		return nil, errors.New("native fabric: explicit hooks, DRAM base and bounded chunk required")
	}
	if len(options.IdentityProfiles) > 64 {
		return nil, errors.New("native fabric: too many identity profiles")
	}
	for i, profile := range options.IdentityProfiles {
		if !profileValid(profile) {
			return nil, errors.New("native fabric: invalid identity profile")
		}
		if profile.Platform == "de25_nano" && profile.ProgrammingProfile == HPSFirstDDRDiagnosticProfile {
			return nil, errors.New("native fabric: DDR reader diagnostic has no runtime admission")
		}
		if profile.Platform == "de25_nano" && profile.ProgrammingProfile == HPSFirstDiagnosticProfile && !profile.RequireBootHandoff {
			return nil, errors.New("native fabric: HPS-first diagnostic requires boot handoff verification")
		}
		for _, previous := range options.IdentityProfiles[:i] {
			if sameProfile(profile, previous) {
				return nil, errors.New("native fabric: duplicate identity profile")
			}
		}
	}
	options.IdentityProfiles = append([]IdentityProfile(nil), options.IdentityProfiles...)
	return &Endpoint{hooks: hooks, ctrl: embeddedcontrol.Control{IO: hooks}, options: options}, nil
}

// SetClock installs the owner's monotonic clock before the endpoint is used.
// It lets completion be checked again after a synchronous platform call.
// Without one, Step's supplied time represents a controlled test clock.
func (e *Endpoint) SetClock(clock func() time.Duration) error {
	if e == nil || e.owner != nil || e.clock != nil || clock == nil {
		return errors.New("native fabric: clock must be set before use")
	}
	e.clock = clock
	return nil
}

// Step is called by the one lifecycle owner. It issues at most one lifecycle
// command and advances it cooperatively. An unresolved command is never
// replayed; the controller and endpoint retain its bytes/region on failure.
func (e *Endpoint) Step(c *fabricmodel.Controller, now time.Duration) error {
	if e == nil || c == nil || e.retired {
		return errors.New("native fabric: endpoint retired")
	}
	if e.owner != nil && e.owner != c {
		return errors.New("native fabric: endpoint belongs to another controller")
	}
	e.owner = c
	e.issued = ""
	e.progressed = false
	e.pollBusy = false
	beforeCommand, beforePhase := e.command, e.phase
	beforeTransfer := e.transfer.Status()
	next, err := c.Next(now)
	if err != nil {
		e.retired = true
		if c.Status().State == fabricmodel.Changing {
			_ = c.AbortUncertain(err)
		}
		return err
	}
	if e.command == nil {
		if next == nil {
			return nil
		}
		e.issued = next.Operation
		e.command = next
		e.phase = "begin"
	} else if next != nil {
		e.retired = true
		return errors.New("native fabric: overlapping lifecycle commands")
	}
	done, result := e.advance()
	e.progressed = next != nil || done || e.pollBusy || beforeCommand != e.command ||
		beforePhase != e.phase || beforeTransfer != e.transfer.Status()
	if !done {
		return nil
	}
	command := e.command
	e.command = nil
	e.phase = ""
	if result.Uncertain {
		e.retired = true
	}
	if e.clock != nil {
		after := e.clock()
		if after < now {
			e.retired = true
			return c.AbortUncertain(errors.New("native fabric: monotonic clock regressed during platform call"))
		}
		now = after
	}
	err = c.Complete(command.Token, result, now)
	if c.Status().State == fabricmodel.RebootRequired {
		e.retired = true
	}
	return err
}

// Issued reports the operation issued by the last Step, for runtime tracing.
// It is observed only by the same serialized owner that calls Step.
func (e *Endpoint) Issued() fabricmodel.Operation { return e.issued }

// Progressed lets a cooperative caller stop a bounded burst of steps when
// the endpoint is waiting for an external drain or firmware response.
func (e *Endpoint) Progressed() bool { return e.progressed }

// Cancel fails an in-flight or next issued command without replay. The
// operation may have touched hardware, so reservations remain held.
func (e *Endpoint) Cancel(c *fabricmodel.Controller, now time.Duration, reason error) error {
	if e == nil || c == nil || e.retired || reason == nil {
		return errors.New("native fabric: cannot cancel endpoint")
	}
	if e.owner != nil && e.owner != c {
		return errors.New("native fabric: endpoint belongs to another controller")
	}
	e.owner = c
	if e.command == nil {
		command, err := c.Next(now)
		if err != nil {
			e.retired = true
			if c.Status().State == fabricmodel.Changing {
				_ = c.AbortUncertain(err)
			}
			return err
		}
		if command == nil {
			return nil // The controller may still be waiting for a frame.
		}
		e.command = command
	}
	e.retired = true
	return c.Complete(e.command.Token, fabricmodel.Completion{Err: reason, Uncertain: true}, now)
}

func failed(err error, uncertain bool) (bool, fabricmodel.Completion) {
	return true, fabricmodel.Completion{Err: err, Uncertain: uncertain}
}
func (e *Endpoint) firmware() error {
	r, err := e.hooks.Invoke([7]uint64{uint64(g.FesEmbeddedFirmwareQuery)})
	if err != nil {
		return err
	}
	if r != ([4]uint64{0, uint64(g.FesEmbeddedFirmwareMagic), uint64(g.FesEmbeddedFirmwareVersion), uint64(g.FesEmbeddedFirmwareFeatures)}) {
		return errors.New("native fabric: unsupported firmware capability tuple")
	}
	return nil
}
func (e *Endpoint) resolve(id fabricmodel.Identity) (embeddedcontrol.Identity, error) {
	if id.Package != (fabricmodel.PackageIdentity{}) {
		return e.packageIdentity(id.Package)
	}
	identity, err := e.hooks.Resolve(id)
	if err == nil && (identity.ABI == 0 || identity.Build == [4]uint32{}) {
		return identity, errors.New("native fabric: empty FPGA identity")
	}
	return identity, err
}
func plain(r [4]uint64) bool { return r[1] == 0 && r[2] == 0 && r[3] == 0 }
func (e *Endpoint) advance() (bool, fabricmodel.Completion) {
	c := e.command
	switch c.Operation {
	case fabricmodel.Save:
		known, err := e.hooks.Save(c.Identity)
		if err != nil {
			return failed(err, !known)
		}
		if !known {
			return failed(errors.New("native fabric: save outcome unresolved"), true)
		}
		return true, fabricmodel.Completion{}
	case fabricmodel.Drain:
		if e.phase == "begin" {
			if err := e.firmware(); err != nil {
				return failed(err, true)
			}
			e.phase = "disable"
			return false, fabricmodel.Completion{}
		}
		if e.phase == "disable" {
			want, err := e.resolve(c.ReaderIdentity)
			if err != nil {
				return failed(err, true)
			}
			if e.cookie == math.MaxUint64 {
				return failed(errors.New("native fabric: drain cookie exhausted"), true)
			}
			cookie := e.cookie + 1
			if err = e.ctrl.Disable(want, c.DisplayGeneration, cookie); err != nil {
				if errors.Is(err, embeddedcontrol.ErrBusy) {
					return false, fabricmodel.Completion{}
				}
				return failed(err, true)
			}
			e.cookie, e.target, e.phase = cookie, want, "drain"
			return false, fabricmodel.Completion{}
		}
		err := e.ctrl.Drained(e.target, c.DisplayGeneration, e.cookie)
		if errors.Is(err, embeddedcontrol.ErrBusy) {
			return false, fabricmodel.Completion{}
		}
		if err != nil {
			return failed(err, true)
		}
		return true, fabricmodel.Completion{Quiescent: true, DisplayGeneration: c.DisplayGeneration}
	case fabricmodel.Contain:
		if e.phase == "begin" {
			if err := e.firmware(); err != nil {
				return failed(err, true)
			}
			e.phase = "contain"
			return false, fabricmodel.Completion{}
		}
		ok, err := e.hooks.Contain()
		if err != nil || !ok {
			if err == nil {
				err = errors.New("native fabric: containment unproven")
			}
			return failed(err, true)
		}
		return true, fabricmodel.Completion{Quiescent: true}
	case fabricmodel.Program:
		return e.program()
	case fabricmodel.Identify:
		if e.phase == "begin" {
			want, err := e.resolve(c.Identity)
			if err != nil {
				return failed(err, false)
			}
			ok, err := e.hooks.ControlOnly()
			if err != nil || !ok {
				if err == nil {
					err = errors.New("native fabric: control-only access unproven")
				}
				return failed(err, true)
			}
			e.target, e.phase = want, "verify"
			return false, fabricmodel.Completion{}
		}
		err := e.ctrl.Verify(e.target)
		if errors.Is(err, embeddedcontrol.ErrBusy) {
			return false, fabricmodel.Completion{}
		}
		if err != nil {
			return failed(err, !errors.Is(err, embeddedcontrol.ErrIdentity))
		}
		return true, fabricmodel.Completion{Identity: c.Identity}
	case fabricmodel.Release:
		if e.phase == "begin" {
			want, err := e.resolve(c.Identity)
			if err != nil {
				return failed(err, true)
			}
			if err = e.ctrl.Verify(want); err != nil {
				if errors.Is(err, embeddedcontrol.ErrBusy) {
					return false, fabricmodel.Completion{}
				}
				return failed(err, true)
			}
			e.target, e.phase = want, "enable"
			return false, fabricmodel.Completion{}
		}
		if e.phase == "enable" {
			if err := e.ctrl.Enable(e.target, c.NextDisplayGeneration); err != nil {
				return failed(err, true)
			}
			e.phase = "release"
			return false, fabricmodel.Completion{}
		}
		ok, err := e.hooks.ReleaseData()
		if err != nil || !ok {
			if err == nil {
				err = errors.New("native fabric: data release unproven")
			}
			return failed(err, true)
		}
		return true, fabricmodel.Completion{}
	default:
		return failed(errors.New("native fabric: unknown lifecycle operation"), true)
	}
}

func (e *Endpoint) program() (bool, fabricmodel.Completion) {
	c := e.command
	if e.phase == "begin" {
		if len(c.Bytes) == 0 {
			return failed(errors.New("native fabric: empty image"), false)
		}
		e.imageHash = sha256.Sum256(c.Bytes)
		if c.Identity.Package != (fabricmodel.PackageIdentity{}) {
			if _, err := e.packageIdentity(c.Identity.Package); err != nil {
				return failed(err, false)
			}
			if err := e.verifyBootHandoff(c.Identity.Package); err != nil {
				return failed(err, false)
			}
			if hex.EncodeToString(e.imageHash[:]) != c.Identity.Package.ProgrammedSHA256 {
				return failed(errors.New("native fabric: programming bytes differ from admitted digest"), false)
			}
		}
		region, err := e.hooks.Reserve(c.Bytes)
		if region != nil {
			e.retained = append(e.retained, region)
		}
		if err != nil {
			return failed(err, true)
		}
		beforeStart := func() error {
			if c.Identity.Package != (fabricmodel.PackageIdentity{}) {
				return e.verifyBootHandoff(c.Identity.Package)
			}
			return nil
		}
		e.transfer, err = NewConfigurationTransfer(e.hooks, c.Bytes, region, e.options.DRAMBase, e.options.ChunkBytes, beforeStart)
		if err != nil {
			return failed(err, true)
		}
		e.phase = e.transfer.Status().Phase
		return false, fabricmodel.Completion{}
	}
	done, busy, err := e.transfer.Advance()
	e.pollBusy = busy
	e.phase = e.transfer.Status().Phase
	if err != nil {
		return failed(err, true)
	}
	if !done {
		return false, fabricmodel.Completion{}
	}
	e.retained = e.retained[:len(e.retained)-1]
	e.transfer = nil
	return true, fabricmodel.Completion{ProgrammedDigest: e.imageHash}
}
