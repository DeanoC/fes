// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

func TestProgramConsoleRequestsLaunchReturnAndParkWithRealService(t *testing.T) {
	f := serviceTest(t)
	requests := make([]packagestore.Request, 3)
	for i := range requests {
		requests[i], _, _ = addProgram(t, f.loader, f.files, i)
	}
	console, err := NewProgramConsole(f.s, requests)
	if err != nil {
		t.Fatal(err)
	}
	status := console.Handle(context.Background(), []byte(`{"op":"status"}`))
	if !status.OK || status.State != ProgramReady || len(status.Choices) != 3 {
		t.Fatal(status)
	}
	// Caller mutations cannot rewrite the retained catalogue.
	requests[0].Path = "absent"
	for i := range 3 {
		launch := fmt.Sprintf(`{"op":"launch","owner":%q,"generation":%d,"program":%d}`, status.Owner, status.Generation, i)
		status = console.Handle(context.Background(), []byte(launch))
		if !status.OK || status.State != ProgramRunning {
			t.Fatal(status)
		}
		for _, op := range []string{"hold", "keyboard", "run"} {
			command := fmt.Sprintf(`{"op":%q,"owner":%q,"generation":%d`, op, status.Owner, status.Generation)
			if op == "keyboard" {
				command += `,"rows":[0,0,0,0,0,0,0,0,0]`
			}
			status = console.Handle(context.Background(), []byte(command+"}"))
			if !status.OK {
				t.Fatal(status)
			}
		}
		op := "return"
		if i == 2 {
			op = "park"
		}
		status = console.Handle(context.Background(), []byte(fmt.Sprintf(`{"op":%q,"owner":%q,"generation":%d}`, op, status.Owner, status.Generation)))
		if !status.OK {
			t.Fatal(status)
		}
		if i < 2 && status.State != ProgramReady || i == 2 && status.State != ProgramParked {
			t.Fatal(status)
		}
		calls, opens := f.cpu.calls, f.files.opens
		if reply := console.Handle(context.Background(), []byte(launch)); reply.OK || f.cpu.calls != calls || f.files.opens != opens {
			t.Fatal("replayed launch", reply)
		}
	}
	if f.cpu.configurations != 3 || f.cpu.closes != 3 || f.lease.returns != 2 || f.lease.r.Active {
		t.Fatal("wrong lifecycle counts")
	}
}

func TestProgramConsoleMalformedAndForeignRequestsPerformNoIO(t *testing.T) {
	f := serviceTest(t)
	request, _, _ := addProgram(t, f.loader, f.files, 0)
	console, err := NewProgramConsole(f.s, []packagestore.Request{request})
	if err != nil {
		t.Fatal(err)
	}
	cases := []string{
		``, `[]`, `{}`, `{"op":"status","op":"launch"}`, `{"op":"status"} {"op":"status"}`, `{"op":"status","extra":1}`,
		`{"op":"launch","owner":"0000000000000011","generation":0}`, `{"op":"launch","owner":"0000000000000012","generation":0,"program":0}`,
		`{"op":"launch","owner":"0000000000000011","generation":null,"program":0}`, `{"op":"launch","owner":"0000000000000011","generation":0,"program":null}`,
		`{"op":"launch","owner":"0000000000000011","generation":-1,"program":0}`, `{"op":"launch","owner":"0000000000000011","generation":0,"program":1}`,
		`{"op":"launch","owner":"0000000000000011","generation":0,"program":0,"base":0}`, `{"op":"launch","owner":"0000000000000011","generation":0,"program":0,"program":0}`,
		`{"op":"return","owner":"0000000000000011","generation":0}`, `{"op":"park","owner":"0000000000000011","generation":0}`,
		`{"op":"keyboard","owner":"0000000000000011","generation":0,"rows":[]}`, `{"op":"write","owner":"0000000000000011","generation":0}`,
		strings.Repeat(" ", ProgramCommandMaxBytes+1),
	}
	calls, opens := f.cpu.calls, f.files.opens
	for _, raw := range cases {
		if result := console.Handle(context.Background(), []byte(raw)); result.OK || f.cpu.calls != calls || f.files.opens != opens || f.s.Status().State != ProgramReady {
			t.Fatalf("%s: %+v", raw, result)
		}
	}
	if !console.Handle(context.Background(), []byte(`{"op":"status"}`)).OK {
		t.Fatal("bad input retired console")
	}
}
