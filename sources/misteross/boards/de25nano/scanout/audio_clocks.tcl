# Engineering observations only. Read the existing fitted clocks and paths;
# add no clocks, delays, exceptions or reviewed-finding substitutions.
cd [lindex $quartus(args) 0]
set output [lindex $quartus(args) 1]
project_open golden_top
create_timing_netlist
read_sdc
update_timing_netlist
set stream [open [file join $output clocks.tsv] w]
puts $stream "clock\tperiod_ns\twaveform\tedges\tmaster"
foreach name {
    fes_audio_inst|clock|iopll_0_outclk0
    de25_i2s_bit_clock de25_i2s_bit_clock_output
    de25_i2s_word_clock de25_i2s_word_clock_output
} {
    set clock [get_clocks $name]
    if {[get_collection_size $clock] != 1} {error "missing unique audio clock $name"}
    puts $stream [join [list $name [get_clock_info -period $clock] \
        [get_clock_info -waveform $clock] [get_clock_info -edges $clock] \
        [get_clock_info -master_clock $clock]] "\t"]
}
close $stream
set master [get_clock_info -targets [get_clocks {fes_audio_inst|clock|iopll_0_outclk0}]]
if {[get_collection_size $master] != 1} {error "missing unique audio PLL output"}
set stream [open [file join $output paths.tsv] w]
puts $stream "corner\tclock\tpath\tbound\tcount\tdelay_ns"
set corners [get_available_operating_conditions]
if {[get_collection_size $corners] < 1} {error "no fitted operating conditions"}
foreach_in_collection corner $corners {
    set_operating_conditions $corner
    update_timing_netlist
    set name [get_operating_conditions_info -display_name $corner]
    foreach {label bit port} {bit_clock 1 HDMI_SCLK word_clock 7 HDMI_LRCLK} {
        set phase_name [format {fes_audio_inst|audio|serializer|phase[%d]} $bit]
        set phase [get_registers $phase_name]
        set clock_pin [get_pins -compatibility_mode -nocase "${phase_name}|clk"]
        set target [get_ports $port]
        foreach endpoint [list $phase $clock_pin $target] {
            if {[get_collection_size $endpoint] != 1} {error "audio clock endpoint changed"}
        }
        foreach kind {network output} {
            if {$kind eq "network"} {
                set launch $master; set capture $clock_pin
            } else {
                set launch $phase; set capture $target
            }
            foreach bound {min max} {
                set options {}
                if {$bound eq "min"} {set options {-min_path}}
                set result [report_path {*}$options -from $launch -to $capture -npaths 1 \
                    -file [file join $output "${corner}_${label}_${kind}_${bound}.rpt"]]
                if {[lindex $result 0] != 1} {error "missing audio $kind path"}
                puts $stream [join [list $name $label $kind $bound {*}$result] "\t"]
            }
        }
    }
}
close $stream
delete_timing_netlist
project_close
