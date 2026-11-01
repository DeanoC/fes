//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

// build.py replaces this constant with bytes from the verified sealed package.
// There is no usable payload in a build that bypasses that producer.
const coreImage = ""
