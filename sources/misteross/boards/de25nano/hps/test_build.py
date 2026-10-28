"""Exercise source preparation and parameter review without invoking Quartus."""
import copy
from contextlib import redirect_stderr, redirect_stdout
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import shutil
import stat
import subprocess
import sys
import tempfile
import unittest
from unittest import mock
import warnings
import xml.etree.ElementTree as ET
import zipfile


HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("de25_hps_builder", HERE / "build.py")
BUILD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BUILD)
PREFIX = "Demonstration/SoC_FPGA/GHRD/"
IP = "http://www.accellera.org/XMLSchema/IPXACT/1685-2014"
ALTERA = "http://www.altera.com/XMLSchema/IPXact2014/extensions"
HPS = "hps_subsys/ip/hps_subsys/agilex_hps.ip"
EMIF = "hps_subsys/ip/qsys_top/emif_io96b_hps.ip"
BOOT = "software/u-boot/spl/u-boot-spl-dtb.hex"
PLL = "ip/pll.ip"
TOP = b'''module golden_top(input CLOCK2_50, input LPDDR4A_REFCLK_p,
input [1:0] KEY, input [3:0] SW, output [7:0] LED);
wire                   system_reset_n;
assign                 system_reset_n = ~ninit_done;
pll pll_inst (
.refclk   (system_clk_50),
.locked   (),
.rst      (ninit_done),
.outclk_0 (system_clk_100_internal)
);
qsys_top soc_inst (
.emif_hps_emif_ref_clk_0_clk (LPDDR4A_REFCLK_p),
.reset_reset_n                             (system_reset_n)
);
// Debounce logic to clean out glitches within 1ms
debounce debounce_inst (
.clk                                       (system_clk_100_internal),
.reset_n                                   (system_reset_n),
.data_in                                   (fpga_button_pio),
.data_out                                  (fpga_debounced_buttons)
);
always @(posedge system_clk_100_internal or negedge system_reset_n) begin
  if (~system_reset_n)
    heartbeat_count <= 25'd0;
  else
    heartbeat_count <= heartbeat_count + 25'd1;
end
endmodule
'''


def parameter(name, value):
    return (f'<ipxact:parameter parameterId="{name}" type="string">'
            f'<ipxact:name>{name}</ipxact:name><ipxact:value>{value}</ipxact:value>'
            '</ipxact:parameter>')


def component_xml(root_parameters, nested_parameters=None):
    """Use the real IP-XACT packaged-EMIF parameter nesting, not a flat mock."""
    nested = ""
    if nested_parameters is not None:
        nested = (
            '<altera:altera_packaged_subsystems altera:version="1.1">'
            '<altera:altera_internal_packaged_subsystem altera:name="emif_io96b_hps">'
            '<altera:sub_modules><altera:sub_module altera:name="emif_0_lpddr4">'
            '<ipxact:parameters>' + ''.join(parameter(*p) for p in nested_parameters) +
            '</ipxact:parameters></altera:sub_module></altera:sub_modules>'
            '</altera:altera_internal_packaged_subsystem>'
            '</altera:altera_packaged_subsystems>')
    return (f'<ipxact:component xmlns:ipxact="{IP}" xmlns:altera="{ALTERA}">'
            '<ipxact:vendorExtensions><altera:altera_module_parameters>'
            '<ipxact:parameters>' + ''.join(parameter(*p) for p in root_parameters) +
            '</ipxact:parameters></altera:altera_module_parameters>' + nested +
            '</ipxact:vendorExtensions></ipxact:component>').encode()


