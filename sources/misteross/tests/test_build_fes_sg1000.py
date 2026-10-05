from __future__ import annotations

import hashlib
import copy
import json
import tomllib
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
from tests.producer_fixture import clean_module, init_source, EXECUTION, FakeInvocation
from pathlib import Path

from scripts.build_fes_sg1000 import (
    OUTPUT_RELATIVE as QUARTUS_OUTPUT,
    PINNED_INPUTS as QUARTUS_PINNED_INPUTS,
    SYSTEMVERILOG_SOURCES,
    VERILOG_SOURCES,
    _manifest,
    project_qsf,
)
from scripts.build_fes_sg1000_oss import (
    OUTPUT_RELATIVE as OSS_OUTPUT,
    PINNED_INPUTS as OSS_PINNED_INPUTS,
    RTL_SOURCES as OSS_RTL_SOURCES,
    PLACER_SEEDS,
    PLACER_QOR_CLOCKS,
    SEED,
    SG1000_GPU_ARCHITECTURES,
    SG1000_GPU_BACKEND,
    SG1000_TOOLCHAIN_LOCK,
    SG1000_TOOLCHAIN_ROOT,
    SG1000_TOOL_COMMITS,
    build_commands,
    create_build_record,
    _manifest as _oss_manifest,
)
from scripts.lockfile import load_lock
from scripts import build_fes_sg1000_oss
from scripts.fes_build_common import BuildError


ROOT = Path(__file__).resolve().parents[1]
GENERATOR = ROOT / "cores/fes-sg1000/diagnostic/generate.py"


