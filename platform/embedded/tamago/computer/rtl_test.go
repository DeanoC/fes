// SPDX-License-Identifier: GPL-3.0-or-later
package computer

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	g "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

type rtlIO struct {
	input  io.WriteCloser
	output *bufio.Reader
	writes int
}

func startRTL(t *testing.T) (*rtlIO, Identity) {
	t.Helper()
	executable := os.Getenv("FES_COMPUTER_MODEL")
	if executable == "" {
		t.Skip("run computer/verify.py with an explicit captured project")
	}
	build, err := hex.DecodeString(os.Getenv("FES_COMPUTER_BUILD_ID"))
	if err != nil || len(build) != 16 {
		t.Fatal("invalid model build ID")
	}
	cmd := exec.Command(executable)
	cmd.Dir = os.Getenv("FES_COMPUTER_MODEL_DIRECTORY")
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		input.Close()
		if err := cmd.Wait(); err != nil {
			t.Error(err)
		}
	})
	want := wanted()
	copy(want.Build[:], build)
	return &rtlIO{input: input, output: bufio.NewReader(output)}, want
}

func (r *rtlIO) call(format string, args ...any) ([]uint64, error) {
	if _, err := fmt.Fprintf(r.input, format+"\n", args...); err != nil {
		return nil, err
	}
	line, err := r.output.ReadString('\n')
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(line)
	if len(fields) < 3 || fields[0] != "R" || fields[1] != "1" {
		return nil, fmt.Errorf("Avalon model: %q", line)
	}
	values := make([]uint64, len(fields)-2)
	for i, field := range fields[2:] {
		values[i], err = strconv.ParseUint(field, 16, 64)
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}
func (r *rtlIO) Read32(offset uint32) (uint32, error) {
	v, err := r.call("read %x", offset)
	if err != nil {
		return 0, err
	}
	if len(v) != 1 {
		return 0, ErrProtocol
	}
	return uint32(v[0]), nil
}
func (r *rtlIO) Write32(offset, word uint32) error {
	r.writes++
	_, err := r.call("write %x %x", offset, word)
	return err
}
func inspect(t *testing.T, r *rtlIO) []uint64 {
	t.Helper()
	v, err := r.call("inspect")
	if err != nil || len(v) != 5 {
		t.Fatalf("inspection: %v %v", v, err)
	}
	return v
}
func advance(t *testing.T, r *rtlIO, cycles int) {
	t.Helper()
	if _, err := r.call("advance %x", cycles); err != nil {
		t.Fatal(err)
	}
}

func TestSelectedRTLFetchObservationTracksRunHoldAndRelaunch(t *testing.T) {
	r, want := startRTL(t)
	limit := limits()
	limit.Now = time.Now
	limit.Timeout = 20 * time.Second
	limit.MaxPolls = 64
	ctx := context.Background()
	c, err := Open(ctx, r, limit, want)
	if err != nil {
		t.Fatal(err)
	}
	activity := func() FetchActivity {
		t.Helper()
		v, err := c.Activity(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if err := c.Hold(ctx); err != nil {
		t.Fatal(err)
	}
	advance(t, r, 1000)
	held := activity()
	// GP traffic and repeated reads while held cannot manufacture fetches.
	if err := c.Keyboard(ctx, [9]uint16{}); err != nil {
		t.Fatal(err)
	}
	advance(t, r, 10000)
	if next := activity(); next != held {
		t.Fatal("held observation changed", held, next)
	}
	for cycle := 0; cycle < 2; cycle++ {
		if err := c.Run(ctx); err != nil {
			t.Fatal(err)
		}
		// No GP commands during this interval: only the real machine runs.
		advance(t, r, 100000)
		active := activity()
		if active.Cycles <= held.Cycles {
			t.Fatal("actual Z80 fetches were not observed", held, active)
		}
		// A count read samples the address, which remains available while the
		// CPU keeps fetching. Another address read cannot silently resample it.
		advance(t, r, 100000)
		address, err := r.Read32(g.FesDe25ComputerFetchAddressOffset)
		if err != nil || address != uint32(active.Address) {
			t.Fatal("fetch observation tore during later CPU activity", active, address, err)
		}
		if err := c.Hold(ctx); err != nil {
			t.Fatal(err)
		}
		advance(t, r, 1000)
		held = activity()
		if held.Cycles < active.Cycles {
			t.Fatal("Hold reset the observation counter", active, held)
		}
		advance(t, r, 10000)
		if next := activity(); next != held {
			t.Fatal("actual fetches continued after Hold", held, next)
		}
	}
	if err := r.Write32(g.FesDe25ComputerFetchCountOffset, 0); err == nil {
		t.Fatal("fetch counter accepted a CPU write")
	}
	if _, err := c.Activity(ctx); !errors.Is(err, ErrProtocol) {
		t.Fatal("write fault did not retire observation", err)
	}
}

func TestSelectedRTLLaunchInputMediaReturnAndRelaunch(t *testing.T) {
	r, want := startRTL(t)
	limit := limits()
	limit.Now = time.Now
	limit.Timeout = 20 * time.Second
	limit.MaxPolls = 64
	c, err := Open(context.Background(), r, limit, want)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := c.Hold(ctx); err != nil {
		t.Fatal(err)
	}
	advance(t, r, 1000)
	a := inspect(t, r)
	advance(t, r, 5000)
	b := inspect(t, r)
	if a[0] != 1 || b[0] != 1 || a[1] != b[1] {
		t.Fatal("held CPU executed", a, b)
	}
	if err := c.Run(ctx); err != nil {
		t.Fatal(err)
	}
	advance(t, r, 100000)
	active := inspect(t, r)
	if active[0] != 0 || active[1] <= b[1]+100 {
		t.Fatal("no actual Z80 bus activity after release", active)
	}
	if err := c.Keyboard(ctx, [9]uint16{0: 16, 8: 1}); err != nil {
		t.Fatal(err)
	}
	keys := inspect(t, r)
	if keys[2] != 16 || keys[3] != 1 {
		t.Fatal("keyboard rows differ", keys)
	}
	// More than one chunk and an odd final byte; verify the actual tape RAM,
	// then replace and eject without issuing an execution-reset command.
	for _, size := range []int{1025, 1, 16375} {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i*37 + 19)
		}
		if err := c.LoadTape(ctx, data); err != nil {
			t.Fatalf("size %d: %v", size, err)
		}
		for i, want := range data {
			v, err := r.call("byte %x", i)
			if err != nil || len(v) != 1 || v[0] != uint64(want) {
				t.Fatalf("tape byte %d: %v %v", i, v, err)
			}
		}
		if inspect(t, r)[0] != 0 {
			t.Fatal("media replacement reset execution")
		}
		if err := c.EjectTape(ctx); err != nil {
			t.Fatal(err)
		}
		if inspect(t, r)[4] != 1 {
			t.Fatal("tape not empty")
		}
	}
	if err := c.Hold(ctx); err != nil {
		t.Fatal(err)
	}
	advance(t, r, 1000)
	a = inspect(t, r)
	advance(t, r, 5000)
	b = inspect(t, r)
	if b[0] != 1 || a[1] != b[1] || b[2] != 0 || b[3] != 0 {
		t.Fatal("return did not hold and neutralize", a, b)
	}
	if err := c.Run(ctx); err != nil {
		t.Fatal(err)
	}
	advance(t, r, 100000)
	if inspect(t, r)[1] <= b[1]+100 {
		t.Fatal("relaunch did not execute")
	}
	if err := c.Hold(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSelectedRTLIdentityAndEpochFailures(t *testing.T) {
	t.Run("mismatch-before-release", func(t *testing.T) {
		r, want := startRTL(t)
		want.Build[15] ^= 1
		if _, err := Open(context.Background(), r, limits(), want); !errors.Is(err, ErrIdentity) {
			t.Fatal(err)
		}
		if inspect(t, r)[0] != 1 || r.writes != 16 {
			t.Fatal("identity mismatch permitted mutation")
		}
	})
	t.Run("reset-poisons-epoch", func(t *testing.T) {
		r, want := startRTL(t)
		c, err := Open(context.Background(), r, limits(), want)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		advance(t, r, 5000)
		if _, err := r.call("reset"); err != nil {
			t.Fatal(err)
		}
		before := r.writes
		if err := c.Run(context.Background()); !errors.Is(err, ErrProtocol) {
			t.Fatal(err)
		}
		if r.writes != before {
			t.Fatal("faulted epoch got a command")
		}
		if err := c.Run(context.Background()); !errors.Is(err, ErrRetired) {
			t.Fatal(err)
		}
		advance(t, r, 1000)
		a := inspect(t, r)
		advance(t, r, 5000)
		if inspect(t, r)[1] != a[1] {
			t.Fatal("poisoned transport left the actual CPU executing")
		}
	})
}
