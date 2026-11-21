# SPDX-License-Identifier: MIT
"""The pc386 Quartus seal's shared definitions (scripts/pc386_quartus.py):
sources, guest rates, machine netlist, project, timing gate, resources, tool
authentication and lockstep gate. No Quartus or Yosys needed."""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import sys
import tempfile
import unittest
from unittest import mock

from scripts import pc386_quartus as quartus
from scripts import fes_build_common as board
from scripts import lockfile
from scripts import yosys_netlist

ROOT = Path(__file__).resolve().parents[1]
BUILD_ID = "0123456789abcdef0123456789abcdef"
IDENTITIES = {"quartus_sh": "version=17.0.2; sha256=" + "1" * 64, "yosys": "commit=1f7d0273; sha256=" + "2" * 64}


# TimeQuest's clock table and multicorner summary, as Quartus 17.0.2 writes
# them for the pc386 shell (seed 1 of the M4 shell, which missed setup), and
# the Fitter Summary's resource rows.
CLOCK_TABLE = """\
; Clock Name                                                 ; Type      ; Period ; Frequency  ; Rise  ; Fall   ;
; FPGA_CLK1_50                                               ; Base      ; 20.000 ; 50.0 MHz   ; 0.000 ; 10.000 ;
; system_clock|pll|general[0].gpll~FRACTIONAL_PLL|vcoph[0]   ; Generated ; 2.325  ; 430.11 MHz ; 0.000 ; 1.162  ;
; system_clock|pll|general[0].gpll~PLL_OUTPUT_COUNTER|divclk ; Generated ; 16.276 ; 61.44 MHz  ; 0.000 ; 8.138  ;
; system_clock|pll|general[1].gpll~PLL_OUTPUT_COUNTER|divclk ; Generated ; 81.380 ; 12.29 MHz  ; 0.000 ; 40.690 ;
; video_clock|pll|general[0].gpll~FRACTIONAL_PLL|vcoph[0]    ; Generated ; 2.244  ; 445.63 MHz ; 0.000 ; 1.122  ;
; video_clock|pll|general[0].gpll~PLL_OUTPUT_COUNTER|divclk  ; Generated ; 13.464 ; 74.27 MHz  ; 0.000 ; 6.732  ;
"""
MISSED = """\
; Worst-case Slack                                            ; -2.454  ; 0.107 ; 7.297    ; 0.394   ; 1.122               ;
; Design-wide TNS                                             ; -49.849 ; 0.0   ; 0.0      ; 0.0     ; 0.0                 ;
"""
MET = """\
; Worst-case Slack                                            ; 0.212   ; 0.107 ; 7.297    ; 0.394   ; 1.122               ;
; Design-wide TNS                                             ; 0.0     ; 0.0   ; 0.0      ; 0.0     ; 0.0                 ;
"""
UNCONSTRAINED = """\
; Illegal Clocks                  ; 0     ; 0    ;
; Unconstrained Clocks            ; 0     ; 0    ;
; Unconstrained Input Ports       ; 2     ; 2    ;
"""
FIT_SUMMARY = """\
; Logic utilization (in ALMs)     ; 9,754 / 41,910 ( 23 % )                     ;
; Total registers                 ; 12637                                       ;
; Total pins                      ; 33 / 314 ( 11 % )                           ;
; Total block memory bits         ; 151,955 / 5,662,720 ( 3 % )                 ;
; Total RAM Blocks                ; 53 / 553 ( 10 % )                           ;
; Total DSP Blocks                ; 21 / 112 ( 19 % )                           ;
"""


