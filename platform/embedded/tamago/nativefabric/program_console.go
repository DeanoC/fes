// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

const ProgramCommandMaxBytes = 1024

type ProgramChoice struct {
	Index     int    `json:"index"`
	PackageID string `json:"package_id"`
}

type ProgramReply struct {
	Scope            string                  `json:"scope"`
	Operation        string                  `json:"op"`
	OK               bool                    `json:"ok"`
	State            ProgramServiceState     `json:"state"`
	Owner            string                  `json:"owner"`
	Generation       uint64                  `json:"generation"`
	Held             bool                    `json:"held"`
	PackageID        string                  `json:"package_id,omitempty"`
	BuildID          string                  `json:"build_id,omitempty"`
	CoreID           string                  `json:"core_id,omitempty"`
	ROMReceiptSHA256 string                  `json:"rom_receipt_sha256,omitempty"`
	Choices          []ProgramChoice         `json:"programs,omitempty"`
	Activity         *computer.FetchActivity `json:"activity,omitempty"`
	Error            string                  `json:"error,omitempty"`
}

// ProgramConsole is a bounded UART request adapter, not a network server or
// compatibility authority. It holds the private live session; callers supply
// only the observed boot owner/generation and an index in a retained SD list.
type ProgramConsole struct {
	operation sync.Mutex
	service   *ProgramService
	requests  []packagestore.Request
	session   ProgramSession
}

func NewProgramConsole(service *ProgramService, choices []packagestore.Request) (*ProgramConsole, error) {
	if service == nil || service.state == nil || service.Status().State != ProgramReady || len(choices) == 0 || len(choices) > 32 {
		return nil, errors.New("native program console: ready service and bounded selections required")
	}
	requests := append([]packagestore.Request(nil), choices...)
	for i := range requests {
		requests[i].Sources = append([]packagestore.Source(nil), requests[i].Sources...)
		requests[i].Parts = append([]packagestore.Part(nil), requests[i].Parts...)
	}
	return &ProgramConsole{service: service, requests: requests}, nil
}

func commandFields(raw []byte) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > ProgramCommandMaxBytes {
		return nil, errors.New("native program console: command size exceeds bound")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return nil, errors.New("native program console: command object required")
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		key, err := d.Token()
		name, valid := key.(string)
		if err != nil || !valid || fields[name] != nil {
			return nil, errors.New("native program console: duplicate or invalid field")
		}
		var value json.RawMessage
		if err = d.Decode(&value); err != nil {
			return nil, err
		}
		fields[name] = value
	}
	last, err := d.Token()
	if err != nil || last != json.Delim('}') {
		return nil, errors.New("native program console: incomplete command")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, errors.New("native program console: trailing command")
	}
	return fields, nil
}

func exactFields(fields map[string]json.RawMessage, names ...string) bool {
	if len(fields) != len(names) {
		return false
	}
	for _, name := range names {
		if len(fields[name]) == 0 || bytes.Equal(bytes.TrimSpace(fields[name]), []byte("null")) {
			return false
		}
	}
	return true
}

func (c *ProgramConsole) reply(op string, activity *computer.FetchActivity, err error) ProgramReply {
	status := c.service.Status()
	r := ProgramReply{Scope: "nano-contained-program-service-v1", Operation: op, OK: err == nil,
		State: status.State, Owner: fmt.Sprintf("%016x", status.Owner), Generation: status.Generation,
		Held: status.Held, PackageID: status.Identity.PackageID, BuildID: status.Identity.BuildID, CoreID: status.Identity.CoreID,
		ROMReceiptSHA256: status.ROMReceiptSHA256, Activity: activity}
	if err != nil {
		r.Error = err.Error()
	}
	if op == "status" {
		for i, request := range c.requests {
			r.Choices = append(r.Choices, ProgramChoice{i, request.PackageID})
		}
	}
	return r
}

// Handle processes exactly one bounded JSON command. Unknown/duplicate fields,
// stale boot/generation and invalid selections reject before hardware I/O.
// The UART loop waits for each reply before accepting another command.
func (c *ProgramConsole) Handle(ctx context.Context, raw []byte) ProgramReply {
	if !c.operation.TryLock() {
		return c.reply("invalid", nil, ErrProgramBusy)
	}
	defer c.operation.Unlock()
	fields, err := commandFields(raw)
	var op string
	if err == nil {
		err = json.Unmarshal(fields["op"], &op)
	}
	if err != nil {
		return c.reply("invalid", nil, err)
	}
	if op == "status" && exactFields(fields, "op") {
		return c.reply(op, nil, nil)
	}
	keys := []string{"op", "owner", "generation"}
	switch op {
	case "launch":
		keys = append(keys, "program")
	case "keyboard":
		keys = append(keys, "rows")
	case "return", "park", "run", "hold", "activity":
	default:
		return c.reply(op, nil, errors.New("native program console: unsupported operation"))
	}
	if !exactFields(fields, keys...) {
		return c.reply(op, nil, errors.New("native program console: complete exact fields required"))
	}
	var owner string
	var generation uint64
	if json.Unmarshal(fields["owner"], &owner) != nil || json.Unmarshal(fields["generation"], &generation) != nil ||
		owner != fmt.Sprintf("%016x", c.service.Status().Owner) {
		return c.reply(op, nil, ErrProgramSession)
	}
	if op == "launch" {
		var index int
		if json.Unmarshal(fields["program"], &index) != nil || index < 0 || index >= len(c.requests) {
			return c.reply(op, nil, errors.New("native program console: selection index outside catalogue"))
		}
		session, err := c.service.Launch(ctx, generation, c.requests[index])
		if err == nil {
			c.session = session
		}
		return c.reply(op, nil, err)
	}
	if c.session.service == nil || c.session.generation != generation {
		return c.reply(op, nil, ErrProgramSession)
	}
	var activity *computer.FetchActivity
	switch op {
	case "return":
		_, err = c.service.Return(ctx, c.session)
	case "park":
		_, err = c.service.Park(ctx, c.session)
	case "run", "hold":
		err = c.service.Execution(ctx, c.session, op == "run")
	case "keyboard":
		var rows []uint16
		if json.Unmarshal(fields["rows"], &rows) != nil || len(rows) != 9 {
			return c.reply(op, nil, errors.New("native program console: exactly nine HID rows required"))
		}
		err = c.service.Keyboard(ctx, c.session, [9]uint16(rows))
	case "activity":
		value, e := c.service.Activity(ctx, c.session)
		err = e
		if err == nil {
			activity = &value
		}
	}
	return c.reply(op, activity, err)
}
