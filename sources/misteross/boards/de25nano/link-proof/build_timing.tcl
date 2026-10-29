# Read-only completed-database check of the two intentional synchronizers.
load_package sta
if {[llength $quartus(args)] != 3} {error "expected project directory, revision, private output"}
set project [file normalize [lindex $quartus(args) 0]]
set revision [lindex $quartus(args) 1]
set output [file normalize [lindex $quartus(args) 2]]
file mkdir $output
cd $project
project_open -revision $revision fes_link_proof
create_timing_netlist
read_sdc
update_timing_netlist
set reset_head [get_registers {reset_sync[0]}]
set reset_tail [get_registers {reset_sync[1]}]
set freeze_head [get_registers {shell_inst|freeze_sync[0]}]
set freeze_tail [get_registers {shell_inst|freeze_sync[1]}]
foreach {name nodes} [list reset_head $reset_head reset_tail $reset_tail freeze_head $freeze_head freeze_tail $freeze_tail] {
    if {[get_collection_size $nodes] != 1} {error "expected exactly one $name"}
    foreach_in_collection node $nodes {puts "REGISTER $name [get_node_info -name $node]"}
}
set clock [get_clocks board50]
if {[get_collection_size $clock] != 1 || [get_clock_info -period $clock] != 20.0} {
    error "expected exact 50 MHz board clock"
}
if {[get_collection_size [get_timing_paths -setup -from [get_ports freeze_req] -to $freeze_head -npaths 1]] != 0} {
    error "asynchronous virtual request ingress remains timed"
}
set stream [open [file join $output synchronizer-paths.tsv] w]
puts $stream "corner\tgroup\tcheck\tfrom\tto\tslack"
set corners [get_available_operating_conditions]
if {[get_collection_size $corners] < 1} {error "missing published operating conditions"}
foreach_in_collection corner $corners {
    set_operating_conditions $corner
    update_timing_netlist
    set corner_name [get_operating_conditions_info -display_name $corner]
    foreach check {setup hold} {
        foreach {name source target} [list reset_stage $reset_head $reset_tail freeze_stage $freeze_head $freeze_tail] {
            set paths [get_timing_paths -$check -from $source -to $target -npaths 1]
            if {[get_collection_size $paths] != 1} {error "$name lost paired $check coverage"}
            foreach_in_collection path $paths {
                set slack [get_path_info -slack $path]
                if {$slack < 0} {error "$name negative $check slack"}
                puts $stream [join [list $corner_name $name $check [get_node_info -name [get_path_info -from $path]] [get_node_info -name [get_path_info -to $path]] $slack] "\t"]
            }
        }
        set paths [get_timing_paths -$check -from $freeze_tail -npaths 4096 -nworst 1]
        set count [get_collection_size $paths]
        if {$count < 1 || $count >= 4096} {error "freeze control fanout coverage is empty or truncated"}
        foreach_in_collection path $paths {
            set slack [get_path_info -slack $path]
            if {$slack < 0} {error "freeze control fanout negative $check slack"}
            puts $stream [join [list $corner_name freeze_fanout $check [get_node_info -name [get_path_info -from $path]] [get_node_info -name [get_path_info -to $path]] $slack] "\t"]
        }
    }
}
close $stream
delete_timing_netlist
project_close
puts "PASS: published-corner reset/freeze stage D and freeze fanout timing retained"
