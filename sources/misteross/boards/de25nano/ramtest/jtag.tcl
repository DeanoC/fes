# SPDX-License-Identifier: GPL-2.0-or-later
# Source in System Console. This file never discovers, opens or programs a kit
# on load. run requires an explicitly selected master service and build ID.
package require Tcl 8.6
namespace eval de25ramtest {
    variable constants [dict create]
    variable here [file dirname [file normalize [info script]]]
}

proc de25ramtest::load_constants {path} {
    variable constants
    set stream [open $path r]
    try {set text [read $stream]} finally {close $stream}
    set selected [dict create]
    foreach line [split $text \n] {
        if {[regexp {^`define FES_DE25_RAMTEST_([A-Z0-9_]+) 32'h([0-9a-f]{8})$} $line -> name value]} {
            if {[dict exists $selected $name]} {error "duplicate generated constant: $name"}
            scan $value %x parsed
            dict set selected $name $parsed
        } elseif {[string match {`define FES_DE25_RAMTEST_*} $line]} {
            error "unsupported generated constant: $line"
        }
    }
    if {![dict exists $selected BUILD_ID_WORD_COUNT] || [dict get $selected BUILD_ID_WORD_COUNT] != 4} {
        error "expected generated 128-bit build ID contract"
    }
    set constants $selected
}

proc de25ramtest::constant {name} {
    variable constants
    if {![dict exists $constants $name]} {error "missing generated constant: $name"}
    return [dict get $constants $name]
}

proc de25ramtest::address {bank name} {
    if {$bank ni {hps fpga sdram}} {error "unknown RAM bank: $bank"}
    return [expr {[constant JTAG_BRIDGE_BASE] + [constant [string toupper $bank]_OFFSET] + [constant $name]}]
}

proc de25ramtest::read_word {master bank name {extra 0}} {
    set words [master_read_32 $master [expr {[address $bank $name] + $extra}] 1]
    if {[llength $words] != 1 || ![string is entier -strict [lindex $words 0]]} {error "invalid master read at $bank/$name"}
    return [expr {[lindex $words 0] & 0xffffffff}]
}

proc de25ramtest::write_word {master bank name value} {
    if {$bank ni {fpga sdram}} {error "HPS writes require an admitted kernel grant and native binding"}
    master_write_32 $master [address $bank $name] [list [expr {$value & 0xffffffff}]]
}

proc de25ramtest::read_wide {master bank name words} {
    set result 0
    for {set index 0} {$index < $words} {incr index} {
        set result [expr {$result | ([read_word $master $bank $name [expr {$index * 4}]] << ($index * 32))}]
    }
    return $result
}

proc de25ramtest::identify {master expected} {
    if {![regexp {^[0-9a-fA-F]{32}$} $expected]} {error "expected build ID must be exactly 32 hexadecimal digits"}
    set result [dict create]
    foreach bank {hps fpga sdram} {
        set capacity [constant [expr {$bank eq "sdram" ? "SDRAM_BYTES" : "LPDDR4_BYTES"}]]
        set rate [expr {$bank eq "sdram" ? 50000000 : 100000000}]
        set bytes [expr {$bank eq "sdram" ? 2 : 16}]
        foreach {register wanted} [list ID_OFFSET [constant IDENTITY] VERSION_OFFSET [constant VERSION] \
            CHANNEL_OFFSET [constant [string toupper $bank]_CHANNEL] CAPACITY_OFFSET $capacity \
            CLOCK_HZ_OFFSET $rate DATA_BYTES_OFFSET $bytes] {
            set value [read_word $master $bank $register]
            if {$value != $wanted} {error "endpoint identity mismatch: $bank/$register ($value != $wanted)"}
        }
        set build [format %032llx [read_wide $master $bank BUILD_ID_OFFSET [constant BUILD_ID_WORD_COUNT]]]
        if {$build ne [string tolower $expected]} {error "build ID mismatch: $bank ($build)"}
        dict set result $bank [dict create build_id $build capacity $capacity clock_hz $rate data_bytes $bytes \
            status [read_word $master $bank STATUS_OFFSET] fault [read_word $master $bank FAULT_OFFSET]]
    }
    return $result
}

