// SPDX-License-Identifier: GPL-3.0-or-later

package nativefabric

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
)

const (
	pickerWidth   = 48
	pickerHeight  = 16
	pickerID      = 0x43504931
	pickerStatus  = 0x7c
	pickerData    = 0x78
	pickerShowing = 1
	pickerPending = 2
	pickerFault   = 4
	pickerGuardID = 0x44324744
)

type c64PickerBatchPlatform interface {
	WithC64PickerBatch(uint64, func(computer.IO) error) error
}

type c64PickerFrame [pickerWidth * pickerHeight]byte

type programC64Picker struct {
	generation uint64
	visible    bool
	frame      c64PickerFrame
	report     func(ProgramServiceStatus, bool, computer.FetchActivity)
}

func c64PickerText(s tenfoot.TapePickerSnapshot) (frame c64PickerFrame) {
	for i := range frame {
		frame[i] = ' '
	}
	line := func(y int, value string, selected bool) {
		row := frame[y*pickerWidth : (y+1)*pickerWidth]
		attribute := byte(0)
		if selected {
			attribute = 0x80
			for i := range row {
				row[i] |= attribute
			}
		}
		i := 1
		for _, ch := range value {
			if i == len(row)-1 {
				break
			}
			if ch == '·' {
				ch = '-'
			} else if ch < ' ' || ch > '~' {
				ch = '?'
			}
			row[i] = byte(ch) | attribute
			i++
		}
	}
	line(1, s.Title, false)
	if s.Current != "" {
		line(2, "In drive: "+s.Current, false)
	}
	for i, row := range s.Rows[:min(10, len(s.Rows))] {
		line(i+4, row.Name, i == s.Index)
	}
	line(14, s.Status, false)
	line(15, "Up/Down select   Enter insert   Esc back", false)
	return frame
}

func (p *ProgramMenuPlatform) EnableC64Picker(report func(ProgramServiceStatus, bool, computer.FetchActivity)) {
	p.c64Picker = &programC64Picker{report: report}
}

func (p *ProgramMenuPlatform) presentC64Picker(ctx context.Context, snapshot tenfoot.TapePickerSnapshot) error {
	if p.c64Picker == nil {
		return nil
	}
	if input, ok := p.inner.(interface{ setC64PickerFocus(bool) }); ok {
		input.setC64PickerFocus(snapshot.Open)
	}
	service := p.service.state
	if !service.operation.TryLock() {
		return nil
	}
	defer service.operation.Unlock()
	state := p.service.Status()
	cache := p.c64Picker
	if state.State != ProgramRunning || state.Identity.ProgrammingProfile != c64Profile ||
		service.computer == nil || service.closed {
		cache.visible, cache.generation = false, 0
		return nil
	}
	frame := c64PickerText(snapshot)
	if cache.generation == state.Generation && cache.visible == snapshot.Open && (!snapshot.Open || cache.frame == frame) ||
		cache.generation != state.Generation && !snapshot.Open {
		return nil
	}
	wasVisible := cache.generation == state.Generation && cache.visible
	if err := service.computer.port.publishC64Picker(ctx, snapshot.Open, &frame); err != nil {
		return err
	}
	cache.frame, cache.generation, cache.visible = frame, state.Generation, snapshot.Open
	if wasVisible != cache.visible && cache.report != nil {
		activity, err := service.computer.Computer().Activity(ctx)
		if err != nil {
			return fmt.Errorf("native C64 picker: observe running fetches: %w", err)
		}
		cache.report(state, cache.visible, activity)
	}
	return nil
}

func (c *computerIO) publishC64Picker(ctx context.Context, show bool, frame *c64PickerFrame) error {
	cpu, ok := c.cpu.(c64PickerBatchPlatform)
	if !ok {
		return errors.New("native C64 picker: missing fixed video aperture")
	}
	bounded, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	var expected uint16
	if err := c.withDataBatch(func(io computer.IO) error {
		guardID, err := io.Read32(0x80)
		if err != nil || guardID != pickerGuardID {
			return errors.Join(err, errors.New("native C64 picker: scanout guard identity absent"))
		}
		tag, err := io.Read32(0x74)
		if err != nil || tag != pickerID {
			return errors.Join(err, errors.New("native C64 picker: FPGA overlay identity absent"))
		}
		initial, err := readC64PickerStatus(io)
		if err != nil || initial&pickerPending != 0 {
			return errors.Join(err, errors.New("native C64 picker: previous frame still pending"))
		}
		expected = uint16(initial>>16) + 1
		if !show {
			return io.Write32(pickerStatus, 3)
		}
		if err := io.Write32(pickerStatus, 1); err != nil {
			return err
		}
		for i := 0; i < len(frame); i += 4 {
			if err := bounded.Err(); err != nil {
				return err
			}
			if err := io.Write32(pickerData, binary.LittleEndian.Uint32(frame[i:i+4])); err != nil {
				return err
			}
		}
		return io.Write32(pickerStatus, 2)
	}, cpu.WithC64PickerBatch); err != nil {
		return err
	}
	for bounded.Err() == nil {
		var status uint32
		err := c.withDataBatch(func(io computer.IO) error {
			var readErr error
			status, readErr = readC64PickerStatus(io)
			return readErr
		}, cpu.WithC64PickerBatch)
		if err != nil {
			return err
		}
		if status&pickerPending == 0 {
			if uint16(status>>16) != expected || (status&pickerShowing != 0) != show {
				return errors.New("native C64 picker: frame acknowledgement differs")
			}
			return nil
		}
		time.Sleep(time.Millisecond)
	}
	return bounded.Err()
}

func readC64PickerStatus(io computer.IO) (uint32, error) {
	status, err := io.Read32(pickerStatus)
	if err != nil {
		return 0, err
	}
	guard, err := io.Read32(0x88)
	if err != nil || guard&7 != 0 || status&pickerFault != 0 {
		return 0, fmt.Errorf("native C64 picker: status=%08x guard=%08x: %v", status, guard, err)
	}
	return status, nil
}
