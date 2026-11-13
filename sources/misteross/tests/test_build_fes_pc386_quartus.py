# SPDX-License-Identifier: MIT
"""The pc386 Quartus seal's sources, guest rates, machine netlist, project,
timing gate, resources and manifest. No Quartus or Yosys needed."""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import tempfile
import tomllib
import unittest
from unittest import mock

from scripts import build_fes_pc386_quartus as quartus
from scripts import fes_build_common as board
from scripts import lockfile
from scripts import yosys_netlist
from scripts.export_core_package import build_identity

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
            text = quartus.project_qsf(ROOT, project, BUILD_ID, 3)
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

    def test_project_rejects_a_bad_build_id_and_seed(self):
        with self.assertRaises(board.BuildError):
            quartus.project_qsf(ROOT, ROOT, "XYZ", 1)
        with self.assertRaises(board.BuildError):
            quartus.project_qsf(ROOT, ROOT, BUILD_ID, 0)


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


class ManifestTest(unittest.TestCase):
    def test_manifest_is_the_system_labelled_quartus(self):
        record = quartus.create_build_record(ROOT, "https://github.com/DeanoC/fes", "a" * 40, IDENTITIES, 1)
        evidence = {"rbf": {"sha256": "3" * 64, "size": 4096}}
        fields = tomllib.loads(quartus.manifest(ROOT, record, evidence, "https://github.com/DeanoC/fes", "a" * 40,
                                                IDENTITIES).decode())
        self.assertEqual(fields["core"]["id"], "fes.pc386")
        self.assertEqual(fields["core"]["name"], "FES pc386 (Quartus)")
        self.assertEqual(fields["abi"], {"id": "fes.computer", "major": 1, "minor": 1})
        self.assertEqual([item["id"] for item in fields["interfaces"]], list(quartus.INTERFACES))
        self.assertEqual(fields["build"]["id"], build_identity(record))
        self.assertIn("Quartus Prime Lite 17.0.2", fields["build"]["toolchain"])
        self.assertIn("Yosys", fields["build"]["toolchain"])
        parameters = json.loads(record)["parameters"]
        self.assertEqual(parameters["compiler"], "quartus-17.0.2")
        self.assertEqual(json.loads(parameters["guest_rates"]), quartus.guest_rates())
        self.assertEqual(json.loads(parameters["firmware"]), quartus.firmware_digests(ROOT))
        self.assertIn("read_verilog -sv -I cores/fes-common/generated", parameters["machine_netlist"])
        self.assertIn(" $MACHINE_SOURCES; hierarchy -top pc386_system ", parameters["machine_netlist"])
        self.assertEqual(len(parameters["machine_sources_sha256"]), 64)

    def test_the_record_names_its_files_from_the_repository_root(self):
        fields = json.loads(quartus.create_build_record(ROOT, "https://github.com/DeanoC/fes", "a" * 40,
                                                        IDENTITIES, 1))
        self.assertEqual(fields["recipe"], "sources/misteross/scripts/build_fes_pc386_quartus.py")
        self.assertEqual(fields["abi_definition"], "sources/misteross/cores/fes-common/generated/fes_computer.vh")

    def test_the_seed_is_part_of_the_build_identity(self):
        first = quartus.create_build_record(ROOT, "https://github.com/DeanoC/fes", "a" * 40, IDENTITIES, 1)
        second = quartus.create_build_record(ROOT, "https://github.com/DeanoC/fes", "a" * 40, IDENTITIES, 2)
        self.assertNotEqual(build_identity(first), build_identity(second))


