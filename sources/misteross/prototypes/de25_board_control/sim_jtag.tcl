# SPDX-License-Identifier: GPL-3.0-or-later
# Host-only System Console facade over the actual Verilated Avalon model.
foreach name {FES_MODEL FES_DIAGNOSTIC_SCRIPT FES_DE25_BASE FES_DE25_DEVICE FES_DE25_MASTER} {
    if {![info exists env($name)] || $env($name) eq {}} {error "missing simulated $name"}
}
set model [open "|[list $env(FES_MODEL) --serve]" r+]
fconfigure $model -buffering line -encoding ascii -translation lf
proc design_load {sof} {return simulated-design}
proc design_link {design device} {return}
proc get_service_paths {kind} {
    if {$kind eq "device"} {return [list $::env(FES_DE25_DEVICE)]}
    if {$kind eq "master"} {return [list $::env(FES_DE25_MASTER)]}
    return {}
}
proc refresh_connections {} {return}
proc claim_service {kind path owner} {return simulated-master}
proc close_service {kind master} {close $::model}
proc read_model {command} {
    puts $::model $command
    flush $::model
    if {[gets $::model line] < 0} {error "simulated Avalon endpoint exited"}
    set fields [split $line]
    if {[lindex $fields 0] ne "R" || [lindex $fields 1] ne "1"} {error "simulated Avalon access rejected"}
    return [lindex $fields 2]
}
proc master_read_32 {master address count} {
    if {$count != 1} {error "unexpected simulated read count"}
    set value [read_model [format "read %x" [expr {$address-$::env(FES_DE25_BASE)}]]]
    scan $value %x result
    return [list $result]
}
proc master_write_32 {master address values} {
    if {[llength $values] != 1} {error "unexpected simulated write count"}
    read_model [format "write %x %x" [expr {$address-$::env(FES_DE25_BASE)}] [lindex $values 0]]
}
source $env(FES_DIAGNOSTIC_SCRIPT)
