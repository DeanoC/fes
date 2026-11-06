# Read-only reset/enable timing coverage on owned retained fitted copies.
load_package sta
cd [lindex $quartus(args) 0]
set output [lindex $quartus(args) 1]
project_open fes_link_proof -revision [lindex $quartus(args) 2]
create_timing_netlist
read_sdc
update_timing_netlist
set source_name {soc_inst|subsys_periph|rst_controller|alt_rst_sync_uq1|altera_reset_synchronizer_int_chain_out}
set source [get_registers -nowarn $source_name]
if {[get_collection_size $source] != 1} {error "reset source no longer unique"}
# Collect every actual direct enable load of this reset net, including
# physical duplicates. The independent pin inventory must match the timed set.
set ena_pins [get_pins -compatibility_mode -nocase {*|ena}]
set epochs [get_fanouts -no_logic -through $ena_pins $source]
if {[get_collection_size $epochs] < 1 || [get_collection_size $epochs] >= 4096} {error "missing or truncated reset-enable coverage"}
set stream [open [file join $output reset-paths.tsv] w]
puts $stream "corner\tcheck\tfrom\tto\tslack_ns\tfrom_clock\tto_clock"
set inventory [open [file join $output reset-inventory.tsv] w]
puts $inventory "kind\tname"
foreach_in_collection node [get_fanouts $source] {puts $inventory "source_fanout\t[get_node_info -name $node]"}
foreach_in_collection node $epochs {puts $inventory "epoch\t[get_node_info -name $node]"}
close $inventory
set corners [get_available_operating_conditions]
if {[get_collection_size $corners] != 4} {error "operating corner coverage differs"}
foreach_in_collection corner $corners {
 set_operating_conditions $corner
 update_timing_netlist
 set name [get_operating_conditions_info -display_name $corner]
 foreach check {setup hold recovery removal} {
  set targets {}
  if {$check in {setup hold}} {
   foreach_in_collection node $epochs {
    set regname [get_node_info -name $node]
    set ena [get_pins -compatibility_mode -nocase "${regname}|ena"]
    if {[get_collection_size $ena] != 1} {error "missing epoch ENA pin"}
    lappend targets $ena
   }
  } else {set targets [list [get_fanouts $source]]}
  set seen [dict create]
  foreach target $targets {
   set paths [get_timing_paths -$check -from $source -to $target -npaths 4096 -nworst 1]
   if {[get_collection_size $paths] >= 4096} {error "truncated reset timing coverage"}
   foreach_in_collection path $paths {
    set sink [get_node_info -name [get_path_info -to $path]]
    if {[dict exists $seen $sink]} {error "duplicate reset endpoint"}
    dict set seen $sink 1
    puts $stream [join [list $name $check \
     [get_node_info -name [get_path_info -from $path]] $sink [get_path_info -slack $path] \
     [get_clock_info -name [get_path_info -from_clock $path]] \
     [get_clock_info -name [get_path_info -to_clock $path]]] "\t"]
   }
  }
  puts "RESET-COVERAGE $name $check [dict size $seen]"
  report_timing -$check -from $source -npaths 4096 -nworst 1 -file [file join $output "${corner}_${check}.rpt"]
 }
}
close $stream
delete_timing_netlist
project_close
