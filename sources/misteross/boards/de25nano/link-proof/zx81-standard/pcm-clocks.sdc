# Live standard-core PCM in the static PR shell. Only toggle/hold ingress D
# pins are asynchronous. Interstage paths and the coherent payload stay timed.
foreach {label sources expected head second} {
    request {fes_audio_inst|audio|request} 1 request_meta request_sync
    acknowledge {fes_audio_inst|audio|acknowledge} 1 ack_meta ack_sync
    hold {standard_pr_audio_hold} 1 hold_meta hold_sync
} {
    set launches [get_registers $sources]
    set first_d [get_pins -compatibility_mode -nocase "fes_audio_inst|audio|${head}|d"]
    set receiving [get_registers "fes_audio_inst|audio|${second}"]
    if {[get_collection_size $launches] != $expected ||
        [get_collection_size $first_d] != 1 || [get_collection_size $receiving] != 1} {
        error "DE25 standard PR PCM $label synchronizer endpoints changed"
    }
    set_false_path -from $launches -to $first_d
}

# The standard core supplies the same mono value to both channels: bit15 and
# bits6:0 are zero; bits14:7 carry the expansion's eight-bit sample. Quartus may
# merge the stereo duplicates or retain either logical bank. Audit every real
# register (including physical replicas), requiring every semantic bit7..14.
foreach kind {held_sample pcm} {
    set nodes [get_registers -nowarn [format {fes_audio_inst|audio|%s[*]} $kind]]
    set semantic {}
    foreach_in_collection node $nodes {
        set name [get_node_info -name $node]
        regsub {~DUPLICATE[0-9]*$} $name {} logical
        if {![regexp [format {^fes_audio_inst\|audio\|%s\[([0-9]+)\]$} $kind] $logical unused bit] ||
            $bit < 0 || $bit > 31 || ($bit % 16) < 7 || ($bit % 16) > 14} {
            error "DE25 standard PR unexpected PCM register $name"
        }
        dict set semantic [expr {$bit % 16}] 1
    }
    if {[lsort -integer [dict keys $semantic]] ne {7 8 9 10 11 12 13 14}} {
        error "DE25 standard PR PCM $kind lost sample-bit coverage"
    }
    set pcm_${kind}_nodes $nodes
}

# The source holds both words until the ACK traverses two audio-domain stages.
# A 10 ns flight is one fast-source period and shorter than one audio period;
# retain net/data/skew and paired min/max path bounds on the complete bundle.
set_net_delay -max 10.000 -from $pcm_held_sample_nodes -to $pcm_pcm_nodes
set_data_delay 10.000 -from $pcm_held_sample_nodes -to $pcm_pcm_nodes
set_max_skew 10.000 -from $pcm_held_sample_nodes -to $pcm_pcm_nodes
set_max_delay 10.000 -from $pcm_held_sample_nodes -to $pcm_pcm_nodes
set_min_delay 0.000 -from $pcm_held_sample_nodes -to $pcm_pcm_nodes
post_message -type info "DE25 standard PR: live mono PCM bundle and first-D-only control crossings retained"