class SourcesTest(unittest.TestCase):
    def test_shell_and_machine_split_the_sources(self):
        self.assertFalse(set(quartus.SHELL_SOURCES) & set(quartus.MACHINE_SOURCES))
        self.assertEqual(len(set(quartus.MACHINE_SOURCES)), len(quartus.MACHINE_SOURCES))
        self.assertEqual(quartus.MACHINE_SOURCES[-1], "cores/fes-pc386/rtl/pc386_system.sv")
        self.assertIn("cores/fes-common/rtl/i386/fes_i386_cpu.sv", quartus.MACHINE_SOURCES)
        self.assertIn("cores/fes-pc386/rtl/pc386_rom.sv", quartus.MACHINE_SOURCES)
        self.assertNotIn("cores/fes-pc386/rtl/top.sv", quartus.MACHINE_SOURCES)

    def test_pinned_inputs_exist(self):
        for path in quartus.PINNED_INPUTS:
            self.assertTrue((ROOT / path).is_file(), path)
        self.assertIn(quartus.FIRMWARE_LOCK, quartus.PINNED_INPUTS)

    def test_the_rom_contents_are_where_pc386_rom_reads_them(self):
        default = re.search(r'parameter ROM_HEX = "([^"]+)"', (ROOT / "cores/fes-pc386/rtl/pc386_rom.sv").read_text())
        self.assertEqual(default.group(1), quartus.ROM_HEX)

    def test_machine_program_elaborates_at_the_sealed_rates(self):
        program = quartus.machine_program(Path("out/pc386_system.v"))
        for name, value in quartus.guest_rates().items():
            self.assertRegex(program, rf"-chparam {name} {value}[ ;]")
        self.assertIn(f"hierarchy -top {quartus.MACHINE_TOP} ", program)
        self.assertIn("memory_map t:$mem_v2 r:WR_PORTS>1 %i a:ramstyle %d;", program)
        # Flattened names become plain in the same run, with the attributes kept (#1581 S4).
        self.assertTrue(program.endswith("attrmap -remove hdlname; rename -unescape; write_verilog out/pc386_system.v"))
        for source in quartus.SHELL_SOURCES:
            self.assertNotIn(source, program)


class RatesTest(unittest.TestCase):
    def test_rates_at_100_mhz_are_the_machine_defaults(self):
        rates = quartus.guest_rates(100_000_000)
        expected = {"ISA": (1, 12), "PIT": (21, 880), "US": (1, 100), "DMA": (1, 24), "DOT0": (1007, 4000),
                    "DOT1": (14161, 50000), "OPL2": (7, 14080), "AUDIO": (3, 6250), "GOV": (1, 1000)}
        for name, pair in expected.items():
            self.assertEqual((rates[f"{name}_N"], rates[f"{name}_D"]), pair, name)
        self.assertEqual(rates["RTC_N"] / rates["RTC_D"], 256 / 781250)
        self.assertEqual(rates["ATA_RESET_CLOCKS"], 1000)
        with self.assertRaises(board.BuildError):
            quartus.guest_rates(50_000_000)

    def test_the_shell_defaults_are_the_sealed_rates(self):
        top = (ROOT / "cores/fes-pc386/rtl/top.sv").read_text()
        for name, value in quartus.guest_rates().items():
            self.assertRegex(top, rf"\b{name} = {value}\b")


class SealProgramTest(unittest.TestCase):
    def test_the_machine_is_the_seal_recipe_at_its_top(self):
        program = quartus.machine_program(Path("out/pc386_system.v"))
        cpu = yosys_netlist.program(yosys_netlist.SEAL, top=yosys_netlist.CPU_TOP, sources=yosys_netlist.CPU_SOURCES,
                                    includes=yosys_netlist.PC386_INCLUDES, netlist="out/pc386_system.v")
        # Only the top, the sources and the parameters differ from the lockstep's CPU program.
        def passes(text):
            return text.split("; hierarchy -top ", 1)[1].split("; ", 1)[1]
        self.assertEqual(passes(program), passes(cpu))
        self.assertTrue(program.startswith("read_verilog -sv -I cores/fes-common/generated "))
        self.assertEqual(quartus.MACHINE_SOURCES[:len(yosys_netlist.CPU_SOURCES)], yosys_netlist.CPU_SOURCES)
        self.assertEqual(quartus.INCLUDE_DIRS, yosys_netlist.PC386_INCLUDES)
        self.assertIn("scripts/yosys_netlist.py", quartus.PINNED_INPUTS)


