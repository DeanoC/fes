//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"runtime"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/serial"
)

// Keep uncertain plans, scopes and platform reservations rooted until reboot.
var programService *nativefabric.ProgramService

func servePrograms(loader *nativefabric.ProgramLoader, store *packagestore.Store, files fs.FS) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	choices, err := nativefabric.ReadProgramSelections(ctx, files, "FES/CORESEL.JSN")
	cancel()
	if err != nil {
		panic(err)
	}
	programService, err = nativefabric.NewProgramService(loader, store, de25nano.ReserveConfiguration,
		computer.Limits{Timeout: time.Second, MaxPolls: 64, Now: time.Now, Yield: runtime.Gosched})
	if err != nil {
		panic(err)
	}
	console, err := nativefabric.NewProgramConsole(programService, choices)
	if err != nil {
		panic(err)
	}
	writeReply := func(reply nativefabric.ProgramReply) {
		raw, err := json.Marshal(reply)
		if err != nil {
			panic(err)
		}
		fmt.Printf("FES_NATIVE_PROGRAM_REPLY %s\n", raw)
	}
	writeReply(console.Handle(context.Background(), []byte(`{"op":"status"}`)))
	line := make([]byte, 0, nativefabric.ProgramCommandMaxBytes)
	discard := false
	for {
		b, ready := serial.Read()
		if !ready {
			runtime.Gosched()
			time.Sleep(time.Millisecond)
			continue
		}
		if b != '\r' && b != '\n' {
			if len(line) == nativefabric.ProgramCommandMaxBytes {
				discard = true
			}
			if !discard {
				line = append(line, b)
			}
			continue
		}
		if len(line) == 0 && !discard {
			continue
		}
		if discard {
			line = line[:0] // Empty commands reject without I/O; never execute a truncated prefix.
		}
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
		reply := console.Handle(ctx, line)
		cancel()
		line, discard = line[:0], false
		writeReply(reply)
		if reply.State == nativefabric.ProgramParked {
			return
		}
	}
}
