# Read-only service selection for the already-claimed cable, after programming.
foreach name {FES_DE25_SOF FES_DE25_USB_PORT} {
    if {![info exists env($name)] || $env($name) eq {}} {error "missing $name"}
}
set design [design_load $env(FES_DE25_SOF)]
set suffix "#$env(FES_DE25_USB_PORT)-iface0#DE25-Nano"
set selected {}
foreach d [get_service_paths device] {
    if {[string first $suffix $d] >= 0} {lappend selected $d}
}
if {[llength $selected] != 1} {error "claimed serial-to-port mapping did not select one JTAG device"}
set device [lindex $selected 0]
design_link $design $device
set until [expr {[clock milliseconds]+10000}]
set masters {}
while {[clock milliseconds] < $until} {
    set masters {}
    foreach m [get_service_paths master] {
        if {[string first "$device/" $m] == 0 && [string match "*/subsys_debug_fpga_m.master" $m]} {lappend masters $m}
    }
    if {[llength $masters] == 1} {break}
    after 100
    update
}
if {[llength $masters] != 1} {error "claimed board did not expose exactly one fabric JTAG master"}
puts "FES_DEVICE $device"
puts "FES_MASTER [lindex $masters 0]"
exit 0