class ToolchainFailureTest(unittest.TestCase):
    def test_missing_shared_slot_says_how_to_provision_the_locked_toolchain(self):
        cache_root = Path("/tmp/fes-toolchains")
        missing = yosys_netlist.NetlistError(
            "cores/fes-pc386/toolchain.lock Yosys (HIP): shared toolchain verification failed: "
            "missing path component: /tmp/fes-toolchains/slots/missing"
        )
        with mock.patch.object(yosys_netlist, "authenticate", side_effect=missing):
            with self.assertRaisesRegex(
                board.BuildError,
                r"make toolchain-fes-pc386 CACHE_ROOT=/tmp/fes-toolchains GPU_ROUTER=HIP",
            ):
                quartus._authenticate_tools(ROOT, cache_root=cache_root, gpu_router="HIP")

    def test_non_missing_toolchain_errors_do_not_suggest_reprovisioning(self):
        failure = (
            "cores/fes-pc386/toolchain.lock Yosys (HIP): shared toolchain verification failed: "
            "ready manifest key does not match"
        )
        with mock.patch.object(yosys_netlist, "authenticate", side_effect=yosys_netlist.NetlistError(failure)):
            with self.assertRaisesRegex(board.BuildError, "ready manifest key does not match") as raised:
                quartus._authenticate_tools(ROOT, cache_root=Path("/tmp/fes-toolchains"), gpu_router="HIP")
        self.assertNotIn("toolchain-fes-pc386", str(raised.exception))


class ProjectTest(unittest.TestCase):
    def test_project_reads_the_netlist_and_the_shell(self):
        with tempfile.TemporaryDirectory() as temporary:
            project = Path(temporary) / "project"
            project.mkdir()
            text = quartus.project_qsf(ROOT, project, BUILD_ID, 3, threads=2)
        self.assertIn('set_global_assignment -name VERILOG_MACRO "QUARTUS=1"', text)
        self.assertIn(f"FES_PC386_BUILD_ID=128'h{BUILD_ID}", text)
        self.assertIn("set_global_assignment -name SEED 3", text)
        self.assertIn(f"HPS_LOCATION {quartus.I2C_SITE} -entity top -to hdmi_i2c", text)
        self.assertIn(f"set_global_assignment -name VERILOG_FILE {quartus.MACHINE_NETLIST}", text)
        self.assertIn('SYSTEMVERILOG_FILE "', text)
        self.assertIn("cores/fes-pc386/rtl/top.sv", text)
        self.assertIn(quartus.SDC, text)
        self.assertIn("set_location_assignment PIN_V11 -to FPGA_CLK1_50", text)
        for source in quartus.MACHINE_SOURCES:
            self.assertNotIn(source, text)

    def test_project_threads_are_the_callers(self):
        with tempfile.TemporaryDirectory() as temporary:
            project = Path(temporary)
            self.assertIn("NUM_PARALLEL_PROCESSORS ALL", quartus.project_qsf(ROOT, project, BUILD_ID, 1, threads="ALL"))
            self.assertIn("NUM_PARALLEL_PROCESSORS 2", quartus.project_qsf(ROOT, project, BUILD_ID, 1, threads=2))

    def test_project_rejects_a_bad_build_id_and_seed(self):
        with self.assertRaises(board.BuildError):
            quartus.project_qsf(ROOT, ROOT, "XYZ", 1, threads=2)
        with self.assertRaises(board.BuildError):
            quartus.project_qsf(ROOT, ROOT, BUILD_ID, 0, threads=2)


