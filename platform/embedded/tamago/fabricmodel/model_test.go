package fabricmodel

import (
	"testing"
	"time"
)

func TestLifecycleInvariants(t *testing.T) {
	if n := RunChecks(func(name string, err error) {
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}); n != 0 {
		t.Fatalf("%d invariant checks failed", n)
	}
}

func TestUnknownProgramRetainsIssuedBuffer(t *testing.T) {
	c, now, err := booted()
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Load(sampleImage("model.buffer", false), *now, *now+time.Second); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 8; n++ {
		cmd, err := c.Next(*now)
		if err != nil || cmd == nil {
			t.Fatalf("command: %v %v", cmd, err)
		}
		if cmd.Operation == Program {
			if err = c.Complete(cmd.Token, Completion{Uncertain: true}, *now); err == nil {
				t.Fatal("unknown program succeeded")
			}
			if c.issued == nil || len(c.issued.Bytes) == 0 || &c.issued.Bytes[0] != &cmd.Bytes[0] {
				t.Fatal("uncertain backend buffer is no longer retained")
			}
			if c.Status().State != RebootRequired {
				t.Fatal(c.Status())
			}
			return
		}
		if err = c.Complete(cmd.Token, CompleteModel(*cmd), *now); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("program command missing")
}
