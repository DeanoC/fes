//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"fmt"
	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
	"sync"
	"time"
)

type registers struct{}

func load32(addr uintptr) uint32
func store32(addr uintptr, value uint32)
func (registers) Read32(offset uint32) uint32  { return load32(0x10808000 + uintptr(offset)) }
func (registers) Write32(offset, value uint32) { store32(0x10808000+uintptr(offset), value) }
func (registers) Now() time.Time               { return time.Now() }

var storageLock sync.Mutex
var attempted bool

// Retain an unresolved lease even when initialization returns an error.
var storageSession *sdcontrol.Session

func OpenStorage() (block.Device, error) {
	if !profileSDControlEnabled || profileSDInputClockHz == 0 {
		return nil, fmt.Errorf("de25-nano: SD is disabled by the firmware handoff")
	}
	storageLock.Lock()
	defer storageLock.Unlock()
	if attempted {
		return nil, fmt.Errorf("de25-nano: storage initialization already attempted")
	}
	attempted = true
	var err error
	storageSession, err = sdcontrol.Open(FabricCPUTransport{}, registers{}, profileSDInputClockHz, sdcontrol.ReadOnly)
	return storageSession, err
}
