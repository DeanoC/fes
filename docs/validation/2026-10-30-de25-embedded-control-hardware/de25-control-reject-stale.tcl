foreach n {FES_DE25_SOF FES_DE25_DEVICE} {if {![info exists env($n)] || $env($n) eq {}} {error "missing $n"}}
set d [design_load $env(FES_DE25_SOF)]
if {[lsearch -exact [get_service_paths device] $env(FES_DE25_DEVICE)] < 0} {error "selected device absent"}
if {[catch {design_link $d $env(FES_DE25_DEVICE)} message]} {
    puts "FES_DIAGNOSTIC_LINK_REJECTED $message"
    exit 0
}
puts stderr "FES_DIAGNOSTIC_LINK_UNEXPECTEDLY_ACCEPTED"
exit 1