class BuildFesSg1000Tests(unittest.TestCase):
    def test_make_entrypoints_use_both_recipes(self) -> None:
        for target, recipe in (
            ("build-fes-sg1000", "scripts/build_fes_sg1000_oss.py"),
            ("build-fes-sg1000-quartus", "scripts/build_fes_sg1000.py"),
        ):
            result = subprocess.run(
                ["make", "-n", target],
                cwd=ROOT,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                check=False,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(
                result.stdout.strip(),
                f'python3 {recipe} --root "{ROOT}"',
            )

    def test_oss_simulation_entrypoint_compiles_conditional_branches(self) -> None:
        result = subprocess.run(
            ["make", "-n", "sim-fes-sg1000-oss"],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("-DFES_SG1000_OSS=1", result.stdout)
        self.assertIn("-DFES_COLECO_OSS=1", result.stdout)
        self.assertIn('-CFLAGS "-DFES_SG1000_OSS=1 -DFES_COLECO_OSS=1"', result.stdout)
        self.assertIn("fes-sg1000-machine-oss", result.stdout)
        default = subprocess.run(
            ["make", "-n", "sim-fes-sg1000"],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(default.returncode, 0, default.stderr)
        self.assertNotIn("FES_SG1000_OSS", default.stdout)
        self.assertNotIn("FES_COLECO_OSS", default.stdout)
        self.assertIn("fes-sg1000-machine", default.stdout)
        self.assertNotIn("fes-sg1000-machine-oss", default.stdout)

    def test_linked_simulation_uses_sealed_rom_path(self) -> None:
        result = subprocess.run(["make", "-n", "sim-fes-sg1000-rom-link"], cwd=ROOT,
                                text=True, capture_output=True, check=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("-DFES_SG1000_ROM_LINK=1", result.stdout)
        self.assertIn("sg1000_rom_link.v", result.stdout)
        self.assertIn("graphics-i-16k.hex", result.stdout)

    def test_oss_toolchain_entrypoint_selects_coleco_compatibility_lock(self) -> None:
        result = subprocess.run(
            ["make", "-n", "toolchain-fes-sg1000"],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("FES_TOOLCHAIN_LOCKFILE=", result.stdout)
        self.assertIn("toolchains/registered-memory.lock", result.stdout)
        self.assertIn("FES_TOOLCHAIN_ROOT=", result.stdout)
        self.assertIn("build/toolchain/fes-sg1000", result.stdout)
        self.assertIn("FES_TOOLCHAIN_GPU_ROUTER=HIP", result.stdout)
        self.assertIn("FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201'", result.stdout)

    def test_oss_commands_use_dual_defines_and_coleco_lock(self) -> None:
        yosys, nextpnr = build_commands(
            ROOT,
            ROOT / OSS_OUTPUT,
            "00112233445566778899aabbccddeeff",
            {"yosys": Path("/tmp/yosys"), "nextpnr-mistral": Path("/tmp/nextpnr-mistral")},
        )
        program = yosys[2]
        self.assertIn("sg1000_machine.sv", program)
        self.assertIn("cores/fes-sg1000/rtl/top.v", program)
        self.assertIn("z80/fes_z80_nmos.sv", program)
        self.assertNotIn("tv80", program)
        self.assertNotIn("t80pa", program)
        self.assertIn("coleco_vdp.sv", program)
        self.assertIn("coleco_dpram.v", program)
        self.assertIn("-DFES_SG1000_OSS=1", program)
        self.assertIn("-DFES_SG1000_ROM_LINK=1", program)
        self.assertIn("cores/fes-sg1000/rtl/sg1000_rom_link.v", OSS_RTL_SOURCES)
        self.assertIn("-DFES_COLECO_OSS=1", program)
        self.assertNotIn("TV80_REFRESH", program)
        self.assertIn("synth_intel_alm -nolutram -nodsp -top top", program)
        self.assertNotIn("coleco_machine.sv", program)
        self.assertNotIn("coleco_reset_rom", program)
        self.assertEqual(PLACER_SEEDS, (2, 3, 4, 1, 5, 6, 7, 8, 9, 10))
        self.assertEqual(PLACER_QOR_CLOCKS, (("system_clock.clocks[0]", 52.224), (None, 74.25), ("system_clock.clocks[1]", 12.288)))
        self.assertEqual(SEED, PLACER_SEEDS[0])
        self.assertEqual(nextpnr[nextpnr.index("--seed") + 1], str(SEED))
        self.assertEqual(nextpnr[nextpnr.index("--placer-heap-timingweight") + 1], "2000")
        self.assertEqual(nextpnr[nextpnr.index("--placer-heap-critexp") + 1], "5")
        self.assertEqual(nextpnr[nextpnr.index("--router") + 1], "gpu")
        self.assertIn("--timing-allow-fail", nextpnr)
        self.assertNotIn("--tmg-ripup", nextpnr)
        joined = " ".join(nextpnr)
        self.assertIn("cores/fes-sg1000/constraints-oss.qsf", joined)
        self.assertIn("cores/fes-sg1000/clocks-oss.sdc", joined)
        self.assertNotIn("cores/fes-sg1000/clocks.sdc", joined)
        self.assertEqual(SG1000_TOOLCHAIN_LOCK, "toolchains/registered-memory.lock")
        self.assertEqual(SG1000_TOOLCHAIN_ROOT, "build/toolchain/fes-sg1000")
        self.assertIn(SG1000_TOOLCHAIN_LOCK, OSS_PINNED_INPUTS)
        self.assertIn("scripts/search_placer_qor.py", OSS_PINNED_INPUTS)
        self.assertEqual(SG1000_TOOL_COMMITS["yosys"], "e2d425dee148cc60c50f4e9b354a10d90eab15f4")
        self.assertEqual(SG1000_TOOL_COMMITS["nextpnr"], "a93fe013af841214ecb4f7be3af0de65f3de3a0f")
        pins = load_lock(ROOT / SG1000_TOOLCHAIN_LOCK)
        from scripts.build_fes_coleco_oss import COLECO_TOOLCHAIN_LOCK
        self.assertEqual(SG1000_TOOLCHAIN_LOCK, COLECO_TOOLCHAIN_LOCK)
        self.assertEqual(pins["yosys"].commit, SG1000_TOOL_COMMITS["yosys"])
        self.assertEqual(pins["nextpnr"].commit, SG1000_TOOL_COMMITS["nextpnr"])
        global_pins = load_lock(ROOT / "toolchain.lock")
        self.assertEqual(global_pins["yosys"].commit, "5391eeb1e91b38a3d0e96d04f24cf921743c9c78")
        self.assertEqual(global_pins["nextpnr"].commit, "585ef60802dcccf745ef3ad7ae2ca3b6f9685146")
        record = create_build_record(
            ROOT,
            "https://example.invalid/misteross.git",
            "a" * 40,
            {"yosys": "test"}, execution=EXECUTION,
        )
        self.assertIn(f'"seed":{SEED}'.encode(), record)
        self.assertEqual(json.loads(record)['parameters']['seed_order'], '2,3,4,1,5,6,7,8,9,10')
        self.assertIn(b'"router":"gpu"', record)
        self.assertIn(b'"gpu_backend":"hip"', record)
        self.assertIn(b'"package_format":3', record)
        self.assertIn(b'"rom_source_size":16384', record)
        self.assertIn(SG1000_GPU_ARCHITECTURES.encode(), record)
        self.assertEqual(SG1000_GPU_BACKEND, "hip")
        self.assertIn("cores/fes-sg1000/rtl/top.v", OSS_RTL_SOURCES)
        self.assertIn("cores/fes-sg1000/rtl/sg1000_machine.sv", OSS_RTL_SOURCES)
        self.assertIn("cores/fes-common/rtl/fes_z80_ce.sv", OSS_RTL_SOURCES)

    def test_oss_routes_bounded_first_pass_ladder(self) -> None:
        invocation = FakeInvocation({}, 1)
        with patch.object(build_fes_sg1000_oss, 'route_after_synth') as route:
            build_fes_sg1000_oss._route_placement(ROOT, ROOT / OSS_OUTPUT,
                Path('/nextpnr'), invocation, 1)
        options = route.call_args.kwargs
        self.assertEqual(options['seeds'], (2, 3, 4, 1, 5, 6, 7, 8, 9, 10))
        self.assertEqual(options['weights'], (2000, 1000))
        self.assertEqual(options['critexp'], 5)
        self.assertEqual(options['mode'], 'first-pass')
        self.assertEqual(options['required'], (("system_clock.clocks[0]", 52.224), (None, 74.25), ("system_clock.clocks[1]", 12.288)))
        self.assertEqual(options['budget'], 20)
        self.assertEqual(options['timeout'], 1800)
        self.assertEqual(options['extra'], ('--router', 'gpu'))
        self.assertEqual(options['gpu_devices'], (1,))
        self.assertEqual(options['env'], invocation.env)
        self.assertEqual(options['audit_source_root'], ROOT)

    def test_oss_prepare_rejects_dangling_output_symlink(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            output = root / build_fes_sg1000_oss.OUTPUT_RELATIVE
            output.mkdir(parents=True)
            (output / "qor-ranking.json").symlink_to(root / "missing-target.json")
            with self.assertRaisesRegex(build_fes_sg1000_oss.BuildError, "regular file"):
                build_fes_sg1000_oss._prepare_output(root)
            self.assertFalse((root / "missing-target.json").exists())

    def test_bounded_search_policy_is_in_functional_build_identity(self) -> None:
        producer = build_fes_sg1000_oss
        with patch.object(producer, "functional_record_fields",
                          side_effect=lambda root, fields, *a, **k: fields):
            record = producer.create_build_record(ROOT, "https://example.invalid/fes",
                "a" * 40, {"yosys": "y", "nextpnr": "n", "mistral": "m"})
            with patch.object(producer, "PLACER_TIMING_WEIGHTS", (1000, 2000)):
                other = producer.create_build_record(ROOT, "https://example.invalid/fes",
                    "a" * 40, {"yosys": "y", "nextpnr": "n", "mistral": "m"})
            with patch.object(producer, "ROUTE_TIMEOUT_SECONDS", 600):
                shorter = producer.create_build_record(ROOT, "https://example.invalid/fes",
                    "a" * 40, {"yosys": "y", "nextpnr": "n", "mistral": "m"})
        self.assertNotEqual(producer.build_identity(record), producer.build_identity(other))
        self.assertNotEqual(producer.build_identity(record), producer.build_identity(shorter))
        params = json.loads(record)["parameters"]
        self.assertEqual(params["seed_order"], "2,3,4,1,5,6,7,8,9,10")
        self.assertEqual(params["placer_heap_timingweights"], "2000,1000")
        self.assertEqual(params["placer_qor_budget"], 20)
        self.assertEqual(params["placer_qor_mode"], "first-pass")
        self.assertEqual(params["placer_qor_workers"], 1)
        self.assertEqual(params["route_timeout_seconds"], 1800)
        self.assertEqual(params["placer_qor_clocks"],
            "system_clock.clocks[0]:52.224,:74.25,system_clock.clocks[1]:12.288")

    def test_final_clock_gate_rejects_each_missing_or_failing_domain(self) -> None:
        producer = build_fes_sg1000_oss
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            (output / "routed.json").write_text('{"modules":{"top":{"cells":{}}}}')
            (output / "core.rbf").write_bytes(b"rbf")
            (output / "nextpnr.log").write_text(
                "Info: Program finished normally.\n50 MHz -> 52.224 MHz\n")
            clocks = {name: {"constraint": mhz, "achieved": mhz + 1}
                for name, mhz in (("system_clock.clocks[0]", 52.224),
                                 ("pixel", 74.25), ("system_clock.clocks[1]", 12.288))}
            def save(fmax):
                (output / "timing.json").write_text(json.dumps({"fmax": fmax,
                    "utilization": {"MISTRAL_FF": {"used": 10, "available": 100}}}))
            with patch.object(producer, "validate_synth_evidence", return_value={"synthesis_cells": {}}), \
                 patch.object(producer, "_i2c_evidence"), \
                 patch.object(producer, "_audio_evidence"), \
                 patch.object(producer, "_require_gpu_backend", return_value="hip"):
                save(clocks)
                self.assertEqual(producer.validate_build_evidence(output)["status"], "pass")
                for name in clocks:
                    wrong = copy.deepcopy(clocks)
                    wrong.pop(name)
                    save(wrong)
                    with self.assertRaises(BuildError): producer.validate_build_evidence(output)
                    wrong = copy.deepcopy(clocks)
                    wrong[name]["achieved"] = wrong[name]["constraint"] - 0.001
                    save(wrong)
                    with self.assertRaises(BuildError): producer.validate_build_evidence(output)

    def test_oss_manifest_requires_linked_cartridge_without_media_mailbox(self) -> None:
        record = b'{"recipe_sha256":"' + b"b" * 64 + b'"}'
        evidence = {
            "build_id": "d" * 32,
            "rbf": {"size": 16, "sha256": "e" * 64},
            "rom": {"id": "cartridge-rom", "role": "cartridge", "source_size": 16384,
                    "file": "rom-map.json", "size": 12, "sha256": "f" * 64},
        }
        manifest = tomllib.loads(_oss_manifest(record, evidence, "https://example.invalid", "a" * 40, {"yosys": "test"}).decode())
        self.assertEqual(manifest["format"], 3)
        self.assertEqual(manifest["core"]["version"], "1.3.0")
        self.assertEqual(manifest["rom"]["source_size"], 16384)
        self.assertEqual({item["id"] for item in manifest["interfaces"]},
                         {"fes.keyboard", "fes.video.fixed-720p60", "fes.audio.pcm-s16-stereo-48k"})

    def test_both_sg1000_recipes_include_audio_sources_and_pins(self) -> None:
        for source in ("fes_sn76489.sv", "coleco_system_pll.v", "fes_audio_output.v", "fes_audio_i2s.v"):
            self.assertTrue(any(item.endswith(source) for item in OSS_RTL_SOURCES), source)
            self.assertTrue(any(item.endswith(source) for item in (*VERILOG_SOURCES, *SYSTEMVERILOG_SOURCES)), source)
        for qsf in (build_fes_sg1000_oss.QSF, "cores/fes-sg1000/constraints.qsf"):
            contents = (ROOT / qsf).read_text()
            for port, pin in {"HDMI_MCLK": "PIN_U11", "HDMI_SCLK": "PIN_T12",
                              "HDMI_LRCLK": "PIN_T11", "HDMI_I2S": "PIN_T13"}.items():
                self.assertIn(f"set_location_assignment {pin} -to {port}", contents)
                self.assertIn(f'set_instance_assignment -name IO_STANDARD "3.3-V LVTTL" -to {port}', contents)

    def test_oss_audio_evidence_rejects_bad_pll_and_pads(self) -> None:
        pins = {"HDMI_MCLK": "PIN_U11", "HDMI_SCLK": "PIN_T12",
                "HDMI_LRCLK": "PIN_T11", "HDMI_I2S": "PIN_T13"}
        cells = {"system_clock.pll": {"type": "altera_pll", "parameters": {
            "output_clock_frequency0": "52.224 MHz", "output_clock_frequency1": "12.288 MHz",
            "reference_clock_frequency": "50.0 MHz"}}}
        ports = {}
        for index, (port, pin) in enumerate(pins.items()):
            ports[port] = {"direction": "output", "bits": [index]}
            cells[port] = {"type": "MISTRAL_OB", "connections": {"PAD": [index], "I": [index + 10]},
                           "attributes": {"LOC": pin, "IO_STANDARD": "3.3-V LVTTL", "NEXTPNR_BEL": "MISTRAL_IO.1"}}
        design = {"modules": {"top": {"ports": ports, "cells": cells}}}
        build_fes_sg1000_oss._audio_evidence(design)
        for mutate in (
            lambda d: d["modules"]["top"]["cells"].pop("system_clock.pll"),
            lambda d: d["modules"]["top"]["cells"]["system_clock.pll"]["parameters"].update({"output_clock_frequency1": "13.000 MHz"}),
            lambda d: d["modules"]["top"]["cells"]["HDMI_I2S"]["connections"].update({"I": ["0"]}),
            lambda d: d["modules"]["top"]["cells"]["HDMI_LRCLK"]["attributes"].update({"LOC": "PIN_BAD"}),
            lambda d: d["modules"]["top"]["ports"].pop("HDMI_SCLK"),
        ):
            wrong = copy.deepcopy(design)
            mutate(wrong)
            with self.assertRaises(BuildError):
                build_fes_sg1000_oss._audio_evidence(wrong)

    def test_oss_audio_timing_rejects_missing_or_failing_domain(self) -> None:
        good = {"system_clock.clocks[1]": {"constraint": 12.288, "achieved": 13.0}}
        self.assertEqual(build_fes_sg1000_oss._audio_timing(good)[2], 13.0)
        for fmax in ({}, {"system_clock.clocks[1]": {"constraint": 12.288, "achieved": 12.287}}):
            with self.assertRaises(BuildError):
                build_fes_sg1000_oss._audio_timing(fmax)

    def test_quartus_project_reuses_coleco_sibling_modules(self) -> None:
        qsf = project_qsf(ROOT, ROOT / QUARTUS_OUTPUT / "project", "00112233445566778899aabbccddeeff")
        self.assertIn('VERILOG_MACRO "QUARTUS=1"', qsf)
        self.assertIn('VERILOG_MACRO "FES_SG1000_BUILD_ID=', qsf)
        self.assertIn("cores/fes-sg1000/rtl/sg1000_machine.sv", qsf)
        self.assertIn("cores/fes-common/rtl/fes_z80_ce.sv", qsf)
        self.assertIn("cores/fes-sg1000/rtl/top.v", qsf)
        self.assertIn("cores/fes-common/rtl/z80/fes_z80_nmos.sv", qsf)
        self.assertNotIn("tv80", qsf)
        self.assertNotIn("t80pa", qsf)
        self.assertIn("cores/fes-common/rtl/coleco_vdp.sv", qsf)
        self.assertIn("cores/fes-common/rtl/fes_computer_gp.v", qsf)
        self.assertNotIn("VHDL_FILE", qsf)
        self.assertNotIn("coleco_reset_rom", qsf)
        self.assertNotIn("coleco_machine.sv", qsf)
        for relative in (*VERILOG_SOURCES, *SYSTEMVERILOG_SOURCES):
            self.assertIn(relative, QUARTUS_PINNED_INPUTS)
        self.assertTrue(all("reset_rom" not in item for item in QUARTUS_PINNED_INPUTS))

    def test_machine_uses_sg1000_map_and_coleco_vdp(self) -> None:
        machine = (ROOT / "cores/fes-sg1000/rtl/sg1000_machine.sv").read_text(encoding="utf-8")
        self.assertIn("coleco_vdp", machine)
        self.assertIn("coleco_dpram", machine)
        self.assertIn("2'b11", machine)
        self.assertIn("port_dc", machine)
        self.assertIn("port_dd", machine)
        self.assertNotIn("coleco_reset_rom", machine)
        self.assertNotIn("JP 0x8000", machine)

    def test_manifest_carries_sg1000_identity(self) -> None:
        record = (
            b'{"format":1,"repository":"https://github.com/DeanoC/misteross.git",'
            b'"revision":"' + (b"a" * 40) + b'","recipe":"scripts/build_fes_sg1000.py",'
            b'"recipe_sha256":"' + (b"b" * 64) + b'","abi_definition":"x",'
            b'"abi_definition_sha256":"' + (b"c" * 64) + b'","dependencies":{},'
            b'"tools":{},"parameters":{}}'
        )
        evidence = {"build_id": "d" * 32, "rbf": {"size": 16, "sha256": "e" * 64}}
        manifest = _manifest(
            record,
            evidence,
            "https://github.com/DeanoC/misteross.git",
            "a" * 40,
            "Quartus Prime Lite 17.0.2",
        )
        self.assertIn(b'id = "fes.sg1000"', manifest)
        self.assertIn(b"FES SG-1000", manifest)
        self.assertIn(b"fes.simple-computer", manifest)
        self.assertNotIn(b"fes.coleco", manifest)
        self.assertNotIn(b"recipe = ", manifest)

    def test_docs_record_quartus_and_oss_recipes(self) -> None:
        readme = (ROOT / "cores/fes-sg1000/README.md").read_text(encoding="utf-8")
        architecture = (ROOT / "docs/architecture.md").read_text(encoding="utf-8")
        root_readme = (ROOT / "README.md").read_text(encoding="utf-8")
        for text in (readme, architecture, root_readme):
            self.assertIn("fes-sg1000", text)
            self.assertIn("fes.sg1000", text)
            self.assertIn("build-fes-sg1000-quartus", text)
            self.assertIn("build-fes-sg1000", text)
            self.assertIn("sim-fes-sg1000-oss", text)
            self.assertIn("fes.simple-computer", text)
        self.assertNotIn("/home/deano/", readme)
        self.assertIn("Quartus", readme)
        self.assertIn("FES_COLECO_OSS", readme)
        self.assertIn("e2d425de", readme)
        self.assertIn("package-only", readme.lower())
        self.assertIn("sim-fes-sg1000-rom-link", readme)
        self.assertIn("in the factory image", readme.lower())
        self.assertNotIn("not in the factory image", readme.lower())
        self.assertNotIn("remain later jobs", readme.lower())

    def test_diagnostic_is_reproducible_and_enters_at_reset(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "graphics-i.rom"
            preview = Path(directory) / "graphics-i.ppm"
            first = subprocess.run(
                [sys.executable, str(GENERATOR), "--output", str(output), "--preview", str(preview)],
                cwd=ROOT,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                check=False,
            )
            self.assertEqual(first.returncode, 0, first.stderr)
            data = output.read_bytes()
            self.assertEqual(data[0], 0xF3)
            self.assertIn(b"\x32\x00\xc0", data)
            self.assertLessEqual(len(data), 16384)
            digest = hashlib.sha256(data).hexdigest()
            second = Path(directory) / "again.rom"
            subprocess.run(
                [sys.executable, str(GENERATOR), "--output", str(second)],
                cwd=ROOT,
                check=True,
            )
            self.assertEqual(second.read_bytes(), data)
            self.assertEqual(hashlib.sha256(second.read_bytes()).hexdigest(), digest)
            padded = Path(directory) / "padded.rom"
            hex_output = Path(directory) / "padded.hex"
            subprocess.run(
                [sys.executable, str(GENERATOR), "--output", str(padded), "--pad-to", "16384",
                 "--hex-output", str(hex_output)],
                cwd=ROOT,
                check=True,
            )
            self.assertEqual(padded.read_bytes(), data + b"\xff" * (16384 - len(data)))
            self.assertEqual(hex_output.read_text().splitlines(), [f"{byte:02x}" for byte in padded.read_bytes()])
            self.assertTrue(preview.is_file())
            self.assertGreater(preview.stat().st_size, 1000)

    def test_sound_diagnostic_writes_psg_and_is_exact_cartridge_size(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "sound-16k.rom"
            result = subprocess.run(
                [sys.executable, str(GENERATOR), "--sound", "--pad-to", "16384",
                 "--output", str(output)], cwd=ROOT, text=True, capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            data = output.read_bytes()
            self.assertEqual(len(data), 16384)
            # Tone 0 divider = 0x100. The repeating diagnostic also switches
            # to white noise, so both sound paths can be checked on the kit.
            self.assertIn(bytes.fromhex("3e80d3403e10d3403e94d340"), data)
            self.assertIn(bytes.fromhex("3ee4d3403ef4d340"), data)
            self.assertIn(bytes.fromhex("3e9fd340"), data)
            self.assertIn(b"\x32\x00\xc0", data)


if __name__ == "__main__":
    unittest.main()


def setUpModule():
    global ROOT, _source_fixture
    _source_fixture, ROOT = clean_module(ROOT)

def tearDownModule():
    _source_fixture.cleanup()
