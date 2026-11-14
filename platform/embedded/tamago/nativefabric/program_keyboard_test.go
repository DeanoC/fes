// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

func keyboardFixture(t *testing.T) (*serviceFixture, *ProgramBackend, *hostclient.Client, hostclient.SessionResult, ProgramKeyboard) {
	t.Helper()
	f, b, c := backendFixture(t)
	play, err := c.Launch(context.Background(), "native-0")
	if err != nil {
		t.Fatal(err)
	}
	k, err := b.BindKeyboard(context.Background(), play.CorePackage.Generation)
	if err != nil {
		t.Fatal(err)
	}
	return f, b, c, play, k
}
func TestProgramKeyboardReportsReachOwnedComputer(t *testing.T) {
	f, b, _, play, k := keyboardFixture(t)
	ctx := context.Background()
	if f.cpu.keyboardWrites != 9 || f.cpu.keyboardRows != ([9]uint16{}) {
		t.Fatal("binding did not neutralize all rows")
	}
	if _, err := b.BindKeyboard(ctx, play.CorePackage.Generation); !errors.Is(err, ErrProgramBusy) {
		t.Fatal("duplicate binding", err)
	}
	// The Enter held from launching the menu must not reach the new core.
	for _, report := range [][]byte{{0, 0, 40, 0, 0, 0, 0, 0}, {0, 0, 40, 0, 0, 0, 0, 0}, {0, 0, 0, 0, 0, 0, 0, 0}} {
		if err := k.Report(ctx, report); err != nil {
			t.Fatal(err)
		}
	}
	if f.cpu.keyboardWrites != 9 {
		t.Fatal("held menu key leaked")
	}
	report := []byte{0x82, 0, 4, 0x7f, 0, 0, 0, 0}
	if err := k.Report(ctx, report); err != nil {
		t.Fatal(err)
	}
	expected := [9]uint16{0: 1 << 4, 7: 1 << 15, 8: 0x82}
	if f.cpu.keyboardRows != expected || f.cpu.keyboardWrites != 18 {
		t.Fatalf("rows=%x writes=%d", f.cpu.keyboardRows, f.cpu.keyboardWrites)
	}
	// Reports are snapshots, not repeats; copying a handle shares its decoder.
	copyHandle := k
	if err := copyHandle.Report(ctx, report); err != nil {
		t.Fatal(err)
	}
	if f.cpu.keyboardWrites != 18 {
		t.Fatal("duplicate report rewrote hardware")
	}
	bad := [][]byte{{0}, {0, 0, 1, 0, 0, 0, 0, 0}, {0, 0, 4, 4, 0, 0, 0, 0}, {0, 0, 0xe0, 0, 0, 0, 0, 0}}
	for _, r := range bad {
		if err := k.Report(ctx, r); err == nil {
			t.Fatal("malformed report accepted", r)
		}
		if f.cpu.keyboardRows != expected || f.cpu.keyboardWrites != 18 {
			t.Fatal("malformed report changed hardware")
		}
	}
	if err := copyHandle.Report(ctx, make([]byte, 8)); err != nil {
		t.Fatal(err)
	}
	if f.cpu.keyboardRows != ([9]uint16{}) || f.cpu.keyboardWrites != 27 {
		t.Fatal("release lost")
	}
	// ABI-unrepresentable usages do not turn into arbitrary keys.
	if err := k.Report(ctx, []byte{0, 0, 0x80, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if f.cpu.keyboardWrites != 27 {
		t.Fatal("unrepresentable usage wrote hardware")
	}
}
func TestProgramKeyboardReturnRevokesReportsAndDisconnect(t *testing.T) {
	f, b, c, play, k := keyboardFixture(t)
	ctx := context.Background()
	if _, err := c.StopExpectedStamped(ctx, play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	play, err := c.Launch(ctx, "native-1")
	if err != nil {
		t.Fatal(err)
	}
	current, err := b.BindKeyboard(ctx, play.CorePackage.Generation)
	if err != nil {
		t.Fatal(err)
	}
	calls, writes := f.cpu.calls, f.cpu.writes
	if err := k.Report(ctx, make([]byte, 8)); !errors.Is(err, ErrProgramSession) {
		t.Fatal(err)
	}
	if err := k.Disconnect(ctx); !errors.Is(err, ErrProgramSession) {
		t.Fatal(err)
	}
	if f.cpu.calls != calls || f.cpu.writes != writes {
		t.Fatal("old handle touched replacement core")
	}
	if err := current.Report(ctx, make([]byte, 8)); err != nil {
		t.Fatal(err)
	}
	if err := current.Report(ctx, []byte{0, 0, 4, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := current.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
	if f.cpu.keyboardRows != ([9]uint16{}) {
		t.Fatal("disconnect left keys down")
	}
	calls = f.cpu.calls
	copied := current
	if err := copied.Disconnect(ctx); !errors.Is(err, ErrProgramSession) {
		t.Fatal(err)
	}
	if f.cpu.calls != calls {
		t.Fatal("disconnect retried")
	}
	replacement, err := b.BindKeyboard(ctx, play.CorePackage.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if err := replacement.Report(ctx, []byte{0, 0, 4, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if f.cpu.keyboardRows != ([9]uint16{}) {
		t.Fatal("reconnect bypassed all-up fence")
	}
}
func TestProgramKeyboardBusyCancellationAndPartialFailure(t *testing.T) {
	f, b, _, _, k := keyboardFixture(t)
	ctx := context.Background()
	if err := k.Report(ctx, make([]byte, 8)); err != nil {
		t.Fatal(err)
	}
	report := []byte{0, 0, 4, 0, 0, 0, 0, 0}
	calls := f.cpu.calls
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if err := k.Report(cancelCtx, report); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	b.operation.Lock()
	err := k.Report(ctx, report)
	b.operation.Unlock()
	if !errors.Is(err, ErrProgramBusy) || f.cpu.calls != calls {
		t.Fatal("busy/canceled input touched hardware", err)
	}
	// Fail after one row has committed: neither retry nor SD resume is safe.
	f.cpu.failKeyboardAt = f.cpu.keyboardWrites + 2
	if err := k.Report(ctx, report); err == nil {
		t.Fatal("partial write hidden")
	}
	if f.s.Status().State != ProgramRebootRequired || f.lease.r.Active || !k.state.closed {
		t.Fatal("partial write did not retire ownership")
	}
	calls, writes := f.cpu.calls, f.cpu.writes
	if err := k.Report(ctx, report); err == nil {
		t.Fatal("retired report accepted")
	}
	if err := k.Disconnect(ctx); err == nil {
		t.Fatal("retired disconnect accepted")
	}
	if f.cpu.calls != calls || f.cpu.writes != writes {
		t.Fatal("retired input retried")
	}
}
func TestProgramKeyboardRejectsUnownedBindings(t *testing.T) {
	f, b, c := backendFixture(t)
	ctx := context.Background()
	calls := f.cpu.calls
	if _, err := b.BindKeyboard(ctx, 0); !errors.Is(err, ErrProgramSession) {
		t.Fatal(err)
	}
	if err := (ProgramKeyboard{}).Report(ctx, make([]byte, 8)); !errors.Is(err, ErrProgramSession) {
		t.Fatal(err)
	}
	if f.cpu.calls != calls {
		t.Fatal("unowned input touched hardware")
	}
	play, err := c.Launch(ctx, "native-0")
	if err != nil {
		t.Fatal(err)
	}
	calls = f.cpu.calls
	if _, err := b.BindKeyboard(ctx, play.CorePackage.Generation+1); !errors.Is(err, ErrProgramSession) {
		t.Fatal(err)
	}
	if f.cpu.calls != calls {
		t.Fatal("wrong generation touched hardware")
	}
	f.cpu.failKeyboardAt = f.cpu.keyboardWrites + 1
	if k, err := b.BindKeyboard(ctx, play.CorePackage.Generation); err == nil || k.state != nil || f.s.Status().State != ProgramRebootRequired {
		t.Fatal("failed neutralization granted input", err)
	}
}
func TestProgramKeyboardRolloverKeepsPressedKeys(t *testing.T) {
	f, _, _, _, k := keyboardFixture(t)
	ctx := context.Background()
	for _, r := range [][]byte{make([]byte, 8), {0, 0, 4, 0, 0, 0, 0, 0}} {
		if err := k.Report(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	writes := f.cpu.keyboardWrites
	if err := k.Report(ctx, []byte{0, 0, 1, 1, 1, 1, 1, 1}); !errors.Is(err, usb.ErrKeyboardRollover) {
		t.Fatal(err)
	}
	if f.cpu.keyboardRows[0] != 1<<4 || f.cpu.keyboardWrites != writes {
		t.Fatal("rollover released key")
	}
	if err := k.Report(ctx, make([]byte, 8)); err != nil {
		t.Fatal(err)
	}
	if f.cpu.keyboardRows != ([9]uint16{}) {
		t.Fatal("post-rollover release lost")
	}
}

type programKeyboardReports struct {
	reports [][]byte
	err     error
	polls   int
}

func (s *programKeyboardReports) PollKeyboard() ([]byte, error) {
	s.polls++
	if len(s.reports) == 0 {
		return nil, s.err
	}
	r := s.reports[0]
	s.reports = s.reports[1:]
	return r, nil
}
func TestProgramKeyboardPollsRealReportContract(t *testing.T) {
	f, _, c, play, k := keyboardFixture(t)
	ctx := context.Background()
	s := &programKeyboardReports{reports: [][]byte{make([]byte, 8), {0, 0, 4, 0, 0, 0, 0, 0}, {0, 0, 1, 1, 1, 1, 1, 1}}}
	if err := k.Poll(ctx, s); err != nil {
		t.Fatal(err)
	}
	if s.polls != 4 || f.cpu.keyboardRows[0] != 1<<4 {
		t.Fatal("missing snapshot synthesized release")
	}
	s.err = errors.New("USB transfer failed")
	writes := f.cpu.keyboardWrites
	if err := k.Poll(ctx, s); !errors.Is(err, s.err) {
		t.Fatal(err)
	}
	if f.cpu.keyboardWrites != writes || f.cpu.keyboardRows[0] != 1<<4 {
		t.Fatal("source error synthesized disconnect")
	}
	s.err = nil
	for range 10 {
		s.reports = append(s.reports, []byte{0, 0, 4, 0, 0, 0, 0, 0})
	}
	polls := s.polls
	if err := k.Poll(ctx, s); err != nil {
		t.Fatal(err)
	}
	if s.polls-polls != 8 || len(s.reports) != 2 {
		t.Fatal("unbounded USB drain")
	}
	if _, err := c.StopExpectedStamped(ctx, play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	polls = s.polls
	if err := k.Poll(ctx, s); !errors.Is(err, ErrProgramSession) {
		t.Fatal(err)
	}
	if s.polls != polls {
		t.Fatal("returned handle consumed USB reports")
	}
}
func TestProgramKeyboardPollStopsAtNativeFailure(t *testing.T) {
	f, _, _, _, k := keyboardFixture(t)
	f.cpu.failKeyboardAt = f.cpu.keyboardWrites + 2
	s := &programKeyboardReports{reports: [][]byte{make([]byte, 8), {0, 0, 4, 0, 0, 0, 0, 0}, make([]byte, 8)}}
	if err := k.Poll(context.Background(), s); err == nil {
		t.Fatal("native write failure hidden")
	}
	if s.polls != 2 || len(s.reports) != 1 || f.s.Status().State != ProgramRebootRequired {
		t.Fatal("poll continued after failure")
	}
}
