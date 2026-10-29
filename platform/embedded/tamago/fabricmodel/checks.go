// SPDX-License-Identifier: GPL-3.0-or-later
package fabricmodel

import (
	"crypto/sha256"
	"errors"
	"time"
)

func sampleImage(id string, idle bool) *Image {
	data := []byte("software model only: " + id)
	image, err := Prepare(id, data, sha256.Sum256(data), Identity{1, 1, uint64(len(data))}, idle)
	if err != nil {
		panic(err)
	}
	return image
}

func drive(c *Controller, now *time.Duration, edit func(*Command, *Completion)) ([]Operation, error) {
	var trace []Operation
	for n := 0; n < 32; n++ {
		cmd, err := c.Next(*now)
		if err != nil {
			return trace, err
		}
		if cmd == nil {
			return trace, nil
		}
		trace = append(trace, cmd.Operation)
		result := CompleteModel(*cmd)
		if edit != nil {
			edit(cmd, &result)
		}
		err = c.Complete(cmd.Token, result, *now)
		*now += time.Millisecond
		if err != nil {
			return trace, err
		}
	}
	return trace, errors.New("model exceeded bounded transition steps")
}

func booted() (*Controller, *time.Duration, error) {
	c, err := New(sampleImage("model.idle", true), 100*time.Millisecond)
	now := time.Duration(0)
	if err == nil {
		err = c.Boot(now, time.Second)
	}
	if err == nil {
		_, err = drive(c, &now, nil)
	}
	return c, &now, err
}
func running() (*Controller, *time.Duration, error) {
	c, now, err := booted()
	if err == nil {
		err = c.Load(sampleImage("model.core-a", false), *now, *now+time.Second)
	}
	if err == nil {
		_, err = drive(c, now, nil)
	}
	return c, now, err
}
func sameOperations(got []Operation, want ...Operation) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// RunChecks exercises the same controller on a host or native Go image. It
// returns failures and never emits a physical-device operation.
func RunChecks(report func(string, error)) int {
	cases := []struct {
		name string
		run  func() error
	}{
		{"boot-order", func() error {
			c, err := New(sampleImage("model.idle", true), time.Second)
			if err != nil {
				return err
			}
			now := time.Duration(0)
			if err = c.Boot(now, time.Second); err != nil {
				return err
			}
			ops, err := drive(c, &now, nil)
			if err != nil {
				return err
			}
			if !sameOperations(ops, Contain, Program, Identify, Release) || c.Status().State != Idle || c.Status().CoreGeneration != 0 || c.Status().DisplayGeneration == 0 {
				return errors.New("invalid idle boot order/authority")
			}
			return nil
		}},
		{"immutable-admission", func() error {
			c, now, err := booted()
			if err != nil {
				return err
			}
			frame, err := c.BeginFrame()
			if err != nil {
				return err
			}
			before := c.Status()
			data := []byte("retained model core")
			sum := sha256.Sum256(data)
			if _, err = Prepare("model.bad", data, [32]byte{}, Identity{1, 2, 3}, false); err == nil {
				return errors.New("bad digest admitted")
			}
			if c.Status() != before {
				return errors.New("failed admission changed owner")
			}
			im, err := Prepare("model.retained", data, sum, Identity{1, 2, 3}, false)
			if err != nil {
				return err
			}
			data[0] ^= 255
			if err = c.Load(im, *now, *now+time.Second); err != nil {
				return err
			}
			if cmd, err := c.Next(*now); err != nil || cmd != nil {
				return errors.New("program issued while renderer owns frame")
			}
			if err = c.FinishFrame(frame, Completion{Quiescent: true}); err != nil {
				return err
			}
			_, err = drive(c, now, func(cmd *Command, _ *Completion) {
				if cmd.Operation == Program && sha256.Sum256(cmd.Bytes) != sum {
					panic("retained image changed")
				}
			})
			return err
		}},
		{"replace-save-before-drain", func() error {
			c, now, err := running()
			if err != nil {
				return err
			}
			old := c.Status().CoreGeneration
			if err = c.Load(sampleImage("model.core-b", false), *now, *now+time.Second); err != nil {
				return err
			}
			ops, err := drive(c, now, nil)
			if err != nil {
				return err
			}
			if !sameOperations(ops, Save, Drain, Contain, Program, Identify, Release) || c.Status().CoreGeneration <= old {
				return errors.New("replacement skipped save/drain or reused generation")
			}
			if !errors.Is(c.CheckInput(old), ErrStale) {
				return errors.New("old input authority survived")
			}
			return nil
		}},
		{"no-command-replay", func() error {
			c, now, err := booted()
			if err != nil {
				return err
			}
			if err = c.Load(sampleImage("model.core-a", false), *now, *now+time.Second); err != nil {
				return err
			}
			cmd, err := c.Next(*now)
			if err != nil || cmd == nil {
				return errors.New("missing first command")
			}
			for i := 0; i < 8; i++ {
				if again, e := c.Next(*now); e != nil || again != nil {
					return errors.New("command replayed before completion")
				}
			}
			if err = c.Complete(cmd.Token, CompleteModel(*cmd), *now); err != nil {
				return err
			}
			if !errors.Is(c.Complete(cmd.Token, CompleteModel(*cmd), *now), ErrStale) {
				return errors.New("old completion accepted")
			}
			return nil
		}},
		{"identity-before-release", func() error {
			c, now, err := booted()
			if err != nil {
				return err
			}
			if err = c.Load(sampleImage("model.core-a", false), *now, *now+time.Second); err != nil {
				return err
			}
			ops, err := drive(c, now, func(cmd *Command, result *Completion) {
				if cmd.Operation == Identify {
					result.Identity.Build++
				}
			})
			if err == nil || !sameOperations(ops, Drain, Contain, Program, Identify) {
				return errors.New("mismatched core reached release")
			}
			ops, err = drive(c, now, nil)
			if err != nil {
				return err
			}
			if !sameOperations(ops, Contain, Program, Identify, Release) || c.Status().State != Idle || c.Status().LastError == "" {
				return errors.New("single recovery lost primary failure")
			}
			return nil
		}},
		{"uncertain-save-retains-owner", func() error {
			c, now, err := running()
			if err != nil {
				return err
			}
			old := c.Status()
			if err = c.Stop(*now, *now+time.Second); err != nil {
				return err
			}
			cmd, err := c.Next(*now)
			if err != nil || cmd == nil || cmd.Operation != Save {
				return errors.New("stop did not save")
			}
			if _, err = c.Next(*now + time.Second); !errors.Is(err, ErrDeadline) {
				return errors.New("save timeout accepted")
			}
			if c.Status().State != RebootRequired || c.Status().ImageID != old.ImageID || len(c.held) == 0 {
				return errors.New("uncertain save dropped owner")
			}
			if !errors.Is(c.Stop(*now, *now+time.Second), ErrReboot) || !errors.Is(c.RecoverIdle(*now, *now+time.Second), ErrReboot) {
				return errors.New("uncertain save replayed")
			}
			if !errors.Is(c.Complete(cmd.Token, CompleteModel(*cmd), *now+time.Second), ErrStale) {
				return errors.New("late save completion accepted")
			}
			return nil
		}},
		{"known-save-failure-retains-owner", func() error {
			c, now, err := running()
			if err != nil {
				return err
			}
			before := c.Status()
			if err = c.Load(sampleImage("model.core-b", false), *now, *now+time.Second); err != nil {
				return err
			}
			ops, err := drive(c, now, func(cmd *Command, result *Completion) {
				if cmd.Operation == Save {
					result.Err = errors.New("save failed")
				}
			})
			if err == nil || !sameOperations(ops, Save) || c.Status().State != Running || c.Status().ImageID != before.ImageID || c.Status().CoreGeneration != before.CoreGeneration || c.Status().InputEnabled {
				return errors.New("save failure retired or re-enabled owner")
			}
			if err = c.Stop(*now, *now+time.Second); err != nil {
				return err
			}
			_, err = drive(c, now, nil)
			return err
		}},
		{"drain-failure-no-program", func() error {
			c, now, err := booted()
			if err != nil {
				return err
			}
			if err = c.Load(sampleImage("model.core-a", false), *now, *now+time.Second); err != nil {
				return err
			}
			ops, err := drive(c, now, func(cmd *Command, result *Completion) {
				if cmd.Operation == Drain {
					result.Err = errors.New("drain ambiguous")
				}
			})
			if err == nil || !sameOperations(ops, Drain) || c.Status().State != RebootRequired {
				return errors.New("unsafe programming after failed drain")
			}
			if !errors.Is(c.RecoverIdle(*now, *now+time.Second), ErrReboot) {
				return errors.New("unknown drain retried")
			}
			return nil
		}},
		{"abandoned-frame-no-reuse", func() error {
			c, now, err := booted()
			if err != nil {
				return err
			}
			frame, err := c.BeginFrame()
			if err != nil {
				return err
			}
			if err = c.Load(sampleImage("model.core-a", false), *now, *now+time.Second); err != nil {
				return err
			}
			if _, err = c.Next(*now + time.Second); !errors.Is(err, ErrDeadline) {
				return errors.New("abandoned frame never expired")
			}
			if c.Status().State != RebootRequired || c.frame == nil {
				return errors.New("renderer ownership freed on timeout")
			}
			if !errors.Is(c.RecoverIdle(*now, *now+time.Second), ErrReboot) {
				return errors.New("renderer memory reused")
			}
			if !errors.Is(c.FinishFrame(frame, Completion{Quiescent: true}), ErrStale) {
				return errors.New("late frame completion accepted")
			}
			return nil
		}},
		{"frame-fault-retains-core", func() error {
			c, _, err := running()
			if err != nil {
				return err
			}
			before := c.Status()
			frame, err := c.BeginFrame()
			if err != nil {
				return err
			}
			if err = c.FinishFrame(frame, Completion{Err: errors.New("frame timeout"), Uncertain: true}); err == nil {
				return errors.New("unknown frame passed")
			}
			if c.Status().State != Running || c.Status().CoreGeneration != before.CoreGeneration || c.Status().DisplayGeneration != 0 {
				return errors.New("frame fault retired core")
			}
			if err = c.ShowDisplay(before.CoreGeneration); err == nil {
				return errors.New("unknown frame reused")
			}
			if err = c.FinishFrame(frame, Completion{Err: errors.New("frame failed, drain confirmed"), Quiescent: true}); err == nil {
				return errors.New("frame error missing")
			}
			if err = c.ShowDisplay(before.CoreGeneration); err != nil {
				return err
			}
			if c.Status().DisplayGeneration <= before.DisplayGeneration {
				return errors.New("display generation reused")
			}
			return nil
		}},
		{"one-recovery-then-explicit", func() error {
			c, now, err := booted()
			if err != nil {
				return err
			}
			if err = c.Load(sampleImage("model.core-a", false), *now, *now+time.Second); err != nil {
				return err
			}
			_, err = drive(c, now, func(cmd *Command, result *Completion) {
				if cmd.Operation == Program {
					result.Err = errors.New("program failed")
				}
			})
			if err == nil {
				return errors.New("program failure ignored")
			}
			_, err = drive(c, now, func(cmd *Command, result *Completion) {
				if cmd.Operation == Program {
					result.Err = errors.New("idle failed")
				}
			})
			if err == nil || c.Status().State != RebootRequired {
				return errors.New("unbounded implicit recovery")
			}
			if !errors.Is(c.Stop(*now, *now+time.Second), ErrReboot) {
				return errors.New("Stop retried failed idle")
			}
			if cmd, e := c.Next(*now); e != nil || cmd != nil {
				return errors.New("implicit recovery continued")
			}
			if err = c.RecoverIdle(*now, *now+time.Second); err != nil {
				return err
			}
			_, err = drive(c, now, nil)
			if err != nil {
				return err
			}
			if c.Status().State != Idle {
				return errors.New("explicit recovery failed")
			}
			return nil
		}},
	}
	failures := 0
	for _, test := range cases {
		err := test.run()
		if err != nil {
			failures++
		}
		if report != nil {
			report(test.name, err)
		}
	}
	return failures
}
