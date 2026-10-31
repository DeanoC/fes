# Read-only analysis of the completed43c248d2df RAM Tester database.
# No assignments, SDC edits, report-database saves or compilation commands.
load_package sta
if {[llength $quartus(args)] != 2} {error "expected project and private output directory"}
set project [file normalize [lindex $quartus(args) 0]]
set output [file normalize [lindex $quartus(args) 1]]
file mkdir $output
cd $project
project_open [file join $project golden_top]
create_timing_netlist
read_sdc
update_timing_netlist
report_sdc -file [file join $output loaded-sdc-before.rpt]
set cal_source [get_registers {fes_ram_calibration|r_cal_done_rst_n}]
set cal_head [get_registers {soc_inst|subsys_periph|fes_ramtest_fpga|fes_ramtest_fpga|calibration[0]}]
set cal_tail [get_registers {soc_inst|subsys_periph|fes_ramtest_fpga|fes_ramtest_fpga|calibration[1]}]
set cal_d [get_pins -compatibility_mode -nocase {soc_inst|subsys_periph|fes_ramtest_fpga|fes_ramtest_fpga|calibration[0]|D}]
foreach {label nodes} [list calibration_source $cal_source calibration_head $cal_head calibration_tail $cal_tail calibration_head_D $cal_d] {
    puts "COUNT $label [get_collection_size $nodes]"
    if {[get_collection_size $nodes] != 1} {error "expected exact one $label"}
    foreach_in_collection node $nodes {puts "NODE $label [get_node_info -name $node]"}
}
load_package drc
set drc_file [file join $output calibration-cdc-compiled.rpt]
drc::run_checks -enable_rules {CDC-50002} -executable timing_signoff -output_file $drc_file \
    -folder_path {Timing Analyzer} -report_panel {FES Calibration Final}
