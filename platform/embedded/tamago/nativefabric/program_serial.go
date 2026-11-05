// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

// ReadProgramCatalog binds friendly UI names to the already selected program
// list. Names and indexes never supply package paths or hardware permissions.
// Read this once, before giving ProgramService ownership of SD transitions.
func ReadProgramCatalog(ctx context.Context, files fs.FS, path string, choices []packagestore.Request) ([]ProgramEntry, error) {
	if ctx == nil || files == nil || !fs.ValidPath(path) || len(choices) == 0 || len(choices) > 32 {
		return nil, errors.New("native program UI: bounded catalog and choices required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := files.Open(path)
	if err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(f, 8193))
	closeErr := f.Close()
	if err := errors.Join(readErr, closeErr, ctx.Err()); err != nil {
		return nil, err
	}
	var catalog struct {
		Format  int    `json:"format"`
		Scope   string `json:"scope"`
		Entries []struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			Program int    `json:"program"`
		} `json:"entries"`
	}
	if err := decodeCompleteRecord(raw, 8192, &catalog); err != nil {
		return nil, err
	}
	if catalog.Format != 1 || catalog.Scope != "nano-tenfoot-catalog-v1" || len(catalog.Entries) == 0 || len(catalog.Entries) > 32 {
		return nil, errors.New("native program UI: unsupported catalog")
	}
	var entries []ProgramEntry
	for _, e := range catalog.Entries {
		if e.Program < 0 || e.Program >= len(choices) {
			return nil, errors.New("native program UI: selected program index out of range")
		}
		entries = append(entries, ProgramEntry{ID: e.ID, Title: e.Title, Request: choices[e.Program]})
	}
	return entries, nil
}

// ProgramSerialPlatform is an explicit state-only serial platform for running
// the full App on the native CPU. Its output is not an HDMI/display grant.
// Read must be nonblocking; Emit must honor ctx and return its write failure.
// Keep the service rooted after exit, including uncertain return failures.
type ProgramSerialPlatform struct {
	service         *ProgramService
	read            func() (byte, bool)
	emit            func(context.Context, []byte) error
	decoder         input.Decoder
	last            time.Time
	previous        []byte
	quit            bool
	stopQueued      bool
	fetchGeneration uint64
}

func NewProgramSerialPlatform(service *ProgramService, read func() (byte, bool), emit func(context.Context, []byte) error) (*ProgramSerialPlatform, error) {
	if service == nil || service.state == nil || read == nil || emit == nil {
		return nil, errors.New("native serial UI: owned service and explicit serial hooks required")
	}
	return &ProgramSerialPlatform{service: service, read: read, emit: emit}, nil
}
func (p *ProgramSerialPlatform) Next(ctx context.Context) (tenfoot.PlatformInput, error) {
	if ctx == nil {
		return tenfoot.PlatformInput{}, errors.New("native serial UI: context required")
	}
	if err := ctx.Err(); err != nil {
		return tenfoot.PlatformInput{}, err
	}
	delay := 50*time.Millisecond - time.Since(p.last)
	if delay > 0 {
		select {
		case <-ctx.Done():
			return tenfoot.PlatformInput{}, ctx.Err()
		case <-time.After(delay):
		}
	}
	now := time.Now()
	p.last = now
	frame := tenfoot.PlatformInput{At: now}
	if p.quit {
		switch p.service.Status().State {
		case ProgramReady, ProgramParked, ProgramRebootRequired:
			frame.Commands = []tenfoot.Command{tenfoot.CmdQuit}
		case ProgramRunning:
			if !p.stopQueued {
				frame.Commands = []tenfoot.Command{tenfoot.CmdStop}
				p.stopQueued = true
			}
		}
		return frame, nil
	}
	event := func(e input.Event) {
		command := tenfoot.CommandFromKey(e.Key)
		if command == tenfoot.CmdQuit {
			p.quit = true
			frame.Commands = nil
			switch p.service.Status().State {
			case ProgramRunning:
				frame.Commands = []tenfoot.Command{tenfoot.CmdStop}
				p.stopQueued = true
			case ProgramReady, ProgramParked, ProgramRebootRequired:
				frame.Commands = []tenfoot.Command{tenfoot.CmdQuit}
			}
			return
		}
		if command != tenfoot.CmdNone {
			frame.Commands = append(frame.Commands, command)
		}
		frame.Text += e.Text
	}
	if e, ok := p.decoder.Flush(now); ok {
		event(e)
	}
	for n := 0; n < 64 && !p.quit; n++ {
		value, ready := p.read()
		if !ready {
			break
		}
		if e, ok := p.decoder.Feed(value, now); ok {
			event(e)
		}
	}
	return frame, nil
}
func (p *ProgramSerialPlatform) Present(ctx context.Context, snapshot tenfoot.Snapshot) error {
	if ctx == nil {
		return errors.New("native serial UI: context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if observation, ok := p.service.FetchObservation(); ok && observation.Generation != p.fetchGeneration {
		data, err := json.Marshal(observation)
		if err != nil {
			return err
		}
		if err := p.emit(ctx, append(append([]byte("FES_NATIVE_CORE_FETCH "), data...), '\n')); err != nil {
			return err
		}
		p.fetchGeneration = observation.Generation
	}
	titles := make([]string, len(snapshot.Games))
	for i, e := range snapshot.Games {
		titles[i] = e.Title
	}
	raw, err := json.Marshal(struct {
		Scope       string               `json:"scope"`
		Display     string               `json:"display"`
		Titles      []string             `json:"titles"`
		Focus       int                  `json:"focus"`
		Loading     bool                 `json:"loading"`
		LoadError   string               `json:"load_error"`
		Session     string               `json:"session"`
		Launch      string               `json:"launch"`
		LaunchError string               `json:"launch_error"`
		RetryStop   bool                 `json:"retry_stop"`
		Native      ProgramServiceStatus `json:"native"`
	}{"nano-tenfoot-serial-v1", "serial-state-only", titles, snapshot.Grid.Focus, snapshot.Loading, snapshot.LoadErr, snapshot.Session.State, snapshot.Launch.Phase, snapshot.Launch.ErrorMessage, snapshot.Session.RetryStop, p.service.Status()})
	if err != nil {
		return err
	}
	if bytes.Equal(raw, p.previous) {
		return nil
	}
	if err := p.emit(ctx, append(append([]byte("FES_NATIVE_TENFOOT_STATE "), raw...), '\n')); err != nil {
		return fmt.Errorf("native serial UI: state output: %w", err)
	}
	p.previous = raw
	return nil
}

// ProgramPreferences stays in RAM. It cannot write a FAT volume after a launch
// has contained SD, and it makes no persistence promise across reboots.
type ProgramPreferences struct {
	mu   sync.Mutex
	data []byte
}

func (p *ProgramPreferences) ReadPreferences() ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.data == nil {
		return []byte(`{"home":"library","attract_enabled":false}`), nil
	}
	return bytes.Clone(p.data), nil
}
func (p *ProgramPreferences) WritePreferences(data []byte) error {
	if len(data) > 65536 || !json.Valid(data) {
		return errors.New("native program UI: invalid bounded preferences")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.data = bytes.Clone(data)
	return nil
}
