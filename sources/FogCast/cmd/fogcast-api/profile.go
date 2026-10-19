package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"syscall"
)

// startProfiles matches fogcast-tenfoot's -cpu-profile and -heap-profile: the
// CPU profile runs until the returned stop, and the heap profile is written on
// SIGUSR1 and again at stop.
func startProfiles(ctx context.Context, cpuPath, heapPath string, stderr io.Writer) (func(), error) {
	var cpu *os.File
	if cpuPath != "" {
		file, err := os.Create(cpuPath)
		if err != nil {
			return nil, err
		}
		if err := pprof.StartCPUProfile(file); err != nil {
			file.Close()
			return nil, err
		}
		cpu = file
	}
	writeHeap := func() {
		file, err := os.Create(heapPath)
		if err == nil {
			runtime.GC()
			err = pprof.WriteHeapProfile(file)
			file.Close()
		}
		if err != nil {
			fmt.Fprintf(stderr, "fogcast-api: heap profile: %v\n", err)
		}
	}
	var heapSignal chan os.Signal
	done := make(chan struct{})
	if heapPath != "" {
		heapSignal = make(chan os.Signal, 1)
		signal.Notify(heapSignal, syscall.SIGUSR1)
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-done:
					return
				case <-heapSignal:
					writeHeap()
				}
			}
		}()
	}
	return func() {
		close(done)
		if heapSignal != nil {
			signal.Stop(heapSignal)
		}
		// Exclude the forced heap GC and serialization from the CPU capture.
		if cpu != nil {
			pprof.StopCPUProfile()
			cpu.Close()
		}
		if heapPath != "" {
			writeHeap()
		}
	}, nil
}
