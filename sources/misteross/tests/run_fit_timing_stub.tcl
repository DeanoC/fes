# SPDX-License-Identifier: MIT
# Runs scripts/fit_timing.tcl under the stubbed STA API.
# Arguments: <output directory> <top entity>
set here [file dirname [file normalize [info script]]]
source [file join $here quartus_sta_stub.tcl]
set quartus(args) $argv
source [file join $here .. scripts fit_timing.tcl]
