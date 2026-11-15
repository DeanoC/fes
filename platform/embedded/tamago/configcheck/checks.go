// SPDX-License-Identifier: GPL-3.0-or-later

// Package configcheck exercises a hypothetical contained firmware endpoint.
// It binds wire-transfer observations to fabricmodel, never to physical SMC.
package configcheck

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdm"
)

// transfer supplies software responses only. The addresses are identities in
// this model; they are never dereferenced or passed to firmware.
func transfer(data []byte, corrupt bool, now *time.Duration) ([32]byte, error, *sdm.ConfigModel) {
	m, err := sdm.NewConfigModel(sdm.HeldBridgeModel, data, 0x96000000, 32, *now, *now+time.Second)
	if err != nil {
		return [32]byte{}, err, m
	}
	var pending []uint64
	var received []byte
	busyPoll, busyDone := true, true
	for range 256 {
		*now += time.Millisecond
		command, err := m.Next(*now)
		if err != nil {
			return [32]byte{}, err, m
		}
		if command == nil {
			if !m.Status().Done || len(pending) != 0 || !bytes.Equal(received, data) {
				return [32]byte{}, errors.New("model transfer did not complete exact bytes"), m
			}
			return sha256.Sum256(received), nil, m
		}
		if repeated, err := m.Next(*now); err != nil || repeated != nil {
			return [32]byte{}, errors.New("model replayed an outstanding firmware command"), m
		}
		var reply [4]uint64
		switch command.Operation {
		case sdm.StartConfig:
			if command.Registers != ([7]uint64{0xc2000001}) {
				return [32]byte{}, errors.New("wrong full-configuration START"), m
			}
		case sdm.WriteConfig:
			want := [7]uint64{0x42000002, 0x96000000 + uint64(len(received)), uint64(len(command.Bytes))}
			if command.Registers != want || len(pending) >= 4 {
				return [32]byte{}, errors.New("wrong or overlapping model write"), m
			}
			received = append(received, command.Bytes...)
			pending = append(pending, command.Registers[1]-0x80000000)
			if len(pending) == 4 {
				reply[0] = 1
			} // Accepted BUSY, not a retry.
		case sdm.PollWrites:
			if command.Registers != ([7]uint64{0xc2000003}) {
				return [32]byte{}, errors.New("wrong completion poll"), m
			}
			if busyPoll {
				reply[0] = 1
				busyPoll = false
			} else {
				n := min(3, len(pending))
				copy(reply[1:], pending[:n])
				pending = pending[n:]
				if corrupt {
					reply[1] = 0x97000000
				}
			}
		case sdm.PollConfigured:
			if command.Registers != ([7]uint64{0xc2000004}) || len(pending) != 0 {
				return [32]byte{}, errors.New("configuration polled before writes drained"), m
			}
			if busyDone {
				reply[0] = 1
				busyDone = false
			}
		default:
			return [32]byte{}, errors.New("unexpected model firmware operation"), m
		}
		if err = m.Complete(command.Token, sdm.ConfigReply{Registers: reply}, *now); err != nil {
			if !m.Status().Failed || m.Status().RetainedBytes < len(data) || m.Status().Done {
				return [32]byte{}, errors.New("failed transfer lost ownership"), m
			}
			return [32]byte{}, err, m
		}
	}
	return [32]byte{}, errors.New("model transfer exceeded step budget"), m
}

type endpoint struct {
	disabled, contained    bool
	reads                  int
	displayGeneration      uint64
	observed               fabricmodel.Identity
	programs, gameReleases int
	fault                  string
	heldConfig             *sdm.ConfigModel
}

func (e *endpoint) complete(c *fabricmodel.Controller, command *fabricmodel.Command, now *time.Duration) fabricmodel.Completion {
	switch command.Operation {
	case fabricmodel.Save:
		return fabricmodel.Completion{}
	case fabricmodel.Drain:
		e.disabled = true
		e.reads = 2
		// Neither a successful CPU frame nor a disabled plane proves that
		// the already accepted reads drained. The controller must wait.
		for e.reads > 0 {
			if next, err := c.Next(*now); err != nil || next != nil {
				return fabricmodel.Completion{Err: errors.New("lifecycle advanced while reads remained"), Uncertain: true}
			}
			e.reads--
		}
		generation := e.displayGeneration
		if command.DisplayGeneration != generation {
			return fabricmodel.Completion{Err: errors.New("disable targeted the wrong reader"), Uncertain: true}
		}
		if e.fault == "stale-drain" {
			generation++
		}
		return fabricmodel.Completion{Quiescent: true, DisplayGeneration: generation}
	case fabricmodel.Contain:
		if e.reads != 0 || (!e.disabled && e.displayGeneration != 0) {
			return fabricmodel.Completion{Err: errors.New("containment before display drain"), Uncertain: true}
		}
		e.contained = true
		e.observed = fabricmodel.Identity{}
		return fabricmodel.Completion{Quiescent: true}
	case fabricmodel.Program:
		if !e.contained || e.reads != 0 {
			return fabricmodel.Completion{Err: errors.New("program before containment"), Uncertain: true}
		}
		e.programs++
		digest, err, model := transfer(command.Bytes, e.fault == "completion", now)
		e.heldConfig = model
		return fabricmodel.Completion{Err: err, Uncertain: err != nil, ProgrammedDigest: digest}
	case fabricmodel.Identify:
		if !e.contained {
			return fabricmodel.Completion{Err: errors.New("identity after bridge release"), Uncertain: true}
		}
		e.observed = command.Identity // Explicitly software-generated observation.
		if e.fault == "identity" && command.ImageID == "model.game" {
			e.observed.Build++
		}
		return fabricmodel.Completion{Identity: e.observed}
	case fabricmodel.Release:
		if !e.contained || e.observed != command.Identity || e.reads != 0 {
			return fabricmodel.Completion{Err: errors.New("release without verified identity and drain"), Uncertain: true}
		}
		e.contained = false
		e.disabled = false
		if command.ImageID == "model.game" {
			e.gameReleases++
		}
		return fabricmodel.Completion{}
	}
	return fabricmodel.Completion{Err: errors.New("unknown lifecycle operation"), Uncertain: true}
}

