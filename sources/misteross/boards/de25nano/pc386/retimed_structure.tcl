# Structural checks sourced by the registered pc386 TimeQuest trace (both models).
proc put_row {stream fields} {
    set escaped {}
    foreach field $fields {
        lappend escaped [string map [list "\t" {\t} "\n" {\n} "\r" {\r}] $field]
    }
    puts $stream [join $escaped "\t"]
}

proc exact_register {name {expected_id {}}} {
    # Node IDs returned by get_fanins/fanouts differ from register IDs. Check
    # a singleton by its full name in TimeQuest's register collection.
    set escaped [string map [list {[} {\[} {]} {\]}] $name]
    foreach spelling [lsort -unique [list $escaped $name]] {
        foreach options {{-nowarn -no_duplicates} {-nowarn}} {
            set query [get_registers {*}$options [list $spelling]]
            if {[get_collection_size $query] != 1} {continue}
            set selected {}
            foreach_in_collection reg $query {
                if {[get_register_info $reg -name] eq $name && ($expected_id eq "" || $reg eq $expected_id)} {
                    set selected $query
                }
            }
            if {$selected ne ""} {return $selected}
        }
    }
    return {}
}

proc clock_names {collection} {
    set result {}
    foreach_in_collection clock [get_clocks -nowarn -of_objects $collection] {
        lappend result [get_clock_info -name $clock]
    }
    return [lsort $result]
}

proc keeper_kind {item expected_clock cache_name} {
    upvar 1 $cache_name cache
    set name [get_node_info $item -name]
    set type [get_node_info $item -type]
    set key [list $type $name]
    if {[dict exists $cache $key]} {return [dict get $cache $key]}
    set clocks {}
    set category non_register
    if {$type eq "port"} {
        set category port
    } elseif {$type eq "reg"} {
        set exact [exact_register $name]
        if {$exact eq ""} {
            set category unresolved
        } else {
            set clocks [clock_names $exact]
            if {[llength $clocks] == 0} {
                set category unclocked
            } elseif {$clocks eq [list $expected_clock]} {
                set category machine
            } else {
                set category foreign
            }
        }
    }
    set result [list $category $type $name [join $clocks ,]]
    dict set cache $key $result
    return $result
}

proc outgoing_async_info {reg target} {
    set raw [get_node_info $reg -fanout_asynch_edges]
    set details {}
    set internal 0
    set other 0
    set unknown 0
    foreach edge $raw {
        if {[catch {
            set type [get_edge_info $edge -type]
            set disabled [get_edge_info $edge -is_disabled]
            set src [get_edge_info $edge -src]
            set dst [get_edge_info $edge -dst]
            set src_type [get_node_info $src -type]
            set src_name [get_node_info $src -name]
            set dst_type [get_node_info $dst -type]
            set dst_name [get_node_info $dst -name]
            lappend details [list $edge $type $disabled $src $src_type $src_name $dst $dst_type $dst_name]
            if {$type ni {synchronous asynchronous clock combinational} || ![string is boolean -strict $disabled]} {
                incr unknown
            } elseif {$type eq "asynchronous" && !$disabled && $src_type eq "reg" && $src_name eq $target
                      && $dst_type eq "pin" && $dst_name eq "${target}|q"} {
                # TimeQuest represents the register's own Q as an intrinsic arc.
                incr internal
            } else {
                incr other
            }
        } failure]} {
            lappend details [list $edge query_error $failure]
            incr unknown
        }
    }
    return [list $raw $details $internal $other $unknown]
}

proc named_peer {path direction} {
    set peer_end [expr {$direction eq "from" ? "to" : "from"}]
    set object [get_path_info -$peer_end $path]
    set clock [get_path_info -${peer_end}_clock $path]
    set name [expr {$object eq "" ? "" : [get_node_info $object -name]}]
    set clock_name [expr {$clock eq "" ? "" : [get_clock_info -name $clock]}]
    return [list $name $clock_name]
}

proc structure_columns {} {
    return {corner node direction exact_matches owner_clocks sync_stage is_latch async_inputs async_outputs clock_outputs
        keepers machine foreign unclocked port non_register unresolved foreign_examples unclocked_examples port_examples
        non_register_examples unresolved_examples setup_inter_clock setup_inter_clock_example setup_false_path
        setup_false_path_example hold_inter_clock hold_inter_clock_example hold_false_path hold_false_path_example
        recovery_from recovery_from_false_path removal_from removal_from_false_path
        async_output_ids async_output_details async_output_own_q async_output_other async_output_unknown status}
}

