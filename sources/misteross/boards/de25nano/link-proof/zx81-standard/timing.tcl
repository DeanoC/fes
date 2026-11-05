# Same protected registered bus contract on the normal standard-core board.
set ::fes_zx81_static_prefix {zx81_bus|}
source [file join [file dirname [info script]] .. zx81-ram timing.tcl]
