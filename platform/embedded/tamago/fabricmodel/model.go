// SPDX-License-Identifier: GPL-3.0-or-later

// Package fabricmodel is an executable specification of the embedded runtime
// ownership boundary. It is not a hardware driver or physical package loader. All
// programming and identity observations remain software simulations. A caller issues each returned command
// once, then supplies its completion; polling never repeats a command.
package fabricmodel

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"math"
	"strings"
	"time"
)

var (
	ErrBusy     = errors.New("fabric model is busy")
	ErrStale    = errors.New("stale fabric model generation or completion")
	ErrReboot   = errors.New("fabric model requires reboot")
	ErrDeadline = errors.New("fabric model operation deadline expired")
)

// Identity belongs only to this software model; it is not a FES GP ABI.
type Identity struct {
	ABI     uint16
	Core    uint32
	Build   uint64
	Package PackageIdentity
}

// PackageIdentity binds a software observation to the complete verified FES
// identity. It is not a physical GP response or a shortened build identifier.
type PackageIdentity struct {
	PackageID, CoreID, ABI, BuildID           string
	ActivationReceiptSHA256, ProgrammedSHA256 string
	Major, Minor                              uint16
}

// MaxImageBytes bounds retained software-model buffers, including linked ROM fixtures.
const MaxImageBytes = 8 << 20

// Image keeps an immutable copy of admitted model bytes. Preparing an image
// neither changes the running owner nor implies production package admission.
type Image struct {
	id       string
	identity Identity
	digest   [32]byte
	data     []byte
	idle     bool
}

func Prepare(id string, data []byte, digest [32]byte, identity Identity, idle bool) (*Image, error) {
	if !strings.HasPrefix(id, "model.") || len(id) > 64 || len(data) == 0 || len(data) > MaxImageBytes || identity.ABI != 1 || identity.Core == 0 || identity.Build == 0 {
		return nil, errors.New("invalid software model image")
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_') {
			return nil, errors.New("invalid model image name")
		}
	}
	if sha256.Sum256(data) != digest {
		return nil, errors.New("model image digest mismatch")
	}
	return &Image{id: id, identity: identity, digest: digest, data: bytes.Clone(data), idle: idle}, nil
}

func (i *Image) valid() bool { return i != nil && sha256.Sum256(i.data) == i.digest }

type State string

const (
	Cold           State = "cold"
	Idle           State = "idle"
	Running        State = "running_model"
	Changing       State = "transition"
	RebootRequired State = "reboot_required"
)

type Operation string

const (
	Save      Operation = "save"
	Drain     Operation = "drain_display"
	Contain   Operation = "contain"
	Program   Operation = "program_model"
	Identify  Operation = "identify_model"
	Release   Operation = "release_model"
	waitFrame Operation = "wait_frame"
)

type Token struct{ transition, sequence uint64 }
type Frame struct{ generation, sequence uint64 }

type Command struct {
	Token     Token
	Operation Operation
	ImageID   string
	Identity  Identity
	// Bytes is a private copy for this command. Mutation cannot alter admission.
	Bytes []byte
}

type Completion struct {
	Err error
	// Uncertain means the backend may still own memory or be executing the
	// command. An error alone describes a known completed failure.
	Uncertain bool
	// Quiescent is required for Contain: the backend has proved no fabric/DMA
	// owner can still access the old memory. Timeout is not such proof.
	Quiescent        bool
	ProgrammedDigest [32]byte
	Identity         Identity
}

type Status struct {
	State             State
	ImageID           string
	CoreGeneration    uint64
	DisplayGeneration uint64
	InputEnabled      bool
	LastError         string
	Package           PackageIdentity
}

type transition struct {
	target   *Image
	phase    Operation
	deadline time.Duration
	recovery bool
	primary  error
}

// Controller has one cooperative owner. Next and Complete separate command
// issue from completion so waits need not block input, storage or networking.
// The runtime, never the UI, owns this controller. No addresses or DMA buffers
// are granted by the model; Frame demonstrates opaque generation ownership.
type Controller struct {
	idle, active                                          *Image
	held                                                  []*Image
	status                                                Status
	pending                                               *transition
	issued                                                *Command
	frame                                                 *Frame
	transitionID, sequence, generation, displayGeneration uint64
	lastTime                                              time.Duration
	recoveryBudget                                        time.Duration
}

func New(idle *Image, recoveryBudget time.Duration) (*Controller, error) {
	if !idle.valid() || !idle.idle || recoveryBudget <= 0 {
		return nil, errors.New("invalid model idle image or recovery budget")
	}
	return &Controller{idle: idle, recoveryBudget: recoveryBudget, status: Status{State: Cold}}, nil
}

