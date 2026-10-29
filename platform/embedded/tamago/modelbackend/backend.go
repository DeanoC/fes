// SPDX-License-Identifier: GPL-3.0-or-later

// Package modelbackend connects tenfoot's existing HTTP client to the retained
// package lifecycle specification. It has no physical hardware backend.
package modelbackend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
	"github.com/DeanoC/fes/platform/embedded/tamago/runtimeadmission"
	"github.com/DeanoC/misteross/expansion"
)

type Source struct {
	ID     string `json:"id"`
	Role   string `json:"role"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type Part struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type Entry struct {
	CompositionID string   `json:"composition_id,omitempty"`
	Parts         []Part   `json:"parts,omitempty"`
	Check         string   `json:"check"`
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Path          string   `json:"path"`
	PackageID     string   `json:"package_id"`
	ReceiptSHA256 string   `json:"receipt_sha256"`
	Sources       []Source `json:"sources"`
	// Fault is restricted to this software endpoint's diagnostic fixtures.
	Fault string `json:"fault,omitempty"`
}
type Config struct {
	Scope   string  `json:"scope"`
	Entries []Entry `json:"entries"`
}
type Event struct {
	Kind      string                `json:"kind"`
	Game      string                `json:"game,omitempty"`
	Operation fabricmodel.Operation `json:"operation,omitempty"`
	Status    fabricmodel.Status    `json:"status"`
	Error     string                `json:"error,omitempty"`
}
type View struct {
	PartsCompositionID               string
	Action                           string
	Status                           fabricmodel.Status
	Busy                             bool
	Game                             string
	Error                            string
	Programs, Saves, Launches, Stops uint64
}
type Frame struct {
	token      fabricmodel.Frame
	Generation uint64
}

type result struct {
	generation uint64
	status     int
	body       []byte
	err        error
	frame      fabricmodel.Frame
}
type call struct {
	bound        bool
	ctx          context.Context
	method       string
	game, flight string
	expected     expectedSession
	frame        fabricmodel.Frame
	generation   uint64
	key          string
	reply        chan result
}
type expectedSession struct {
	ID         string `json:"id"`
	Game       string `json:"game_id"`
	Target     string `json:"target"`
	TargetID   string `json:"target_id"`
	PackageID  string `json:"package_id"`
	Generation uint64 `json:"generation"`
	Flight     string `json:"flight_id"`
}
type prepared struct {
	linked *corepackage.LinkedArchive
	err    error
}
type pending struct {
	composition *expansion.PartsComposition
	call        call
	entry       Entry
	cancel      context.CancelFunc
	ready       chan prepared
	started     bool
	faultUsed   bool
	failure     error
}

// Backend owns one actor. Controller calls, frame tokens and input generations
// are serialized there; HTTP workers never manipulate the controller directly.
type Backend struct {
	composition *expansion.PartsComposition
	gate        *runtimeadmission.Gate
	controller  *fabricmodel.Controller
	entries     []Entry
	read        func(string) ([]byte, error)
	log         func(Event)
	calls       chan call
	done        chan struct{}
	mu          sync.RWMutex
	view        View
	wire        []byte
}

func New(policy runtimeadmission.Policy, config Config, read func(string) ([]byte, error), log func(Event)) (*Backend, error) {
	if config.Scope != "qemu-model-only" || read == nil || len(config.Entries) == 0 || len(config.Entries) > 32 {
		return nil, errors.New("invalid model session configuration")
	}
	gate, err := runtimeadmission.NewModel(policy)
	if err != nil {
		return nil, err
	}
	entries := append([]Entry(nil), config.Entries...)
	seen := map[string]bool{}
	for i, e := range entries {
		if e.ID == "" || seen[e.ID] || e.Path == "" || len(e.PackageID) != 64 || len(e.ReceiptSHA256) != 64 {
			return nil, errors.New("invalid model catalog entry")
		}
		switch e.Fault {
		case "", "cancel", "identity", "program", "save":
		default:
			return nil, errors.New("unsupported model endpoint fault")
		}
		seen[e.ID] = true
		entries[i].Sources = append([]Source(nil), e.Sources...)
		entries[i].Parts = append([]Part(nil), e.Parts...)
	}
	data := []byte("model session idle")
	idle, err := fabricmodel.Prepare("model.idle", data, sha256.Sum256(data), fabricmodel.Identity{ABI: 1, Core: 1, Build: 1}, true)
	if err != nil {
		return nil, err
	}
	controller, err := fabricmodel.New(idle, 2*time.Second)
	if err != nil {
		return nil, err
	}
	if err = controller.Boot(0, 2*time.Second); err != nil {
		return nil, err
	}
	for n := 0; n < 8; n++ {
		command, e := controller.Next(0)
		if e != nil {
			return nil, e
		}
		if command == nil {
			break
		}
		if e = controller.Complete(command.Token, fabricmodel.CompleteModel(*command), 0); e != nil {
			return nil, e
		}
	}
	b := &Backend{gate: gate, controller: controller, entries: entries, read: read, log: log, calls: make(chan call, 8), done: make(chan struct{})}
	b.publish(View{Status: controller.Status()}, expectedSession{})
	go b.run()
	return b, nil
}
func (b *Backend) View() View { b.mu.RLock(); defer b.mu.RUnlock(); return b.view }
func (b *Backend) emit(kind, game string, operation fabricmodel.Operation, err error) {
	if b.log != nil {
		event := Event{Kind: kind, Game: game, Operation: operation, Status: b.controller.Status()}
		if err != nil {
			event.Error = err.Error()
		}
		b.log(event)
	}
}
func (b *Backend) publish(view View, session expectedSession) {
	view.Status = b.controller.Status()
	view.PartsCompositionID = ""
	if view.Status.State == fabricmodel.Running && b.composition != nil {
		view.PartsCompositionID = b.composition.ID
	}
	state := "idle"
	if view.Status.State == fabricmodel.Running {
		state = "active"
	}
	if view.Status.State == fabricmodel.RebootRequired {
		state = "failed"
	}
	if view.Status.State == fabricmodel.Changing {
		state = "launching"
		if view.Action == "stop" {
			state = "stopping"
		}
	}
	wire := map[string]any{"state": state, "execution": "fpga_model", "media": "stopped"}
	if state == "active" {
		wire["id"], wire["game_id"], wire["target"], wire["target_id"], wire["flight_id"] = session.ID, session.Game, session.Target, session.TargetID, session.Flight
		wire["system"], wire["media"] = "model", "active"
		p := view.Status.Package
		wire["core_package"] = &hostclient.SessionCorePackage{CoreID: p.CoreID, PackageID: p.PackageID, Generation: view.Status.CoreGeneration, PartsComposition: b.composition, Gamepad: true, ABI: hostclient.SessionCoreABI{ID: p.ABI, Major: p.Major, Minor: p.Minor}}
	}
	data, _ := json.Marshal(wire)
	b.mu.Lock()
	b.view = view
	b.wire = data
	b.mu.Unlock()
}
func respond(c call, status int, wire any, err error) {
	data, _ := json.Marshal(wire)
	c.reply <- result{status: status, body: data, err: err}
}
func (b *Backend) snapshot() []byte { b.mu.RLock(); defer b.mu.RUnlock(); return bytes.Clone(b.wire) }
func (b *Backend) request(c call) (result, error) {
	c.reply = make(chan result, 1)
	select {
	case <-c.ctx.Done():
		return result{}, c.ctx.Err()
	case <-b.done:
		return result{}, errors.New("model backend closed")
	case b.calls <- c:
	}
	select {
	case reply := <-c.reply:
		return reply, reply.err
	case <-c.ctx.Done():
		return result{}, c.ctx.Err()
	case <-b.done:
		return result{}, errors.New("model backend closed")
	}
}
func (b *Backend) BeginFrame(ctx context.Context) (Frame, error) {
	r, e := b.request(call{ctx: ctx, method: "frame.begin"})
	return Frame{token: r.frame, Generation: r.generation}, e
}
func (b *Backend) FinishFrame(ctx context.Context, frame Frame) error {
	_, e := b.request(call{ctx: ctx, method: "frame.finish", frame: frame.token})
	return e
}
func (b *Backend) Input(ctx context.Context, generation uint64, key string) error {
	_, e := b.request(call{ctx: ctx, method: "input", generation: generation, key: key})
	return e
}
func (b *Backend) Close(ctx context.Context) error {
	_, e := b.request(call{ctx: ctx, method: "close"})
	return e
}

// cooperativeContext gives the existing parser/linker cancellation checkpoints
// a yield on TamaGo, which has no asynchronous goroutine preemption.
type cooperativeContext struct {
	context.Context
	checks uint32
}

func (c *cooperativeContext) Err() error {
	c.checks++
	if runtime.GOOS == "tamago" && c.checks%256 == 0 {
		runtime.Gosched()
	}
	return c.Context.Err()
}
func (b *Backend) prepare(ctx context.Context, e Entry) (*corepackage.LinkedArchive, error) {
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	cooperative := &cooperativeContext{Context: work}
	body, err := b.read(e.Path)
	if err != nil {
		return nil, err
	}
	archive, err := corepackage.RetainArchive(cooperative, int64(len(body)), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if archive.Inspection().PackageID != e.PackageID {
		return nil, errors.New("package selection changed")
	}
	sources := []corepackage.ROMSource{}
	for _, source := range e.Sources {
		data, err := b.read(source.Path)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != source.SHA256 {
			return nil, errors.New("ROM source differs from selected digest")
		}
		sources = append(sources, corepackage.ROMSource{ID: source.ID, Role: source.Role, Data: data})
		if e.Fault == "cancel" {
			cancel()
		}
	}
	parts := []corepackage.PartInput{}
	for _, part := range e.Parts {
		data, err := b.read(part.Path)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != part.SHA256 {
			return nil, errors.New("part archive differs from selected digest")
		}
		parts = append(parts, corepackage.PartInput{ID: part.ID, Archive: data})
	}
	return archive.LinkROMs(cooperative, sources, parts...)
}

func (b *Backend) run() {
	defer close(b.done)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	start := time.Now()
	var job *pending
	var closing *call
	var session expectedSession
	var active Entry
	view := b.View()
	finish := func(err error) {
		if job == nil {
			return
		}
		job.cancel()
		view.Busy = false
		view.Action = ""
		if b.controller.Status().State == fabricmodel.Idle {
			session = expectedSession{}
			b.composition = nil
			active = Entry{}
			view.Game = ""
		}
		if err != nil {
			view.Error = err.Error()
		} else {
			view.Error = ""
		}
		b.publish(view, session)
		if err != nil {
			respond(job.call, 409, map[string]any{"state": sessionState(b.controller.Status()), "error": map[string]any{"code": "MODEL_OPERATION_FAILED", "message": err.Error()}}, nil)
		} else {
			job.call.reply <- result{status: 200, body: b.snapshot()}
		}
		b.emit("finished", job.entry.ID, "", err)
		job = nil
	}
	for {
		select {
		case c := <-b.calls:
			now := time.Since(start)
			if err := c.ctx.Err(); err != nil {
				c.reply <- result{err: err}
				continue
			}
			switch c.method {
			case "frame.begin":
				if closing != nil {
					c.reply <- result{err: fabricmodel.ErrBusy}
					continue
				}
				f, e := b.controller.BeginFrame()
				c.reply <- result{frame: f, generation: b.controller.Status().DisplayGeneration, err: e}
			case "frame.finish":
				e := b.controller.FinishFrame(c.frame, fabricmodel.Completion{Quiescent: true})
				c.reply <- result{err: e}
				if e != nil {
					b.emit("frame.finished", "", "", e)
				}
			case "input":
				if closing != nil {
					c.reply <- result{err: fabricmodel.ErrStale}
					continue
				}
				e := b.controller.CheckInput(c.generation)
				c.reply <- result{err: e}
				b.emit("input", c.key, "", e)
			case "close":
				if closing != nil {
					c.reply <- result{err: fabricmodel.ErrBusy}
					continue
				}
				if job == nil && (b.controller.Status().State == fabricmodel.Idle || b.controller.Status().State == fabricmodel.Running) {
					frame, err := b.controller.BeginFrame()
					if err != nil {
						c.reply <- result{err: err}
						continue
					}
					if err = b.controller.FinishFrame(frame, fabricmodel.Completion{Quiescent: true}); err != nil {
						c.reply <- result{err: err}
						continue
					}
				}
				closing = &c
				if job != nil {
					job.cancel()
				}
			case "launch", "stop":
				if closing != nil || job != nil {
					respond(c, 409, map[string]any{"error": map[string]string{"code": "BUSY", "message": "model operation in progress"}}, nil)
					continue
				}
				if c.method == "stop" && c.bound && (c.expected.ID == "" || c.expected != session) {
					respond(c, 409, map[string]any{"error": map[string]string{"code": "SESSION_CHANGED", "message": "displayed model session changed"}}, nil)
					continue
				}
				ctx, cancel := context.WithCancel(c.ctx)
				job = &pending{call: c, cancel: cancel, entry: active}
				if c.method == "launch" {
					found := false
					for _, entry := range b.entries {
						if entry.ID == c.game {
							job.entry = entry
							found = true
							break
						}
					}
					if !found {
						finish(errors.New("model title is not installed"))
						continue
					}
					job.ready = make(chan prepared, 1)
					ready, entry := job.ready, job.entry
					view.Launches++
					go func() { linked, err := b.prepare(ctx, entry); ready <- prepared{linked, err} }()
				} else {
					view.Stops++
					err := b.controller.Stop(now, now+2*time.Second)
					if err != nil {
						finish(err)
						continue
					}
					job.started = true
				}
				view.Busy = true
				view.Action = c.method
				b.publish(view, session)
				b.emit("requested", c.game, "", nil)
			}
		case <-ticker.C:
			now := time.Since(start)
			if job != nil && !job.started {
				select {
				case p := <-job.ready:
					if p.err == nil {
						p.err = job.call.ctx.Err()
					}
					if closing != nil && p.err == nil {
						p.err = context.Canceled
					}
					if p.err != nil {
						finish(p.err)
						continue
					}
					if err := b.gate.LoadLinkedModel(b.controller, p.linked, job.entry.PackageID, job.entry.ReceiptSHA256, now, now+2*time.Second); err != nil {
						finish(err)
						continue
					}
					job.composition = p.linked.Receipt().PartsComposition
					job.started = true
					b.emit("admitted", job.entry.ID, "", nil)
				default:
				}
			}
			command, err := b.controller.Next(now)
			if err != nil && job != nil {
				job.failure = err
			}
			if command != nil {
				completion := fabricmodel.CompleteModel(*command)
				game := ""
				if job != nil {
					game = job.entry.ID
					target := command.ImageID == "model.linked"
					if !job.faultUsed {
						switch {
						case target && job.entry.Fault == "identity" && command.Operation == fabricmodel.Identify:
							completion.Identity.Package.ActivationReceiptSHA256 = "wrong model receipt"
							job.faultUsed = true
						case target && job.entry.Fault == "program" && command.Operation == fabricmodel.Program:
							completion.ProgrammedDigest = [32]byte{}
							job.faultUsed = true
						case job.call.method == "stop" && active.Fault == "save" && command.Operation == fabricmodel.Save:
							completion.Err = errors.New("injected model save failure")
							active.Fault = ""
							job.faultUsed = true
						}
					}
					if target && job.call.ctx.Err() != nil && (command.Operation == fabricmodel.Program || command.Operation == fabricmodel.Identify || command.Operation == fabricmodel.Release) {
						completion.Err = job.call.ctx.Err()
					}
					if closing != nil && target && (command.Operation == fabricmodel.Program || command.Operation == fabricmodel.Identify || command.Operation == fabricmodel.Release) {
						completion.Err = context.Canceled
					}
				}
				if command.Operation == fabricmodel.Program {
					view.Programs++
				}
				if command.Operation == fabricmodel.Save {
					view.Saves++
				}
				err = b.controller.Complete(command.Token, completion, now)
				if err != nil && job != nil {
					job.failure = err
				}
				b.emit("command", game, command.Operation, err)
			}
			if job != nil && job.started && b.controller.Status().State != fabricmodel.Changing {
				if job.failure == nil && job.call.method == "launch" && b.controller.Status().State == fabricmodel.Running {
					active = job.entry
					b.composition = job.composition
					view.Game = active.ID
					s := b.controller.Status()
					session = expectedSession{ID: fmt.Sprintf("model-%d", s.CoreGeneration), Game: active.ID, Target: "qemu-model", TargetID: "qemu-model", PackageID: s.Package.PackageID, Generation: s.CoreGeneration, Flight: job.call.flight}
				}
				finish(job.failure)
			}
			if closing != nil && job == nil {
				switch b.controller.Status().State {
				case fabricmodel.Idle:
					frame, err := b.controller.BeginFrame()
					if err != nil {
						closing.reply <- result{err: err}
						closing = nil
						continue
					}
					if err = b.controller.FinishFrame(frame, fabricmodel.Completion{Quiescent: true}); err != nil {
						closing.reply <- result{err: err}
						closing = nil
						continue
					}
					session = expectedSession{}
					b.composition = nil
					view.Game = ""
					view.Busy = false
					view.Action = ""
					b.publish(view, session)
					closing.reply <- result{}
					return
				case fabricmodel.Running:
					if err := b.controller.Stop(now, now+2*time.Second); err != nil {
						closing.reply <- result{err: err}
						return
					}
				case fabricmodel.RebootRequired:
					closing.reply <- result{err: fabricmodel.ErrReboot}
					return
				}
			}
			b.publish(view, session)
		}
	}
}
func sessionState(s fabricmodel.Status) string {
	if s.State == fabricmodel.Running {
		return "active"
	}
	if s.State == fabricmodel.RebootRequired {
		return "failed"
	}
	return "idle"
}
