# SPDX-License-Identifier: MIT
# The timing gate of an internal virtual-pin characterisation fit
# (scripts/fit_i386_device.py). No board I/O timing or hardware acceptance is
# established here. Every published operating condition is enumerated, and
# setup, hold, recovery, removal and minimum pulse width must all pass at
# every one of them. Nothing is relaxed: a corner that misses is a failure.
load_package sta
if {[llength $quartus(args)] != 2} {error "expected an output directory and a top entity"}
set output [file normalize [lindex $quartus(args) 0]]
set top [lindex $quartus(args) 1]
project_open $top
create_timing_netlist
read_sdc
update_timing_netlist
# Exactly the clocks the SDC creates, not merely one: a design whose second
# clock went virtual or was optimised away would otherwise be analysed on the
# survivor alone and reported as covered.
set clocks [get_clocks *]
set expected [llength [split [string trim $::env(FES_FIT_CLOCKS)]]]
if {[get_collection_size $clocks] != $expected} {
    error "expected exactly $expected clock(s), found [get_collection_size $clocks]"
}
if {[get_collection_size [all_registers]] < 1} {error "fitted design has no registers"}

# Quartus Pro takes -all; the 17.x standard edition may not.
if {[catch {set corners [get_available_operating_conditions -all]}]} {
    set corners [get_available_operating_conditions]
}
if {[get_collection_size $corners] < 1} {error "missing published operating conditions"}

