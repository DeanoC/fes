from __future__ import annotations

import hashlib
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from scripts.build_fes_sms import (
    OUTPUT_RELATIVE as QUARTUS_OUTPUT,
    PINNED_INPUTS as QUARTUS_PINNED_INPUTS,
    SYSTEMVERILOG_SOURCES,
    VERILOG_SOURCES,
    _manifest,
    project_qsf,
)
from scripts.build_fes_sms_oss import (
    OUTPUT_RELATIVE as OSS_OUTPUT,
    PINNED_INPUTS as OSS_PINNED_INPUTS,
    PLACER_QOR_BUDGET,
    PLACER_SEEDS,
    PLACER_TIMING_WEIGHTS,
    RTL_SOURCES as OSS_RTL_SOURCES,
    SEED,
    SMS_GPU_ARCHITECTURES,
    SMS_GPU_BACKEND,
    SMS_TOOLCHAIN_LOCK,
    SMS_TOOLCHAIN_ROOT,
    SMS_TOOL_COMMITS,
    build_commands,
    create_build_record,
)
from scripts.lockfile import load_lock


ROOT = Path(__file__).resolve().parents[1]
GENERATOR = ROOT / "cores/fes-sms/diagnostic/generate.py"


class BuildFesSmsTests(unittest.TestCase):
    def test_make_entrypoints_use_both_recipes(self) -> None:
        for target, recipe in (
            ("build-fes-sms", "scripts/build_fes_sms_oss.py"),
            ("build-fes-sms-quartus", "scripts/build_fes_sms.py"),
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

    def test_simulation_entrypoint_is_default_machine_check(self) -> None:
        result = subprocess.run(
            ["make", "-n", "sim-fes-sms"],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("sms_machine", result.stdout)
        self.assertIn("sms_vdp", result.stdout)
        self.assertIn("sms_psg", result.stdout)
        self.assertIn("sms_hdmi_i2s", result.stdout)
        self.assertIn("fes-sms-machine", result.stdout)
        self.assertIn("fes-sms-vdp", result.stdout)
        self.assertIn("fes-sms-psg", result.stdout)
        self.assertIn("fes-sms-i2s", result.stdout)
        self.assertIn("fes-sms-gp", result.stdout)
        self.assertIn("stream-exchanges.json", result.stdout)
        self.assertIn("ENABLE_MEDIA_STREAM=1", result.stdout)
        self.assertNotIn("FES_SMS_OSS", result.stdout)
        self.assertNotIn("fes-sms-machine-oss", result.stdout)
        self.assertNotIn("build_fes_sms_oss", result.stdout)

    def test_oss_simulation_entrypoint_compiles_conditional_branches(self) -> None:
        result = subprocess.run(
            ["make", "-n", "sim-fes-sms-oss"],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("-DFES_SMS_OSS=1", result.stdout)
        self.assertIn("-DFES_COLECO_OSS=1", result.stdout)
        self.assertIn('-CFLAGS "-DFES_SMS_OSS=1 -DFES_COLECO_OSS=1"', result.stdout)
        self.assertIn("fes-sms-machine-oss", result.stdout)
        self.assertNotIn("build_fes_sms_oss", result.stdout)

    def test_oss_toolchain_entrypoint_selects_coleco_compatibility_lock(self) -> None:
        result = subprocess.run(
            ["make", "-n", "toolchain-fes-sms"],
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
        self.assertIn("build/toolchain/fes-sms", result.stdout)
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
        self.assertIn("sms_machine.sv", program)
        self.assertIn("sms_vdp.sv", program)
        self.assertIn("sms_psg.sv", program)
        self.assertIn("sms_hdmi_i2s.v", program)
        self.assertIn("sms_video_720p.v", program)
        self.assertIn("cores/fes-sms/rtl/top.v", program)
        self.assertIn("tv80_core.v", program)
        self.assertIn("coleco_vdp.sv", program)
        self.assertIn("coleco_dpram.v", program)
        self.assertIn("-DFES_SMS_OSS=1", program)
        self.assertIn("-DFES_COLECO_OSS=1", program)
        self.assertIn("-DTV80_REFRESH=1", program)
        self.assertIn("synth_intel_alm -nolutram -nodsp -top top", program)
        self.assertNotIn("coleco_machine.sv", program)
        self.assertNotIn("sg1000_machine.sv", program)
        self.assertNotIn("coleco_reset_rom", program)
        self.assertEqual(SEED, 10)
        self.assertEqual(nextpnr[nextpnr.index("--seed") + 1], str(SEED))
        self.assertEqual(nextpnr[nextpnr.index("--placer-heap-timingweight") + 1], "1000")
        self.assertEqual(nextpnr[nextpnr.index("--placer-heap-critexp") + 1], "5")
        self.assertEqual(nextpnr[nextpnr.index("--router") + 1], "gpu")
        self.assertIn("--timing-allow-fail", nextpnr)
        self.assertNotIn("--tmg-ripup", nextpnr)
        joined = " ".join(nextpnr)
        self.assertIn("cores/fes-sms/constraints-oss.qsf", joined)
        self.assertIn("cores/fes-sms/clocks-oss.sdc", joined)
        self.assertNotIn("cores/fes-sms/clocks.sdc", joined)
        self.assertEqual(SMS_TOOLCHAIN_LOCK, "toolchains/registered-memory.lock")
        self.assertEqual(SMS_TOOLCHAIN_ROOT, "build/toolchain/fes-sms")
        self.assertIn(SMS_TOOLCHAIN_LOCK, OSS_PINNED_INPUTS)
        self.assertEqual(SMS_TOOL_COMMITS["yosys"], "e2d425dee148cc60c50f4e9b354a10d90eab15f4")
        self.assertEqual(SMS_TOOL_COMMITS["nextpnr"], "0fad53a75a0218941c417ec6bb58bdede9070987")
        pins = load_lock(ROOT / SMS_TOOLCHAIN_LOCK)
        from scripts.build_fes_coleco_oss import COLECO_TOOLCHAIN_LOCK
        self.assertEqual(SMS_TOOLCHAIN_LOCK, COLECO_TOOLCHAIN_LOCK)
        self.assertEqual(pins["yosys"].commit, SMS_TOOL_COMMITS["yosys"])
        self.assertEqual(pins["nextpnr"].commit, SMS_TOOL_COMMITS["nextpnr"])
        self.assertEqual(
            [line.strip() for line in
             (ROOT / "cores/fes-sms/clocks-oss.sdc").read_text().splitlines()
             if line.strip() and not line.lstrip().startswith("#")],
            ["create_clock -name FPGA_CLK1_50 -period 20.000 [get_ports {FPGA_CLK1_50}]"],
        )
        sms_pins = (ROOT / "cores/fes-sms/constraints-oss.qsf").read_text(encoding="utf-8")
        coleco_pins = (ROOT / "cores/fes-coleco/constraints-oss.qsf").read_text(encoding="utf-8")
        self.assertNotEqual(sms_pins, coleco_pins)
        self.assertIn("HDMI_I2S0", sms_pins)
        self.assertIn("PIN_T13", sms_pins)
        self.assertIn("HDMI_MCLK", sms_pins)
        self.assertIn("PIN_U11", sms_pins)
        self.assertIn("HDMI_LRCLK", sms_pins)
        self.assertIn("PIN_T11", sms_pins)
        self.assertIn("HDMI_SCLK", sms_pins)
        self.assertIn("PIN_T12", sms_pins)
        self.assertNotIn("HDMI_I2S0", coleco_pins)
        global_pins = load_lock(ROOT / "toolchain.lock")
        self.assertEqual(global_pins["yosys"].commit, "ec34fcf38986217af9b5558936044b7197d968a7")
        self.assertEqual(global_pins["nextpnr"].commit, "30ac6f47bd94aec97467bee9fcd2ff09643fbc55")
        record = create_build_record(
            ROOT,
            "https://example.invalid/misteross.git",
            "a" * 40,
            {"yosys": "test"},
        )
        self.assertIn(f'"seed":{SEED}'.encode(), record)
        self.assertEqual(PLACER_QOR_BUDGET, len(PLACER_SEEDS) * len(PLACER_TIMING_WEIGHTS))
        self.assertGreater(PLACER_QOR_BUDGET, len(PLACER_SEEDS))
        self.assertIn(f'"placer_qor_budget":{PLACER_QOR_BUDGET}'.encode(), record)
        self.assertIn(b'"router":"gpu"', record)
        self.assertIn(b'"gpu_backend":"hip"', record)
        self.assertIn(SMS_GPU_ARCHITECTURES.encode(), record)
        self.assertEqual(SMS_GPU_BACKEND, "hip")
        self.assertIn("cores/fes-sms/rtl/top.v", OSS_RTL_SOURCES)
        self.assertIn("cores/fes-sms/rtl/sms_machine.sv", OSS_RTL_SOURCES)

    def test_quartus_project_reuses_coleco_sibling_modules(self) -> None:
        qsf = project_qsf(ROOT, ROOT / QUARTUS_OUTPUT / "project", "00112233445566778899aabbccddeeff")
        self.assertIn('VERILOG_MACRO "QUARTUS=1"', qsf)
        self.assertIn('VERILOG_MACRO "FES_SMS_BUILD_ID=', qsf)
        self.assertIn("cores/fes-sms/rtl/sms_machine.sv", qsf)
        self.assertIn("cores/fes-sms/rtl/sms_vdp.sv", qsf)
        self.assertIn("cores/fes-sms/rtl/sms_psg.sv", qsf)
        self.assertIn("cores/fes-sms/rtl/sms_hdmi_i2s.v", qsf)
        self.assertIn("cores/fes-sms/rtl/sms_video_720p.v", qsf)
        self.assertIn("cores/fes-sms/rtl/top.v", qsf)
        self.assertIn("cores/fes-common/rtl/tv80/tv80_core.v", qsf)
        self.assertIn("cores/fes-common/rtl/coleco_vdp.sv", qsf)
        self.assertIn("cores/fes-common/rtl/fes_computer_gp.v", qsf)
        self.assertNotIn("VHDL_FILE", qsf)
        self.assertNotIn("coleco_reset_rom", qsf)
        self.assertNotIn("coleco_machine.sv", qsf)
        self.assertNotIn("sg1000_machine.sv", qsf)
        self.assertNotIn("fes.mastersystem", qsf)
        for relative in (*VERILOG_SOURCES, *SYSTEMVERILOG_SOURCES):
            self.assertIn(relative, QUARTUS_PINNED_INPUTS)
        self.assertTrue(all("reset_rom" not in item for item in QUARTUS_PINNED_INPUTS))

    def test_machine_uses_sms_map_int_and_mode4_vdp(self) -> None:
        machine = (ROOT / "cores/fes-sms/rtl/sms_machine.sv").read_text(encoding="utf-8")
        vdp = (ROOT / "cores/fes-sms/rtl/sms_vdp.sv").read_text(encoding="utf-8")
        self.assertIn("sms_vdp", machine)
        self.assertIn("sms_psg", machine)
        self.assertIn("8'h7e", (ROOT / "cores/fes-sms/rtl/sms_psg.sv").read_text(encoding="utf-8"))
        self.assertIn("8'h7f", (ROOT / "cores/fes-sms/rtl/sms_psg.sv").read_text(encoding="utf-8"))
        top = (ROOT / "cores/fes-sms/rtl/top.v").read_text(encoding="utf-8")
        self.assertIn("sms_hdmi_i2s", top)
        self.assertIn("HDMI_I2S0", top)
        self.assertIn("sms_mode4_vdp", vdp)
        self.assertIn("coleco_vdp", vdp)
        self.assertIn("cram [0:31]", vdp)
        self.assertIn("coleco_dpram", machine)
        self.assertIn("ADDRWIDTH(15)", machine)
        self.assertIn("NUMWORDS(32768)", machine)
        self.assertIn("ADDRWIDTH(13)", machine)
        self.assertIn("NUMWORDS(8192)", machine)
        self.assertIn("15'h7fff", machine)
        self.assertIn("cpu_addr[12:0]", machine)
        self.assertIn("2'b11", machine)
        self.assertIn("port_dc", machine)
        self.assertIn("port_dd", machine)
        self.assertIn(".INT_n(vdp_irq_n)", machine)
        self.assertIn(".NMI_n(1'b1)", machine)
        self.assertNotIn("coleco_reset_rom", machine)
        self.assertNotIn("JP 0x8000", machine)
        self.assertNotIn("fes.mastersystem", machine)

    def test_manifest_carries_sms_identity(self) -> None:
        record = (
            b'{"format":1,"repository":"https://github.com/DeanoC/misteross.git",'
            b'"revision":"' + (b"a" * 40) + b'","recipe":"scripts/build_fes_sms.py",'
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
        self.assertIn(b'id = "fes.sms"', manifest)
        self.assertIn(b"FES Master System", manifest)
        self.assertIn(b'version = "1.2.0"', manifest)
        self.assertIn(b"fes.simple-computer", manifest)
        self.assertIn(b'id = "fes.media.blob-stream"', manifest)
        self.assertIn(b"fes.media.blob", manifest)
        self.assertNotIn(b"fes.coleco", manifest)
        self.assertNotIn(b"fes.sg1000", manifest)
        self.assertNotIn(b"fes.mastersystem", manifest)
        self.assertNotIn(b"recipe = ", manifest)

    def test_docs_record_quartus_and_oss_recipes(self) -> None:
        readme = (ROOT / "cores/fes-sms/README.md").read_text(encoding="utf-8")
        architecture = (ROOT / "docs/architecture.md").read_text(encoding="utf-8")
        root_readme = (ROOT / "README.md").read_text(encoding="utf-8")
        for text in (readme, architecture, root_readme):
            self.assertIn("fes-sms", text)
            self.assertIn("fes.sms", text)
            self.assertIn("build-fes-sms-quartus", text)
            self.assertIn("build-fes-sms", text)
            self.assertIn("sim-fes-sms", text)
            self.assertIn("sim-fes-sms-oss", text)
            self.assertIn("fes.simple-computer", text)
            self.assertRegex(text, r"(not `fes\.mastersystem`|Do not use `fes\.mastersystem`)")
        self.assertNotIn("/home/deano/", readme)
        self.assertIn("Quartus", readme)
        self.assertIn("8 KiB", readme)
        self.assertIn("32 KiB", readme)
        self.assertIn("blob-stream", readme)
        self.assertIn("FES_COLECO_OSS", readme)
        self.assertIn("Mode 4", readme)
        self.assertIn("SN76489", readme)
        self.assertIn("0x7E", readme)
        self.assertIn("I2S", readme)
        for text in (readme, architecture, root_readme):
            self.assertIn("first-pass", text)
            self.assertIn("HeAP 1000", text)
        self.assertIn("e2d425de", readme)
        self.assertIn("later jobs", readme.lower())

    def test_diagnostic_is_reproducible_and_enters_at_reset(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "mode4.rom"
            preview = Path(directory) / "mode4.ppm"
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
            self.assertEqual(data[4:7], bytes((0xC3, 0x00, 0x40)))  # JP 4000
            self.assertIn(b"\x31\xf0\xdf", data)  # LD SP,DFF0
            self.assertIn(b"\x32\x00\xc0", data)
            self.assertIn(b"\xd3\x7f", data)
            self.assertIn(b"\xd3\x7e", data)
            self.assertGreater(len(data), 16384)
            self.assertLessEqual(len(data), 32768)
            self.assertNotEqual(data[0x4000], 0xFF)
            self.assertIn(bytes((0x76, 0x18, 0xFD)), data)
            digest = hashlib.sha256(data).hexdigest()
            second = Path(directory) / "again.rom"
            subprocess.run(
                [sys.executable, str(GENERATOR), "--output", str(second)],
                cwd=ROOT,
                check=True,
            )
            self.assertEqual(second.read_bytes(), data)
            self.assertEqual(hashlib.sha256(second.read_bytes()).hexdigest(), digest)
            too_small = subprocess.run(
                [sys.executable, str(GENERATOR), "--output", str(Path(directory) / "bad.rom"),
                 "--pad-to", "16384"],
                cwd=ROOT,
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertNotEqual(too_small.returncode, 0)
            self.assertIn("32768", too_small.stderr)
            padded = Path(directory) / "padded.rom"
            subprocess.run(
                [sys.executable, str(GENERATOR), "--output", str(padded), "--pad-to", "32768"],
                cwd=ROOT,
                check=True,
            )
            self.assertEqual(padded.read_bytes(), data + b"\xff" * (32768 - len(data)))
            self.assertTrue(preview.is_file())
            self.assertGreater(preview.stat().st_size, 1000)
            header, _, pixels = preview.read_bytes().partition(b"\n255\n")
            self.assertEqual(header, b"P6\n1280 720")
            self.assertEqual(len(pixels), 1280 * 720 * 3)

            def ppm_at(x: int, y: int) -> tuple[int, int, int]:
                offset = (y * 1280 + x) * 3
                return (pixels[offset], pixels[offset + 1], pixels[offset + 2])

            # Logical (4,0) is an opaque plus pixel (white); (0,0) is transparent (black).
            self.assertEqual(ppm_at(393, 168), (255, 255, 255))
            self.assertEqual(ppm_at(385, 168), (0, 0, 0))
            self.assertNotEqual(ppm_at(393, 168), (0, 255, 64))
            hil = Path(directory) / "hil.rom"
            subprocess.run(
                [sys.executable, str(GENERATOR), "--output", str(hil), "--interactive"],
                cwd=ROOT,
                check=True,
            )
            hil_data = hil.read_bytes()
            self.assertEqual(hil_data[4:7], bytes((0xC3, 0x00, 0x40)))
            self.assertGreater(len(hil_data), 16384)
            self.assertNotIn(bytes((0x76, 0x18, 0xFD)), hil_data)
            self.assertTrue(any(hil_data[i] == 0xC3 and hil_data[i + 2] == 0x40
                                for i in range(0x4000, len(hil_data) - 2)))

    def test_published_stream_pin_and_required_interface(self) -> None:
        pin = (ROOT / "docs/contracts/MISTER-PACKAGES-PIN.txt").read_text(encoding="utf-8")
        self.assertIn("c8c8dfd1fcb0503ac92baf6b365d93e8d26a0854", pin)
        fixtures = (ROOT / "cores/fes-sms/generated/stream-exchanges.json").read_bytes()
        self.assertEqual(
            hashlib.sha256(fixtures).hexdigest(),
            "3b186ea15c6cbed8c682f09c7a17824a03afaefae12d264ae17282461d8e854f",
        )
        contract = (ROOT / "docs/contracts/media-stream-1.0.md").read_bytes()
        self.assertEqual(
            hashlib.sha256(contract).hexdigest(),
            "aed93f66983d6edf09d4f1ea926027ebc700e95af266ae3872a2840aec9a79cb",
        )
        header = (ROOT / "cores/fes-sms/generated/fes_simple_computer.vh").read_bytes()
        self.assertEqual(
            hashlib.sha256(header).hexdigest(),
            "fd074e6958ea16ff277a5071bcd1e0c7b78fc984c8e7a58f9eea61c974324caa",
        )
        top = (ROOT / "cores/fes-sms/rtl/top.v").read_text(encoding="utf-8")
        self.assertIn("ENABLE_MEDIA_STREAM(1)", top)
        gp = (ROOT / "cores/fes-common/rtl/fes_computer_gp.v").read_text(encoding="utf-8")
        self.assertIn("if (!ENABLE_MEDIA_STREAM || media_open)", gp)
        from scripts.build_fes_sms_oss import _manifest as oss_manifest_fn

        record = (
            b'{"format":1,"repository":"https://github.com/DeanoC/misteross.git",'
            b'"revision":"' + (b"a" * 40) + b'","recipe":"scripts/build_fes_sms_oss.py",'
            b'"recipe_sha256":"' + (b"b" * 64) + b'","abi_definition":"x",'
            b'"abi_definition_sha256":"' + (b"c" * 64) + b'","dependencies":{},'
            b'"tools":{},"parameters":{}}'
        )
        evidence = {
            "build_id": "d" * 32,
            "rbf": {"size": 16, "sha256": "e" * 64},
        }
        oss = oss_manifest_fn(
            record,
            evidence,
            "https://github.com/DeanoC/misteross.git",
            "a" * 40,
            {"yosys": "test"},
        )
        self.assertIn(b'id = "fes.media.blob-stream"', oss)
        self.assertIn(b"required = true", oss)


if __name__ == "__main__":
    unittest.main()
