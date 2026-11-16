//go:build tamago && arm64 && de25nano && !de25tenfootserial

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"io/fs"
)

const tenfootSerialLinked = false

func serveProgramTenfoot(*nativefabric.ProgramLoader, *packagestore.Store, fs.FS) {
	panic("serial Tenfoot was not selected at build time")
}
