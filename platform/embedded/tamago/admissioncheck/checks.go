// SPDX-License-Identifier: GPL-3.0-or-later

// Package admissioncheck exercises the same checks on the host and native Go.
// All endpoint completions are explicitly software-model observations.
package admissioncheck

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
	"github.com/DeanoC/fes/platform/embedded/tamago/runtimeadmission"
)

type Case struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	PackageID   string `json:"package_id"`
	Expected    string `json:"expected"`
	RuntimeCode string `json:"runtime_code"`
}

type Result struct {
	Name           string `json:"name"`
	PackageID      string `json:"package_id"`
	Code           string `json:"code"`
	OwnerUnchanged bool   `json:"owner_unchanged"`
}

func drive(c *fabricmodel.Controller, now *time.Duration, corrupt bool) error {
	for n := 0; n < 20; n++ {
		*now += time.Millisecond
		cmd, err := c.Next(*now)
		if err != nil {
			return err
		}
		if cmd == nil {
			if c.Status().State == fabricmodel.Changing {
				return fmt.Errorf("model stalled")
			}
			return nil
		}
		result := fabricmodel.CompleteModel(*cmd)
		if corrupt && cmd.Operation == fabricmodel.Identify {
			result.Identity.Package.BuildID = "wrong full build identity"
			if c.Complete(cmd.Token, result, *now) == nil {
				return fmt.Errorf("wrong identity released")
			}
			corrupt = false
			continue
		}
		if err = c.Complete(cmd.Token, result, *now); err != nil {
			return err
		}
	}
	return fmt.Errorf("model did not settle")
}

// Run checks actual verified fixture bytes. It never uses a physical backend.
func Run(policy runtimeadmission.Policy, cases []Case, read func(string) ([]byte, error), report func(Result)) error {
	gate, err := runtimeadmission.NewModel(policy)
	if err != nil {
		return err
	}
	if len(cases) == 0 || len(cases) > 512 {
		return fmt.Errorf("invalid admission case count")
	}
	data := []byte("software idle")
	idle, err := fabricmodel.Prepare("model.idle", data, sha256.Sum256(data), fabricmodel.Identity{ABI: 1, Core: 1, Build: 1}, true)
	if err != nil {
		return err
	}
	c, err := fabricmodel.New(idle, time.Second)
	if err != nil {
		return err
	}
	now := time.Duration(0)
	if err = c.Boot(now, time.Second); err != nil {
		return err
	}
	if err = drive(c, &now, false); err != nil {
		return err
	}
	var first *corepackage.Archive
	var firstID string
	accepted, rejected := 0, 0
	for _, test := range cases {
		body, err := read(test.Path)
		if err != nil {
			return err
		}
		archive, err := corepackage.RetainArchive(context.Background(), int64(len(body)), bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("verified fixture %s: %w", test.Name, err)
		}
		if archive.Inspection().PackageID != test.PackageID {
			return fmt.Errorf("fixture identity mismatch")
		}
		// Keep an outstanding frame and capture the full current owner's state.
		frame, err := c.BeginFrame()
		if err != nil {
			return err
		}
		before := c.Status()
		err = gate.LoadModel(c, archive, test.PackageID, now, now+time.Second)
		code := runtimeadmission.ErrorCode(err)
		if code != test.Expected {
			return fmt.Errorf("%s: got %s expected %s (%v); runtime=%s", test.Name, code, test.Expected, err, test.RuntimeCode)
		}
		if err != nil {
			if c.Status() != before {
				return fmt.Errorf("rejection retired owner: %s", test.Name)
			}
			cmd, nextErr := c.Next(now)
			if cmd != nil || nextErr != nil {
				return fmt.Errorf("rejection issued command")
			}
			rejected++
		} else {
			// Saving the current core may precede frame drain; programming may not.
			if before.State == fabricmodel.Running {
				cmd, nextErr := c.Next(now)
				if nextErr != nil || cmd == nil || cmd.Operation != fabricmodel.Save {
					return fmt.Errorf("replacement did not save previous owner")
				}
				if err = c.Complete(cmd.Token, fabricmodel.CompleteModel(*cmd), now); err != nil {
					return err
				}
			}
			if cmd, nextErr := c.Next(now); cmd != nil || nextErr != nil {
				return fmt.Errorf("admission bypassed outstanding frame")
			}
			accepted++
		}
		if err = c.FinishFrame(frame, fabricmodel.Completion{Quiescent: true}); err != nil {
			return err
		}
		if code == "none" {
			if err = drive(c, &now, false); err != nil {
				return err
			}
			d := archive.Inspection().Descriptor
			want := fabricmodel.PackageIdentity{PackageID: test.PackageID, CoreID: d.Core.ID, ABI: d.ABI.ID, Major: uint16(d.ABI.Major), Minor: uint16(d.ABI.Minor), BuildID: d.Build.ID}
			if c.Status().Package != want || c.Status().State != fabricmodel.Running {
				return fmt.Errorf("model published wrong package identity")
			}
			if c.CheckInput(before.CoreGeneration) == nil {
				return fmt.Errorf("old input generation survived replacement")
			}
			if first == nil {
				first, firstID = archive, test.PackageID
			}
		}
		report(Result{test.Name, test.PackageID, code, code != "none"})
	}
	if first == nil || accepted == 0 || rejected == 0 {
		return fmt.Errorf("fixtures must exercise admission and rejection")
	}
	// Missing backend and missing exact-package decision must fail before mutation.
	missing := policy
	missing.Decisions = nil
	missingGate, err := runtimeadmission.NewModel(missing)
	if err != nil {
		return err
	}
	for _, test := range []struct {
		gate     *runtimeadmission.Gate
		id, code string
	}{
		{nil, firstID, "backend_unavailable"}, {&runtimeadmission.Gate{}, firstID, "backend_unavailable"},
		{missingGate, firstID, "backend_unavailable"}, {gate, string(bytes.Repeat([]byte("0"), 64)), "package_mismatch"},
	} {
		before := c.Status()
		frame, err := c.BeginFrame()
		if err != nil {
			return err
		}
		if code := runtimeadmission.ErrorCode(test.gate.LoadModel(c, first, test.id, now, now+time.Second)); code != test.code || c.Status() != before {
			return fmt.Errorf("default rejection changed owner: %s", code)
		}
		if err = c.FinishFrame(frame, fabricmodel.Completion{Quiescent: true}); err != nil {
			return err
		}
	}
	// A full identity mismatch must recover to idle without publishing the package.
	if err = gate.LoadModel(c, first, firstID, now, now+time.Second); err != nil {
		return err
	}
	if err = drive(c, &now, true); err != nil {
		return err
	}
	if c.Status().State != fabricmodel.Idle || c.Status().Package != (fabricmodel.PackageIdentity{}) || c.Status().LastError == "" {
		return fmt.Errorf("identity failure did not recover to idle")
	}
	return nil
}
