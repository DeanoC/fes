# Read-only audit of the completed DE25 ZX81 frame-owner database.
load_package sta
if {[llength $quartus(args)] != 3} {error "expected project directory, revision and private output"}
set project [file normalize [lindex $quartus(args) 0]]
set revision [lindex $quartus(args) 1]
set output [file normalize [lindex $quartus(args) 2]]
file mkdir $output
cd $project
project_open -preserve_revision_order -revision $revision fes_link_proof
create_timing_netlist
read_sdc
update_timing_netlist
set groups {
    metadata {*|video|published_rows[*]} {*|video|front_rows[*]} 9
    request_tail {*|video|request_sync[0]} {*|video|request_sync[1]} 1
    acknowledge_tail {*|video|acknowledge_sync[0]} {*|video|acknowledge_sync[1]} 1
    system_release {*|video|sys_release[0]} {*|video|sys_release[1]} 1
    pixel_release {*|video|pix_release[0]} {*|video|pix_release[1]} 1
    readiness_tail {*|pixel_transport_ready_sync[0]} {*|pixel_transport_ready_sync[1]} 1
    readiness_reset_release {*|pixel_transport_reset_release[0]} {*|pixel_transport_reset_release[1]} 1
}
set stream [open [file join $output owner-paths.tsv] w]
puts $stream "corner\tgroup\tcheck\tfrom\tto\tslack_ns\tdata_delay_ns\tlaunch_clock\tcapture_clock"
set corners [get_available_operating_conditions]
if {[get_collection_size $corners] != 4} {error "expected four published operating corners"}
foreach_in_collection corner $corners {
    set_operating_conditions $corner
    update_timing_netlist
    set corner_name [get_operating_conditions_info -display_name $corner]
    foreach {group source sink count} $groups {
        set starts [get_registers $source]
        set ends [get_registers $sink]
        if {[get_collection_size $starts] != $count || [get_collection_size $ends] != $count} {
            error "$group changed physical endpoint count"
        }
        foreach check {setup hold} {
            set paths [get_timing_paths -$check -from $starts -to $ends -npaths 128 -nworst 1]
            if {[get_collection_size $paths] < $count || [get_collection_size $paths] >= 128} {
                error "$corner_name $group missing/truncated $check paths"
            }
            set covered {}
            foreach_in_collection path $paths {
                set from [get_node_info -name [get_path_info -from $path]]
                set to [get_node_info -name [get_path_info -to $path]]
                set slack [get_path_info -slack $path]
                set data [get_path_info -data_delay $path]
                if {$slack < 0} {error "$corner_name $group negative $check slack $slack"}
                if {$group eq "metadata" && $check eq "setup" && $data > 10.0} {
                    error "$corner_name metadata flight exceeds the 10 ns handshake bound"
                }
                dict set covered $to 1
                puts $stream [join [list $corner_name $group $check $from $to $slack $data \
                    [get_clock_info -name [get_path_info -from_clock $path]] \
                    [get_clock_info -name [get_path_info -to_clock $path]]] "\t"]
            }
            if {[dict size $covered] != $count} {error "$group lost a physical endpoint"}
        }
    }
    set reset_source [get_registers {*|pixel_transport_reset_release[1]}]
    set reset_ends [get_registers {*|pixel_transport_ready_sync[*]}]
    if {[get_collection_size $reset_source] != 1 || [get_collection_size $reset_ends] != 2} {
        error "readiness reset-release endpoints changed"
    }
    foreach check {recovery removal} {
        set paths [get_timing_paths -$check -from $reset_source -to $reset_ends -npaths 128 -nworst 1]
        if {[get_collection_size $paths] != 2} {error "readiness reset lost $check timing"}
        set covered {}
        foreach_in_collection path $paths {
            set from [get_node_info -name [get_path_info -from $path]]
            set to [get_node_info -name [get_path_info -to $path]]
            set slack [get_path_info -slack $path]
            if {$slack < 0} {error "readiness reset has negative $check slack"}
            dict set covered $to 1
            puts $stream [join [list $corner_name readiness_reset $check $from $to $slack \
                [get_path_info -data_delay $path] [get_clock_info -name [get_path_info -from_clock $path]] \
                [get_clock_info -name [get_path_info -to_clock $path]]] "\t"]
        }
        if {[dict size $covered] != 2} {error "readiness reset lost a $check endpoint"}
    }
    report_net_delay -nworst 32 -file [file join $output "net-delay-[string map { { } _ / _} $corner_name].txt"]
}
close $stream
delete_timing_netlist
project_close
puts "PASS: complete four-corner frame metadata and synchronizer-tail setup/hold coverage"
