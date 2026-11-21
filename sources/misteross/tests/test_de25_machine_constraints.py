"""Evaluate the DE25 machine SDC's bracketed endpoint patterns in Tcl."""
from pathlib import Path
import re
import shutil
import subprocess
import unittest


BOARD = Path(__file__).resolve().parents[1] / "boards/de25nano"
SDCS = (
    BOARD / "spectrum/spectrum-clocks.sdc",
    BOARD / "atari-st/atari-st-clocks.sdc",
    BOARD / "pcxt/pcxt-clocks.sdc",
)


class MachineConstraintTclTests(unittest.TestCase):
    @unittest.skipUnless(shutil.which("tclsh"), "Tcl required")
    def test_quoted_bracket_patterns_are_literal_for_both_machines(self):
        for sdc in SDCS:
            with self.subTest(machine=sdc.parent.name):
                content = sdc.read_text()
                start = content.index("    set launches [remove_from_collection ")
                end = content.index("    if {[get_collection_size $launches]", start)
                gp_statements = content[start:end]
                machine_release = re.findall(
                    r"^set machine_release \[get_pins .+$", content, re.MULTILINE
                )
                self.assertEqual(len(machine_release), 1)

                script = """
set base {soc_inst|subsys_periph|machine}
set register_queries {}
set pin_queries {}
proc get_registers {pattern} {
    lappend ::register_queries $pattern
    return [list $pattern]
}
proc remove_from_collection {collection ignored} {return $collection}
proc get_pins {args} {
    lappend ::pin_queries [lindex $args end]
    return {pin0 pin1}
}
foreach {source sink toggle} {
    request_payload machine_gpo 31
    reply_payload gpi 23
} {
""" + gp_statements + """
}
""" + machine_release[0] + """
set expected_register_queries {
    soc_inst|subsys_periph|machine|gp_crossing|request_payload[*]
    soc_inst|subsys_periph|machine|gp_crossing|request_payload[31]
    soc_inst|subsys_periph|machine|gp_crossing|machine_gpo[*]
    soc_inst|subsys_periph|machine|gp_crossing|machine_gpo[31]
    soc_inst|subsys_periph|machine|gp_crossing|reply_payload[*]
    soc_inst|subsys_periph|machine|gp_crossing|reply_payload[23]
    soc_inst|subsys_periph|machine|gp_crossing|gpi[*]
    soc_inst|subsys_periph|machine|gp_crossing|gpi[23]
}
if {$register_queries ne $expected_register_queries} {
    error "GP endpoint patterns changed: $register_queries"
}
set expected_pin_query {soc_inst|subsys_periph|machine|machine_release[*]|aclr soc_inst|subsys_periph|machine|machine_release[*]|clrn}
if {$pin_queries ne [list $expected_pin_query]} {
    error "machine reset endpoint pattern changed: $pin_queries"
}
puts PASS
"""
                result = subprocess.run(
                    ["tclsh"], input=script, text=True, capture_output=True, timeout=10
                )
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn("PASS", result.stdout)


if __name__ == "__main__":
    unittest.main()