class TimingTest(unittest.TestCase):
    def test_a_met_report_passes_with_each_clock(self):
        timing = quartus.validate_timing_report(CLOCK_TABLE + MET + UNCONSTRAINED)
        self.assertEqual(timing["analyses"]["setup"], {"worst_slack_ns": 0.212, "tns_ns": 0.0})
        self.assertEqual({label: clock["mhz"] for label, clock in timing["clocks"].items()},
                         {"system": 61.44, "audio": 12.29, "pixel": 74.27})

    def test_a_setup_miss_fails(self):
        with self.assertRaisesRegex(board.BuildError, "Setup timing fails"):
            quartus.validate_timing_report(CLOCK_TABLE + MISSED + UNCONSTRAINED)

    def test_a_miss_that_rounds_to_zero_fails(self):
        with self.assertRaisesRegex(board.BuildError, "Setup timing fails"):
            quartus.validate_timing_report(CLOCK_TABLE + MET.replace("; 0.212  ", "; -0.000 ") + UNCONSTRAINED)
        with self.assertRaisesRegex(board.BuildError, "Setup timing fails"):
            quartus.validate_timing_report(CLOCK_TABLE + MET.replace("; 0.0     ; 0.0   ;", "; -0.000  ; 0.0   ;")
                                           + UNCONSTRAINED)

    def test_a_clock_timequest_cannot_analyse_fails(self):
        unconstrained = UNCONSTRAINED.replace("Unconstrained Clocks            ; 0     ; 0 ",
                                              "Unconstrained Clocks            ; 1     ; 1 ")
        with self.assertRaisesRegex(board.BuildError, "unconstrained clocks: 1 setup"):
            quartus.validate_timing_report(CLOCK_TABLE + MET + unconstrained)
        with self.assertRaisesRegex(board.BuildError, "unconstrained paths summary"):
            quartus.validate_timing_report(CLOCK_TABLE + MET)

    def test_a_wrong_or_missing_clock_fails(self):
        with self.assertRaisesRegex(board.BuildError, "system clock is 54.0 MHz"):
            quartus.validate_timing_report(CLOCK_TABLE.replace("16.276 ; 61.44 MHz", "18.519 ; 54.0 MHz") + MET
                                           + UNCONSTRAINED)
        with self.assertRaisesRegex(board.BuildError, "pixel clock"):
            quartus.validate_timing_report(CLOCK_TABLE.replace("video_clock", "other_clock") + MET + UNCONSTRAINED)
        with self.assertRaisesRegex(board.BuildError, "worst-case slack"):
            quartus.validate_timing_report(CLOCK_TABLE)


class ResourcesTest(unittest.TestCase):
    def test_the_fit_summary_gives_each_resource(self):
        self.assertEqual(quartus.fit_resources(FIT_SUMMARY), {
            "alms": {"used": 9754, "available": 41910}, "registers": {"used": 12637},
            "ram_blocks": {"used": 53, "available": 553}, "block_memory_bits": {"used": 151955, "available": 5662720},
            "dsp_blocks": {"used": 21, "available": 112},
        })

    def test_a_missing_row_fails(self):
        with self.assertRaisesRegex(board.BuildError, "Total DSP Blocks"):
            quartus.fit_resources(FIT_SUMMARY.replace("Total DSP Blocks", "Total DSPs"))


class GpuRouterTest(unittest.TestCase):
    """The router selects which of the toolchain lock's slots is authenticated."""

    def test_an_unknown_router_fails(self):
        with self.assertRaisesRegex(board.BuildError, "unsupported GPU router"):
            quartus._authenticate_tools(ROOT, gpu_router="Metal")

    def authenticate(self, **kwargs):
        yosys = mock.Mock(identity=IDENTITIES["yosys"])
        with tempfile.TemporaryDirectory() as temporary:
            quartus_sh = Path(temporary) / "quartus_sh"
            quartus_sh.write_bytes(b"not quartus")
            with mock.patch.object(quartus.board, "_authenticate_tools", return_value={"yosys": yosys}) as shared, \
                    mock.patch.object(quartus, "locate_quartus", return_value=(Path(temporary), quartus_sh, "v")):
                quartus._authenticate_tools(ROOT, **kwargs)
        return shared.call_args.kwargs

    def test_each_router_authenticates_its_own_slot(self):
        hip = self.authenticate()
        self.assertEqual((hip["gpu_router"], hip["hip_architectures"], hip["expected_configuration"]),
                         ("HIP", board.FES_GPU_ARCHITECTURES, {"nextpnr": board.FES_TOOLCHAIN_CONFIGURATION}))
        with mock.patch.object(quartus.board, "cuda_toolchain_configuration", return_value="cuda-lane"):
            cuda = self.authenticate(gpu_router="CUDA", cache_root=Path("/cache"))
        self.assertEqual((cuda["gpu_router"], cuda["hip_architectures"], cuda["expected_configuration"],
                          cuda["cache_root"], cuda["lock_path"]),
                         ("CUDA", "unused", {"nextpnr": "cuda-lane"}, Path("/cache"), ROOT / quartus.LOCK))