set stream [open [file join $output timing.tsv] w]
puts $stream "corner\thold_only\tcheck\tcoverage\tpaths\tworst_slack_ns\tfrom\tto"
set failed 0
set json_rows {}
set fmax_rows {}
set index 0
foreach_in_collection corner $corners {
    set_operating_conditions $corner
    update_timing_netlist
    set name [get_operating_conditions_info -display_name $corner]
    if {[catch {set hold_only [get_operating_conditions_info -is_hold_only $corner]}]} {
        set hold_only 0
    }
    regsub -all {[\\"]} $name {\\&} json_name
    set analyses {}
    foreach check {setup hold recovery removal} {
        # Some published corners provide short-path models only; keep them in
        # the inventory, require their hold/removal coverage, and mark
        # setup/recovery as unsupported by that model rather than as a pass.
        set coverage analyzed
        if {$hold_only && $check in {setup recovery}} {
            set paths {}
            set coverage unsupported_hold_only_model
        } else {
            set paths [get_timing_paths -$check -npaths 1]
        }
        set count [get_collection_size $paths]
        set slack null
        set from {}
        set to {}
        if {$count > 0} {
            foreach_in_collection path $paths {
                set slack [get_path_info -slack $path]
                set from [get_node_info -name [get_path_info -from $path]]
                set to [get_node_info -name [get_path_info -to $path]]
                if {![string is double -strict $slack] || [regexp -nocase {nan|inf} $slack]} {
                    error "nonfinite or nonnumeric $check slack at $name"
                }
                if {$slack < 0 || [string match -* $slack]} {set failed 1}
            }
        } elseif {$coverage ne "unsupported_hold_only_model"} {
            if {$check in {setup hold}} {error "missing $check timing paths at $name"}
            # Recovery and removal have no paths when every reset is
            # synchronous. A design that is known to have asynchronous resets
            # must not have them vanish: zero paths there would mean the resets
            # were swept or mis-elaborated, and labelling that "not applicable"
            # would hide it. FES_FIT_ASYNC_RESET names such a design.
            if {[info exists ::env(FES_FIT_ASYNC_RESET)]
                && $::env(FES_FIT_ASYNC_RESET) ne "0"} {
                error "$check has no paths at $name, but this design is declared to have\
                       asynchronous resets: they were swept or mis-elaborated"
            }
            set coverage not_applicable_synchronous_reset
        }
        if {$coverage eq "analyzed"} {set analysed($check) 1}
        puts $stream [join [list $name $hold_only $check $coverage $count $slack $from $to] "\t"]
        lappend analyses "\"$check\": {\"coverage\": \"$coverage\", \"paths\": $count, \"worst_slack_ns\": $slack}"
        if {$coverage ne "unsupported_hold_only_model"} {
            report_timing -$check -npaths 20 -nworst 1 -detail full_path \
                -file [file join $output "corner${index}_${check}.rpt"]
        }
    }
    # The register-to-register setup path: the design's own worst path, with
    # no virtual port path in it.
    if {!$hold_only} {
        set internal [get_timing_paths -setup -from [all_registers] -to [all_registers] -npaths 1]
        if {[get_collection_size $internal] != 1} {error "missing internal register setup path at $name"}
        foreach_in_collection path $internal {
            set slack [get_path_info -slack $path]
            puts $stream [join [list $name $hold_only internal_setup analyzed 1 $slack \
                [get_node_info -name [get_path_info -from $path]] \
                [get_node_info -name [get_path_info -to $path]]] "\t"]
            lappend analyses "\"internal_setup\": {\"coverage\": \"analyzed\", \"paths\": 1, \"worst_slack_ns\": $slack}"
        }
        report_timing -setup -from [all_registers] -to [all_registers] -npaths 20 -nworst 1 \
            -detail full_path -file [file join $output "corner${index}_internal_setup.rpt"]
    }
    set pulses [get_min_pulse_width -nworst 1]
    if {[llength $pulses] < 1} {error "missing minimum pulse-width coverage at $name"}
    set pulse Inf
    foreach item $pulses {
        set slack [lindex $item 0]
        if {![string is double -strict $slack] || [regexp -nocase {nan|inf} $slack]} {
            error "nonfinite or nonnumeric pulse-width slack at $name"
        }
        if {$slack < 0 || [string match -* $slack]} {set failed 1}
        if {$slack < $pulse} {set pulse $slack}
    }
    set analysed(minimum_pulse_width) 1
    puts $stream [join [list $name $hold_only minimum_pulse_width analyzed [llength $pulses] $pulse {} {}] "\t"]
    lappend analyses "\"minimum_pulse_width\": {\"coverage\": \"analyzed\", \"paths\": [llength $pulses], \"worst_slack_ns\": $pulse}"
    # The fitter's own restricted-Fmax estimate at this corner, per clock.
    # It is an estimate at this constraint, not a closure: the method's ladder
    # decides what the design closes at.
    set fmax {}
    if {![catch {set info [get_clock_fmax_info]}]} {
        foreach row $info {
            regsub -all {[\\"]} [lindex $row 0] {\\&} clock_name
            lappend fmax "{\"clock\": \"$clock_name\", \"fmax\": \"[lindex $row 1]\", \"restricted_fmax\": \"[lindex $row 2]\"}"
        }
    }
    lappend fmax_rows "{\"corner\": \"$json_name\", \"clocks\": \[[join $fmax ,]\]}"
    lappend json_rows "{\"corner\": \"$json_name\", \"hold_only\": [expr {$hold_only ? "true" : "false"}], \"analyses\": {[join $analyses ,]}}"
    incr index
}
close $stream
set summary [open [file join $output timing.json] w]
set status [expr {$failed ? "fail" : "pass"}]
puts $summary "{\"status\": \"$status\", \"corner_count\": $index, \"corners\": \[[join $json_rows ,]\], \"fmax\": \[[join $fmax_rows ,]\]}"
close $summary
delete_timing_netlist
project_close
if {$failed} {error "timing FAIL at the fitted constraint; retained timing.tsv and the per-corner reports"}
# Name the analyses that actually had paths, not a fixed list: a design whose
# resets are all synchronous has zero recovery and removal paths, and saying
# they passed would overstate what was checked.
set passed {}
foreach check {setup hold recovery removal minimum_pulse_width} {
    if {[info exists analysed($check)]} {lappend passed $check}
}
set skipped {}
foreach check {setup hold recovery removal minimum_pulse_width} {
    if {![info exists analysed($check)]} {lappend skipped $check}
}
set message "PASS: [join $passed /] across all $index published corners"
if {[llength $skipped] > 0} {
    append message "; [join $skipped /] had no paths to analyse and were not applicable"
}
puts $message
