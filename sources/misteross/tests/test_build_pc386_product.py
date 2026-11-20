# SPDX-License-Identifier: MIT
"""The pc386 Quartus seal's sources, guest rates, machine netlist, project,
timing gate, resources and manifest. No Quartus or Yosys needed."""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import tomllib
import unittest
from unittest import mock

import sys
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'cores/fes-pc386/tools'))
import build_pc386_product as quartus
from scripts import fes_build_common as board
from scripts import lockfile
from scripts import yosys_netlist
from scripts.export_core_package import build_identity

ROOT = Path(__file__).resolve().parents[1]
BUILD_ID = "0123456789abcdef0123456789abcdef"
IDENTITIES = {"quartus_sh": "version=17.0.2; sha256=" + "1" * 64, "yosys": "commit=1f7d0273; sha256=" + "2" * 64}
EXECUTION = {"toolchain": "quartus", "gpu_device": "lease", "selection_only": True}


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


class ManifestTest(unittest.TestCase):
    def test_manifest_is_the_system_labelled_quartus(self):
        record = quartus.create_build_record(ROOT, "https://github.com/DeanoC/fes", "a" * 40, IDENTITIES, 1, execution=EXECUTION)
        evidence = {"rbf": {"sha256": "3" * 64, "size": 4096}}
        fields = tomllib.loads(quartus.manifest(ROOT, record, evidence, "https://github.com/DeanoC/fes", "a" * 40,
                                                IDENTITIES).decode())
        self.assertEqual(fields["core"]["id"], "fes.pc386")
        self.assertEqual(fields["core"]["name"], "FES pc386 (Quartus)")
        self.assertEqual(fields["abi"], {"id": "fes.computer", "major": 1, "minor": 1})
        self.assertEqual([item["id"] for item in fields["interfaces"]], list(quartus.INTERFACES))
        self.assertEqual(fields["build"]["id"], build_identity(record))
        self.assertIn(IDENTITIES["quartus_sh"], fields["build"]["toolchain"])
        self.assertIn(IDENTITIES["yosys"], fields["build"]["toolchain"])
        parameters = json.loads(record)["parameters"]
        self.assertEqual(parameters["compiler"], "quartus-17.0.2")
        self.assertEqual(json.loads(parameters["guest_rates"]), quartus.guest_rates())
        self.assertEqual(json.loads(parameters["firmware"]),
                         {**quartus.firmware_digests(ROOT), "rom_hex": quartus.pinned_rom_digest(ROOT)})
        self.assertEqual(parameters["front_end"], "yosys")
        self.assertEqual(len(parameters["front_end_program_sha256"]), 64)
        self.assertEqual(len(parameters["machine_sources_sha256"]), 64)

    def test_the_record_names_its_files_from_the_repository_root(self):
        fields = json.loads(quartus.create_build_record(ROOT, "https://github.com/DeanoC/fes", "a" * 40,
                                                        IDENTITIES, 1, execution=EXECUTION))
        self.assertEqual(fields["source_path"], "sources/misteross")
        self.assertEqual(fields["recipe"], "cores/fes-pc386/tools/build_pc386_product.py")
        self.assertEqual(fields["abi_definition"], "cores/fes-common/generated/fes_computer.vh")

    def test_format2_build_id_enters_the_actual_quartus_project(self):
        record = quartus.create_build_record(ROOT, "https://github.com/DeanoC/fes", "a" * 40,
                                            IDENTITIES, 1, execution=EXECUTION)
        build_id = build_identity(record)
        with tempfile.TemporaryDirectory() as temporary:
            project = quartus.write_project(ROOT, Path(temporary), build_id, 1, threads=2)
            qsf = (project / 'top.qsf').read_text()
        self.assertIn('FES_PC386_BUILD_ID=128\'h' + build_id, qsf)
        self.assertIn('NUM_PARALLEL_PROCESSORS 2', qsf)

    def test_the_product_seed_is_selected_by_the_recipe(self):
        first = quartus.create_build_record(ROOT, "https://github.com/DeanoC/fes", "a" * 40,
                                            IDENTITIES, 1, execution=EXECUTION)
        with self.assertRaisesRegex(board.BuildError, "seed differs"):
            quartus.create_build_record(ROOT, "https://github.com/DeanoC/fes", "a" * 40,
                                        IDENTITIES, 2, execution=EXECUTION)
        changed = quartus.selected_recipe(ROOT)
        changed["quartus"]["seeds"] = [2]
        with mock.patch.object(quartus, "selected_recipe", return_value=changed):
            second = quartus.create_build_record(ROOT, "https://github.com/DeanoC/fes", "a" * 40,
                                                 IDENTITIES, 2, execution=EXECUTION)
        self.assertNotEqual(build_identity(first), build_identity(second))


