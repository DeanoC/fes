# SPDX-License-Identifier: GPL-3.0-or-later
# Explicit, bounded-register JTAG diagnostic. This script does not program a
# device. The caller must select the claimed board, linked image and master.
foreach name {FES_DE25_SOF FES_DE25_DEVICE FES_DE25_MASTER FES_DE25_BASE FES_DE25_BUILD_ID FES_DE25_ABI} {
    if {![info exists env($name)] || $env($name) eq {}} {error "missing explicit $name"}
}
if {![regexp {^[0-9a-f]{32}$} $env(FES_DE25_BUILD_ID)]} {error "invalid expected build ID"}
if {![string is integer -strict $env(FES_DE25_ABI)] || $env(FES_DE25_ABI) != 0x10203040} {
    puts "FES_PROFILE_REJECTED writes=0"
    error "this injected-response recipe requires the on-chip diagnostic ABI"
}
set design [design_load $env(FES_DE25_SOF)]
set device $env(FES_DE25_DEVICE)
set until [expr {[clock milliseconds] + 10000}]
while {[lsearch -exact [get_service_paths device] $device] < 0 && [clock milliseconds] < $until} {
    after 100
    update
}
if {[lsearch -exact [get_service_paths device] $device] < 0} {error "explicit claimed board absent"}
design_link $design $device
set service $env(FES_DE25_MASTER)
set until [expr {[clock milliseconds] + 10000}]
while {[lsearch -exact [get_service_paths master] $service] < 0 && [clock milliseconds] < $until} {
    after 100
    update
}
if {[lsearch -exact [get_service_paths master] $service] < 0} {error "explicit board master absent"}
set master [claim_service master $service de25-embedded-identity]
set base $env(FES_DE25_BASE)
set writes 0
proc readreg {offset} {
    global master base
    return [expr {[lindex [master_read_32 $master [expr {$base+$offset}] 1] 0] & 0xffffffff}]
}
proc writereg {offset value} {
    global master base writes
    master_write_32 $master [expr {$base+$offset}] [list $value]
    incr writes
}
proc require {condition message} {
    if {![uplevel 1 [list expr $condition]]} {error $message}
}
set result [catch {
    set magic [readreg 0x00]
    set version [readreg 0x04]
    set abi [readreg 0x08]
    set w0 [readreg 0x0c]; set w1 [readreg 0x10]; set w2 [readreg 0x14]; set w3 [readreg 0x18]
    set identity [format %08x%08x%08x%08x $w3 $w2 $w1 $w0]
    if {$magic != 0x46455345 || $version != 0x00010000 || $abi != $env(FES_DE25_ABI) ||
        $identity ne $env(FES_DE25_BUILD_ID)} {
        puts "FES_IDENTITY_REJECTED writes=$writes"
        error "physical identity mismatch"
    }
    puts "FES_IDENTITY_MATCH build=$identity abi=$abi writes=$writes"
    require {[readreg 0x38] == 0 && [readreg 0x3c] == 0} "reset reported drain"
    writereg 0x24 1; writereg 0x28 0; writereg 0x34 1
    writereg 0x84 0x1000; writereg 0x88 1; writereg 0x80 0x0c
    require {([readreg 0x8c] & 4) != 0 && [readreg 0x90] == 0x1000} "offered address was not held"
    writereg 0x2c 7; writereg 0x30 0; writereg 0x34 2
    require {[readreg 0x38] == 2 && ([readreg 0x8c] & 4) != 0} "disable lost held address"
    writereg 0x80 0x05
    require {([readreg 0x8c] & 8) != 0} "offered address was not accepted"
    writereg 0x80 0x07
    require {[readreg 0x38] == 2 && [readreg 0x3c] == 1} "partial burst falsely drained"
    writereg 0x80 0x07
    require {[readreg 0x38] == 4 && [readreg 0x3c] == 0 && [readreg 0x40] == 1 && [readreg 0x48] == 7} "drain acknowledgement differs"
    puts "FES_DRAIN_PASS generation=1 cookie=7"
    writereg 0x24 2; writereg 0x28 0; writereg 0x34 1
    writereg 0x84 0x2000; writereg 0x88 0; writereg 0x80 0x0d
    require {([readreg 0x8c] & 8) != 0} "fault-test address was not accepted"
    writereg 0x80 0x17
    require {([readreg 0x38] & 8) != 0 && ([readreg 0x38] & 4) == 0} "bad response did not latch fault"
    puts "FES_FAULT_PASS writes=$writes"
} message options]
set closed [catch {close_service master $master} close_message]
if {$closed} {puts stderr "FES_CLOSE_FAILED $close_message"; exit 1}
puts "FES_MASTER_CLOSED"
if {$result} {puts stderr "FES_DIAGNOSTIC_FAILED $message"; exit 1}
puts "FES_DIAGNOSTIC_PASS"
exit 0