func (c *Controller) Status() Status { return c.status }

func (c *Controller) begin(target *Image, phase Operation, now, deadline time.Duration) error {
	if now < c.lastTime || deadline <= now {
		return ErrDeadline
	}
	if c.pending != nil {
		return ErrBusy
	}
	if !target.valid() {
		return errors.New("model admission changed")
	}
	if c.transitionID == math.MaxUint64 {
		return ErrReboot
	}
	c.lastTime = now
	c.transitionID++
	c.pending = &transition{target: target, phase: phase, deadline: deadline}
	c.held = []*Image{c.active, target}
	c.status.State = Changing
	c.status.InputEnabled = false
	c.status.LastError = ""
	return nil
}

func (c *Controller) Boot(now, deadline time.Duration) error {
	if c.status.State != Cold {
		return ErrBusy
	}
	return c.begin(c.idle, Contain, now, deadline)
}

func (c *Controller) Load(image *Image, now, deadline time.Duration) error {
	if c.status.State == RebootRequired {
		return ErrReboot
	}
	if c.status.State != Idle && c.status.State != Running {
		return ErrBusy
	}
	if image == nil || image.idle {
		return errors.New("load requires a model core image")
	}
	phase := waitFrame
	if c.status.State == Running {
		phase = Save
	}
	return c.begin(image, phase, now, deadline)
}

func (c *Controller) Stop(now, deadline time.Duration) error {
	if c.status.State == RebootRequired {
		return ErrReboot
	}
	if c.status.State == Idle {
		return nil
	}
	if c.status.State != Running {
		return ErrBusy
	}
	return c.begin(c.idle, Save, now, deadline)
}

// RecoverIdle is explicit. An unknown drain/contain outcome cannot be made safe
// by retrying; only a model reboot may clear that ownership uncertainty.
func (c *Controller) RecoverIdle(now, deadline time.Duration) error {
	if c.status.State != RebootRequired {
		return ErrBusy
	}
	if c.issued != nil || c.frame != nil {
		return ErrReboot
	}
	return c.begin(c.idle, Contain, now, deadline)
}

func (c *Controller) fail(err error, now time.Duration, uncertain bool) error {
	phase := c.pending.phase
	recovery := c.pending.recovery
	primary := c.pending.primary
	issued := c.issued
	c.issued = nil
	c.status.LastError = err.Error()
	if phase == Save && !uncertain {
		c.pending = nil
		c.held = nil
		c.status.State = Running
		// Active image and core generation survive a failed save, but the old
		// input authority has already retired and must not be silently restored.
		return err
	}
	if uncertain || recovery || phase == Drain || phase == Contain || phase == waitFrame {
		c.status.State = RebootRequired
		// Non-nil issued marks unresolved backend ownership. It is deliberately
		// never replayed or freed by Stop or RecoverIdle.
		if uncertain {
			c.issued = issued
			if c.issued == nil {
				c.issued = &Command{}
			}
		}
		c.pending = nil
		c.status.DisplayGeneration = 0
		return err
	}
	if primary == nil {
		primary = err
	}
	c.pending = &transition{target: c.idle, phase: Contain, deadline: now + c.recoveryBudget, recovery: true, primary: primary}
	c.status.State = Changing
	c.status.DisplayGeneration = 0
	c.held = append(c.held, c.idle)
	return err
}

// Next returns at most one command. Until Complete, subsequent calls return nil.
// Frame waiting is read-only; deadline expiry retains ownership and never
// assumes that the renderer or device has stopped touching memory.
func (c *Controller) Next(now time.Duration) (*Command, error) {
	if now < c.lastTime {
		return nil, errors.New("model monotonic clock regressed")
	}
	c.lastTime = now
	if c.pending == nil {
		return nil, nil
	}
	if now >= c.pending.deadline {
		return nil, c.fail(ErrDeadline, now, c.issued != nil)
	}
	if c.issued != nil {
		return nil, nil
	}
	if c.pending.phase == waitFrame {
		if c.frame != nil {
			return nil, nil
		}
		c.pending.phase = Drain
	}
	if c.sequence == math.MaxUint64 {
		return nil, c.fail(ErrReboot, now, true)
	}
	c.sequence++
	image := c.pending.target
	if c.pending.phase == Save {
		image = c.active
	}
	cmd := Command{Token: Token{c.transitionID, c.sequence}, Operation: c.pending.phase, ImageID: image.id, Identity: image.identity}
	if cmd.Operation == Program {
		cmd.Bytes = bytes.Clone(image.data)
	}
	c.issued = &cmd
	out := cmd
	return &out, nil
}