func lifecycle(fault string) error {
	image := func(id string, idle bool) *fabricmodel.Image {
		data := bytes.Repeat([]byte(id+" payload;"), 24)
		i, err := fabricmodel.Prepare(id, data, sha256.Sum256(data), fabricmodel.Identity{ABI: 1, Core: 1, Build: uint64(len(id))}, idle)
		if err != nil {
			panic(err)
		}
		return i
	}
	c, err := fabricmodel.New(image("model.idle", true), time.Second)
	if err != nil {
		return err
	}
	now := time.Duration(0)
	e := endpoint{}
	drive := func() error {
		for range 64 {
			now += time.Millisecond
			command, err := c.Next(now)
			if err != nil {
				return err
			}
			if command == nil {
				return nil
			}
			result := e.complete(c, command, &now)
			err = c.Complete(command.Token, result, now)
			if err != nil && c.Status().State != fabricmodel.Changing {
				return err
			}
			if command.Operation == fabricmodel.Release && err == nil {
				e.displayGeneration = c.Status().DisplayGeneration
			}
		}
		return errors.New("lifecycle exceeded step budget")
	}
	if err = c.Boot(now, time.Second); err != nil {
		return err
	}
	if err = drive(); err != nil {
		return err
	}
	e.fault = fault
	before := e.programs
	if err = c.Load(image("model.game", false), now, now+time.Second); err != nil {
		return err
	}
	err = drive()
	switch fault {
	case "stale-drain", "completion":
		if err == nil || c.Status().State != fabricmodel.RebootRequired || !e.contained && fault == "completion" || e.gameReleases != 0 {
			return fmt.Errorf("unsafe failed transition: %+v %v", c.Status(), err)
		}
		if fault == "completion" && (e.heldConfig == nil || !e.heldConfig.Status().Failed || e.heldConfig.Status().Outstanding == 0) {
			return errors.New("uncertain firmware buffers lost their owner")
		}
		if fault == "stale-drain" && e.programs != before {
			return errors.New("stale drain programmed an image")
		}
		if next, _ := c.Next(now); next != nil {
			return errors.New("failed transition implicitly retried")
		}
		return nil
	case "identity":
		if err != nil || c.Status().State != fabricmodel.Idle || e.gameReleases != 0 || e.programs != before+2 {
			return errors.New("identity failure released game or lost bounded idle recovery")
		}
		return nil
	}
	if err != nil || c.Status().State != fabricmodel.Running || e.gameReleases != 1 {
		return fmt.Errorf("game failed to start: %v", err)
	}
	if err = c.Stop(now, now+time.Second); err != nil {
		return err
	}
	if err = drive(); err != nil {
		return err
	}
	if c.Status().State != fabricmodel.Idle || e.gameReleases != 1 || e.programs != before+2 {
		return errors.New("launch/return did not preserve lifecycle ordering")
	}
	return nil
}

// Run exercises the same checks on the host and native QEMU. No board API is
// imported, and no SMC, register access or physical completion is generated.
func Run(report func(string, error)) int {
	cases := []struct {
		name string
		run  func() error
	}{
		{"reference-firmware-blocked", func() error {
			m, err := sdm.NewConfigModel(sdm.ReferenceTFAV212, []byte("payload"), 0x96000000, 32, 0, time.Second)
			if m != nil || !errors.Is(err, sdm.ErrUnsafeFirmware) {
				return errors.New("reference firmware admitted")
			}
			return nil
		}},
		{"accepted-busy-exact-bytes", func() error {
			data := bytes.Repeat([]byte("model payload"), 30)
			now := time.Duration(0)
			digest, err, _ := transfer(data, false, &now)
			if err != nil {
				return err
			}
			if digest != sha256.Sum256(data) {
				return errors.New("transferred bytes mismatch")
			}
			return nil
		}},
		{"contained-launch-return", func() error { return lifecycle("") }},
		{"stale-drain-blocks-program", func() error { return lifecycle("stale-drain") }},
		{"unknown-completion-retains-owner", func() error { return lifecycle("completion") }},
		{"identity-before-release", func() error { return lifecycle("identity") }},
	}
	failures := 0
	for _, c := range cases {
		err := c.run()
		if err != nil {
			failures++
		}
		if report != nil {
			report(c.name, err)
		}
	}
	return failures
}
