// SPDX-License-Identifier: GPL-3.0-or-later
// Package nativefabric connects the exploratory native lifecycle to injectable
// contained firmware and FPGA control operations. It has no board binding.
package nativefabric

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
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
}

// Hooks are trusted, serialized, synchronous and bounded-return operations.
// Contain and ControlOnly must check the bridge masks and all other masters;
// ControlOnly admits only HPS-to-FPGA CSR while data masters remain contained.
// Save reports whether an error is a known, completed result. Resolve binds an
// admitted model identity to the intended full FPGA build/ABI identity.
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
	DRAMBase   uint64
	ChunkBytes int
}

type Endpoint struct {
	hooks          Hooks
	owner          *fabricmodel.Controller
	clock          func() time.Duration
	ctrl           embeddedcontrol.Control
	options        Options
	command        *fabricmodel.Command
	phase          string
	cookie         uint64
	target         embeddedcontrol.Identity
	imageHash      [32]byte
	chunkHashes    [][32]byte
	region         *Region
	offset, amount int
	retained       []*Region
	retired        bool
	issued         fabricmodel.Operation
	progressed     bool
	pollBusy       bool
}

func New(hooks Hooks, options Options) (*Endpoint, error) {
	if hooks == nil || options.DRAMBase == 0 || options.ChunkBytes <= 0 || options.ChunkBytes > 0xffff0 {
		return nil, errors.New("native fabric: explicit hooks, DRAM base and bounded chunk required")
	}
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
	beforeCommand, beforePhase, beforeOffset := e.command, e.phase, e.offset
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
	e.progressed = next != nil || done || e.pollBusy || beforeCommand != e.command || beforePhase != e.phase || beforeOffset != e.offset
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
	identity, err := e.hooks.Resolve(id)
	if err == nil && (identity.ABI == 0 || identity.Build == [4]uint32{}) {
		return identity, errors.New("native fabric: empty FPGA identity")
	}
	return identity, err
}
func (e *Endpoint) call(req [7]uint64) ([4]uint64, error) { return e.hooks.Invoke(req) }
func plain(r [4]uint64) bool                              { return r[1] == 0 && r[2] == 0 && r[3] == 0 }
func (e *Endpoint) stable() bool {
	return e.region != nil && sha256.Sum256(e.region.Bytes) == e.imageHash && sha256.Sum256(e.command.Bytes) == e.imageHash
}
func (e *Endpoint) stableChunk() bool {
	if e.region == nil || e.offset < 0 || e.amount <= 0 || e.offset+e.amount > len(e.region.Bytes) || e.offset+e.amount > len(e.command.Bytes) {
		return false
	}
	index := e.offset / e.options.ChunkBytes
	return index < len(e.chunkHashes) && sha256.Sum256(e.region.Bytes[e.offset:e.offset+e.amount]) == e.chunkHashes[index] &&
		sha256.Sum256(e.command.Bytes[e.offset:e.offset+e.amount]) == e.chunkHashes[index]
}

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
	switch e.phase {
	case "begin":
		if len(c.Bytes) == 0 {
			return failed(errors.New("native fabric: empty image"), false)
		}
		e.imageHash = sha256.Sum256(c.Bytes)
		region, err := e.hooks.Reserve(c.Bytes)
		if region != nil {
			e.retained = append(e.retained, region)
			e.region = region
		}
		if err != nil {
			return failed(err, true)
		}
		if region == nil || region.Release == nil || len(region.Bytes) != len(c.Bytes) || !bytes.Equal(region.Bytes, c.Bytes) ||
			region.Address%64 != 0 || region.Address <= e.options.DRAMBase || region.Address-e.options.DRAMBase > math.MaxUint32 ||
			uint64(len(region.Bytes))-1 > math.MaxUint32-(region.Address-e.options.DRAMBase) ||
			uint64(len(region.Bytes))-1 > math.MaxUint64-region.Address {
			return failed(errors.New("native fabric: invalid reserved DMA region"), true)
		}
		e.offset = 0
		e.chunkHashes = nil
		for offset := 0; offset < len(c.Bytes); offset += e.options.ChunkBytes {
			end := min(offset+e.options.ChunkBytes, len(c.Bytes))
			e.chunkHashes = append(e.chunkHashes, sha256.Sum256(c.Bytes[offset:end]))
		}
		e.phase = "start"
		return false, fabricmodel.Completion{}
	case "start":
		if !e.stable() {
			return failed(errors.New("native fabric: staged image changed"), true)
		}
		r, err := e.call([7]uint64{start})
		if err != nil || r[0] != 0 || !plain(r) {
			if err == nil {
				err = fmt.Errorf("native fabric: START returned %v", r)
			}
			return failed(err, true)
		}
		e.phase = "write"
		return false, fabricmodel.Completion{}
	case "write":
		if e.offset == len(e.region.Bytes) {
			e.phase = "done"
			return false, fabricmodel.Completion{}
		}
		e.amount = min(e.options.ChunkBytes, len(e.region.Bytes)-e.offset)
		if !e.stableChunk() {
			return failed(errors.New("native fabric: staged image changed"), true)
		}
		r, err := e.call([7]uint64{write, e.region.Address + uint64(e.offset), uint64(e.amount)})
		if err != nil || (r[0] != 0 && r[0] != 1) || !plain(r) {
			if err == nil {
				err = fmt.Errorf("native fabric: WRITE returned %v", r)
			}
			return failed(err, true)
		}
		// BUSY also accepted this write. Never reissue it.
		e.phase = "completion"
		return false, fabricmodel.Completion{}
	case "completion":
		if !e.stableChunk() {
			return failed(errors.New("native fabric: asynchronous image mutation"), true)
		}
		r, err := e.call([7]uint64{completed})
		if err != nil {
			return failed(err, true)
		}
		if r[0] == 1 && plain(r) {
			e.pollBusy = true
			return false, fabricmodel.Completion{}
		}
		want := e.region.Address + uint64(e.offset) - e.options.DRAMBase
		if r != ([4]uint64{0, want, 0, 0}) {
			return failed(fmt.Errorf("native fabric: unexpected completion %v", r), true)
		}
		e.offset += e.amount
		e.phase = "write"
		return false, fabricmodel.Completion{}
	case "done":
		if !e.stable() {
			return failed(errors.New("native fabric: staged image changed"), true)
		}
		r, err := e.call([7]uint64{isdone})
		if err != nil {
			return failed(err, true)
		}
		if r[0] == 1 && plain(r) {
			e.pollBusy = true
			return false, fabricmodel.Completion{}
		}
		if r != ([4]uint64{}) {
			return failed(fmt.Errorf("native fabric: ISDONE returned %v", r), true)
		}
		e.phase = "retire"
		return false, fabricmodel.Completion{}
	case "retire":
		if err := e.region.Release(); err != nil {
			return failed(err, true)
		}
		e.retained = e.retained[:len(e.retained)-1]
		e.region = nil
		e.chunkHashes = nil
		return true, fabricmodel.Completion{ProgrammedDigest: e.imageHash}
	}
	return failed(errors.New("native fabric: invalid program phase"), true)
}
