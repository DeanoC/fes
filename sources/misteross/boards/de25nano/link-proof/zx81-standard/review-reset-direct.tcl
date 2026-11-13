load_package sta
cd [lindex $quartus(args) 0]
set output [lindex $quartus(args) 1]
project_open fes_link_proof -revision [lindex $quartus(args) 2]
create_timing_netlist
read_sdc
update_timing_netlist
set source [get_registers -nowarn {soc_inst|subsys_periph|rst_controller|alt_rst_sync_uq1|altera_reset_synchronizer_int_chain_out}]
if {[get_collection_size $source] != 1} {error "reset source no longer unique"}
set reset_pins [get_pins -compatibility_mode -nocase {*|clrn *|prn *|aclr *|aset}]
set ena_pins [get_pins -compatibility_mode -nocase {*|ena}]
puts "PIN-INVENTORY reset [get_collection_size $reset_pins] ena [get_collection_size $ena_pins]"
set stream [open [file join $output direct-inventory.tsv] w]
puts $stream "kind\tname"
foreach kind {direct_async all_async direct_ena all_ena} {
 if {$kind eq "direct_async"} {set nodes [get_fanouts -no_logic -through $reset_pins $source]} elseif {$kind eq "all_async"} {set nodes [get_fanouts -through $reset_pins $source]} elseif {$kind eq "all_ena"} {set nodes [get_fanouts -through $ena_pins $source]} else {
  set ena [get_pins -compatibility_mode -nocase {*|ena}]
  set nodes [get_fanouts -no_logic -through $ena $source]
 }
 foreach_in_collection node $nodes {puts $stream "$kind\t[get_node_info -name $node]"}
 puts "DIRECT-INVENTORY $kind [get_collection_size $nodes]"
 if {[get_collection_size $nodes] == 0} {error "empty reset topology inventory"}
}
close $stream
delete_timing_netlist
project_close