proc open_structure_trace {path} {
    set output [open $path w]
    put_row $output [structure_columns]
    return $output
}

proc trace_structure_corner {output label targets matched expected_clock} {
    set blank_query_fields [lrepeat [expr {[llength [structure_columns]] - 5}] {}]
    set errors 0
    set cache {}
    foreach target $targets {
        set matches {}
        if {[dict exists $matched $target]} {set matches [dict get $matched $target]}
        set exact [llength $matches]
        set selected {}
        set reg [lindex $matches 0]
        if {$exact == 1} {set selected [exact_register $target $reg]}
        foreach direction {from to} {
            if {$selected eq ""} {
                put_row $output [concat [list $label $target $direction $exact] $blank_query_fields missing-or-ambiguous]
                incr errors
                continue
            }
            if {[catch {
                set clocks [clock_names $selected]
                set sync_stage [get_register_info $reg -is_synchronizer]
                set is_latch [get_register_info $reg -is_latch]
                set async_in [llength [get_node_info $reg -asynch_edges]]
                lassign [outgoing_async_info $reg $target] async_ids async_details async_own_q async_other async_unknown
                set async_out [llength $async_ids]
                set clock_out [llength [get_node_info $reg -fanout_clock_edges]]

                # Inspect synchronous and asynchronous data connectivity. The
                # target's own clock input is covered separately by its owner.
                set query [expr {$direction eq "from" ? "get_fanouts" : "get_fanins"}]
                set keepers [$query -synch -asynch -stop_at_clocks $selected]
                set total [get_collection_size $keepers]
                set counts [dict create machine 0 foreign 0 unclocked 0 port 0 non_register 0 unresolved 0]
                set examples {}
                foreach_in_collection neighbor $keepers {
                    lassign [keeper_kind $neighbor $expected_clock cache] category type name peer_clocks
                    dict incr counts $category
                    if {$category ne "machine" && [dict get $counts $category] <= 3} {
                        dict lappend examples $category [list $type $name $peer_clocks]
                    }
                }
                set exclusion {}
                set excluded_paths 0
                foreach analysis {setup hold} {
                    foreach filter {inter_clock false_path} {
                        set paths [get_timing_paths -$analysis -$direction $selected -$filter -npaths 1]
                        set count [get_collection_size $paths]
                        set example {}
                        foreach_in_collection path $paths {set example [named_peer $path $direction]}
                        lappend exclusion $count $example
                        incr excluded_paths $count
                    }
                }
                set reset_paths [lrepeat 4 {}]
                if {$direction eq "from"} {
                    set reset_paths {}
                    # Query visible paths on the launching register as well as
                    # checking its structural async fanout above.
                    foreach analysis {recovery removal} {
                        foreach filter {{} {-false_path}} {
                            set paths [get_timing_paths -$analysis -from $selected {*}$filter -npaths 1 -nworst 1]
                            set count [get_collection_size $paths]
                            lappend reset_paths $count
                            incr excluded_paths $count
                        }
                    }
                }
                set quantities [list $label $target $direction $exact [join $clocks ,] $sync_stage \
                                     $is_latch $async_in $async_out $clock_out $total]
                foreach category {machine foreign unclocked port non_register unresolved} {
                    lappend quantities [dict get $counts $category]
                }
                foreach category {foreign unclocked port non_register unresolved} {
                    lappend quantities [expr {[dict exists $examples $category] ? [dict get $examples $category] : ""}]
                }
                set clean [expr {$clocks eq [list $expected_clock] && $sync_stage == 0 && $is_latch == 0 \
                    && $async_in == 0 && $async_own_q <= 1 && $async_other == 0 && $async_unknown == 0 && $clock_out == 0 \
                    && $total > 0 && [dict get $counts machine] == $total \
                    && $excluded_paths == 0}]
                put_row $output [concat $quantities $exclusion $reset_paths \
                    [list $async_ids $async_details $async_own_q $async_other $async_unknown [expr {$clean ? "clear" : "hazard"}]]]
            } failure]} {
                put_row $output [concat [list $label $target $direction $exact] $blank_query_fields [list "query-error: $failure"]]
                incr errors
            }
        }
        flush $output
    }
    return $errors
}
