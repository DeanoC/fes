# SPDX-License-Identifier: GPL-3.0-or-later
# Explicit maintenance diagnostic. No discovery, programming or CPU MMIO.
foreach name {FES_DE25_SOF FES_DE25_DEVICE FES_DE25_MASTER FES_DE25_BASE FES_DE25_ID FES_DE25_VERSION} {
    if {![info exists env($name)] || $env($name) eq {}} {error "missing explicit $name"}
}
set design [design_load $env(FES_DE25_SOF)]
set device $env(FES_DE25_DEVICE)
set discovery_deadline [expr {[clock milliseconds] + 10000}]
while {[lsearch -exact [get_service_paths device] $device] < 0 && [clock milliseconds] < $discovery_deadline} {
    refresh_connections
    after 100
}
if {[lsearch -exact [get_service_paths device] $device] < 0} {error "claimed device absent after bounded discovery"}
design_link $design $device
set service $env(FES_DE25_MASTER)
set master_deadline [expr {[clock milliseconds] + 10000}]
while {[lsearch -exact [get_service_paths master] $service] < 0 && [clock milliseconds] < $master_deadline} {
    after 100
    update
}
if {[lsearch -exact [get_service_paths master] $service] < 0} {error "explicit fabric CSR master absent after bounded discovery"}
set base $env(FES_DE25_BASE)
set master [claim_service master $service de25-zx81-hardware]
try {
    if {[lindex [master_read_32 $master $base 1] 0] != $env(FES_DE25_ID) ||
        [lindex [master_read_32 $master [expr {$base+4}] 1] 0] != $env(FES_DE25_VERSION)} {
        error "physical computer endpoint ID/version mismatch"
    }
    fconfigure stdout -buffering line
    puts "FES_JTAG_READY"
    while {[gets stdin line] >= 0} {
        if {$line eq "Q"} {break}
        if {![regexp {^(R|W) ([0-9]+) ([0-9]+)$} $line -> op offset value] ||
            $offset ni {0 4 8 12 16 20} || $value > 0xffffffff ||
            ($op eq "W" && $offset != 12)} {error "refused access outside the selected computer CSR contract"}
        if {$op eq "R"} {
            set result [lindex [master_read_32 $master [expr {$base+$offset}] 1] 0]
        } else {
            master_write_32 $master [expr {$base+$offset}] [list $value]
            set result 0
        }
        puts "FES_JTAG_REPLY [expr {$result & 0xffffffff}]"
    }
} finally {close_service master $master}
puts "FES_JTAG_CLOSED"
exit
