"""Execute the actual capture SDC with controlled netlist inventories.

The full producer checks a real compiled netlist; these cases ensure missing
CDC endpoints and resurrected private resets cannot silently remove bounds.
"""
from pathlib import Path
import shutil
import subprocess
import unittest

SDC=Path(__file__).resolve().parents[1]/'prototypes/de25_board_control/clocks.sdc'


class CaptureConstraintTests(unittest.TestCase):
    @unittest.skipUnless(shutil.which('tclsh'), 'Tcl required')
    def test_actual_sdc_keeps_exact_ingress_cuts_and_rejects_changed_inventory(self):
        stub=r'''
set cuts {}
set bounds {}
proc get_collection_size {nodes} {llength $nodes}
proc nodes {count} {set result {}; for {set i 0} {$i<$count} {incr i} {lappend result node$i}; return $result}
proc get_registers {args} {
    global fault
    set pattern [lindex $args end]
    if {[string first sys_release $pattern]>=0} {return [nodes [expr {$fault eq "old-reset"}]]}
    if {[string first published_rows $pattern]>=0 || [string first front_rows $pattern]>=0} {
        return [nodes [expr {$fault eq "metadata" ? 8 : 9}]]
    }
    if {$fault eq "token" && [string first request_sync $pattern]>=0} {return {}}
    return [list $pattern]
}
proc get_pins {args} {
    global fault
    set pattern [lindex $args end]
    if {[string first clrn $pattern]>=0} {return {}}
    if {[string first sync_regs $pattern]>=0} {return [nodes 4]}
    if {[string first pixel_transport_reset_release $pattern]>=0} {
        return [nodes [expr {$fault eq "pixel-reset" ? 1 : 2}]]
    }
    if {$fault eq "ready"} {return {}}
    return [list $pattern]
}
proc get_ports {name} {return $name}
proc create_generated_clock {args} {}
proc post_message {args} {}
proc set_false_path {args} {global cuts; lappend cuts $args}
foreach command {set_net_delay set_data_delay set_max_skew set_max_delay set_min_delay} {
    proc $command {args} {global bounds; lappend bounds $args}
}
'''
        for fault in ('none','old-reset','metadata','token','pixel-reset','ready'):
            script='set fault '+fault+'\n'+stub+'\nsource {'+str(SDC)+'}\n'
            script+='if {[llength $cuts]!=5 || [llength $bounds]!=5} {error "timing contract lost cuts or metadata bounds"}\nputs PASS\n'
            # Tcl on stdin otherwise reports an error and keeps processing.
            script='if {[catch {\n'+script+'\n} reason]} {puts stderr $reason; exit 1}\n'
            with self.subTest(fault=fault):
                result=subprocess.run(['tclsh'],input=script,text=True,capture_output=True,timeout=10)
                if fault=='none':
                    self.assertEqual(result.returncode,0,result.stderr)
                    self.assertIn('PASS',result.stdout)
                else:
                    self.assertEqual(result.returncode,1,result.stdout+result.stderr)
                    self.assertIn('DE25',result.stderr)


if __name__=='__main__':unittest.main()
