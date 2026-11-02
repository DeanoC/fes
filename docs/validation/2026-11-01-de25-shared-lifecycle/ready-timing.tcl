# Read-only audit of the completed diagnostic's two readiness synchronizers.
load_package sta
if {[llength $quartus(args)] != 2} {error "expected project directory and private output"}
set project [file normalize [lindex $quartus(args) 0]]
set output [file normalize [lindex $quartus(args) 1]]
file mkdir $output
cd $project
project_open -preserve_revision_order -revision golden_top golden_top
create_timing_netlist
read_sdc
update_timing_netlist
set stream [open [file join $output readiness-paths.tsv] w]
puts $stream "corner\tgroup\tcheck\tfrom\tto\tslack"
set rows 0
set corners [get_available_operating_conditions]
if {[get_collection_size $corners] < 1} {error "missing published operating conditions"}
foreach_in_collection corner $corners {
 set_operating_conditions $corner
 update_timing_netlist
 set name [get_operating_conditions_info -display_name $corner]
 foreach {group source first second} {
  hdmi {fes_video_inst|hdmi_config|READY} {fes_video_inst|hdmi_ready_sync[0]} {fes_video_inst|hdmi_ready_sync[1]}
  control {fes_video_inst|video_ready_source} {soc_inst|subsys_periph|fes_computer|fes_computer|video_ready_sync[0]} {soc_inst|subsys_periph|fes_computer|fes_computer|video_ready_sync[1]}
 } {
  set launch [get_registers $source]
  set stage0 [get_registers $first]
  set stage1 [get_registers $second]
  foreach nodes [list $launch $stage0 $stage1] {
   if {[get_collection_size $nodes] != 1} {error "$group physical register cardinality changed"}
  }
  foreach check {setup hold} {
   set excluded [get_timing_paths -$check -from $launch -to $stage0 -npaths 1]
   if {[get_collection_size $excluded] != 0} {error "$group asynchronous first-stage ingress is unexpectedly timed"}
   set paths [get_timing_paths -$check -from $stage0 -to $stage1 -npaths 1]
   if {[get_collection_size $paths] != 1} {error "$group stage-to-stage $check coverage lost"}
   foreach_in_collection path $paths {
    set slack [get_path_info -slack $path]
    if {$slack < 0} {error "$group $check negative slack $slack"}
    puts $stream "$name\t$group\t$check\t[get_node_info -name [get_path_info -from $path]]\t[get_node_info -name [get_path_info -to $path]]\t$slack"
    incr rows
   }
  }
 }
}
close $stream
if {$rows != 4 * [get_collection_size $corners]} {error "incomplete readiness coverage"}
delete_timing_netlist
project_close
puts "PASS readiness audit: $rows setup/hold paths, all published corners; only source-to-first-stage ingress excluded"