class LockstepGateTest(unittest.TestCase):
    """The sealer's --lockstep-report gate (#1581 S5)."""

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.output = Path(self.temporary.name)
        self.yosys = mock.Mock(path=Path("yosys"), identity=IDENTITIES["yosys"])
        self.netlist = b"module fes_i386_cpu(); endmodule\n"
        verilator = lockfile.load_lock(ROOT / quartus.I386_LOCK)["verilator"].commit
        self.report = {
            "run": {"passed": True, "default_suite": True, "complete_corpus": True, "arguments": {}},
            "oracle": {**quartus.oracle_identity(ROOT), "linked_sha256": {
                name: "1" * 64 for name in quartus.LINKED_NAMES}, "testbench_sha256": "2" * 64},
            "verilator": {"lock": quartus.I386_LOCK, "commit": verilator, "sha256": "v", "version": "V",
                          "slot": "s"},
            "netlist": {"program_sha256": yosys_netlist.program_sha256(yosys_netlist.SEAL),
                        "cpu_source_sha256": yosys_netlist.cpu_source_sha256(ROOT),
                        "netlist_sha256": hashlib.sha256(self.netlist).hexdigest(),
                        "yosys": yosys_netlist.lock_record(ROOT, self.yosys, "HIP", quartus.LOCK)},
        }

    def tearDown(self):
        self.temporary.cleanup()

    def check(self, report, netlist=None):
        def regenerate(yosys, output, root, **_):
            output.mkdir(parents=True, exist_ok=True)
            (output / "fes_i386_cpu.netlist.v").write_bytes(self.netlist if netlist is None else netlist)
            return output / "fes_i386_cpu.netlist.v"
        data = json.dumps(report).encode()
        with mock.patch.object(quartus.yosys_netlist, "seal_netlist", side_effect=regenerate) as seal:
            result = quartus.check_lockstep_report(ROOT, data, self.yosys, "HIP", self.output)
        self.assertEqual(seal.call_args.args, (Path("yosys"), self.output / "lockstep", ROOT))
        return data, result

    def refused(self, report, pattern, netlist=None):
        with self.assertRaisesRegex(board.BuildError, pattern):
            self.check(report, netlist)

    def test_a_matching_report_seals_and_is_recorded(self):
        data, result = self.check(self.report)
        self.assertEqual(result, {
            "report_sha256": hashlib.sha256(data).hexdigest(),
            "netlist_sha256": self.report["netlist"]["netlist_sha256"],
            "netlist_program_sha256": self.report["netlist"]["program_sha256"],
            "yosys_tool": IDENTITIES["yosys"],
            "cpu_source_closure_sha256": self.report["netlist"]["cpu_source_sha256"],
            "simulator": self.report["verilator"], "oracle": self.report["oracle"]})

    def test_a_failed_or_partial_run_is_refused(self):
        with self.assertRaisesRegex(board.BuildError, "not JSON"):
            quartus.check_lockstep_report(ROOT, b"{", self.yosys, "HIP", self.output)
        self.refused({**self.report, "run": {"passed": False, "default_suite": True}}, "did not pass")
        self.refused({key: value for key, value in self.report.items() if key != "run"}, "did not pass")
        self.refused({**self.report, "run": {"passed": True, "default_suite": False, "complete_corpus": True,
                                             "arguments": {"opcode": "x"}}},
                     "not the default suite")
        self.refused({key: value for key, value in self.report.items() if key != "netlist"}, "not a --netlist run")

    def test_a_default_suite_report_must_prove_a_complete_corpus(self):
        legacy = {**self.report["run"]}
        del legacy["complete_corpus"]
        self.refused({**self.report, "run": legacy}, "does not prove a complete corpus")
        self.refused({**self.report, "run": {**self.report["run"], "complete_corpus": False}},
                     "does not prove a complete corpus")

    def test_each_input_identity_must_match(self):
        for key, value in (("program_sha256", "0" * 64), ("cpu_source_sha256", "0" * 64),
                           ("yosys", {**self.report["netlist"]["yosys"], "tool": "commit=x; sha256=y"}),
                           ("yosys", {**self.report["netlist"]["yosys"], "gpu_router": "CUDA"}),
                           ("yosys", {**self.report["netlist"]["yosys"], "lock_sha256": "0" * 64})):
            with self.subTest(key=key, value=value):
                self.refused({**self.report, "netlist": {**self.report["netlist"], key: value}},
                             f"netlist {key} is")

    def test_oracle_identity_and_digest_evidence_are_required(self):
        self.refused({key: value for key, value in self.report.items() if key != "oracle"}, "oracle identity")
        for key in ("revision", "patch_sha256", "provisioner_sha256", "adapter_sha256", "suite_sha256"):
            with self.subTest(key=key):
                self.refused({**self.report, "oracle": {**self.report["oracle"], key: "0" * 64}}, "oracle identity")
        for change in ({"linked_sha256": {}}, {"testbench_sha256": "invalid"}):
            with self.subTest(change=change):
                self.refused({**self.report, "oracle": {**self.report["oracle"], **change}}, "binary digests")
        # Host-specific binaries are evidence, not cross-host equality inputs.
        varied = {**self.report["oracle"], "testbench_sha256": "3" * 64,
                  "linked_sha256": {name: "4" * 64 for name in quartus.LINKED_NAMES}}
        self.check({**self.report, "oracle": varied})
        with tempfile.TemporaryDirectory() as empty, self.assertRaises(board.BuildError):
            quartus.oracle_identity(Path(empty))
        with self.assertRaisesRegex(board.BuildError, "changed during build"):
            quartus.require_same_oracle(ROOT, {**self.report["oracle"], "patch_sha256": "0" * 64})

    def test_oracle_provisioner_changed_during_the_build_is_refused(self):
        # The seal reads the provisioner as data (#1984 S1): an edit after the
        # lockstep gate moves provisioner_sha256, which the final check refuses.
        with tempfile.TemporaryDirectory() as temporary:
            other = Path(temporary)
            for name in ("scripts/fes_i386_oracle.py", quartus.BOCHS_PATCH, *quartus.SUITE_SOURCES,
                         *(f"{quartus.ORACLE_SIM}/{name}" for name in quartus.ORACLE_SOURCES)):
                (other / name).parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(ROOT / name, other / name)
            # The module that defines oracle_identity holds the running checkout.
            with mock.patch.object(sys.modules[quartus.oracle_identity.__module__], "ROOT", other):
                expected = quartus.oracle_identity(other)
                script = other / "scripts/fes_i386_oracle.py"
                script.write_bytes(script.read_bytes() + b"\n")
                with self.assertRaisesRegex(board.BuildError, "changed during build"):
                    quartus.require_same_oracle(other, expected)

    def test_cross_root_oracle_source_must_match_running_pin(self):
        with tempfile.TemporaryDirectory() as temporary:
            other = Path(temporary)
            paths = ("scripts/fes_i386_oracle.py", quartus.BOCHS_PATCH,
                     *quartus.SUITE_SOURCES,
                     *(f"cores/fes-common/sim/i386/{name}" for name in quartus.ORACLE_SOURCES))
            for name in paths:
                target = other / name
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(ROOT / name, target)
            script = other / "scripts/fes_i386_oracle.py"
            contents = script.read_bytes()
            self.assertEqual(quartus.oracle_identity(other), quartus.oracle_identity(ROOT))
            script.write_bytes(contents + b"\n# an edited provisioner\n")
            with self.assertRaisesRegex(board.BuildError, "target oracle provisioning script differs"):
                quartus.oracle_identity(other)
            script.write_bytes(contents)
            adapter = other / "cores/fes-common/sim/i386/bochs_oracle.cpp"
            adapter.write_bytes(adapter.read_bytes() + b"\n")
            self.assertNotEqual(quartus.oracle_identity(other)["adapter_sha256"], self.report["oracle"]["adapter_sha256"])
            bench = other / "cores/fes-common/sim/i386/lockstep_tb.cpp"
            bench.write_bytes(bench.read_bytes() + b"\n")
            self.assertNotEqual(quartus.oracle_identity(other)["suite_sha256"], self.report["oracle"]["suite_sha256"])

    def test_the_verilator_must_be_the_i386_locks(self):
        self.refused({**self.report, "verilator": {**self.report["verilator"], "commit": "0" * 40}}, "Verilator")
        self.refused({**self.report, "verilator": {**self.report["verilator"], "lock": quartus.LOCK}}, "Verilator")

    def test_the_regenerated_cpu_netlist_must_match(self):
        self.refused(self.report, "CPU netlist", netlist=b"module other(); endmodule\n")

    def test_the_gate_reads_pinned_inputs(self):
        for path in (quartus.I386_LOCK, "scripts/lockfile.py", "scripts/yosys_netlist.py",
                     "scripts/fes_i386_oracle.py", quartus.BOCHS_PATCH):
            self.assertIn(path, quartus.PINNED_INPUTS)
        for name in quartus.ORACLE_SOURCES:
            self.assertIn(f"cores/fes-common/sim/i386/{name}", quartus.PINNED_INPUTS)
        for name in quartus.SUITE_SOURCES:
            self.assertIn(name, quartus.PINNED_INPUTS)
        self.assertIn(quartus.LOCKSTEP_REPORT, quartus.BUILD_OUTPUTS)


