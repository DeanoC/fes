# Append all-corner PCM and synchronizer coverage to the shared boundary audit.
load_package sta
set project [file normalize [lindex $quartus(args) 0]]
set revision [lindex $quartus(args) 1]
set output [file normalize [lindex $quartus(args) 2]]
cd $project
project_open -preserve_revision_order -revision $revision fes_link_proof
create_timing_netlist
read_sdc
update_timing_netlist
set held [get_registers -nowarn {fes_audio_inst|audio|held_sample[*]}]
set pcm [get_registers -nowarn {fes_audio_inst|audio|pcm[*]}]
if {[get_collection_size $held] < 8 || [get_collection_size $pcm] < 8} {
    error "standard PR PCM lost physical payload registers"
}
set groups [list pcm_payload $held $pcm]
foreach {label head second} {
    pcm_request request_meta request_sync
    pcm_acknowledge ack_meta ack_sync
    pcm_hold hold_meta hold_sync
} {
    set launch [get_registers "fes_audio_inst|audio|$head"]
    set capture [get_registers "fes_audio_inst|audio|$second"]
    if {[get_collection_size $launch] != 1 || [get_collection_size $capture] != 1} {
        error "$label lost its actual two-stage synchronizer"
    }
    lappend groups $label $launch $capture
}
proc pcm_path_record {stream corner group check path} {
    set from [get_path_info -from_clock $path]
    set to [get_path_info -to_clock $path]
    set from_clock [get_clock_info -name $from]
    set to_clock [get_clock_info -name $to]
    set from_inverted [get_path_info -from_clock_is_inverted $path]
    set to_inverted [get_path_info -to_clock_is_inverted $path]
    if {$from_inverted || $to_inverted} {error "$group unexpected clock edge"}
    if {$group eq "pcm_payload"} {
        if {$from_clock ne {pll_inst|iopll_0_outclk0} ||
            ![string match {fes_audio_inst|clock|*} $to_clock] ||
            [get_clock_info -period $from] != 10.0 ||
            [get_clock_info -period $to] < 81.0 || [get_clock_info -period $to] > 82.0} {
            error "$group unexpected system/audio clock relationship"
        }
    } elseif {$from_clock ne $to_clock ||
              ($group eq "pcm_request" && $to_clock ne {pll_inst|iopll_0_outclk0}) ||
              ($group ne "pcm_request" && ![string match {fes_audio_inst|clock|*} $to_clock])} {
        error "$group interstage path is not timed in its receiving domain"
    }
    set slack [get_path_info -slack $path]
    if {$slack < 0} {error "$group negative $check slack"}
    puts $stream [join [list $corner $group $check \
        [get_node_info -name [get_path_info -from $path]] \
        [get_node_info -name [get_path_info -to $path]] $slack \
        $from_clock $to_clock $from_inverted $to_inverted \
        [get_path_info -clock_relationship $path]] "\t"]
}
set stream [open [file join $output boundary-paths.tsv] a]
set corners [get_available_operating_conditions]
if {[get_collection_size $corners] < 1} {error "missing PCM operating conditions"}
foreach_in_collection corner $corners {
    set_operating_conditions $corner
    update_timing_netlist
    set corner_name [get_operating_conditions_info -display_name $corner]
    foreach check {setup hold} {
        foreach {name sources captures} $groups {
            set expected {}
            foreach_in_collection capture $captures {dict set expected [get_node_info -name $capture] 1}
            set covered {}
            # Query every physical source, covering all source/sink pairs and
            # replicas rather than only a group's single worst launch.
            foreach_in_collection source $sources {
                set paths [get_timing_paths -$check -from $source -to $captures -npaths 4096 -nworst 1]
                set count [get_collection_size $paths]
                if {$count == 0 || $count >= 4096} {error "$name lost or truncated $check source coverage"}
                foreach_in_collection path $paths {
                    set sink [get_node_info -name [get_path_info -to $path]]
                    if {![dict exists $expected $sink]} {error "$name timed an unexpected sink"}
                    dict set covered $sink 1
                    pcm_path_record $stream $corner_name $name $check $path
                }
            }
            if {[lsort [dict keys $covered]] ne [lsort [dict keys $expected]]} {
                error "$name missing a physical $check capture endpoint"
            }
        }
    }
}
close $stream
delete_timing_netlist
project_close
puts "PASS: paired all-corner PCM payload and interstage coverage retained"
