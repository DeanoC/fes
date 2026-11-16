//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"fmt"
	"github.com/DeanoC/fes/platform/embedded/tamago/board"
	"time"
)

func main() {
	if _, err := board.InitDevices(); err != nil {
		panic(err)
	}
	keyboard, err := board.OpenKeyboard()
	if err != nil {
		panic(err)
	}
	fmt.Printf("FES_KEYBOARD_READY board=%s\n", board.Name)
	for {
		events, err := keyboard.Poll()
		if err != nil {
			panic(err)
		}
		for _, event := range events {
			fmt.Printf("FES_KEYBOARD_EVENT key=%s\n", event.Key)
		}
		time.Sleep(time.Millisecond)
	}
}
