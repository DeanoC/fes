# Trace the fitted pc386 retiming advisory under the normal Pro lease.
# quartus_sta -t retimed_tension.tcl golden_top TARGETS.txt OUTPUT.tsv
proc name_or_empty {option object} {
    if {$object eq ""} {return ""}
    if {$option eq "clock"} {return [get_clock_info -name $object]}
    return [get_node_info -name $object]
}

set revision [lindex $argv 0]
set expected_clock {fes_machine_pll|machine_pll_outclk0}
set project_prefix {soc_inst|subsys_periph|fes_scanout|fes_scanout|system|}
set list_file [open [lindex $argv 1] r]
set targets {}
foreach line [split [string trim [read $list_file]] "\n"] {
    set target [string trim $line]
    if {![string match "${project_prefix}*" $target] || $target in $targets} {
        error "target outside the pc386 system or repeated: $target"
    }
    lappend targets $target
}
close $list_file
if {[llength $targets] == 0} {error "no TMC endpoints requested"}

set output [open [lindex $argv 2] w]
project_open $revision -revision $revision
create_timing_netlist
read_sdc
update_timing_netlist
set corners [get_available_operating_conditions]
if {[get_collection_size $corners] == 0} {error "no fit operating conditions"}
set problems 0
set matched [dict create]
set available [get_registers -nowarn "${project_prefix}*"]
foreach_in_collection reg $available {
    set name [get_register_info $reg -name]
    if {$name in $targets} {dict lappend matched $name $reg}
}

puts $output "corner\tnode\tcheck\tdirection\texact_matches\towner_clocks\tasync_edges\tsync_stage\tfrom_clock\tto_clock\tslack_ns\tfrom\tto\tstatus"
foreach_in_collection corner $corners {
    set_operating_conditions $corner
    update_timing_netlist
    set label [get_operating_conditions_info -display_name $corner]
    foreach target $targets {
        set exact 0
        if {[dict exists $matched $target]} {set exact [llength [dict get $matched $target]]}
        if {$exact != 1} {
            puts $output [join [list $label $target ownership {} $exact {} {} {} {} {} {} {} {} missing] "\t"]
            incr problems
            continue
        }
        set reg [lindex [dict get $matched $target] 0]
        set escaped [string map [list {[} {\[} {]} {\]}] $target]
        # Use a singleton collection for commands that require a collection.
        # Compare it to the enumerated exact object rather than trusting a glob.
        set selected {}
        foreach spelling [list $target $escaped] {
            set query [get_registers -nowarn [list $spelling]]
            if {[get_collection_size $query] != 1} {continue}
            foreach_in_collection candidate $query {
                if {$candidate eq $reg} {set selected $query}
            }
            if {$selected ne ""} {break}
        }
        if {$selected eq ""} {
            puts $output [join [list $label $target ownership {} $exact {} {} {} {} {} {} {} {} ambiguous-query] "\t"]
            incr problems
            continue
        }
        set clocks {}
        foreach_in_collection clock [get_clocks -nowarn -of_objects $selected] {
            lappend clocks [get_clock_info -name $clock]
        }
        set core [list $label $target ownership {} $exact [join $clocks ,]]
        set edges [llength [get_register_info $reg -asynch_edges]]
        set synchronizer [get_register_info $reg -is_synchronizer]
        set owner_ok [expr {$clocks eq [list $expected_clock] && $synchronizer == 0}]
        puts $output [join [concat $core [list $edges $synchronizer {} {} {} {} {} \
                                                  [expr {$owner_ok ? "ok" : "unexpected"}]]] "\t"]
        if {!$owner_ok} {incr problems}

        foreach {check direction} {setup from setup to hold from hold to recovery to removal to} {
            set core [list $label $target $check $direction $exact [join $clocks ,] $edges $synchronizer]
            set paths [get_timing_paths -$check -$direction $selected -npaths 1]
            set path_count [get_collection_size $paths]
            if {$path_count == 0} {
                set applicable [expr {$check in {setup hold} || $edges != 0}]
                puts $output [join [concat $core [list {} {} {} {} {} \
                                                     [expr {$applicable ? "missing" : "n/a"}]]] "\t"]
                if {$applicable} {incr problems}
                continue
            }
            foreach_in_collection path $paths {
                set from [name_or_empty node [get_path_info -from $path]]
                set to [name_or_empty node [get_path_info -to $path]]
                set from_clock [name_or_empty clock [get_path_info -from_clock $path]]
                set to_clock [name_or_empty clock [get_path_info -to_clock $path]]
                set slack [get_path_info -slack $path]
                set requested_end [expr {$direction eq "from" ? $from : $to}]
                set endpoint_ok [expr {$requested_end eq $target}]
                set slack_ok [expr {[string is double -strict $slack] && $slack >= 0}]
                set status [expr {$endpoint_ok && $slack_ok ? "ok" : "unexpected"}]
                puts $output [join [concat $core [list $from_clock $to_clock $slack $from $to $status]] "\t"]
                if {$status ne "ok"} {incr problems}
            }
        }
        flush $output
    }
}
close $output
delete_timing_netlist
project_close
if {$problems != 0} {error "$problems missing or unexpected ownership/path checks"}
