//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

// build.py overlays this file with validated firmware handoff constants.
// Intentionally no default RAM window or guessed card identity: direct builds
// with -tags de25nano must fail until a handoff profile has been supplied.
