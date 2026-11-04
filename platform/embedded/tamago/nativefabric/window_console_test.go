// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestWindowConsoleThreeFullRequestCycles(t *testing.T) {
	r := windowServiceTest(t)
	c, err := NewWindowConsole(r.s, r.h, r.request)
	if err != nil {
		t.Fatal(err)
	}
	request := func(op string) WindowReply {
		v := r.s.Status()
		raw := fmt.Sprintf(`{"op":%q,"owner":"%016x","generation":%d}`, op, v.Owner, v.Generation)
		return c.Handle(context.Background(), []byte(raw), r.f.frame)
	}
	for cycle := 0; cycle < 3; cycle++ {
		r.f.colour(byte(cycle + 2))
		if result := request("present"); !result.OK || result.Frames != 2 {
			t.Fatal(result)
		}
		if cycle == 2 {
			if result := request("park"); !result.OK || result.State != ProgramParked {
				t.Fatal(result)
			}
			break
		}
		if result := request("return"); !result.OK || result.State != ProgramReady {
			t.Fatal(result)
		}
		if result := request("launch"); !result.OK || result.State != ProgramRunning || result.Frames != 1 {
			t.Fatal(result)
		}
	}
	if r.lease.returns != 2 || r.f.cpu.closes != 3 || r.f.cpu.releases != 0 {
		t.Fatal("wrong ownership")
	}
}

func TestWindowConsoleRejectsMalformedStaleAndPackageSuppliedControlsWithoutIO(t *testing.T) {
	r := windowServiceTest(t)
	c, err := NewWindowConsole(r.s, r.h, r.request)
	if err != nil {
		t.Fatal(err)
	}
	v := r.s.Status()
	valid := fmt.Sprintf(`{"op":"present","owner":"%016x","generation":%d}`, v.Owner, v.Generation)
	for _, raw := range []string{"", `null`, `{"op":"status","op":"present"}`, `{"op":"status","extra":1}`,
		valid + `{}`, strings.Replace(valid, `"generation":1`, `"generation":0`, 1), strings.Replace(valid, fmt.Sprintf("%016x", v.Owner), "0000000000000000", 1),
		strings.Replace(valid, `"generation":1`, `"generation":null`, 1), strings.Replace(valid, `"generation":1`, `"generation":1,"address":2499805184`, 1),
		strings.Replace(valid, `"present"`, `"launch"`, 1), strings.Repeat("x", ProgramCommandMaxBytes+1)} {
		calls, reads, files := r.f.cpu.calls, r.f.cpu.reads, r.files.opens
		if result := c.Handle(context.Background(), []byte(raw), r.f.frame); result.OK {
			t.Fatal("accepted", raw, result)
		}
		if r.f.cpu.calls != calls || r.f.cpu.reads != reads || r.files.opens != files {
			t.Fatal("invalid command performed I/O", raw)
		}
	}
	status := c.Handle(context.Background(), []byte(`{"op":"status"}`), nil)
	if !status.OK || status.Scope != "nano-contained-window-service-v1" || status.Owner != fmt.Sprintf("%016x", v.Owner) {
		t.Fatal(status)
	}
}