class GpuRouterTest(unittest.TestCase):
    """--gpu-router selects the toolchain lock's HIP or CUDA slot, and the record names it."""

    REPOSITORY = "https://github.com/DeanoC/fes"

    def record(self, *args):
        return quartus.create_build_record(ROOT, self.REPOSITORY, "a" * 40, IDENTITIES, 1, *args, execution=EXECUTION)

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
        self.assertEqual(build_identity(cuda), build_identity(self.record("HIP")))
        self.assertNotEqual(json.loads(cuda)["identities"]["toolchain"],
                            json.loads(self.record("HIP"))["identities"]["toolchain"])

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
                    mock.patch.object(quartus.machine, "locate_quartus", return_value=(Path(temporary), quartus_sh, "v")):
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
                         ("CUDA", "unused", {"nextpnr": "cuda-lane"}, Path("/cache"), ROOT / quartus.TOOLCHAIN_LOCK))

    def test_controlled_environment_preserves_hip_and_cuda_probe_keys(self):
        from scripts import toolchain_cache
        with tempfile.TemporaryDirectory() as temp:
            fake = Path(temp) / 'probe-bin'
            fake.mkdir()
            # Keep command identity probing real while making the test
            # independent of the host's installed compiler and build tools.
            names = ('git', 'cc', 'c++', 'cmake', 'ninja', 'make', 'perl',
                     'python3', 'python3-config', 'pkg-config', 'autoconf',
                     'flex', 'bison', 'help2man', 'hipcc', 'nvcc')
            body = '#!/bin/sh\nprintf "%s synthetic 1\\n" "${0##*/}"\n'
            for name in names:
                command = fake / name
                command.write_text(body)
                command.chmod(0o755)
            original = {'PATH': str(fake), 'HOME': temp,
                        'USER': 'fixture', 'LOGNAME': 'fixture', 'LANG': 'C'}
            paths = {'yosys': Path('/usr/bin/yosys'), 'quartus': Path('/usr/bin/quartus_sh')}
            for router in ('HIP', 'CUDA'):
                selectors = ({'ROCM_PATH': temp + '/rocm', 'HIPCC': str(fake / 'hipcc')}
                             if router == 'HIP' else
                             {'CUDA_HOME': temp + '/cuda', 'CUDA_PATH': temp + '/cuda',
                              'CUDACXX': str(fake / 'nvcc')})
                # Dependency probes normally inspect real headers and libs;
                # isolate those unrelated inputs, retaining command probes,
                # environment validation and real request/key derivation.
                with self.subTest(router=router), \
                        mock.patch.dict(os.environ, {**original, **selectors}, clear=True), \
                        mock.patch.object(toolchain_cache, '_dependency_inventory',
                                          return_value={'synthetic': 'stable'}) as dependencies:
                    config = quartus.gpu_choice(router)
                    kwargs = {'cache_root': Path(temp) / 'cache', 'gpu_router': router,
                              'hip_architectures': config[1],
                              'cuda_architectures': board._cuda_architectures_from_configuration(config[2])}
                    before = toolchain_cache.request_from_environment(ROOT, ROOT / quartus.TOOLCHAIN_LOCK, **kwargs)
                    controlled = quartus.execution_environment(Path(temp) / 'private-home', paths)
                    with mock.patch.dict(os.environ, controlled, clear=True):
                        after = toolchain_cache.request_from_environment(ROOT, ROOT / quartus.TOOLCHAIN_LOCK, **kwargs)
                    keys = toolchain_cache.tool_keys(before)
                    self.assertEqual(keys, toolchain_cache.tool_keys(after))
                    self.assertEqual(before.environment, after.environment)
                    self.assertEqual(str(fake), controlled['PATH'])
                    for name, value in selectors.items():
                        self.assertEqual(value, controlled[name])
                    self.assertTrue(all(Path(item['path']).parent == fake
                                        for item in before.compiler_identity['commands'].values()))
                    self.assertEqual(2, dependencies.call_count)
                    selected = fake / ('hipcc' if router == 'HIP' else 'nvcc')
                    unselected = fake / ('nvcc' if router == 'HIP' else 'hipcc')
                    unselected.write_text(body + '# different unselected compiler\n')
                    unchanged = toolchain_cache.request_from_environment(ROOT, ROOT / quartus.TOOLCHAIN_LOCK, **kwargs)
                    self.assertEqual(keys, toolchain_cache.tool_keys(unchanged))
                    unselected.write_text(body)
                    selected.write_text(body + '# different selected compiler\n')
                    changed = toolchain_cache.request_from_environment(ROOT, ROOT / quartus.TOOLCHAIN_LOCK, **kwargs)
                    self.assertNotEqual(keys['nextpnr'], toolchain_cache.tool_keys(changed)['nextpnr'])
                    selected.unlink()
                    with self.assertRaisesRegex(toolchain_cache.CacheError, 'required host command is missing'):
                        toolchain_cache.request_from_environment(ROOT, ROOT / quartus.TOOLCHAIN_LOCK, **kwargs)
                    selected.write_text(body)
                    selected.chmod(0o755)
                    (fake / 'cmake').unlink()
                    with self.assertRaisesRegex(toolchain_cache.CacheError, 'required host command is missing: cmake'):
                        toolchain_cache.request_from_environment(ROOT, ROOT / quartus.TOOLCHAIN_LOCK, **kwargs)
                    (fake / 'cmake').write_text(body)
                    (fake / 'cmake').chmod(0o755)

    def test_other_checkout_and_symlink_alias_are_rejected_before_authentication(self):
        with tempfile.TemporaryDirectory() as temporary:
            other = Path(temporary) / 'other'
            other.mkdir()
            alias = Path(temporary) / 'alias'
            alias.symlink_to(ROOT, target_is_directory=True)
            for root in (other, alias):
                with self.subTest(root=root), mock.patch.object(quartus, '_authenticate_tools') as authenticate:
                    with self.assertRaisesRegex(board.BuildError, 'differs from the running producer'):
                        quartus.authenticate_product_tools(root, cache_root=Path('/cache'))
                    with self.assertRaisesRegex(board.BuildError, 'differs from the running producer'):
                        quartus.build(root, lockstep_report=root / 'absent.json')
                    authenticate.assert_not_called()
                    self.assertFalse((other / 'build').exists())
            direct = subprocess.run([sys.executable, str(ROOT / quartus.RECIPE), '--root', str(alias),
                                     '--with-lockstep'], cwd=ROOT, capture_output=True, text=True)
            self.assertNotEqual(direct.returncode, 0)
            self.assertIn('differs from the running producer', direct.stderr)
            self.assertFalse((other / 'build').exists())

    def test_slot_probe_uses_the_locked_nextpnr_cache_key(self):
        from scripts import toolchain_cache
        request = object()
        slot = Path('/tmp/fixture-slot')
        with mock.patch.object(toolchain_cache, 'request_from_environment', return_value=request) as make, \
                mock.patch.object(toolchain_cache, 'install_directory', return_value=slot) as directory, \
                mock.patch.object(toolchain_cache, '_check_no_symlink_components') as check:
            self.assertEqual(quartus._slot_directory(ROOT, Path('/cache'), 'CUDA'), slot)
        self.assertEqual(make.call_args.args, (ROOT, ROOT / quartus.TOOLCHAIN_LOCK))
        self.assertEqual(make.call_args.kwargs['gpu_router'], 'CUDA')
        self.assertEqual(make.call_args.kwargs['cuda_architectures'],
                         board._cuda_architectures_from_configuration(quartus.gpu_choice('CUDA')[2]))
        directory.assert_called_once_with(request, 'nextpnr')
        check.assert_called_once_with(slot, allow_missing=True)

    def test_auto_selection_supports_either_slot_without_hiding_corruption(self):
        selected = {"yosys": board.AuthenticatedTool(Path('/tools/yosys'), 'yosys'),
                    "quartus_sh": Path('/tools/quartus'),
                    "identities": {"quartus_sh": 'quartus'}}
        with tempfile.TemporaryDirectory() as temp:
            hip, cuda = Path(temp) / 'hip', Path(temp) / 'cuda'
            cuda.mkdir()
            def slot(_root, _cache, router):
                return hip if router == 'HIP' else cuda
            with mock.patch.object(quartus.shutil, 'which', return_value='/available/compiler'), \
                    mock.patch.object(quartus, '_slot_directory', side_effect=slot), \
                    mock.patch.object(quartus, '_authenticate_tools', return_value=selected) as authenticate:
                tools = quartus.authenticate_product_tools(ROOT, cache_root=Path('/cache'))
            self.assertEqual(tools.gpu_router, 'CUDA')
            self.assertEqual([call.kwargs['gpu_router'] for call in authenticate.call_args_list], ['CUDA'])
            hip.mkdir()  # A present but incomplete slot must not fall through.
            with mock.patch.object(quartus.shutil, 'which', return_value='/available/compiler'), \
                    mock.patch.object(quartus, '_slot_directory', side_effect=slot), \
                    mock.patch.object(quartus, '_authenticate_tools',
                                      side_effect=board.BuildError('ready manifest is missing: hip')) as authenticate:
                with self.assertRaisesRegex(board.BuildError, 'ready manifest is missing'):
                    quartus.authenticate_product_tools(ROOT, cache_root=Path('/cache'))
            self.assertEqual([call.kwargs['gpu_router'] for call in authenticate.call_args_list], ['HIP'])
        with self.assertRaisesRegex(board.BuildError, 'select --gpu-router'):
            quartus.authenticate_product_tools(ROOT)


    def test_build_uses_the_selected_slot_and_controlled_environment(self):
        import pc386_seal_evidence
        tools = quartus.ProductTools({
            "yosys": board.AuthenticatedTool(Path("/tools/yosys/bin/yosys"), IDENTITIES["yosys"]),
            "quartus": board.AuthenticatedTool(Path("/tools/quartus/bin/quartus_sh"), IDENTITIES["quartus_sh"]),
        }, "CUDA")
        zero_qsf = "fixed build id " + "0" * 32
        record = json.dumps({"parameters": {"quartus_qsf_sha256": hashlib.sha256(zero_qsf.encode()).hexdigest()},
                             "identities": {"record": "1" * 64},
                             "tools": {name: tool.identity for name, tool in tools.items()}}).encode()
        environments = []
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'build').mkdir()
            report = root / 'report.json'
            report.write_text('{}')
            def write_project(_root, output, build_id, seed, **_):
                project = output / 'project'
                project.mkdir()
                (project / 'top.qsf').write_text('fixed build id ' + build_id)
                return project
            def observe(*_):
                environments.append(dict(os.environ))
            order = []
            def elaborate(*_):
                order.append('yosys')
                observe()
            from contextlib import ExitStack
            with ExitStack() as stack:
                stack.enter_context(mock.patch.dict(os.environ, {'FES_TEST_AMBIENT_UNSAFE': 'yes'}))
                def patch(obj, name, **kwargs):
                    return stack.enter_context(mock.patch.object(obj, name, **kwargs))
                patch(quartus, 'ROOT', new=root)
                patch(quartus, '_require_clean_source', return_value=(self.REPOSITORY, 'a' * 40))
                authenticate = patch(quartus, 'authenticate_product_tools', return_value=tools)
                patch(quartus, 'execution_inputs', return_value=EXECUTION)
                create = patch(quartus, 'create_build_record', return_value=record)
                patch(quartus, 'build_identity', return_value='a' * 32)
                patch(quartus, 'selected_recipe', return_value={'quartus': {'threads': 2}})
                patch(quartus, 'quartus_lock', return_value=mock.Mock(version='v'))
                patch(quartus.board, '_prepare_output', return_value=root)
                write = patch(quartus.board, '_write_atomic')
                patch(quartus, 'write_project', side_effect=write_project)
                patch(quartus, 'check_rom', side_effect=lambda checked: order.append(('rom', checked)))
                patch(quartus, 'elaborate_machine', side_effect=elaborate)
                patch(quartus, 'run_quartus', side_effect=observe)
                patch(quartus, 'validate_build_evidence', return_value={'rbf': {'sha256': '0' * 64, 'size': 1}})
                patch(quartus, 'firmware_digests', return_value={})
                patch(quartus.board, '_sha256', return_value='0' * 64)
                patch(quartus.yosys_netlist, 'cpu_source_sha256', return_value='c' * 64)
                patch(quartus.yosys_netlist, 'lock_record', return_value={})
                patch(quartus, 'manifest', return_value=b'')
                patch(quartus, 'export_package', return_value=root / ('b' * 64))
                gate = patch(quartus, 'check_lockstep_report', return_value={'oracle': {}})
                patch(quartus, 'require_same_oracle')
                patch(pc386_seal_evidence, 'publish', return_value=root / 'ev')
                patch(pc386_seal_evidence, 'publish_local', return_value=root / 'package')
                self.assertEqual(quartus.build(root, cache_root=Path('/cache'), gpu_router='CUDA',
                                               lockstep_report=report), root / 'package')
                self.assertEqual(os.environ['FES_TEST_AMBIENT_UNSAFE'], 'yes')
        self.assertEqual(gate.call_args.args[1:4], (b'{}', tools['yosys'], 'CUDA'))
        self.assertEqual([call.kwargs['gpu_router'] for call in authenticate.call_args_list], ['CUDA', 'CUDA'])
        self.assertEqual([call.args[-1] for call in create.call_args_list], ['CUDA', 'CUDA'])
        # The seal checks the ROM the recipe wrote against its pin before and
        # after Yosys reads it, and never writes it (#1984 S1).
        self.assertEqual(order, [('rom', root), 'yosys', ('rom', root)])
        self.assertEqual(len(environments), 2)
        for env in environments:
            self.assertNotIn('FES_TEST_AMBIENT_UNSAFE', env)
            self.assertEqual(env['QUARTUS_ROOTDIR'], '/tools/quartus')
        summary = next(json.loads(call.args[1]) for call in write.call_args_list
                       if call.args[0].name == 'build-summary.json')
        self.assertEqual(summary['gpu_backend'], 'cuda')

    def test_the_command_line_passes_the_router(self):
        for argv, router in (([], None), (["--gpu-router", "CUDA"], "CUDA")):
            with mock.patch.object(quartus, "build", return_value=Path("package")) as build, \
                    mock.patch("sys.argv", ["build_pc386_product.py", "--lockstep-report", "r.json", *argv]), \
                    mock.patch("builtins.print"):
                self.assertEqual(quartus.main(), 0)
            self.assertEqual(build.call_args.kwargs["gpu_router"], router)
            self.assertEqual(build.call_args.kwargs["lockstep_report"], Path("r.json"))
        with mock.patch("sys.argv", ["build_pc386_product.py"]), mock.patch("sys.stderr"), \
                self.assertRaises(SystemExit):
            quartus.main()
        with mock.patch("sys.argv", ["build_pc386_product.py", "--gpu-router", "Metal"]), \
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
            "oracle": {**quartus.oracle_identity(ROOT), "linked_sha256": {
                name: "1" * 64 for name in quartus.LINKED_NAMES}, "testbench_sha256": "2" * 64},
            "verilator": {"lock": quartus.I386_LOCK, "commit": verilator, "sha256": "v", "version": "V",
                          "slot": "s"},
            "netlist": {"program_sha256": yosys_netlist.program_sha256(yosys_netlist.SEAL),
                        "cpu_source_sha256": yosys_netlist.cpu_source_sha256(ROOT),
                        "netlist_sha256": hashlib.sha256(self.netlist).hexdigest(),
                        "yosys": yosys_netlist.lock_record(ROOT, self.yosys, "HIP", quartus.TOOLCHAIN_LOCK)},
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
        self.refused({**self.report, "verilator": {**self.report["verilator"], "lock": quartus.TOOLCHAIN_LOCK}}, "Verilator")

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
    """Every version string the producer writes derives from quartus.lock and
    is byte-identical to the literals the producer wrote before the lock."""

    def test_locks_are_source_bound_and_described_in_the_record(self):
        fields = json.loads(quartus.create_build_record(ROOT, "https://github.com/DeanoC/fes", "a" * 40,
                                                        IDENTITIES, 1, execution=EXECUTION))
        self.assertEqual(fields["format"], 2)
        self.assertEqual(fields["parameters"]["identity_policy"], "design-v2")
        self.assertIn(quartus.TOOLCHAIN_LOCK, fields["source_inputs"])
        self.assertIn(quartus.QUARTUS_LOCK, fields["source_inputs"])
        self.assertEqual(fields["parameters"]["compiler"], "quartus-17.0.2")

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
                    mock.patch.object(quartus.machine, "locate_quartus",
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
