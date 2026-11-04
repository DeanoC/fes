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
	Scope      string              `json:"scope"`
	Operation  string              `json:"op"`
	OK         bool                `json:"ok"`
	State      ProgramServiceState `json:"state"`
	Owner      string              `json:"owner"`
	Generation uint64              `json:"generation"`
	Frames     uint64              `json:"frames"`
	PackageID  string              `json:"package_id"`
	Error      string              `json:"error,omitempty"`
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
