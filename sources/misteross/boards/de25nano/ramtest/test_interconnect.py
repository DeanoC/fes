"""Guard the off-board generated transport harness; no vendor tools required."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("ram_generated_transport", Path(__file__).with_name("interconnect_sim.py"))
transport = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(transport)


class RamtestInterconnectTests(unittest.TestCase):
    def test_outbound_qip_source_is_rejected_before_read(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            project = root / "project"
            (project / "qsys_top").mkdir(parents=True)
            (root / "outside.v").write_text("not a selected producer file")
            (project / "qsys_top/qsys_top.qip").write_text(
                'set_global_assignment -name VERILOG_FILE [file join $::quartus(qip_path) "../../outside.v"]\n')
            with self.assertRaises(ValueError):
                transport.source_closure(project)

    def test_symlink_qip_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            project = root / "project"
            (project / "qsys_top").mkdir(parents=True)
            outside = root / "outside.qip"
            outside.write_text("")
            (project / "qsys_top/qsys_top.qip").symlink_to(outside)
            with self.assertRaisesRegex(ValueError, "symlink"):
                transport.source_closure(project)

    def test_active_hard_ram_is_rejected_without_a_substitute(self):
        with tempfile.TemporaryDirectory() as temporary:
            xml = Path(temporary) / "elaboration.xml"
            xml.write_text('<root><module origName="altera_merlin_router"/></root>')
            transport.check_elaboration(xml)
            for name in ("altera_syncram", "altsyncram"):
                xml.write_text(f'<root><module origName="{name}"/></root>')
                with self.assertRaisesRegex(ValueError, "hard RAM"):
                    transport.check_elaboration(xml)

    def test_duplicate_generated_ports_are_rejected(self):
        source = 'module broken (input wire same, output wire same\n);\nendmodule\n'
        with self.assertRaises(ValueError):
            transport.ports(source, "broken")

    def test_hps_enabled_receipt_never_reaches_tools(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            project = root / "producer/project"
            project.mkdir(parents=True)
            (project.parent / "inputs.json").write_text(json.dumps({
                "source_classification": "committed", "fes_revision": "1" * 40,
                "ramtest": {"hps_destructive_enabled": True, "build_id": "1" * 32}}))
            (project.parent / "generation.json").write_text('{"ramtest":{}}')
            output = root / "simulation"
            with patch.object(transport.subprocess, "run") as tool, self.assertRaisesRegex(ValueError, "HPS writes disabled"):
                transport.main(["--project", str(project), "--output", str(output)])
            tool.assert_not_called()
            self.assertEqual(json.loads((output / "simulation.json").read_text())["status"], "failed")

    def test_existing_output_is_preserved(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            project, output = root / "project", root / "output"
            project.mkdir(); output.mkdir()
            sentinel = output / "sentinel"
            sentinel.write_text("keep")
            with self.assertRaises(FileExistsError):
                transport.main(["--project", str(project), "--output", str(output)])
            self.assertEqual(sentinel.read_text(), "keep")


if __name__ == "__main__":
    unittest.main()
