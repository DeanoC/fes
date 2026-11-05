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
set hold_canonical {soc_inst|subsys_periph|fes_scanout|fes_scanout|core|gp_mailbox|exec_reset fes_video_inst|fault}
set hold_causes [get_registers $hold_canonical]
set hold_source [get_registers {standard_pr_audio_hold}]
if {[get_collection_size $hold_causes] < 2 || [get_collection_size $hold_source] != 1} {
    error "standard PR PCM lost its registered system-domain mute source"
}
foreach_in_collection cause $hold_causes {
    set physical [get_node_info -name $cause]
    regsub {~DUPLICATE[0-9]*$} $physical {} logical
    if {$logical ni $hold_canonical} {error "unexpected PCM mute source $physical"}
}
lappend groups pcm_hold_source $hold_causes $hold_source
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
    set fanout [get_fanouts $launch]
    set fanin [get_fanins $capture -synch]
    if {[get_collection_size $fanout] != 1 || [get_collection_size $fanin] != 1 ||
        [get_node_info -name $fanout] ne "fes_audio_inst|audio|$second" ||
        [get_node_info -name $fanin] ne "fes_audio_inst|audio|$head"} {
        error "$label synchronizer has an unexpected intermediate fanin or fanout"
    }
    lappend groups $label $launch $capture
}
proc pcm_mtbf_controls {text} {
    set chains {}
    set current ""
    foreach line [split $text "\n"] {
        if {[regexp {^Synchronizer Chain #[0-9]+:} $line]} {
            if {$current ne ""} {lappend chains $current}
            set current $line
        } elseif {$current ne ""} {append current "\n" $line}
    }
    if {$current ne ""} {lappend chains $current}
    set controls {}
    foreach chain $chains {
        set fields {}
        foreach line [split $chain "\n"] {
            if {![string match {;*} $line]} {continue}
            set columns [split $line {;}]
            if {[llength $columns] < 4} {continue}
            set key [string trim [lindex $columns 1]]
            set value [string trim [lindex $columns 2]]
            if {$key ni {{Synchronization Node} {Method of Synchronizer Identification}
                {Included in Design MTBF} {Number of Synchronization Registers in Chain}
                {Worst-Case MTBF (years)}
                {Data Toggle Rate Used in MTBF Calculation - Fd (millions of transitions / sec)}}} {continue}
            if {[dict exists $fields $key] && [dict get $fields $key] ne $value} {
                error "inconsistent PCM metastability report field $key"
            }
            dict set fields $key $value
        }
        if {![dict exists $fields {Synchronization Node}]} {continue}
        set node [dict get $fields {Synchronization Node}]
        if {$node ni {fes_audio_inst|audio|request_meta fes_audio_inst|audio|ack_meta fes_audio_inst|audio|hold_meta}} {continue}
        if {[dict exists $controls $node]} {error "duplicate PCM synchronizer report $node"}
        foreach {field expected} {
            {Method of Synchronizer Identification} {Forced If Asynchronous}
            {Included in Design MTBF} Yes
            {Number of Synchronization Registers in Chain} 2
            {Worst-Case MTBF (years)} {Greater than 1 Billion}
        } {
            if {![dict exists $fields $field] || [dict get $fields $field] ne $expected} {
                error "$node has missing or unqualified PCM metastability field $field"
            }
        }
        set fd {Data Toggle Rate Used in MTBF Calculation - Fd (millions of transitions / sec)}
        if {![dict exists $fields $fd] ||
            ![string is double -strict [dict get $fields $fd]] ||
            [dict get $fields $fd] != 100.0} {
            error "$node did not use the conservative 100 MHz PCM control toggle bound"
        }
        dict set controls $node $fields
    }
    if {[dict size $controls] != 3} {error "missing PCM synchronizer MTBF coverage"}
    return $controls
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
              ($group in {pcm_request pcm_hold_source} && $to_clock ne {pll_inst|iopll_0_outclk0}) ||
              ($group ni {pcm_request pcm_hold_source} && ![string match {fes_audio_inst|clock|*} $to_clock])} {
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
    set mtbf_file [file join $output "${corner}_pcm-metastability.rpt"]
    report_metastability -nchains 1000 -file $mtbf_file
    set mtbf_stream [open $mtbf_file r]
    set mtbf_text [read $mtbf_stream]
    close $mtbf_stream
    pcm_mtbf_controls $mtbf_text
    foreach check {setup hold} {
        foreach {name sources captures} $groups {
            set expected {}
            foreach_in_collection capture $captures {dict set expected [get_node_info -name $capture] 1}
            set covered {}
            set causes_covered {}
            # Query every physical source, covering all source/sink pairs and
            # replicas rather than only a group's single worst launch.
            foreach_in_collection source $sources {
                set paths [get_timing_paths -$check -from $source -to $captures -npaths 4096 -nworst 1]
                set count [get_collection_size $paths]
                # Physical replicas of either cause can drive other fanouts.
                # Audit every replica that reaches this register, and require
                # both logical causes to reach it in every corner/check.
                if {$name eq "pcm_hold_source" && $count == 0} {continue}
                if {$count == 0 || $count >= 4096} {error "$name lost or truncated $check source coverage"}
                foreach_in_collection path $paths {
                    if {$name eq "pcm_hold_source"} {
                        set physical [get_node_info -name [get_path_info -from $path]]
                        regsub {~DUPLICATE[0-9]*$} $physical {} logical
                        if {$logical ni $hold_canonical} {error "unexpected timed PCM mute source $physical"}
                        dict set causes_covered $logical 1
                    }
                    set sink [get_node_info -name [get_path_info -to $path]]
                    if {![dict exists $expected $sink]} {error "$name timed an unexpected sink"}
                    dict set covered $sink 1
                    pcm_path_record $stream $corner_name $name $check $path
                }
            }
            if {[lsort [dict keys $covered]] ne [lsort [dict keys $expected]]} {
                error "$name missing a physical $check capture endpoint"
            }
            if {$name eq "pcm_hold_source" && [lsort [dict keys $causes_covered]] ne [lsort $hold_canonical]} {
                error "$name missing a logical $check mute cause"
            }
        }
    }
}
close $stream
delete_timing_netlist
project_close
puts "PASS: paired all-corner PCM payload and interstage coverage retained"
