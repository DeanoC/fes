// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	stdimage "image"
	"sync"

	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

type WindowReply struct {
	Scope      string                `json:"scope"`
	Operation  string                `json:"op"`
	OK         bool                  `json:"ok"`
	State      ProgramServiceState   `json:"state"`
	Owner      string                `json:"owner"`
	Generation uint64                `json:"generation"`
	Frames     uint64                `json:"frames"`
	PackageID  string                `json:"package_id"`
	LastFrame  *WindowFrameReceipt   `json:"last_frame,omitempty"`
	LastReturn *WindowReturnReceipt  `json:"last_return,omitempty"`
	Storage    *WindowStorageReceipt `json:"storage,omitempty"`
	Error      string                `json:"error,omitempty"`
}

// These are cached observations, never permissions or display/session handles.
// In particular, last_return remains the preceding scope after a later launch.
type WindowFrameReceipt struct {
	SourceGeneration uint64 `json:"source_generation"`
	ReaderGeneration uint64 `json:"reader_generation"`
	Cookie           uint32 `json:"cookie"`
	Bytes            uint32 `json:"bytes"`
	PublishedCRC32   uint32 `json:"published_crc32"`
	ReadCRC32        uint32 `json:"read_crc32"`
	FrameSHA256      string `json:"frame_sha256"`
	DrainGeneration  uint64 `json:"drain_generation"`
	DrainCookie      uint64 `json:"drain_cookie"`
}
type WindowReturnReceipt struct {
	SourceGeneration uint64 `json:"source_generation"`
	ClosedGeneration uint64 `json:"closed_generation"`
	Frames           uint64 `json:"frames"`
	Reset            uint32 `json:"reset"`
	MastersContained bool   `json:"masters_contained"`
}
type WindowStorageReceipt struct {
	Generation uint64 `json:"generation"`
	Active     bool   `json:"active"`
	Prepared   bool   `json:"prepared"`
}

// WindowConsole adapts bounded status/launch/present/return/park requests. Its
// sole package selection comes from the platform; pixels come from the trusted
// renderer at Handle, never from the command. Private display handles stay here.
type WindowConsole struct {
	operation sync.Mutex
	service   *WindowService
	handle    WindowHandle
	request   packagestore.Request
}

func NewWindowConsole(service *WindowService, initial WindowHandle, request packagestore.Request) (*WindowConsole, error) {
	if service == nil || service.state == nil || initial.service != service.state ||
		service.Status().State != ProgramRunning || initial.generation != service.Status().Generation ||
		request.PackageID != service.Status().PackageID {
		return nil, ErrWindowServiceSession
	}
	request.Sources = append([]packagestore.Source(nil), request.Sources...)
	request.Parts = append([]packagestore.Part(nil), request.Parts...)
	return &WindowConsole{service: service, handle: initial, request: request}, nil
}
func (c *WindowConsole) reply(op string, err error) WindowReply {
	v := c.service.Status()
	r := WindowReply{Scope: "nano-contained-window-service-v1", Operation: op, OK: err == nil, State: v.State,
		Owner: fmt.Sprintf("%016x", v.Owner), Generation: v.Generation, Frames: v.Frames, PackageID: v.PackageID}
	if err != nil {
		r.Error = err.Error()
	}
	if v.State != ProgramRebootRequired {
		f := v.LastPresentation
		r.LastFrame = &WindowFrameReceipt{SourceGeneration: f.Memory.Source.Bridges.Generation, ReaderGeneration: f.Frame.Generation,
			Cookie: f.Frame.Cookie, Bytes: f.Frame.DeliveredBytes, PublishedCRC32: f.Memory.CRC32, ReadCRC32: f.Frame.CRC32,
			FrameSHA256: fmt.Sprintf("%x", f.Memory.FrameSHA256), DrainGeneration: f.Drain.Generation, DrainCookie: f.Drain.Cookie}
		if v.LastReturn.After.Generation != 0 {
			closed := v.LastReturn
			r.LastReturn = &WindowReturnReceipt{SourceGeneration: closed.Source.Bridges.Generation, ClosedGeneration: closed.After.Generation,
				Frames: closed.Frames, Reset: closed.After.ResetMask, MastersContained: closed.After.MastersContained}
		}
		r.Storage = &WindowStorageReceipt{Generation: v.Storage.Generation, Active: v.Storage.Active, Prepared: v.Storage.Prepared}
	}
	return r
}

func (c *WindowConsole) Handle(ctx context.Context, raw []byte, frame *stdimage.RGBA) WindowReply {
	if c == nil || c.service == nil {
		return WindowReply{Scope: "nano-contained-window-service-v1", Operation: "invalid", Error: ErrWindowServiceSession.Error()}
	}
	if !c.operation.TryLock() {
		return c.reply("invalid", ErrWindowServiceBusy)
	}
	defer c.operation.Unlock()
	fields, err := commandFields(raw)
	var op string
	if err == nil {
		err = json.Unmarshal(fields["op"], &op)
	}
	if err != nil {
		return c.reply("invalid", err)
	}
	if op == "status" && exactFields(fields, "op") {
		return c.reply(op, nil)
	}
	if (op != "launch" && op != "present" && op != "return" && op != "park") || !exactFields(fields, "op", "owner", "generation") {
		return c.reply(op, errors.New("native window console: exact operation/owner/generation required"))
	}
	var owner string
	var generation uint64
	v := c.service.Status()
	if json.Unmarshal(fields["owner"], &owner) != nil || json.Unmarshal(fields["generation"], &generation) != nil ||
		owner != fmt.Sprintf("%016x", v.Owner) || generation != v.Generation {
		return c.reply(op, ErrWindowServiceSession)
	}
	if op == "launch" {
		h, err := c.service.Launch(ctx, generation, c.request, frame)
		if err == nil {
			c.handle = h
		}
		return c.reply(op, err)
	}
	if c.handle.generation != generation {
		return c.reply(op, ErrWindowServiceSession)
	}
	switch op {
	case "present":
		_, err = c.service.Present(ctx, c.handle, frame)
	case "return":
		_, err = c.service.Return(ctx, c.handle)
	case "park":
		_, err = c.service.Park(ctx, c.handle)
	}
	return c.reply(op, err)
}
