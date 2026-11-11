# Read-only fitted fanout and complete local output-reset timing.
load_package sta
cd [lindex $quartus(args) 0]
set output [lindex $quartus(args) 1]
project_open golden_top
create_timing_netlist
read_sdc
update_timing_netlist
set stream [open [file join $output reset-paths.tsv] w]
set inventory [open [file join $output reset-inventory.tsv] w]
foreach stage {0 1} {
 set source [get_registers [format {fes_video_inst|output_release[%d]} $stage]]
 if {[get_collection_size $source] != 1} {error "reset stage no longer unique"}
 foreach_in_collection node [get_fanouts $source] {puts $inventory [join [list $stage [get_node_info -name $node]] "\t"]}
}
close $inventory
set source [get_registers {fes_video_inst|output_release[1]}]
set corners [get_available_operating_conditions]
if {[get_collection_size $corners] != 4} {error "operating corner coverage differs"}
foreach_in_collection corner $corners {
 set_operating_conditions $corner
 update_timing_netlist
 set model [get_operating_conditions_info -display_name $corner]
 foreach check {recovery removal} {
  set paths [get_timing_paths -$check -from $source -npaths 256 -nworst 1]
  puts "RESET-COVERAGE $model $check [get_collection_size $paths]"
  if {[get_collection_size $paths] >= 256} {error "truncated reset timing coverage"}
  foreach_in_collection path $paths {
   puts $stream [join [list $model $check [get_node_info -name [get_path_info -from $path]] \
    [get_node_info -name [get_path_info -to $path]] [get_path_info -slack $path] \
    [get_clock_info -name [get_path_info -from_clock $path]] \
    [get_clock_info -name [get_path_info -to_clock $path]]] "\t"]
  }
 }
}
close $stream
delete_timing_netlist
project_close
