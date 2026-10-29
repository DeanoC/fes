# Read-only fitted connectivity audit for source-declared constant request bits.
load_package atoms
if {[llength $quartus(args)] != 3} {error "expected project, revision, private output"}
set project [file normalize [lindex $quartus(args) 0]]
set revision [lindex $quartus(args) 1]
set output [file normalize [lindex $quartus(args) 2]]
file mkdir $output
cd $project
project_open -preserve_revision_order -revision $revision fes_link_proof
read_atom_netlist -type cmp
set stream [open [file join $output constant-cones.tsv] w]
puts $stream "register\tff_d_inverted\tipport\tipport_mask_raw\tipport_input\tipport_input_inverted\tipport_output_inverted\tleaf\tleaf_mask\tfitted_claim\trtl_contract"
set bits {}
foreach_in_collection node [get_atom_nodes -type FF] {
    set name [get_atom_node_info -node $node -key name]
    if {![regexp {^slot_inst\|core\|bus_q\[(3[89]|4[0-3])\](~DUPLICATE[0-9]*)?$} $name -> bit]} {continue}
    dict set bits $bit 1
    set data_ports {}
    foreach port [get_atom_iports -node $node] {
        if {[get_atom_port_info -node $node -port_id $port -type iport -key type] eq "D"} {lappend data_ports $port}
    }
    if {[llength $data_ports] != 1} {error "$name lacks exactly one physical D input"}
    set data [lindex $data_ports 0]
    set d_inverted [get_atom_port_info -node $node -port_id $data -type iport -key is_inverted]
    set pair [get_atom_port_info -node $node -port_id $data -type iport -key fanin]
    if {[llength $pair] != 2} {error "$name has a disconnected D input"}
    lassign $pair boundary boundary_port
    set boundary_name [get_atom_node_info -node $boundary -key name]
    set expected [format {slot_inst|bus_request[%d]~IPORT} $bit]
    if {$boundary_name ne $expected || [get_atom_node_info -node $boundary -key type] ne "LCELL_COMB"} {
        error "$name unexpected physical IPORT driver $boundary_name"
    }
    set mask [get_atom_node_info -node $boundary -key LUTMASK_SUM]
    set ports [get_atom_iports -node $boundary]
    if {[llength $ports] != 1 || $mask eq ""} {error "$name IPORT is not a single driven constant cone"}
    set port [lindex $ports 0]
    set type [get_atom_port_info -node $boundary -port_id $port -type iport -key type]
    if {![regexp {^DATA[A-F]$} $type]} {error "$name unexpected IPORT input $type"}
    set pair [get_atom_port_info -node $boundary -port_id $port -type iport -key fanin]
    if {[llength $pair] != 2} {error "$name disconnected IPORT source"}
    lassign $pair leaf leaf_port
    set leaf_name [get_atom_node_info -node $leaf -key name]
    set leaf_mask [get_atom_node_info -node $leaf -key LUTMASK_SUM]
    if {$leaf_name ne "~GND" || [get_atom_node_info -node $leaf -key type] ne "LCELL_COMB" ||
            $leaf_mask ne "0" || [llength [get_atom_iports -node $leaf]] != 0} {
        error "$name unexpected or variable constant-cone leaf"
    }
    set input_inverted [get_atom_port_info -node $boundary -port_id $port -type iport -key is_inverted]
    set output_inverted [get_atom_port_info -node $boundary -port_id $boundary_port -type oport -key is_inverted]
    foreach flag [list $d_inverted $input_inverted $output_inverted] {
        if {$flag ni {0 1}} {error "$name missing physical inversion property"}
    }
    puts $stream [join [list $name $d_inverted $boundary_name $mask $type $input_inverted $output_inverted \
        $leaf_name $leaf_mask driven-constant-cone zero-from-captured-RTL-not-independent-LUT-evaluation] "\t"]
}
if {[lsort -integer [dict keys $bits]] ne {38 39 40 41 42 43}} {error "constant request registers missing"}
close $stream
project_close
puts "PASS: six source-declared constant inputs have retained FFs and closed, driven fitted cones"
