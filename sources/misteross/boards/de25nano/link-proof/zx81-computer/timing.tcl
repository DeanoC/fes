# Same registered slot contract, with the static bus now inside the computer.
set ::fes_zx81_static_prefix {soc_inst|subsys_periph|fes_computer|fes_computer|external_bus.}
source [file join [file dirname [info script]] .. zx81-ram timing.tcl]
