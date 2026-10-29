# Read-only completed-database audit of physical PR boundary registers.
load_package sta
if {[llength $quartus(args)] != 3} {error "expected project directory, revision, private output"}
set project [file normalize [lindex $quartus(args) 0]]
set revision [lindex $quartus(args) 1]
set output [file normalize [lindex $quartus(args) 2]]
file mkdir $output
cd $project
project_open -preserve_revision_order -revision $revision fes_link_proof
create_timing_netlist
read_sdc
update_timing_netlist
set clock [get_clocks {pll_inst|iopll_0_outclk0}]
if {[get_collection_size $clock] != 1 || [get_clock_info -period $clock] != 10.0} {
    error "missing preserved 100 MHz PR clock"
}
set emif_reference [get_clocks {soc_inst|subsys_hps|emif_hps|emif_io96b_hps|emif_0_lpddr4|emif_0_lpddr4_0_ref_clock}]
if {[get_collection_size $emif_reference] != 1 || [get_clock_info -period $emif_reference] != 6.0} {
    error "missing preserved generated 6 ns EMIF reference clock"
}
set reset_head [get_registers {fes_top_reset_inst|sync_regs[0]}]
set reset_tail [get_registers {fes_top_reset_inst|sync_regs[1]}]
if {[get_collection_size $reset_head] != 1 || [get_collection_size $reset_tail] != 1} {
    error "configuration reset release must retain exactly two registers"
}
set static {soc_inst|subsys_periph|fes_pr_control|fes_pr_control}
set address_names {}
for {set bit 0} {$bit < 8} {incr bit} {lappend address_names [format {slot_inst|core|address_q[%d]} $bit]}
set data_names {}
for {set bit 0} {$bit < 32} {incr bit} {lappend data_names [format {%s|response_data_q[%d]} $static $bit]}
set logical_groups [list \
    request_ingress {slot_inst|core|request_q} [list {slot_inst|core|request_q}] \
    reset_ingress {slot_inst|core|reset_q} [list {slot_inst|core|reset_q}] \
    address_ingress {slot_inst|core|address_q[*]} $address_names \
    valid_egress "$static|response_valid_q" [list "$static|response_valid_q"] \
    data_egress [format {%s|response_data_q[*]} $static] $data_names]
# Ingress bits38..43 carry peek_addr[13:8], explicitly zero for0x40xx.
# All other bits feed the diagnostic checksum and must retain endpoints.
set bus_ingress_names {}
for {set bit 0} {$bit < 46} {incr bit} {
    if {$bit >= 38 && $bit <= 43} {continue}
    lappend bus_ingress_names [format {slot_inst|core|bus_q[%d]} $bit]
}
foreach_in_collection node [get_registers {slot_inst|core|bus_q[*]}] {
    set actual [get_node_info -name $node]
    regsub {~DUPLICATE[0-9]*$} $actual {} logical
    if {![regexp {^slot_inst\|core\|bus_q\[([0-9]+)\]$} $logical -> bit] || $bit > 45} {
        error "unexpected persona bus endpoint $actual"
    }
    if {[lsearch -exact $bus_ingress_names $logical] < 0} {lappend bus_ingress_names $logical}
}
lappend logical_groups bus_ingress {slot_inst|core|bus_q[*]} $bus_ingress_names
# The RAM persona must retain all 16 read/peek data bits. Flags 16..18
# are explicitly wired zero; presence19 is the persona's constant0/1.
# Empty/base data0..15 are explicitly zero. Only these source-defined
# constants may fold; audit any physical registers they still produce.
set required_bus_egress {}
if {$revision eq "persona_b"} {
    for {set bit 0} {$bit < 16} {incr bit} {lappend required_bus_egress [format {zx81_static|response_q[%d]} $bit]}
    lappend required_bus_egress {zx81_static|response_q[19]}
}
set bus_egress_names $required_bus_egress
foreach_in_collection node [get_registers {zx81_static|response_q[*]}] {
    set actual [get_node_info -name $node]
    regsub {~DUPLICATE[0-9]*$} $actual {} logical
    if {![regexp {^zx81_static\|response_q\[([0-9]+)\]$} $logical -> bit] || $bit > 19} {
        error "unexpected static bus endpoint $actual"
    }
    if {[lsearch -exact $bus_egress_names $logical] < 0} {lappend bus_egress_names $logical}
}
if {[llength $bus_egress_names]} {lappend logical_groups bus_egress {zx81_static|response_q[*]} $bus_egress_names}
set boundary_groups {}
foreach {name pattern canonical} $logical_groups {
    set nodes [get_registers $pattern]
    set physical {}
    foreach_in_collection node $nodes {
        set actual [get_node_info -name $node]
        # Quartus may retain the protected logical FF and create physical
        # replicas. Audit every replica instead of dropping it from timing.
        regsub {~DUPLICATE[0-9]*$} $actual {} logical
        if {[lsearch -exact $canonical $logical] < 0 || [lsearch -exact $physical $actual] >= 0} {
            error "$name unexpected or duplicated physical boundary identity $actual"
        }
        lappend physical $actual
        puts "REGISTER $name $actual"
    }
    foreach required $canonical {
        if {[lsearch -exact $physical $required] < 0} {error "$name missing protected logical register $required"}
    }
    puts "GROUP $name logical=[llength $canonical] physical=[llength $physical]"
    lappend boundary_groups $name $nodes $physical
}
proc path_record {stream corner group check path} {
    set from_clock [get_clock_info -name [get_path_info -from_clock $path]]
    set to_clock [get_clock_info -name [get_path_info -to_clock $path]]
    set from_inverted [get_path_info -from_clock_is_inverted $path]
    set to_inverted [get_path_info -to_clock_is_inverted $path]
    if {$from_clock ne {pll_inst|iopll_0_outclk0} || $to_clock ne $from_clock || $from_inverted || $to_inverted} {
        error "$group unexpected launch/capture clock or edge"
    }
    set slack [get_path_info -slack $path]
    if {$slack < 0} {error "$group negative $check slack"}
    puts $stream [join [list $corner $group $check [get_node_info -name [get_path_info -from $path]] \
        [get_node_info -name [get_path_info -to $path]] $slack $from_clock $to_clock \
        $from_inverted $to_inverted [get_path_info -clock_relationship $path]] "\t"]
}
set stream [open [file join $output boundary-paths.tsv] w]
puts $stream "corner\tgroup\tcheck\tfrom\tto\tslack\tlaunch_clock\tcapture_clock\tlaunch_inverted\tcapture_inverted\tclock_relationship"
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
            path_record $stream $corner_name reset_release $check $path
        }
        foreach {name nodes physical} $boundary_groups {
            set paths [get_timing_paths -$check -to $nodes -npaths 4096 -nworst 1]
            set count [get_collection_size $paths]
            if {$count < [llength $physical] || $count >= 4096} {error "$name lost or truncated $check coverage"}
            set covered {}
            foreach_in_collection path $paths {
                set endpoint [get_node_info -name [get_path_info -to $path]]
                if {[lsearch -exact $physical $endpoint] < 0} {error "$name timed an unexpected endpoint $endpoint"}
                dict set covered $endpoint 1
                path_record $stream $corner_name $name $check $path
            }
            if {[lsort [dict keys $covered]] ne [lsort $physical]} {error "$name missing a physical endpoint from $check coverage"}
        }
    }
}
close $stream
delete_timing_netlist
project_close
puts "PASS: all published-corner reset and physical PR boundary setup/hold paths retained"
