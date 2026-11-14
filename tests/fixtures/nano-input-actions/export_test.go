package nativefabric

import (
	"bytes"
	"context"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"os"
	"strconv"
	"testing"
)

func TestExportActualProgramInputActions(t *testing.T) {
	path := os.Getenv("FES_INPUT_ACTION_EXPORT_PATH")
	if path == "" {
		t.Fatal("explicit output required")
	}
	owner, err := strconv.ParseUint(os.Getenv("FES_INPUT_ACTION_EXPORT_OWNER"), 16, 64)
	if err != nil || owner == 0 {
		t.Fatal("fixture owner required", err)
	}
	f := serviceTest(t)
	var output bytes.Buffer
	p, err := NewProgramSerialPlatform(f.s, func() (byte, bool) { return 0, false }, func(_ context.Context, data []byte) error { _, e := output.Write(data); return e })
	if err != nil {
		t.Fatal(err)
	}
	for generation := uint64(0); generation <= 6; generation++ {
		v := f.s.Status()
		v.Owner = owner
		v.Generation = generation
		v.State = ProgramReady
		command := tenfoot.CmdSelect
		if generation%2 == 1 {
			v.State = ProgramRunning
			command = tenfoot.CmdStop
		}
		if generation == 6 {
			command = tenfoot.CmdQuit
		}
		f.s.state.publish(v)
		if err := p.observeProgramCommands(context.Background(), "uart", []tenfoot.Command{command}); err != nil {
			t.Fatal(err)
		}
	}
	if f.cpu.calls != 0 {
		t.Fatal("diagnostic export touched firmware")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(output.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	t.Log("exported seven actual Go input-action observations; injected status, zero firmware calls")
}
