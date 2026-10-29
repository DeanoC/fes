# Read-only completed-database audit of physical PR boundary registers.
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
set clock [get_clocks {pll_inst|iopll_0_outclk0}]
if {[get_collection_size $clock] != 1 || [get_clock_info -period $clock] != 10.0} {
    error "missing preserved 100 MHz PR clock"
}
set reset_head [get_registers {fes_top_reset_inst|sync_regs[0]}]
set reset_tail [get_registers {fes_top_reset_inst|sync_regs[1]}]
if {[get_collection_size $reset_head] != 1 || [get_collection_size $reset_tail] != 1} {
    error "configuration reset release must retain exactly two registers"
}
set boundary_groups [list \
    request_ingress [get_registers {slot_inst|core|request_q}] 1 \
    reset_ingress [get_registers {slot_inst|core|reset_q}] 1 \
    address_ingress [get_registers {slot_inst|core|address_q[*]}] 8 \
    valid_egress [get_registers {*fes_pr_control*|response_valid_q}] 1 \
    data_egress [get_registers {*fes_pr_control*|response_data_q[*]}] 32]
foreach {name nodes expected} $boundary_groups {
    if {[get_collection_size $nodes] != $expected} {error "$name physical boundary count differs from $expected"}
    foreach_in_collection node $nodes {puts "REGISTER $name [get_node_info -name $node]"}
}
set stream [open [file join $output boundary-paths.tsv] w]
puts $stream "corner\tgroup\tcheck\tfrom\tto\tslack"
set corners [get_available_operating_conditions]
if {[get_collection_size $corners] < 1} {error "missing published operating conditions"}
foreach_in_collection corner $corners {
    set_operating_conditions $corner
    update_timing_netlist
    set corner_name [get_operating_conditions_info -display_name $corner]
    foreach check {setup hold} {
        set paths [get_timing_paths -$check -from $reset_head -to $reset_tail -npaths 1]
        if {[get_collection_size $paths] != 1} {error "reset release lost paired $check coverage"}
        foreach_in_collection path $paths {
            set slack [get_path_info -slack $path]
            if {$slack < 0} {error "reset release negative $check slack"}
            puts $stream [join [list $corner_name reset_release $check [get_node_info -name [get_path_info -from $path]] [get_node_info -name [get_path_info -to $path]] $slack] "\t"]
        }
        foreach {name nodes expected} $boundary_groups {
            set paths [get_timing_paths -$check -to $nodes -npaths 4096 -nworst 1]
            set count [get_collection_size $paths]
            if {$count < $expected || $count >= 4096} {error "$name lost or truncated $check coverage"}
            foreach_in_collection path $paths {
                set slack [get_path_info -slack $path]
                if {$slack < 0} {error "$name negative $check slack"}
                puts $stream [join [list $corner_name $name $check [get_node_info -name [get_path_info -from $path]] [get_node_info -name [get_path_info -to $path]] $slack] "\t"]
            }
        }
    }
}
close $stream
delete_timing_netlist
project_close
puts "PASS: all published-corner reset and physical PR boundary setup/hold paths retained"