class GpuRouterTest(unittest.TestCase):
    """--gpu-router selects the toolchain lock's HIP or CUDA slot, and the record names it."""

    REPOSITORY = "https://github.com/DeanoC/fes"

    def record(self, *args):
        return quartus.create_build_record(ROOT, self.REPOSITORY, "a" * 40, IDENTITIES, 1, *args)

    def test_hip_is_the_default_slot(self):
        self.assertEqual(self.record(), self.record("HIP"))
        parameters = json.loads(self.record())["parameters"]
        self.assertEqual(parameters["gpu_backend"], "hip")
        self.assertEqual(parameters["toolchain_configuration"], board.FES_TOOLCHAIN_CONFIGURATION)

    def test_the_cuda_slot_is_recorded_and_has_its_own_build_id(self):
        with mock.patch.dict(os.environ):
            os.environ.pop("FES_TOOLCHAIN_CUDA_ARCHITECTURES", None)
            cuda = self.record("CUDA")
        parameters = json.loads(cuda)["parameters"]
        self.assertEqual(parameters["gpu_backend"], "cuda")
        self.assertEqual(parameters["toolchain_configuration"], board.FES_CUDA_TOOLCHAIN_CONFIGURATION)
        self.assertNotEqual(build_identity(cuda), build_identity(self.record("HIP")))

    def test_an_unknown_router_fails(self):
        with self.assertRaisesRegex(board.BuildError, "unsupported GPU router"):
            self.record("Metal")
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

    def test_build_keeps_the_router_through_the_final_checks(self):
        tools = {"yosys": mock.Mock(path=Path("yosys"), identity=IDENTITIES["yosys"]), "quartus_sh": Path("quartus_sh"),
                 "quartus_root": Path("q"), "quartus_version": "v", "identities": dict(IDENTITIES)}
        evidence = {"rbf": {"sha256": "0" * 64, "size": 1}}
        with tempfile.TemporaryDirectory() as temporary, \
                mock.patch.object(quartus, "require_clean_source", return_value=(self.REPOSITORY, "a" * 40)), \
                mock.patch.object(quartus, "_authenticate_tools", return_value=tools) as authenticate, \
                mock.patch.object(quartus, "create_build_record", return_value=self.record()) as record, \
                mock.patch.object(quartus.board, "_prepare_output", return_value=Path(temporary)), \
                mock.patch.object(quartus.board, "_write_atomic") as write, \
                mock.patch.object(quartus, "write_project"), mock.patch.object(quartus, "firmware_rom"), \
                mock.patch.object(quartus, "elaborate_machine"), mock.patch.object(quartus, "run_quartus"), \
                mock.patch.object(quartus, "validate_build_evidence", return_value=evidence), \
                mock.patch.object(quartus, "firmware_digests", return_value={}), \
                mock.patch.object(quartus.board, "_sha256", return_value="0" * 64), \
                mock.patch.object(quartus, "manifest", return_value=b""), \
                mock.patch.object(quartus, "export_package", return_value=Path("package")), \
                mock.patch.object(quartus, "check_lockstep_report", return_value={"report_sha256": "r"}) as gate:
            report = Path(temporary) / "report.json"
            report.write_text("{}")
            self.assertEqual(quartus.build(ROOT, cache_root=Path("/cache"), gpu_router="CUDA", lockstep_report=report),
                             Path("package"))
        self.assertEqual(gate.call_args.args[1:4], (b"{}", tools["yosys"], "CUDA"))
        self.assertEqual([call.kwargs for call in authenticate.call_args_list],
                         [{"cache_root": Path("/cache"), "gpu_router": "CUDA"}] * 2)
        self.assertEqual([call.args[-1] for call in record.call_args_list], ["CUDA", "CUDA"])
        summary = next(json.loads(call.args[1]) for call in write.call_args_list
                       if call.args[0].name == "build-summary.json")
        self.assertEqual(summary["gpu_backend"], "cuda")
        self.assertEqual(summary["netlist"]["program_sha256"], yosys_netlist.program_sha256(yosys_netlist.SEAL))
        self.assertEqual(summary["netlist"]["cpu_source_sha256"], yosys_netlist.cpu_source_sha256(ROOT))
        self.assertEqual(summary["netlist"]["yosys"]["tool"], IDENTITIES["yosys"])
        self.assertEqual(summary["netlist"]["yosys"]["gpu_router"], "CUDA")
        self.assertEqual(summary["lockstep"], {"report_sha256": "r"})
        self.assertEqual([call.args[1] for call in write.call_args_list
                          if call.args[0].name == quartus.LOCKSTEP_REPORT], [b"{}"])

    def test_the_command_line_passes_the_router(self):
        for argv, router in (([], "HIP"), (["--gpu-router", "CUDA"], "CUDA")):
            with mock.patch.object(quartus, "build", return_value=Path("package")) as build, \
                    mock.patch("sys.argv", ["build_fes_pc386_quartus.py", "--lockstep-report", "r.json", *argv]), \
                    mock.patch("builtins.print"):
                self.assertEqual(quartus.main(), 0)
            self.assertEqual(build.call_args.kwargs["gpu_router"], router)
            self.assertEqual(build.call_args.kwargs["lockstep_report"], Path("r.json"))
        with mock.patch("sys.argv", ["build_fes_pc386_quartus.py"]), mock.patch("sys.stderr"), \
                self.assertRaises(SystemExit):
            quartus.main()
        with mock.patch("sys.argv", ["build_fes_pc386_quartus.py", "--gpu-router", "Metal"]), \
                mock.patch("sys.stderr"), self.assertRaises(SystemExit):
            quartus.main()


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
            "simulator": self.report["verilator"]})

    def test_a_missing_report_is_refused(self):
        with self.assertRaisesRegex(board.BuildError, "needs --lockstep-report"):
            quartus.build(ROOT, cache_root=Path("/cache"))
        with self.assertRaisesRegex(board.BuildError, "cannot read the lockstep report"):
            quartus.build(ROOT, cache_root=Path("/cache"), lockstep_report=self.output / "absent.json")

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

    def test_the_verilator_must_be_the_i386_locks(self):
        self.refused({**self.report, "verilator": {**self.report["verilator"], "commit": "0" * 40}}, "Verilator")
        self.refused({**self.report, "verilator": {**self.report["verilator"], "lock": quartus.LOCK}}, "Verilator")

    def test_the_regenerated_cpu_netlist_must_match(self):
        self.refused(self.report, "CPU netlist", netlist=b"module other(); endmodule\n")

    def test_the_gate_reads_pinned_inputs(self):
        for path in (quartus.I386_LOCK, "scripts/lockfile.py", "scripts/yosys_netlist.py"):
            self.assertIn(path, quartus.PINNED_INPUTS)
        self.assertIn(quartus.LOCKSTEP_REPORT, quartus.BUILD_OUTPUTS)