proc de25ramtest::snapshot {master bank} {
    if {$bank ni {fpga sdram}} {error "HPS writes, including SNAPSHOT, are disabled"}
    set previous [read_word $master $bank SERIAL_OFFSET]
    if {$previous == 0xffffffff} {error "snapshot serial exhausted"}
    write_word $master $bank COMMAND_OFFSET [constant COMMAND_SNAPSHOT]
    set serial $previous
    for {set poll 0} {$poll < 128 && $serial == $previous} {incr poll} {
        set serial [read_word $master $bank SERIAL_OFFSET]
    }
    if {$serial != $previous + 1} {error "snapshot command did not advance serial exactly once"}
    set result [dict create serial $serial]
    foreach {name register words} {
        status SNAPSHOT_STATUS_OFFSET 1 fault SNAPSHOT_FAULT_OFFSET 1 generation SNAPSHOT_GENERATION_OFFSET 2
        sequence SNAPSHOT_SEQUENCE_OFFSET 1 pattern PATTERN_OFFSET 1 cursor CURSOR_OFFSET 1 errors ERRORS_OFFSET 1
        first_address FIRST_ADDRESS_OFFSET 1 written_bytes WRITTEN_OFFSET 2 read_bytes READ_OFFSET 2
        write_cycles WRITE_CYCLES_OFFSET 2 read_cycles READ_CYCLES_OFFSET 2 expected EXPECTED_OFFSET 4
        actual ACTUAL_OFFSET 4 bad_bits BAD_BITS_OFFSET 4 samples SAMPLES_OFFSET 1 first_min FIRST_MIN_OFFSET 1
        first_max FIRST_MAX_OFFSET 1 first_sum FIRST_SUM_OFFSET 2 last_min LAST_MIN_OFFSET 1
        last_max LAST_MAX_OFFSET 1 last_sum LAST_SUM_OFFSET 2
    } {
        dict set result $name [read_wide $master $bank $register $words]
    }
    if {[read_word $master $bank SERIAL_OFFSET] != $serial} {error "snapshot changed during read; exclusive master access required"}
    return $result
}

proc de25ramtest::run {service expected bank {timeout_ms 10000} {poll_ms 10} {length {}}} {
    if {$bank ni {fpga sdram}} {error "only FPGA B and SDRAM destructive tests are allowed; HPS requires kernel ownership"}
    if {$service eq {} || ![regexp {^[0-9a-fA-F]{32}$} $expected]} {error "explicit master service and 128-bit expected build ID required"}
    foreach value [list $timeout_ms $poll_ms] {
        if {![string is integer -strict $value] || $value <= 0 || $value > 3600000} {error "host wait/poll must be positive bounded milliseconds"}
    }
    if {[lsearch -exact [get_service_paths master] $service] < 0} {error "selected master service is unavailable"}
    set master [claim_service master $service de25-ramtest]
    set started 0
    try {
        # All identities are verified before the first write, including the
        # untouched HPS endpoint. No CPU physical base is inferred from this map.
        set endpoints [identify $master $expected]
        set selected [dict get $endpoints $bank]
        set status [dict get $selected status]
        if {[dict get $selected fault] != 0 || ($status & ([constant STATUS_BUSY] | [constant STATUS_FAULT] | [constant STATUS_RESET_UNCERTAIN])) != 0} {
            error "selected bank is busy or faulted; preserve its evidence"
        }
        if {($status & [constant STATUS_CALIBRATED]) == 0} {error "selected memory is not calibrated/initialized"}
        set capacity [dict get $selected capacity]
        if {$length eq {}} {set length $capacity}
        if {![string is entier -strict $length] || $length < 32 || $length > $capacity || ($length & ($length - 1)) != 0} {
            error "test length must be a power of two from 32 bytes through bank capacity"
        }
        set generation [read_wide $master $bank GENERATION_OFFSET 2]
        set sequence [read_word $master $bank SEQUENCE_OFFSET]
        if {$generation == 0 || $sequence == 0xffffffff} {error "invalid/exhausted staged generation or sequence"}
        incr sequence
        foreach {name value} [list BASE_OFFSET 0 BASE_HIGH_OFFSET 0 SPAN_OFFSET $capacity SPAN_HIGH_OFFSET 0 \
            TRANSFER_OFFSET 0 LENGTH_OFFSET $length SEQUENCE_OFFSET $sequence] {
            write_word $master $bank $name $value
        }
        write_word $master $bank COMMAND_OFFSET [constant COMMAND_START]
        set started 1
        set deadline [expr {[clock milliseconds] + $timeout_ms}]
        while {1} {
            set observed [snapshot $master $bank]
            dict set observed endpoints $endpoints
            if {[dict get $observed generation] != $generation || [dict get $observed sequence] != $sequence} {
                error "snapshot does not match the requested generation/sequence"
            }
            set status [dict get $observed status]
            if {($status & [constant STATUS_BUSY]) == 0} {
                set passed [expr {($status & [constant STATUS_DONE]) != 0 &&
                    ($status & ([constant STATUS_FAULT] | [constant STATUS_RESET_UNCERTAIN])) == 0 &&
                    [dict get $observed fault] == 0 && [dict get $observed errors] == 0}]
                dict set observed outcome [expr {$passed ? "PASS" : "FAIL"}]
                return $observed
            }
            if {[clock milliseconds] >= $deadline} {
                # STOP is a request. A missing memory response can keep BUSY
                # asserted forever; host timeout neither frees memory nor drains it.
                write_word $master $bank COMMAND_OFFSET [constant COMMAND_STOP]
                dict set observed outcome HOST_TIMEOUT
                dict set observed stop_requested 1
                dict set observed drain_proven 0
                return $observed
            }
            after $poll_ms
        }
    } on error {message options} {
        if {$started} {
            set stopped [expr {![catch {write_word $master $bank COMMAND_OFFSET [constant COMMAND_STOP]}]}]
            append message "; STOP request attempted=$stopped; drain remains unproven"
        }
        return -options $options $message
    } finally {
        close_service master $master
    }
}

de25ramtest::load_constants [file join $de25ramtest::here generated fes_de25_ramtest.vh]
