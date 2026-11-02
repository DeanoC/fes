// SPDX-License-Identifier: GPL-3.0-or-later
package admissioncheck

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
	"github.com/DeanoC/fes/platform/embedded/tamago/runtimeadmission"
)

type LinkedCase struct {
	Name             string `json:"name"`
	Path             string `json:"path"`
	PackageID        string `json:"package_id"`
	ProgrammedSHA256 string `json:"programmed_sha256"`
	ProgrammedSize   int64  `json:"programmed_size"`
	Sources          []struct {
		ID   string `json:"id"`
		Role string `json:"role"`
		Path string `json:"path"`
	} `json:"sources"`
}
type LinkedResult struct {
	Name             string `json:"name"`
	PackageID        string `json:"package_id"`
	ReceiptSHA256    string `json:"receipt_sha256"`
	ProgrammedSHA256 string `json:"programmed_sha256"`
	ProgrammedSize   int64  `json:"programmed_size"`
}

// RunLinked uses real synthetic Mistral vectors and mutates/restores only the
// runner-owned source files. The golden output hash comes from their oracle.
func RunLinked(policy runtimeadmission.Policy, cases []LinkedCase, read func(string) ([]byte, error), write func(string, []byte) error, report func(LinkedResult)) error {
	gate, err := runtimeadmission.NewModel(policy)
	if err != nil {
		return err
	}
	if len(cases) == 0 || len(cases) > 16 {
		return fmt.Errorf("invalid linked case count")
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
	for _, test := range cases {
		if err = linkedCase(gate, c, &now, test, read, write, report); err != nil {
			return fmt.Errorf("%s: %w", test.Name, err)
		}
	}
	return nil
}
func linkedCase(gate *runtimeadmission.Gate, c *fabricmodel.Controller, now *time.Duration, test LinkedCase, read func(string) ([]byte, error), write func(string, []byte) error, report func(LinkedResult)) (err error) {
	ctx := context.Background()
	body, err := read(test.Path)
	if err != nil {
		return err
	}
	archive, err := corepackage.RetainArchive(ctx, int64(len(body)), bytes.NewReader(body))
	if err != nil {
		return err
	}
	if archive.Inspection().PackageID != test.PackageID {
		return fmt.Errorf("package identity mismatch")
	}
	compatibility, err := runtimeadmission.NewDE10Compatibility()
	if err != nil {
		return err
	}
	if err = compatibility.CheckArchive(archive, test.PackageID); err != nil {
		return fmt.Errorf("native linked package compatibility: %w", err)
	}
	sources := []corepackage.ROMSource{}
	for _, source := range test.Sources {
		data, err := read(source.Path)
		if err != nil {
			return err
		}
		sources = append(sources, corepackage.ROMSource{ID: source.ID, Role: source.Role, Data: data})
	}
	linked, err := archive.LinkROMs(ctx, sources)
	if err != nil {
		return err
	}
	receiptID := linked.ReceiptSHA256()
	r := linked.Receipt()
	encoded, _ := json.Marshal(r)
	original := bytes.Clone(sources[0].Data)
	sources[0].Data[0] ^= 1
	// Restore even when a later assertion fails. A native error will be captured
	// by the runner, which also requires the complete disk hash to be unchanged.
	defer func() {
		if restore := write(test.Sources[0].Path, original); restore != nil {
			err = restore
		}
	}()
	if err = write(test.Sources[0].Path, sources[0].Data); err != nil {
		return err
	}
	changed, err := read(test.Sources[0].Path)
	if err != nil || !bytes.Equal(changed, sources[0].Data) {
		return fmt.Errorf("source mutation did not reach storage: %v", err)
	}
	alternate, err := archive.LinkROMs(ctx, sources)
	if err != nil {
		return err
	}
	if alternate.ReceiptSHA256() == receiptID {
		return fmt.Errorf("changed source reused receipt")
	}
	// Neither a returned receipt nor read buffers may mutate the opaque object.
	if r.ROMLink != nil {
		r.ROMLink.SourceSHA256 = "changed"
	}
	if r.ROMLinks != nil {
		r.ROMLinks.Sources[0].ID = "changed"
	}
	current, _ := json.Marshal(linked.Receipt())
	if !bytes.Equal(current, encoded) || linked.ReceiptSHA256() != receiptID {
		return fmt.Errorf("receipt escaped ownership")
	}
	retained, err := linked.Source(test.Sources[0].ID, test.Sources[0].Role)
	if err != nil {
		return err
	}
	retainedData, err := io.ReadAll(retained)
	if err != nil || !bytes.Equal(retainedData, original) {
		return fmt.Errorf("retained source changed")
	}
	output, err := io.ReadAll(linked.Programmed())
	if err != nil {
		return err
	}
	sum := sha256.Sum256(output)
	if int64(len(output)) != test.ProgrammedSize || hex.EncodeToString(sum[:]) != test.ProgrammedSHA256 {
		return fmt.Errorf("linked bytes differ from Mistral oracle")
	}
	output[0] ^= 1
	body[0] ^= 1
	// All denied activations must leave the previous owner and frame intact.
	before := c.Status()
	frame, err := c.BeginFrame()
	if err != nil {
		return err
	}
	for _, attempt := range []struct {
		linked            *corepackage.LinkedArchive
		id, receipt, code string
	}{
		{alternate, test.PackageID, receiptID, "receipt_mismatch"},
		{linked, test.PackageID, alternate.ReceiptSHA256(), "receipt_mismatch"},
		{linked, string(bytes.Repeat([]byte("0"), 64)), receiptID, "package_mismatch"},
		{nil, test.PackageID, receiptID, "receipt_mismatch"},
		{&corepackage.LinkedArchive{}, test.PackageID, receiptID, "receipt_mismatch"},
	} {
		code := runtimeadmission.ErrorCode(gate.LoadLinkedModel(c, attempt.linked, attempt.id, attempt.receipt, *now, *now+time.Second))
		if code != attempt.code || c.Status() != before {
			return fmt.Errorf("linked rejection changed owner: %s", code)
		}
		if cmd, e := c.Next(*now); e != nil || cmd != nil {
			return fmt.Errorf("linked rejection issued command")
		}
	}
	if code := runtimeadmission.ErrorCode(gate.LoadModel(c, archive, test.PackageID, *now, *now+time.Second)); code != "unsupported_abi" || c.Status() != before {
		return fmt.Errorf("raw ROM package bypassed receipt")
	}
	if _, e := archive.LinkROMs(ctx, nil); e == nil || c.Status() != before {
		return fmt.Errorf("missing sources accepted")
	}
	wrong := append([]corepackage.ROMSource(nil), sources...)
	wrong[0].Role = "wrong"
	if _, e := archive.LinkROMs(ctx, wrong); e == nil || c.Status() != before {
		return fmt.Errorf("wrong source role accepted")
	}
	if err = gate.LoadLinkedModel(c, linked, test.PackageID, receiptID, *now, *now+time.Second); err != nil {
		return err
	}
	if before.State == fabricmodel.Running {
		cmd, e := c.Next(*now)
		if e != nil || cmd == nil || cmd.Operation != fabricmodel.Save {
			return fmt.Errorf("linked replacement did not save")
		}
		if err = c.Complete(cmd.Token, fabricmodel.CompleteModel(*cmd), *now); err != nil {
			return err
		}
	}
	if cmd, e := c.Next(*now); e != nil || cmd != nil {
		return fmt.Errorf("linked activation bypassed frame")
	}
	if err = c.FinishFrame(frame, fabricmodel.Completion{Quiescent: true}); err != nil {
		return err
	}
	if err = drive(c, now, false); err != nil {
		return err
	}
	binding := c.Status().Package
	d := archive.Inspection().Descriptor
	want := fabricmodel.PackageIdentity{PackageID: test.PackageID, CoreID: d.Core.ID, ABI: d.ABI.ID,
		Major: uint16(d.ABI.Major), Minor: uint16(d.ABI.Minor), BuildID: d.Build.ID,
		Platform: d.Target.Platform, Device: d.Target.Device, ProgrammingProfile: d.Target.ProgrammingProfile,
		ActivationReceiptSHA256: receiptID, ProgrammedSHA256: test.ProgrammedSHA256}
	if c.Status().State != fabricmodel.Running || binding != want {
		return fmt.Errorf("linked identity not published after release")
	}
	if c.CheckInput(before.CoreGeneration) == nil {
		return fmt.Errorf("stale input authority survived linked replacement")
	}
	report(LinkedResult{test.Name, test.PackageID, receiptID, test.ProgrammedSHA256, test.ProgrammedSize})
	// A receipt mismatch in the simulated live identity must never release this
	// package. The existing controller must perform bounded idle recovery.
	if err = gate.LoadLinkedModel(c, linked, test.PackageID, receiptID, *now, *now+time.Second); err != nil {
		return err
	}
	corrupted := false
	for n := 0; n < 20; n++ {
		*now += time.Millisecond
		cmd, e := c.Next(*now)
		if e != nil {
			return e
		}
		if cmd == nil {
			break
		}
		completion := fabricmodel.CompleteModel(*cmd)
		if !corrupted && cmd.Operation == fabricmodel.Identify {
			completion.Identity.Package.ActivationReceiptSHA256 = "wrong receipt"
			if c.Complete(cmd.Token, completion, *now) == nil {
				return fmt.Errorf("live receipt mismatch accepted")
			}
			corrupted = true
			continue
		}
		if err = c.Complete(cmd.Token, completion, *now); err != nil {
			return err
		}
	}
	if !corrupted || c.Status().State != fabricmodel.Idle || c.Status().Package != (fabricmodel.PackageIdentity{}) {
		return fmt.Errorf("live receipt mismatch did not recover")
	}
	return nil
}
