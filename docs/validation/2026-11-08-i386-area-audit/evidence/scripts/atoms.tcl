load_package atoms
load_package advanced_device
project_open top -revision top
read_atom_netlist -type cmp
set f [open $::env(ATOM_OUT) w]
foreach_in_collection a [get_atom_nodes] {
  set t [get_atom_node_info -node $a -key TYPE]
  set n [get_atom_node_info -node $a -key NAME]
  set l ""
  catch {set l [get_atom_node_info -node $a -key LOCATION]}
  puts $f "$t\t$l\t$n"
}
close $f
project_close