func (c *Controller) Complete(token Token, result Completion, now time.Duration) error {
	if c.pending == nil || c.issued == nil || token != c.issued.Token {
		return ErrStale
	}
	if now < c.lastTime {
		return errors.New("model monotonic clock regressed")
	}
	c.lastTime = now
	if now >= c.pending.deadline {
		return c.fail(ErrDeadline, now, true)
	}
	phase := c.pending.phase
	if result.Uncertain && result.Err == nil {
		result.Err = errors.New("uncertain model completion")
	}
	if result.Err != nil {
		return c.fail(result.Err, now, result.Uncertain || phase == Drain || phase == Contain)
	}
	c.issued = nil
	switch phase {
	case Save:
		c.pending.phase = waitFrame
	case Drain:
		c.pending.phase = Contain
	case Contain:
		if !result.Quiescent {
			return c.fail(errors.New("containment did not prove quiescence"), now, true)
		}
		c.status.DisplayGeneration = 0
		c.pending.phase = Program
	case Program:
		if result.ProgrammedDigest != c.pending.target.digest {
			return c.fail(errors.New("programmed model digest mismatch"), now, false)
		}
		c.pending.phase = Identify
	case Identify:
		if result.Identity != c.pending.target.identity {
			return c.fail(errors.New("live model identity mismatch"), now, false)
		}
		c.pending.phase = Release
	case Release:
		if c.generation == math.MaxUint64 || c.displayGeneration == math.MaxUint64 {
			return c.fail(ErrReboot, now, true)
		}
		c.generation++
		c.displayGeneration++
		c.active = c.pending.target
		c.status = Status{State: Running, ImageID: c.active.id, CoreGeneration: c.generation, DisplayGeneration: c.displayGeneration, InputEnabled: true, Package: c.active.identity.Package}
		if c.active.idle {
			c.status.State = Idle
			c.status.CoreGeneration = 0
			c.status.InputEnabled = false
		}
		if c.pending.primary != nil {
			c.status.LastError = c.pending.primary.Error()
		}
		c.pending = nil
		c.held = nil
	default:
		return errors.New("invalid model phase")
	}
	return nil
}

func (c *Controller) BeginFrame() (Frame, error) {
	if c.pending != nil || c.frame != nil || c.status.DisplayGeneration == 0 {
		return Frame{}, ErrBusy
	}
	if c.sequence == math.MaxUint64 {
		return Frame{}, ErrReboot
	}
	c.sequence++
	f := Frame{c.status.DisplayGeneration, c.sequence}
	c.frame = &f
	return f, nil
}

// FinishFrame rejects stale and single-use preparations. Quiescent must mean
// both renderer and backend have relinquished their memory access; a timeout
// alone cannot finish a frame. A fault disables only the plane and retains the
// running core. Unknown frame ownership stays reserved until this same token
// receives a confirmed drain completion.
func (c *Controller) FinishFrame(frame Frame, result Completion) error {
	if c.status.State == RebootRequired || c.frame == nil || frame != *c.frame {
		return ErrStale
	}
	if !result.Quiescent || result.Uncertain {
		c.status.DisplayGeneration = 0
		c.status.LastError = "frame ownership is unresolved"
		return errors.New(c.status.LastError)
	}
	c.frame = nil
	if result.Err != nil {
		c.status.DisplayGeneration = 0
		c.status.LastError = result.Err.Error()
		return result.Err
	}
	return nil
}

func (c *Controller) ShowDisplay(coreGeneration uint64) error {
	if c.pending != nil || c.frame != nil || c.status.State != Running || coreGeneration != c.status.CoreGeneration {
		return ErrStale
	}
	if c.status.DisplayGeneration != 0 {
		return nil
	}
	if c.displayGeneration == math.MaxUint64 {
		return ErrReboot
	}
	c.displayGeneration++
	c.status.DisplayGeneration = c.displayGeneration
	return nil
}

func (c *Controller) CheckInput(coreGeneration uint64) error {
	if c.pending != nil || !c.status.InputEnabled || c.status.State != Running || coreGeneration != c.status.CoreGeneration {
		return ErrStale
	}
	return nil
}

// CompleteModel supplies the expected software-only observation. It exists for
// the emulated endpoint and must never be used as a physical completion proof.
func CompleteModel(cmd Command) Completion {
	switch cmd.Operation {
	case Contain:
		return Completion{Quiescent: true}
	case Program:
		return Completion{ProgrammedDigest: sha256.Sum256(cmd.Bytes)}
	case Identify:
		return Completion{Identity: cmd.Identity}
	default:
		return Completion{}
	}
}
