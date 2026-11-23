"""Evaluate the DE25 machine SDC's bracketed endpoint patterns in Tcl."""
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest


BOARD = Path(__file__).resolve().parents[1] / "boards/de25nano"
SDCS = (
    BOARD / "spectrum/spectrum-clocks.sdc",
    BOARD / "atari-st/atari-st-clocks.sdc",
    BOARD / "c64/c64-clocks.sdc",
    BOARD / "pcxt/pcxt-clocks.sdc",
)
# The C64 keeps its memory in block RAM: no SDRAM controller, so no
# machine-clock release chain to constrain. The PC/XT's SDRAM has one.
RELEASES = {"spectrum": 1, "atari-st": 1, "c64": 0, "pcxt": 1}
# Word 0x18 of the shared transport is display readiness on Linux and the
# native upload shell's fetch-bus snapshot; the SDC picks by the fetch toggle.
TRANSPORT_STATUS = 'set fetch_tag [get_registers "${base}|transport|fetch_request"]\n'
# Each query returns its own pattern as the collection, so the log shows the
# exact strings Quartus would receive. Only the native fetch toggle exists in
# the native shell; the snapshot buses are 48 bits.
STATUS_HARNESS = r"""
set base {soc_inst|subsys_periph|machine}
set native NATIVE
proc get_registers {pattern} {
    puts "get_registers\t$pattern"
    return $pattern
}
proc get_pins {args} {
    puts "get_pins\t[join $args \t]"
    return [lindex $args end]
}
proc get_collection_size {collection} {
    if {[string match {*|transport|fetch_request} $collection]} {return $::native}
    if {[string match {*\[\*\]} $collection]} {return 48}
    return 1
}
proc record {command args} {puts "$command\t[join $args \t]"}
foreach command {set_false_path set_net_delay set_data_delay set_max_skew set_max_delay set_min_delay} {
    interp alias {} $command {} record $command
}
"""


def run_tcl(script):
    """Run a script from a file: tclsh reading stdin reports an error and carries on."""
    with tempfile.TemporaryDirectory() as directory:
        path = Path(directory) / "check.tcl"
        path.write_text(script)
        return subprocess.run(["tclsh", str(path)], text=True, capture_output=True, timeout=10)


def transport_status(native):
    """Every query and exception one branch of the 0x18 crossing makes, in order."""
    transport = "soc_inst|subsys_periph|machine|transport|"
    if not native:
        ready = "soc_inst|subsys_periph|machine|video_ready_source"
        first = transport + "video_ready_sync[0]|d"
        return [("get_registers", transport + "fetch_request"),
                ("get_registers", ready),
                ("get_pins", "-compatibility_mode", "-nocase", first),
                ("get_registers", transport + "video_ready_sync[1]"),
                ("set_false_path", "-from", ready, "-to", first)]
    rows = [("get_registers", transport + "fetch_request")]
    for source in ("fetch_request", "fetch_reply"):
        first = transport + source + "_sync[0]|d"
        rows += [("get_registers", transport + source),
                 ("get_pins", "-compatibility_mode", "-nocase", first),
                 ("get_registers", transport + source + "_sync[1]"),
                 ("set_false_path", "-from", transport + source, "-to", first)]
    launch, capture = transport + "fetch_payload[*]", transport + "fetch_sample[*]"
    rows += [("get_registers", launch), ("get_registers", capture)]
    for bound in (("set_net_delay", "-max", "10.000"), ("set_data_delay", "10.000"),
                  ("set_max_skew", "10.000"), ("set_max_delay", "10.000"), ("set_min_delay", "0.000")):
        rows.append((*bound, "-from", launch, "-to", capture))
    return rows


class MachineConstraintTclTests(unittest.TestCase):
    @unittest.skipUnless(shutil.which("tclsh"), "Tcl required")
    def test_quoted_bracket_patterns_are_literal_for_every_machine(self):
        for sdc in SDCS:
            with self.subTest(machine=sdc.parent.name):
                content = sdc.read_text()
                start = content.index("    set launches [remove_from_collection ")
                end = content.index("    if {[get_collection_size $launches]", start)
                gp_statements = content[start:end]
                machine_release = re.findall(
                    r"^set machine_release \[get_pins .+$", content, re.MULTILINE
                )
                self.assertEqual(len(machine_release), RELEASES[sdc.parent.name])
                release_check = """
set expected_pin_query {soc_inst|subsys_periph|machine|machine_release[*]|aclr soc_inst|subsys_periph|machine|machine_release[*]|clrn}
if {$pin_queries ne [list $expected_pin_query]} {
    error "machine reset endpoint pattern changed: $pin_queries"
}
""" if machine_release else """
if {$pin_queries ne {}} {error "unexpected pin queries: $pin_queries"}
"""

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
""" + "".join(machine_release) + """
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
if {$register_queries ne [list {*}$expected_register_queries]} {
    error "GP endpoint patterns changed: $register_queries"
}
""" + release_check + """
puts PASS
"""
                result = run_tcl(script)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn("PASS", result.stdout)

    @unittest.skipUnless(shutil.which("tclsh"), "Tcl required")
    def test_transport_status_patterns_are_literal_in_linux_and_native_shells(self):
        selected = [sdc for sdc in SDCS if TRANSPORT_STATUS in sdc.read_text()]
        self.assertLessEqual({"spectrum", "c64"}, {sdc.parent.name for sdc in selected})
        for sdc in selected:
            content = sdc.read_text()
            start = content.index(TRANSPORT_STATUS)
            block = content[start:content.index("\n}\n", start) + 3]
            for native in (0, 1):
                with self.subTest(machine=sdc.parent.name, native=bool(native)):
                    result = run_tcl(STATUS_HARNESS.replace("NATIVE", str(native)) + block)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertEqual([tuple(line.split("\t")) for line in result.stdout.splitlines()],
                                     transport_status(native))


if __name__ == "__main__":
    unittest.main()
