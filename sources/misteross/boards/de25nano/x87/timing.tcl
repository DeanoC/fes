# SPDX-License-Identifier: MIT
# Fitted internal virtual-pin characterization. No board I/O acceptance.
load_package sta
if {[llength $quartus(args)] != 1} {error "expected private build output directory"}
set output [file normalize [lindex $quartus(args) 0]]
cd [file join $output project]
project_open fes_de25_x87
create_timing_netlist
read_sdc
update_timing_netlist
set clocks [get_clocks *]
if {[get_collection_size $clocks] != 1} {error "expected exactly one x87 clock"}
set clock [get_clocks x87_clock]
if {[get_collection_size $clock] != 1 || [get_clock_info -period $clock] != 5.0} {
    error "x87 clock must remain exactly 5.000 ns (200 MHz)"
}
if {[get_collection_size [all_registers]] < 1} {error "fitted x87 has no registers"}
set corners [get_available_operating_conditions -all]
if {[get_collection_size $corners] < 1} {error "missing published operating conditions"}
set stream [open [file join $output timing.tsv] w]
puts $stream "corner\thold_only\tcheck\tcoverage\tpaths\tworst_slack_ns\tfrom\tto"
set failed 0
set json_rows {}
set corner_index 0
foreach_in_collection corner $corners {
    set_operating_conditions $corner
    update_timing_netlist
    set name [get_operating_conditions_info -display_name $corner]
    set hold_only [get_operating_conditions_info -is_hold_only $corner]
    regsub -all {[\\"]} $name {\\&} json_name
    set analyses {}
    foreach check {setup hold recovery removal} {
        # Some published corners deliberately provide short-path models only.
        # Keep them in the inventory and require hold/removal coverage, while
        # explicitly marking setup/recovery as unsupported by that model.
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
                    error "nonfinite or nonnumeric $check slack"
                }
                if {$slack < 0 || [string match -* $slack]} {set failed 1}
            }
        } elseif {$coverage ne "unsupported_hold_only_model"} {
            if {$check in {setup hold}} {error "missing $check timing paths at $name"}
            set coverage not_applicable_synchronous_reset
        }
        # Recovery/removal have no paths when all resets are synchronous.
        puts $stream [join [list $name $hold_only $check $coverage $count $slack $from $to] "\t"]
        lappend analyses "\"$check\": {\"coverage\": \"$coverage\", \"paths\": $count, \"worst_slack_ns\": $slack}"
        if {$coverage ne "unsupported_hold_only_model"} {
            report_timing -$check -npaths 20 -nworst 1 -detail full_path \
                -file [file join $output "corner${corner_index}_${check}.rpt"]
        }
    }
    if {!$hold_only} {
        set internal_paths [get_timing_paths -setup -from [all_registers] -to [all_registers] -npaths 1]
        if {[get_collection_size $internal_paths] != 1} {error "missing internal register setup path at $name"}
        foreach_in_collection path $internal_paths {
            set slack [get_path_info -slack $path]
            puts $stream [join [list $name $hold_only internal_setup analyzed 1 $slack \
                [get_node_info -name [get_path_info -from $path]] \
                [get_node_info -name [get_path_info -to $path]]] "\t"]
            lappend analyses "\"internal_setup\": {\"coverage\": \"analyzed\", \"paths\": 1, \"worst_slack_ns\": $slack}"
        }
        report_timing -setup -from [all_registers] -to [all_registers] -npaths 20 -nworst 1 \
            -detail full_path -file [file join $output "corner${corner_index}_internal_setup.rpt"]
    }
    set pulses [get_min_pulse_width -nworst 1]
    if {[llength $pulses] < 1} {error "missing minimum pulse-width coverage at $name"}
    set pulse_slack Inf
    foreach pulse $pulses {
        set slack [lindex $pulse 0]
        if {![string is double -strict $slack] || [regexp -nocase {nan|inf} $slack]} {
            error "nonfinite or nonnumeric pulse-width slack"
        }
        if {$slack < 0 || [string match -* $slack]} {set failed 1}
        if {$slack < $pulse_slack} {set pulse_slack $slack}
    }
    puts $stream [join [list $name $hold_only minimum_pulse_width analyzed [llength $pulses] $pulse_slack {} {}] "\t"]
    lappend analyses "\"minimum_pulse_width\": {\"coverage\": \"analyzed\", \"paths\": [llength $pulses], \"worst_slack_ns\": $pulse_slack}"
    lappend json_rows "{\"corner\": \"$json_name\", \"hold_only\": [expr {$hold_only ? "true" : "false"}], \"analyses\": {[join $analyses ,]}}"
    incr corner_index
}
close $stream
set summary [open [file join $output timing.json] w]
set status [expr {$failed ? "fail" : "pass"}]
puts $summary "{\"status\": \"$status\", \"clock_hz\": 200000000, \"period_ns\": 5.0, \"corners\": \[[join $json_rows ,]\]}"
close $summary
delete_timing_netlist
project_close
if {$failed} {error "x87 200 MHz all-corner timing FAIL; retained timing.tsv and per-corner reports"}
puts "PASS: x87 200 MHz setup/hold/recovery/removal/minimum-pulse-width gate across all $corner_index published corners"
