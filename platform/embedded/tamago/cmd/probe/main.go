//go:build tamago

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/board"
)

func main() {
	fmt.Printf("FES native Go probe os=%s arch=%s EL=%d counter=%dHz\n",
		runtime.GOOS, runtime.GOARCH, board.ExceptionLevel(), board.CounterFrequency())
	if runtime.GOOS != "tamago" || runtime.GOARCH != "arm64" ||
		board.ExceptionLevel() != 1 || board.CounterFrequency() == 0 {
		panic("unexpected boot contract")
	}
	before := time.Now()
	work := make(chan []byte)
	go func() {
		buffer := make([]byte, 1<<20)
		for i := range buffer {
			buffer[i] = byte(i * 31)
		}
		time.Sleep(2 * time.Millisecond)
		work <- buffer
	}()
	buffer := <-work
	if time.Since(before) < 2*time.Millisecond || buffer[12345] != byte((12345*31)%256) {
		panic("timer/goroutine/allocation probe failed")
	}
	runtime.GC()
	fmt.Println("PASS timer goroutine allocation GC")
	// This is TamaGo's volatile memory filesystem, not FAT32 or an SD card.
	payload := []byte("FES RAM filesystem probe\n")
	if err := os.WriteFile("/tmp/fes-probe.txt", payload, 0600); err != nil {
		panic(err)
	}
	got, err := os.ReadFile("/tmp/fes-probe.txt")
	if err != nil || !bytes.Equal(got, payload) {
		panic("volatile filesystem probe failed")
	}
	fmt.Println("PASS volatile memory filesystem (not SD/FAT32)")
	fmt.Printf("FES_NATIVE_GO_PROBE PASS os=tamago board=%s\n", board.Name)
}