class QuartusLockGoldenTest(unittest.TestCase):
    """Every version string the producer writes derives from quartus.lock and
    is byte-identical to the literals the producer wrote before the lock."""

    def test_the_lock_is_pinned_but_not_a_record_field(self):
        self.assertIn(quartus.QUARTUS_LOCK, quartus.PINNED_INPUTS)
        self.assertIn(quartus.LOCK, quartus.PINNED_INPUTS)
        self.assertEqual(quartus.LOCK, "cores/fes-pc386/toolchain.lock")
        fields = json.loads(quartus.create_build_record(ROOT, "https://github.com/DeanoC/fes", "a" * 40,
                                                        IDENTITIES, 1))
        self.assertEqual(set(fields), {"format", "repository", "revision", "recipe", "recipe_sha256",
                                       "abi_definition", "abi_definition_sha256", "dependencies", "tools",
                                       "parameters"})
        self.assertEqual(set(fields["parameters"]), {
            "compiler", "device", "top", "seed", "toolchain_lock", "toolchain_lock_sha256", "system_clock_hz",
            "pixel_clock_hz", "audio_clock_hz", "reference_clock_hz", "audio_sample_hz", "guest_rates", "cpu",
            "firmware", "ata_disk", "machine_netlist", "machine_sources_sha256", "gpu_backend",
            "toolchain_configuration"})
        self.assertEqual(fields["parameters"]["compiler"], "quartus-17.0.2")
        self.assertEqual(fields["parameters"]["toolchain_lock"], "cores/fes-pc386/toolchain.lock")

    def test_project_version_lines(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            project = quartus.write_project(ROOT, output, BUILD_ID, 1)
            qpf = (project / "top.qpf").read_text(encoding="utf-8")
            qsf = (project / "top.qsf").read_text(encoding="utf-8")
        self.assertEqual(qpf, 'QUARTUS_VERSION = "17.0";\nPROJECT_REVISION = "top";\n')
        self.assertIn('set_global_assignment -name LAST_QUARTUS_VERSION "17.0.2 Lite Edition"', qsf.splitlines())

    def test_toolchain_label(self):
        self.assertEqual(quartus.toolchain_label(ROOT, IDENTITIES),
                         f"Quartus Prime Lite 17.0.2 ({IDENTITIES['quartus_sh']}); "
                         f"machine elaborated by Yosys ({IDENTITIES['yosys']})")

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
