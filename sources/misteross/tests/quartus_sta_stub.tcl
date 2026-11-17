# SPDX-License-Identifier: MIT
# A stand-in for the Quartus STA Tcl API, enough of it to run
# scripts/fit_timing.tcl against a described design without a fitter.
#
# It exists for one reason: the gates in fit_timing.tcl are claims about what
# was analysed, and the only way to show a gate catches what it claims is to
# present it with a design that violates the claim. A real counterexample
# would mean sweeping a clock domain out of pc386_system and running a
# forty-minute machine fit to watch the script wave it through. This describes
# that design instead.
#
# Configured by environment variables, all optional:
#   STUB_CLOCKS        clocks the SDC creates    (default "clk_clock pixel_clk_clock")
#   STUB_PATH_CLOCKS   clocks that have register paths (default: STUB_CLOCKS)
#   STUB_CORNERS       corner display names            (default two models)
#   STUB_HOLD_ONLY     hold-only corner names, |-separated (default none)
#   STUB_SLACK         slack reported for every path   (default 1.0)
#   STUB_ASYNC_PATHS   1 if recovery/removal have paths (default 0)
proc stub_env {name fallback} {
    if {[info exists ::env($name)] && $::env($name) ne ""} {return $::env($name)}
    return $fallback
}
set stub_clocks [stub_env STUB_CLOCKS "clk_clock pixel_clk_clock"]
set stub_path_clocks [stub_env STUB_PATH_CLOCKS $stub_clocks]
set stub_corners [stub_env STUB_CORNERS "Slow 100C Model|Fast 0C Model"]
set stub_hold_only [stub_env STUB_HOLD_ONLY ""]
set stub_slack [stub_env STUB_SLACK 1.0]
set stub_async [stub_env STUB_ASYNC_PATHS 0]

proc load_package {args} {}
proc project_open {args} {}
proc project_close {args} {}
proc create_timing_netlist {args} {}
proc delete_timing_netlist {args} {}
proc read_sdc {args} {}
proc update_timing_netlist {args} {}
proc set_operating_conditions {args} {}
proc report_timing {args} {}

# Collections are plain lists here, which is why these two are the same thing.
proc get_collection_size {collection} {return [llength $collection]}
proc foreach_in_collection {var collection body} {
    upvar 1 $var item
    foreach element $collection {
        set item $element
        uplevel 1 $body
    }
}

proc get_clocks {args} {
    # A pattern filters, as the real one does: returning every clock for
    # `get_clocks pixel_clk_clock` would make a domain-scoped query look
    # design-wide and the stub would hide the very gap it is here to present.
    set pattern [lindex $args end]
    if {[llength $args] < 1 || $pattern eq "*"} {return $::stub_clocks}
    set matched {}
    foreach clock $::stub_clocks {
        if {[string match $pattern $clock]} {lappend matched $clock}
    }
    return $matched
}
proc all_registers {args} {
    # One register per domain that has paths: a swept domain has none.
    set registers {}
    foreach clock $::stub_path_clocks {lappend registers "${clock}_reg"}
    return $registers
}
proc get_available_operating_conditions {args} {return [split $::stub_corners |]}
proc get_operating_conditions_info {flag corner} {
    switch -- $flag {
        -display_name {return $corner}
        -is_hold_only {return [expr {[lsearch -exact [split $::stub_hold_only |] $corner] >= 0}]}
    }
    error "stub: unknown get_operating_conditions_info flag $flag"
}

# The point of the stub. A query scoped to a clock answers for that clock; an
# unscoped query answers for the design, which is what lets a fit with one
# swept domain satisfy an unscoped gate.
proc _stub_scope {arguments} {
    foreach flag {-from_clock -to_clock} {
        set at [lsearch -exact $arguments $flag]
        if {$at >= 0} {return [lindex $arguments [expr {$at + 1}]]}
    }
    return ""
}
proc get_timing_paths {args} {
    set check setup
    foreach candidate {setup hold recovery removal} {
        if {[lsearch -exact $args -$candidate] >= 0} {set check $candidate}
    }
    if {$check in {recovery removal} && !$::stub_async} {return {}}
    set scope [_stub_scope $args]
    if {$scope ne ""} {
        # get_clocks returns the clock list, so a -from_clock [get_clocks x]
        # arrives as that list; match any of its elements.
        set hit 0
        foreach clock $scope {
            if {[lsearch -exact $::stub_path_clocks $clock] >= 0} {set hit 1}
        }
        if {!$hit} {return {}}
        return [list "path_${check}_[lindex $scope 0]"]
    }
    if {[llength $::stub_path_clocks] < 1} {return {}}
    return [list "path_${check}_[lindex $::stub_path_clocks 0]"]
}
proc get_path_info {flag path} {
    switch -- $flag {
        -slack {return $::stub_slack}
        -from {return "${path}_from"}
        -to {return "${path}_to"}
    }
    error "stub: unknown get_path_info flag $flag"
}
proc get_node_info {flag node} {
    if {$flag eq "-name"} {return $node}
    error "stub: unknown get_node_info flag $flag"
}
proc get_min_pulse_width {args} {return [list [list $::stub_slack "clk"]]}
proc get_clock_fmax_info {args} {
    set rows {}
    foreach clock $::stub_path_clocks {lappend rows [list "${clock}_clock" 100.0 100.0]}
    return $rows
}