if {![file exists $drc_file]} {error "missing compiled calibration DRC query report"}
set f [open $drc_file]; set report [read $f]; close $f
if {![regexp {; CDC-50002[^;\n]*;\s*High\s*;\s*([0-9]+)\s*;\s*0\s*;} $report -> violations] || $violations != 0} {error "compiled calibration CDC remains open"}
puts "DRC compiled violations=$violations waived=0"
if {[get_collection_size [get_timing_paths -setup -from $cal_source -to $cal_head -npaths 1]] != 0} {error "compiled calibration ingress was not cut"}
set cal_log [open [file join $output calibration-paths.tsv] w]
puts $cal_log "group\tcorner\tcheck\tfrom\tto\tslack"
set log [open [file join $output paths.tsv] w]
puts $log "group\tcorner\tbit\tcheck\tfrom\tto\tfrom_clock\tto_clock\tfrom_inverted\tto_inverted\tlaunch_time\tlatch_time\tclock_relationship\tslack"
set heads [get_registers {*|sampled_dq[*]}]
if {[get_collection_size $heads] != 16} {error "expected exactly16 sampled_dq registers"}
set prefixes [list]
foreach_in_collection reg $heads {
    set name [get_node_info -name $reg]
    if {![regexp {^(.*\|)sampled_dq\[([0-9]+)\]$} $name -> prefix bit] || $bit < 0 || $bit > 15} {error "unexpected sample register: $name"}
    if {[lsearch -exact $prefixes $prefix] < 0} {lappend prefixes $prefix}
}
if {[llength $prefixes] != 1} {error "ambiguous sample hierarchy"}
set prefix [lindex $prefixes 0]
set tails [get_registers [format {%sreaddata[*]} $prefix]]
if {[get_collection_size $tails] != 16} {error "expected exactly16 local readdata registers"}
set clk [get_clocks ramtest_sdram50]
if {[get_collection_size $clk] != 1 || [get_clock_info -period $clk] != 20.0} {error "expected exact50MHz SDRAM clock"}
foreach name {ramtest_sdram50 ramtest_sdram_forwarded} {
    set clock [get_clocks $name]
    if {[get_collection_size $clock] != 1} {error "expected exact clock$name"}
    puts "CLOCK $name period=[get_clock_info -period $clock] waveform=[get_clock_info -waveform $clock] inverted=[get_clock_info -is_inverted $clock] type=[get_clock_info -type $clock]"
}
set corners [get_available_operating_conditions]
if {[get_collection_size $corners] < 1} {error "no published operating conditions"}
set corner_index 0
foreach_in_collection corner $corners {
    set_operating_conditions $corner
    update_timing_netlist
    set corner_name [get_operating_conditions_info -display_name $corner]
    puts "CORNER $corner_index $corner_name"
    foreach check {setup hold} {
        set stage [get_timing_paths -$check -from $cal_head -to $cal_tail -npaths 1]
        if {[get_collection_size $stage] != 1} {error "calibration second stage is not timed"}
        foreach_in_collection path $stage {
            set slack [get_path_info -slack $path]
            puts $cal_log [join [list stage $corner_name $check [get_node_info -name [get_path_info -from $path]] [get_node_info -name [get_path_info -to $path]] $slack] "\t"]
            puts "CALIBRATION_STAGE $corner_index $check $slack"
            if {$slack < 0} {error "negative calibration second-stage path"}
        }
        set fanout [get_timing_paths -$check -from $cal_tail -npaths 8192 -nworst 1]
        set count [get_collection_size $fanout]
        if {$count < 1 || $count >= 8192} {error "invalid bounded calibration fanout inventory"}
        set minimum 1e30
        foreach_in_collection path $fanout {
            set slack [get_path_info -slack $path]
            if {$slack < $minimum} {set minimum $slack}
            puts $cal_log [join [list fanout $corner_name $check [get_node_info -name [get_path_info -from $path]] [get_node_info -name [get_path_info -to $path]] $slack] "\t"]
            if {$slack < 0} {error "negative calibration fanout path"}
        }
        puts "CALIBRATION_FANOUT_COUNT $corner_index $check $count minimum=$minimum"
        report_timing -$check -from $cal_head -to $cal_tail -npaths 1 -detail full_path \
            -file [file join $output ${corner_index}-calibration-stage-${check}.rpt]
        report_timing -$check -from $cal_tail -npaths 1 -detail full_path \
            -file [file join $output ${corner_index}-calibration-fanout-${check}.rpt]
        for {set bit 0} {$bit < 16} {incr bit} {
            set head [get_registers [format {%ssampled_dq[%d]} $prefix $bit]]
            set tail [get_registers [format {%sreaddata[%d]} $prefix $bit]]
            if {[get_collection_size $head] != 1 || [get_collection_size $tail] != 1} {error "missing exact bit$bit register pair"}
            foreach group {data enable} {
                if {$group eq "data"} {
                    set command [list get_timing_paths -$check -fall_from_clock $clk -rise_to_clock $clk -from $head -to $tail -npaths 1]
                } else {
                    set command [list get_timing_paths -$check -rise_from_clock $clk -fall_to_clock $clk -to $head -npaths 1]
                }
                set paths [{*}$command]
                if {[get_collection_size $paths] != 1} {error "missing internal $group $check bit$bit path in $corner_name"}
                foreach_in_collection path $paths {
                    set from [get_node_info -name [get_path_info -from $path]]
                    set to [get_node_info -name [get_path_info -to $path]]
                    set from_clock [get_clock_info -name [get_path_info -from_clock $path]]
                    set to_clock [get_clock_info -name [get_path_info -to_clock $path]]
                    set from_inv [get_path_info -from_clock_is_inverted $path]
                    set to_inv [get_path_info -to_clock_is_inverted $path]
                    set want_from_inv [expr {$group eq "data" ? 1 : 0}]
                    if {$from_clock ne "ramtest_sdram50" || $to_clock ne "ramtest_sdram50" ||
                        $from_inv != $want_from_inv || $to_inv == $want_from_inv} {error "unexpected clock/edge identity for $group bit$bit"}
                    set slack [get_path_info -slack $path]
                    puts $log [join [list $group $corner_name $bit $check $from $to $from_clock $to_clock $from_inv $to_inv \
                        [get_path_info -launch_time $path] [get_path_info -latch_time $path] \
                        [get_path_info -clock_relationship $path] $slack] "\t"]
                    flush $log
                    if {$slack < 0} {error "negative internal $group $check bit$bit path"}
                }
            }
        }
        report_timing -$check -fall_from_clock $clk -rise_to_clock $clk -from $heads -to $tails -npaths 1 -detail full_path \
            -file [file join $output ${corner_index}-data-${check}.rpt]
        report_timing -$check -rise_from_clock $clk -fall_to_clock $clk -to $heads -npaths 1 -detail full_path \
            -file [file join $output ${corner_index}-enable-${check}.rpt]
    }
    incr corner_index
}
close $log
close $cal_log
puts "PASS16 paired internal data paths and16 internal enable paths at$corner_index published operating conditions"
project_close