def vendor_files():
    return {
        "golden_top.qsf": ('set_global_assignment -name DEVICE A5EB013BB23BE4SCS\n' +
            ''.join(f'set_location_assignment PIN_{pin} -to LED[{index}]\n'
                    f'set_instance_assignment -name IO_STANDARD "1.1-V" -to LED[{index}]\n'
                    for index, pin in ((0, "DF35"), (1, "DJ32"), (2, "DN22"), (7, "DP30")))).encode(),
        "golden_top.v": TOP,
        "qsys_top.qsys": b'<system name="qsys_top"/>\n',
        "hps_subsys/hps_subsys.qsys": b'<system name="hps_subsys"/>\n',
        "peripheral_subsys/peripheral_subsys.qsys": b'<system name="peripheral_subsys"/>\n',
        "ghrd_timing.sdc": (
            b'# 50MHz board input clock, 150MHz for EMIF refclk\n'
            b'create_clock -name MAIN_CLOCK -period 20 [get_ports CLOCK2_50]\n'
            b'create_clock -name EMIF_REF_CLOCK -period "166.6666MHz" [get_ports DDR4_REFCLK_p]\n'
            b'set_false_path -from [get_ports {CPU_RESET_n}]\n'
            b'source ./jtag.sdc\n'
            b'set_false_path -from [get_ports {KEY[*]}] -to *\n'
            b'set_false_path -from [get_ports {SW[*]}] -to *\n'
            b'set_false_path -from [get_ports {LEDR[*]}] -to *\n'
            b'set_false_path -from * -to [get_ports {LEDR[*]}]\n'
            b'set_false_path -from fpga_reset_n_debounced -to {soc_inst|rst_controller_*|altera_reset_synchronizer_int_chain[1]}\n'),
        "jtag.sdc": b'# Retained vendor JTAG timing contract.\n',
        HPS: component_xml((("device", BUILD.DEVICE), ("H2F_Width", "128"),
                            ("LWH2F_Width", "32"))),
        PLL: component_xml((("gui_reference_clock_frequency", "50.0"),
                           ("gui_output_clock_frequency0", "100.0"), ("gui_use_locked", "true"))),
        EMIF: component_xml((("SYSINFO_DEVICE", BUILD.DEVICE),), (
            ("PHY_REFCLK_FREQ_MHZ", "166.6666"),
            ("PHY_REFCLK_FREQ_MHZ_AUTOSET_EN", "false"),
            ("MEM_CHANNEL_CS_WIDTH", "1"))),
        BOOT: b'00112233445566778899aabbccddeeff\n',
        "software/hps_debug/hps_wipe.s": b'// Retained vendor boot/debug source.\n',
        "custom_ip/f2sdram_adapter/f2sdram_adapter_hw.tcl": b'# Retained custom IP description.\n',
        "custom_ip/f2sdram_adapter/f2sdram_adapter.sv": b'module f2sdram_adapter; endmodule\n',
    }


def handoff_tree():
    """Minimal real SOPCINFO module/connection/parameter/value vocabulary."""
    root = ET.Element("EnsembleReport", {"name": "qsys_top", "kind": "qsys_top"})
    ET.SubElement(root, "module", {"name": "subsys_periph_fes_mailbox",
                  "path": "subsys_periph.fes_mailbox", "kind": "fes_de25_mailbox", "version": "1.0"})
    identities = (
        ("avalon", "subsys_hps_agilex_hps.lwhps2fpga", "subsys_periph_pb_cpu_0.s0", {}),
        ("avalon", "subsys_periph_pb_cpu_0.m0", "subsys_periph_fes_mailbox.avs",
         {"baseAddress": hex(BUILD.mailbox_base())}),
        ("clock", "subsys_periph_periph_clk.out_clk", "subsys_periph_fes_mailbox.clk",
         {"clockRateSysInfo": "100000000"}),
        ("reset", "subsys_periph_periph_rst_in.out_reset", "subsys_periph_fes_mailbox.reset", {}),
    )
    for kind, start, end, parameters in identities:
        connection = ET.SubElement(root, "connection", {"kind": kind, "start": start, "end": end})
        for name, value in parameters.items():
            item = ET.SubElement(connection, "parameter", {"name": name})
            ET.SubElement(item, "value").text = value
    return ET.ElementTree(root)


class HpsBuildTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="fes-de25-hps-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.archive_number = 0

    def archive(self, *, changed=None, extras=(), missing=()):
        files = vendor_files()
        files.update(changed or {})
        for name in missing:
            files.pop(name)
        archive = self.root / ("vendor-" + str(self.archive_number) + ".zip")
        self.archive_number += 1
        with warnings.catch_warnings():
            warnings.simplefilter("ignore", UserWarning)
            with zipfile.ZipFile(archive, "w") as output:
                for name, data in files.items():
                    output.writestr(PREFIX + name, data)
                for name, data, mode in extras:
                    item = zipfile.ZipInfo(PREFIX + name)
                    item.create_system = 3
                    item.external_attr = mode << 16
                    output.writestr(item, data)
        selected = {
            "format": 1, "board": "de25-nano", "device": BUILD.DEVICE,
            "archive": {"sha256": BUILD.digest(archive), "prefix": PREFIX,
                        "file": archive.name, "url": "https://example.invalid/vendor.zip"},
        }
        return archive, selected, files

    def prepare(self, archive, selected, output=None):
        with mock.patch.object(BUILD, "lock", return_value=selected):
            return BUILD.prepare_project(archive, output or self.root / "prepared")

    def generation_tool(self, directory):
        tool = directory / "quartus/bin/quartus_sh"
        qsys = directory / "qsys/bin/qsys-script"
        qsys.parent.mkdir(parents=True)
        qsys.write_text("#!/bin/sh\nexit 99\n")
        qsys.chmod(0o755)
        return tool

    def write_handoff(self, project, tree=None):
        path = project / "qsys_top/qsys_top.sopcinfo"
        path.parent.mkdir(parents=True, exist_ok=True)
        (tree or handoff_tree()).write(path, encoding="utf-8", xml_declaration=True)
        header = project / "fes_mailbox/generated/fes_de25_mailbox.vh"
        if not header.exists():
            header.parent.mkdir(parents=True, exist_ok=True)
            header.write_bytes((HERE / "generated/fes_de25_mailbox.vh").read_bytes())
        return path

    def test_selected_commit_blobs_ignore_later_working_tree_changes(self):
        archive, selected, _ = self.archive()
        original = BUILD.fes_sources()
        original["vendor.lock.toml"] = (
            'format = 1\nboard = "de25-nano"\ndevice = "' + BUILD.DEVICE + '"\n'
            '[archive]\nurl = "https://example.invalid/vendor.zip"\nfile = "vendor.zip"\n'
            'prefix = "' + PREFIX + '"\nsha256 = "' + selected["archive"]["sha256"] + '"\n').encode()
        repository = self.root / "frozen-repository"
        hps = repository / "sources/misteross/boards/de25nano/hps"
        for name, data in original.items():
            target = hps.parent / "build.py" if name == "board_build.py" else hps / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)

        def git(*arguments):
            return subprocess.check_output(["git", "-C", str(repository), *arguments], stderr=subprocess.PIPE)

        git("init", "-q")
        git("config", "user.name", "FES diagnostic test")
        git("config", "user.email", "fes-test@example.invalid")
        git("add", ".")
        git("commit", "-qm", "frozen source")
        revision = git("rev-parse", "HEAD").decode().strip()
        # Model source edits or a checkout advancing after the driver captures
        # its revision. Neither the lock, offset, script nor RTL may come from it.
        for name in ("fes_de25_mailbox.v", "generated/fes_de25_mailbox.vh",
                     "integrate.tcl", "vendor.lock.toml", "fes_de25_top_reset.v", "fes_de25_top.sdc"):
            (hps / name).write_text("later working-tree input; must never be selected\n")
        output = self.root / "frozen-project"
        with mock.patch.object(BUILD, "ROOT", repository), mock.patch.object(BUILD, "HERE", hps):
            BUILD.prepare_project(archive, output, revision)
        for name, data in original.items():
            self.assertEqual((output / "project/fes_mailbox" / name).read_bytes(), data, name)
        inputs = json.loads((output / "inputs.json").read_text())
        self.assertEqual(inputs["fes_revision"], revision)
        self.assertEqual(inputs["source_classification"], "committed")
        self.assertEqual(inputs["vendor_archive_sha256"], selected["archive"]["sha256"])
        self.assertEqual(inputs["mailbox_peripheral_offset"], BUILD.mailbox_base())
        self.assertIn(hex(BUILD.mailbox_base()), (output / "project/integrate-mailbox.tcl").read_text())

    def test_source_change_during_preparation_stops_before_ip_generation(self):
        revision = "b" * 40
        committed = {"fes_revision": revision, "source_classification": "committed"}
        changed = {"base_revision": revision, "source_classification": "working-copy-diagnostic"}
        output = self.root / "race-output"
        error = io.StringIO()
        with mock.patch.object(BUILD, "source_identity", side_effect=(committed, changed)), \
                mock.patch.object(BUILD.BOARD_BUILD, "locate_quartus", return_value=(self.root / "tool", "test-only")), \
                mock.patch.object(BUILD, "prepare_project", return_value=output) as prepare, \
                mock.patch.object(BUILD, "generate_project") as generate, redirect_stderr(error), redirect_stdout(io.StringIO()):
            with self.assertRaises(SystemExit) as stopped:
                BUILD.main(["build", "--vendor-archive", str(self.root / "archive.zip"), "--output", str(output)])
            self.assertEqual(stopped.exception.code, 1)
            self.assertIn("source identity changed", error.getvalue())
            self.assertEqual(prepare.call_args.args[2], revision)
            generate.assert_not_called()

    def test_dirty_rtl_is_working_copy_evidence_and_cannot_generate_or_build(self):
        revision = "a" * 40
        with mock.patch.object(BUILD.subprocess, "check_output", side_effect=(revision + "\n", "")):
            committed = BUILD.source_identity()
        self.assertEqual(committed["source_classification"], "committed")
        self.assertEqual(committed["fes_revision"], revision)
        self.assertEqual(len(committed["fes_revision"]), 40)
        self.assertNotIn("base_revision", committed)
        dirty_status = " M sources/misteross/boards/de25nano/hps/fes_de25_mailbox.v\n"
        with mock.patch.object(BUILD.subprocess, "check_output", side_effect=(revision + "\n", dirty_status)):
            dirty = BUILD.source_identity()
        self.assertEqual(dirty["source_classification"], "working-copy-diagnostic")
        self.assertEqual(dirty["base_revision"], revision)
        self.assertNotIn("fes_revision", dirty)

        archive, selected, _ = self.archive()
        for action in ("generate", "build"):
            output = self.root / ("dirty-" + action)
            error = io.StringIO()
            with self.subTest(action=action), \
                    mock.patch.object(BUILD.BOARD_BUILD, "locate_quartus", return_value=(self.root / "quartus_sh", "test-only")), \
                    mock.patch.object(BUILD, "source_identity", return_value=dirty), \
                    mock.patch.object(BUILD, "prepare_project") as prepare, redirect_stderr(error):
                with self.assertRaises(SystemExit) as stopped:
                    BUILD.main([action, "--vendor-archive", str(archive), "--output", str(output)])
                self.assertEqual(stopped.exception.code, 1)
                self.assertIn("requires clean committed source", error.getvalue())
                prepare.assert_not_called()
                self.assertFalse(output.exists())

        # Inspection still exercises the real extractor and records the dirty
        # source as a working copy instead of labelling it as the old HEAD.
        output = self.root / "dirty-project-inspection"
        with mock.patch.object(BUILD, "lock", return_value=selected), \
                mock.patch.object(BUILD, "source_identity", return_value=dirty), \
                mock.patch.object(BUILD.BOARD_BUILD, "locate_quartus") as locate, redirect_stdout(io.StringIO()):
            self.assertEqual(BUILD.main(["project", "--vendor-archive", str(archive),
                                         "--output", str(output)]), 0)
            locate.assert_not_called()
        inputs = json.loads((output / "inputs.json").read_text())
        self.assertEqual(inputs["base_revision"], revision)
        self.assertEqual(inputs["source_classification"], "working-copy-diagnostic")
        self.assertNotIn("fes_revision", inputs)

    def test_extracts_locked_sources_and_excludes_cached_generation(self):
        excluded = (
            "output_files/stale.v", "dni/stale.qsys", "db/stale.ip",
            "incremental_db/stale.v", "greybox_tmp/stale.v", "ip/core/synth/stale.v",
            "ip/core/core.sopcinfo", "hps_subsys/hps_subsys.qsys.legacy",
        )
        archive, selected, files = self.archive(extras=tuple(
            (name, b"do not import cached compiler bytes", stat.S_IFREG | 0o644)
            for name in excluded))
        output = self.root / "extracted"
        recorded = BUILD.extract_vendor(archive, output, selected)
        self.assertEqual(set(recorded), set(files))
        for name, data in files.items():
            self.assertEqual((output / name).read_bytes(), data)
            self.assertEqual(recorded[name], hashlib.sha256(data).hexdigest())
        for name in excluded:
            self.assertFalse((output / name).exists(), name)

    def test_archive_hash_and_symlink_inputs_fail_before_extraction(self):
        archive, selected, _ = self.archive()
        wrong = copy.deepcopy(selected)
        wrong["archive"]["sha256"] = "0" * 64
        output = self.root / "no-output"
        with self.assertRaisesRegex(ValueError, "regular file matching"):
            BUILD.extract_vendor(archive, output, wrong)
        self.assertFalse(output.exists())
        link = self.root / "archive-link.zip"
        link.symlink_to(archive)
        with self.assertRaisesRegex(ValueError, "regular file matching"):
            BUILD.extract_vendor(link, output, selected)
        self.assertFalse(output.exists())

    def test_archive_rejects_unsafe_paths_links_and_duplicate_sources(self):
        cases = (
            ("../escaped.v", stat.S_IFREG | 0o644),
            ("/absolute.v", stat.S_IFREG | 0o644),
            ("windows\\escaped.v", stat.S_IFREG | 0o644),
            ("linked.v", stat.S_IFLNK | 0o777),
            ("pipe.v", stat.S_IFIFO | 0o600),
            ("socket.v", stat.S_IFSOCK | 0o600),
            ("golden_top.qsf", stat.S_IFREG | 0o644),
        )
        for index, (name, mode) in enumerate(cases):
            with self.subTest(member=name):
                archive, selected, _ = self.archive(extras=((name, b"untrusted", mode),))
                with self.assertRaisesRegex(ValueError, "unsafe|duplicate"):
                    BUILD.extract_vendor(archive, self.root / ("reject-" + str(index)), selected)
        self.assertFalse((self.root / "escaped.v").exists())

    def test_missing_required_vendor_input_is_rejected(self):
        archive, selected, _ = self.archive(missing=("hps_subsys/hps_subsys.qsys",))
        with self.assertRaisesRegex(ValueError, "missing hps_subsys/hps_subsys.qsys"):
            BUILD.extract_vendor(archive, self.root / "missing", selected)

    def test_preparation_preserves_hps_boot_bytes_and_refuses_existing_outputs(self):
        archive, selected, files = self.archive()
        output = self.prepare(archive, selected)
        project = output / "project"
        for name in (HPS, EMIF, PLL, BOOT, "software/hps_debug/hps_wipe.s", "jtag.sdc"):
            self.assertEqual((project / name).read_bytes(), files[name], name)
        vendor = json.loads((output / "vendor-inputs.json").read_text())
        for name, data in files.items():
            self.assertEqual(vendor["source_sha256"][name], hashlib.sha256(data).hexdigest())
        inputs = json.loads((output / "inputs.json").read_text())
        self.assertEqual(inputs["physical_mailbox_address"], "not-established")
        self.assertEqual(inputs["boot_firmware_compatibility"], "not-qualified")
        self.assertNotIn("timing_acceptance", inputs)
        self.assertIn('IP_SEARCH_PATHS "custom_ip/**/*;fes_mailbox"',
                      (project / "golden_top.qsf").read_text())
        (output / "retained-sof").write_bytes(b"previous artifact")
        with self.assertRaises(FileExistsError):
            self.prepare(archive, selected, output)
        self.assertEqual((output / "retained-sof").read_bytes(), b"previous artifact")

    def test_constraint_repair_keeps_emif_and_generated_pll_clock_ownership(self):
        archive, selected, files = self.archive()
        output = self.prepare(archive, selected)
        repaired = (output / "project/ghrd_timing.sdc").read_text()
        self.assertIn('-period "166.6666MHz" [get_ports LPDDR4A_REFCLK_p]', repaired)
        self.assertNotIn('MAIN_CLOCK', repaired)
        original_exceptions = [line for line in files["ghrd_timing.sdc"].decode().splitlines()
                               if line.startswith("set_false_path")]
        surviving_exceptions = [line for line in repaired.splitlines()
                                if line.startswith("set_false_path")]
        self.assertEqual(surviving_exceptions, [line for line in original_exceptions
                         if "KEY[*]" in line or "SW[*]" in line])
        self.assertNotIn("set_false_path -from [get_ports {LED[*]}]", repaired)
        self.assertNotIn("set_false_path -from system_reset_n", repaired)
        inputs = json.loads((output / "inputs.json").read_text())
        self.assertEqual(len(inputs["reviewed_constraint_repairs"]["removed_unmatched_exceptions"]), 4)
        self.assertIn("MAIN_CLOCK", inputs["reviewed_constraint_repairs"]["removed_duplicate_reference_clock"])

    def test_top_reset_patch_preserves_generated_controllers_and_pll_reset(self):
        archive, selected, files = self.archive()
        output = self.prepare(archive, selected)
        project = output / "project"
        top = (project / "golden_top.v").read_text()
        self.assertIn("fpga_reset_n = ~ninit_done & pll_locked;", top)
        self.assertIn(".locked   (pll_locked)", top)
        self.assertIn(".rst      (ninit_done)", top)
        self.assertIn(".reset_reset_n                             (fpga_reset_n)", top)
        self.assertIn(".reset_n                                   (top_reset_n)", top)
        self.assertIn("negedge top_reset_n", top)
        self.assertIn(".async_reset_n (fpga_reset_n)", top)
        self.assertNotIn("system_reset_n", top)
        for name in ("qsys_top.qsys", "peripheral_subsys/peripheral_subsys.qsys", HPS, EMIF, PLL, BOOT):
            self.assertEqual((project / name).read_bytes(), files[name], name)
        qsf = (project / "golden_top.qsf").read_text()
        self.assertIn("VERILOG_FILE fes_mailbox/fes_de25_top_reset.v", qsf)
        self.assertIn("SDC_FILE fes_mailbox/fes_de25_top.sdc", qsf)
        inputs = json.loads((output / "inputs.json").read_text())
        repair = inputs["reviewed_top_reset_repairs"]
        self.assertEqual(repair["top_reset_consumers"], ["debounce_inst", "heartbeat_count"])
        # The receipt must describe every actual vendor-top change.
        original = files["golden_top.v"].decode()
        for patch in repair["patches"]:
            original = original.replace(patch["before"], patch["after"])
        self.assertEqual(original, top)

    def test_changed_top_pll_led_or_duplicate_clock_contract_fails_closed(self):
        original = vendor_files()
        cases = (
            {"golden_top.v": TOP.replace(b".locked   ()", b".locked   (other_lock)")},
            {"golden_top.v": TOP.replace(b".rst      (ninit_done)", b".rst      (other_reset)")},
            {"golden_top.v": TOP.replace(b"if (~system_reset_n)", b"if (system_reset_n == 0)")},
            {"golden_top.v": TOP.replace(b"endmodule", b"wire another_use = system_reset_n;\nendmodule")},
            {PLL: original[PLL].replace(b">50.0<", b">48.0<")},
            {PLL: original[PLL].replace(b">100.0<", b">125.0<")},
            {PLL: original[PLL].replace(b">true<", b">false<")},
            {"golden_top.qsf": original["golden_top.qsf"].replace(b"PIN_DF35", b"PIN_AB1")},
            {"golden_top.qsf": original["golden_top.qsf"].replace(b'"1.1-V"', b'"1.2-V"')},
            {"ghrd_timing.sdc": original["ghrd_timing.sdc"].replace(b"-name MAIN_CLOCK", b"-name OTHER_CLOCK")},
        )
        for index, changed in enumerate(cases):
            with self.subTest(change=index):
                archive, selected, _ = self.archive(changed=changed)
                with self.assertRaisesRegex(ValueError, "contract changed|connection changed|patch target changed|unreviewed"):
                    self.prepare(archive, selected, self.root / ("contract-change-" + str(index)))

    def test_changed_nested_emif_reference_frequency_or_top_connection_is_rejected(self):
        original = vendor_files()
        cases = (
            {EMIF: original[EMIF].replace(b">166.6666<", b">200.0<")},
            {EMIF: original[EMIF].replace(b">false<", b">true<")},
            {"golden_top.v": original["golden_top.v"].replace(
                b'emif_hps_emif_ref_clk_0_clk (LPDDR4A_REFCLK_p)',
                b'emif_hps_emif_ref_clk_0_clk (CLOCK2_50)')},
        )
        for index, changed in enumerate(cases):
            with self.subTest(change=next(iter(changed))):
                archive, selected, _ = self.archive(changed=changed)
                with self.assertRaisesRegex(ValueError, "reference frequency changed|reviewed EMIF reference port"):
                    self.prepare(archive, selected, self.root / ("changed-" + str(index)))

    def test_parameter_inventory_includes_packaged_emif_ddr_settings(self):
        archive, selected, _ = self.archive()
        output = self.prepare(archive, selected)
        inventory = BUILD.module_parameters(output / "project" / EMIF)
        self.assertEqual(inventory["SYSINFO_DEVICE"], BUILD.DEVICE)
        self.assertEqual(inventory["submodule/emif_0_lpddr4/MEM_CHANNEL_CS_WIDTH"], "1")
        self.assertEqual(inventory["submodule/emif_0_lpddr4/PHY_REFCLK_FREQ_MHZ"], "166.6666")
        recorded = json.loads((output / "inputs.json").read_text())
        self.assertEqual(recorded["vendor_hps_emif_parameters"][EMIF], inventory)

    def test_generation_rejects_changed_original_hps_and_nested_ddr_parameters(self):
        cases = ((HPS, "H2F_Width", "64"), (EMIF, "MEM_CHANNEL_CS_WIDTH", "2"))
        for index, (name, parameter_id, value) in enumerate(cases):
            with self.subTest(parameter=parameter_id):
                archive, selected, files = self.archive()
                output = self.prepare(archive, selected, self.root / ("generation-" + str(index)))
                tool = self.generation_tool(self.root / ("tools-" + str(index)))

                def injected_generation(argv, directory, log, env=None):
                    # Only the parameter-review gate is under test. No fake
                    # Quartus result or successful compiler evidence is emitted.
                    Path(log).write_text("test-injected parameter mutation\n")
                    if "--generate_project_ip_files" in argv:
                        path = Path(directory) / name
                        tree = ET.parse(path)
                        parameters = tree.findall(".//{" + IP + "}parameter")
                        item = next(item for item in parameters
                                    if item.get("parameterId") == parameter_id)
                        item.find("{" + IP + "}value").text = value
                        tree.write(path, encoding="utf-8", xml_declaration=True)

                with mock.patch.object(BUILD, "run_logged", side_effect=injected_generation):
                    with self.assertRaisesRegex(ValueError, "changed original HPS/EMIF parameters"):
                        BUILD.generate_project(output, tool, "parameter-review unit test")
                report = json.loads((output / "hps-emif-parameter-review.json").read_text())
                changes = report["changed_original_module_parameters"][name]
                key = parameter_id if name == HPS else "submodule/emif_0_lpddr4/" + parameter_id
                self.assertEqual(changes[key]["after"], value)
                self.assertEqual(report["boot_firmware_compatibility"], "not-qualified")
                self.assertEqual((output / "project" / BOOT).read_bytes(), files[BOOT])
                self.assertFalse((output / "build.json").exists())

    def test_generation_reviews_original_parameters_and_boot_data_without_promoting_acceptance(self):
        for mutate_boot in (False, True):
            with self.subTest(mutate_boot=mutate_boot):
                archive, selected, files = self.archive()
                output = self.prepare(archive, selected,
                                      self.root / ("boot-review-" + str(mutate_boot)))
                tool = self.generation_tool(self.root / ("boot-tools-" + str(mutate_boot)))

                def injected_generation(argv, directory, log, env=None):
                    Path(log).write_text("test parameter/boot review stage\n")
                    if "--generate_project_ip_files" in argv:
                        self.write_handoff(Path(directory))
                        if mutate_boot:
                            (Path(directory) / BOOT).write_bytes(b"changed original vendor boot data")

                with mock.patch.object(BUILD, "run_logged", side_effect=injected_generation):
                    if mutate_boot:
                        with self.assertRaisesRegex(ValueError, "changed vendor boot data"):
                            BUILD.generate_project(output, tool, "parameter-review unit test")
                    else:
                        BUILD.generate_project(output, tool, "parameter-review unit test")
                        self.assertEqual((output / "project" / BOOT).read_bytes(), files[BOOT])
                report = json.loads((output / "hps-emif-parameter-review.json").read_text())
                self.assertEqual(report["changed_original_module_parameters"], {})
                self.assertEqual(report["boot_firmware_compatibility"], "not-qualified")
                self.assertFalse((output / "build.json").exists())
                if not mutate_boot:
                    generated = json.loads((output / "generation.json").read_text())
                    self.assertEqual(generated["mailbox_instance"], "subsys_periph.fes_mailbox")
                    self.assertEqual(generated["physical_mailbox_address"], "not-established")

    def test_generated_handoff_requires_instance_upstream_bridge_offset_clock_and_reset(self):
        project = self.root / "handoff-project"
        path = self.write_handoff(project)
        verified = BUILD.verify_mailbox_handoff(project)
        self.assertEqual(verified["peripheral_offset"], BUILD.mailbox_base())
        self.assertEqual(verified["clock_hz"], 100000000)
        self.assertEqual(verified["sha256"], BUILD.digest(path))
        self.assertEqual(verified["physical_mailbox_address"], "not-established")
        for fault in ("wrong_kind", "duplicate_instance", "wrong_base", "wrong_clock_rate",
                      "missing_upstream_bridge", "missing_mailbox_bridge", "missing_clock", "missing_reset"):
            with self.subTest(fault=fault):
                tree = handoff_tree()
                root = tree.getroot()
                if fault == "wrong_kind":
                    root.find("module").set("kind", "unrelated_registers")
                elif fault == "duplicate_instance":
                    root.append(copy.deepcopy(root.find("module")))
                elif fault in ("wrong_base", "wrong_clock_rate"):
                    name = "baseAddress" if fault == "wrong_base" else "clockRateSysInfo"
                    item = root.find("connection/parameter[@name='" + name + "']/value")
                    item.text = "0x0" if fault == "wrong_base" else "50000000"
                else:
                    index = {"missing_upstream_bridge": 0, "missing_mailbox_bridge": 1,
                             "missing_clock": 2, "missing_reset": 3}[fault]
                    root.remove(root.findall("connection")[index])
                self.write_handoff(project, tree)
                with self.assertRaisesRegex(ValueError, "mailbox instance|connectivity differs|offset or vendor clock"):
                    BUILD.verify_mailbox_handoff(project)

    def test_failed_subprocess_retains_diagnostic_output(self):
        log = self.root / "failed-command.log"
        with self.assertRaisesRegex(ValueError, r"command failed \(7\); retained"):
            BUILD.run_logged([sys.executable, "-c", "print('DE25 diagnostic command failed'); raise SystemExit(7)"],
                             self.root, log)
        self.assertIn("DE25 diagnostic command failed", log.read_text())

    def test_compile_rejects_missing_or_empty_sof_without_success_receipt(self):
        for empty_file in (False, True):
            with self.subTest(empty_file=empty_file):
                archive, selected, _ = self.archive()
                output = self.prepare(archive, selected,
                                      self.root / ("missing-sof-" + str(empty_file)))
                if empty_file:
                    artifact = output / "project/output_files/golden_top.sof"
                    artifact.parent.mkdir()
                    artifact.write_bytes(b"")
                with mock.patch.object(BUILD, "run_logged"):
                    with self.assertRaisesRegex(ValueError, "no nonempty HPS diagnostic SOF"):
                        BUILD.compile_project(output, self.root / "quartus_sh")
                self.assertFalse((output / "build.json").exists())

    @unittest.skipUnless(shutil.which("tclsh"), "Tcl parser is required for quoting round-trip")
    def test_absolute_output_path_is_literal_in_generated_tcl(self):
        archive, selected, _ = self.archive()
        output = self.prepare(archive, selected, self.root /
                              'output with $undefined [error FES_UNSAFE] "quotes" \\slash\nnewline')
        script = (output / "project/integrate-mailbox.tcl").read_text()
        assignment = next(line for line in script.splitlines()
                          if line.startswith("set peripheral_system "))
        result = subprocess.run(
            [shutil.which("tclsh")], text=True, capture_output=True, check=True,
            input=assignment + '\nputs [binary encode hex [encoding convertto utf-8 $peripheral_system]]\n')
        expected = str(output / "project/peripheral_subsys/peripheral_subsys.qsys")
        self.assertEqual(result.stdout.strip(), expected.encode().hex(), result.stderr)
        self.assertNotIn("@PERIPHERAL_SYSTEM@", script)
        self.assertNotIn("@MAILBOX_BASE@", script)


if __name__ == "__main__":
    unittest.main()
