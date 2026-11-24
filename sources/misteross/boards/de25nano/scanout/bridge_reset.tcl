# Read-only: the window bridge's FIFO reset synchronizers, their clear sources,
# their loads, and every load's recovery and removal at every corner.
load_package sta
cd [lindex $quartus(args) 0]
set output [lindex $quartus(args) 1]
project_open golden_top
create_timing_netlist
read_sdc
update_timing_netlist
set b {soc_inst|subsys_periph|fes_win_cdc|fes_win_cdc}
set instances {}
foreach fifo {cmd rsp} {
 foreach side {write read} {
  lappend instances "$b|${fifo}_fifo|my_altera_avalon_dc_fifo_${fifo}_fifo|${side}_reset_sync"
 }
}
set chains [open [file join $output chains.tsv] w]
set loads [open [file join $output loads.tsv] w]
foreach inst $instances {
 set regs [get_registers "$inst|*"]
 if {[get_collection_size $regs] == 0} {error "reset synchronizer missing: $inst"}
 foreach_in_collection reg $regs {
  set name [get_node_info -name $reg]
  set clear [get_pins -compatibility_mode -nocase -nowarn [list "$name|clrn" "$name|aclr"]]
  if {[get_collection_size $clear] != 1} {error "reset synchronizer register has no single clear: $name"}
  foreach_in_collection source [get_fanins $clear] {puts $chains [join [list $inst $name [get_node_info -name $source]] "\t"]}
 }
 set out [get_registers "$inst|altera_reset_synchronizer_int_chain_out"]
 if {[get_collection_size $out] != 1} {error "reset synchronizer output missing: $inst"}
 foreach_in_collection node [get_fanouts $out] {puts $loads [join [list $inst [get_node_info -name $node]] "\t"]}
}
close $chains
close $loads
set stream [open [file join $output paths.tsv] w]
set corners [get_available_operating_conditions]
if {[get_collection_size $corners] != 4} {error "operating corner coverage differs"}
foreach_in_collection corner $corners {
 set_operating_conditions $corner
 update_timing_netlist
 set model [get_operating_conditions_info -display_name $corner]
 foreach inst $instances {
  set out [get_registers "$inst|altera_reset_synchronizer_int_chain_out"]
  foreach check {recovery removal} {
   set paths [get_timing_paths -$check -from $out -npaths 1024 -nworst 1]
   if {[get_collection_size $paths] >= 1024} {error "truncated reset timing coverage"}
   foreach_in_collection path $paths {
    puts $stream [join [list $model $check $inst [get_node_info -name [get_path_info -to $path]] \
     [get_path_info -slack $path] [get_clock_info -name [get_path_info -from_clock $path]] \
     [get_clock_info -name [get_path_info -to_clock $path]]] "\t"]
   }
  }
 }
}
close $stream
delete_timing_netlist
project_close