class QuartusLockGoldenTest(unittest.TestCase):
    """Every Quartus version string the seal writes derives from quartus.lock."""

    def test_the_locks_are_pinned_inputs(self):
        self.assertIn(quartus.QUARTUS_LOCK, quartus.PINNED_INPUTS)
        self.assertIn(quartus.LOCK, quartus.PINNED_INPUTS)
        self.assertEqual(quartus.LOCK, "cores/fes-pc386/toolchain.lock")

    def test_project_version_lines(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            project = quartus.write_project(ROOT, output, BUILD_ID, 1, threads=2)
            qpf = (project / "top.qpf").read_text(encoding="utf-8")
            qsf = (project / "top.qsf").read_text(encoding="utf-8")
        self.assertEqual(qpf, 'QUARTUS_VERSION = "17.0";\nPROJECT_REVISION = "top";\n')
        self.assertIn('set_global_assignment -name LAST_QUARTUS_VERSION "17.0.2 Lite Edition"', qsf.splitlines())

    def test_quartus_identity_and_version_line(self):
        version = "Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition"
        with tempfile.TemporaryDirectory() as temporary:
            quartus_sh = Path(temporary) / "quartus_sh"
            quartus_sh.write_bytes(b"not quartus")
            digest = hashlib.sha256(b"not quartus").hexdigest()
            yosys = mock.Mock(identity=IDENTITIES["yosys"])
            with mock.patch.object(quartus.board, "_authenticate_tools", return_value={"yosys": yosys}), \
                    mock.patch.object(quartus, "locate_quartus",
                                      return_value=(Path(temporary), quartus_sh, version)) as locate, \
                    mock.patch.dict(os.environ, {"QUARTUS_ROOTDIR": temporary}):
                tools = quartus._authenticate_tools(ROOT)
        lock = locate.call_args.args[2]
        self.assertEqual(lock.path, "cores/fes-pc386/quartus.lock")
        self.assertEqual(lock.version, version)
        self.assertEqual(tools["identities"], {"yosys": IDENTITIES["yosys"],
                                               "quartus_sh": f"version=17.0.2; sha256={digest}"})
        self.assertEqual(tools["quartus_version"], version)

    def test_a_missing_or_bad_lock_fails_as_a_build_error(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            with self.assertRaisesRegex(board.BuildError, "not a regular file"):
                quartus.quartus_lock(root)
            lock = root / quartus.QUARTUS_LOCK
            lock.parent.mkdir(parents=True)
            shutil.copyfile(ROOT / quartus.QUARTUS_LOCK, lock)
            self.assertEqual(quartus.quartus_lock(root).qsf_version, "17.0.2 Lite Edition")
            lock.write_text(lock.read_text().replace('edition = "lite"', 'edition = "pro"'))
            with self.assertRaises(board.BuildError):
                quartus.quartus_lock(root)


if __name__ == "__main__":
    unittest.main()
