package linuxinput

import (
	"os"
	"testing"
	"time"
)

func TestReaderPipeJS(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	r := NewReader()
	defer r.Close()
	if err := r.Add(pr, KindJoystick, "pipe.js", "selftest"); err != nil {
		t.Fatal(err)
	}
	if _, err := pw.Write(EncodeJS(32767, JSEventAxis, 0)); err != nil {
		t.Fatal(err)
	}
	got := waitPoll(t, r, 2*time.Second)
	_ = pw.Close()
	if len(got) != 1 || got[0].Action != ActionRight || !got[0].Active {
		t.Fatalf("got %+v", got)
	}
	if got[0].Source != "selftest" {
		t.Fatalf("source %q", got[0].Source)
	}
	info := r.Info()
	if len(info) != 1 || info[0].Kind != KindJoystick {
		t.Fatalf("info %+v", info)
	}
}

func TestReaderPipeEvdevQuit(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	r := NewReader()
	defer r.Close()
	if err := r.Add(pr, KindEvdev, "event0", "kbd"); err != nil {
		t.Fatal(err)
	}
	if _, err := pw.Write(EncodeEvdev(evKey, keyEsc, 1)); err != nil {
		t.Fatal(err)
	}
	got := waitPoll(t, r, 2*time.Second)
	_ = pw.Close()
	if len(got) != 1 || got[0].Action != ActionQuit || !got[0].Active {
		t.Fatalf("got %+v", got)
	}
}

func TestReaderCloseUnblocks(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pw.Close()
	r := NewReader()
	if err := r.Add(pr, KindJoystick, "pipe.js", "hold"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- r.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close hung")
	}
}

func TestOpenPathsMissing(t *testing.T) {
	if _, err := OpenPaths([]string{"/no/such/input-node"}); err == nil {
		t.Fatal("expected error")
	}
}

func waitPoll(t *testing.T, r *Reader, d time.Duration) []Mapped {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if got := r.Poll(); len(got) > 0 {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timeout waiting for mapped event")
	return nil
}
