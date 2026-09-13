from __future__ import annotations

import subprocess
import tempfile
import unittest
from pathlib import Path

from scripts.build_fes_coleco import (
    OUTPUT_RELATIVE as QUARTUS_OUTPUT,
    PINNED_INPUTS as QUARTUS_PINNED_INPUTS,
    project_qsf,
)
from scripts.build_fes_coleco_oss import (
    COLECO_TOOLCHAIN_LOCK,
    COLECO_TOOLCHAIN_ROOT,
    OUTPUT_RELATIVE as OSS_OUTPUT,
    PINNED_INPUTS,
    RTL_SOURCES,
    BuildError,
    COLECO_GPU_ARCHITECTURES,
    COLECO_GPU_BACKEND,
    COLECO_TOOLCHAIN_CONFIGURATION,
    _require_gpu_backend,
    _prepare_output,
    _manifest,
    build_commands,
    create_build_record,
)
from scripts.lockfile import load_lock


ROOT = Path(__file__).resolve().parents[1]


class BuildFesColecoTests(unittest.TestCase):
    def test_make_entrypoints_use_both_recipes(self) -> None:
        for target, recipe in (
            ("build-fes-coleco", "scripts/build_fes_coleco_oss.py"),
            ("build-fes-coleco-quartus", "scripts/build_fes_coleco.py"),
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

    def test_coleco_toolchain_entrypoint_selects_core_lock_and_hip_backend(self) -> None:
        result = subprocess.run(
            ["make", "-n", "toolchain-fes-coleco"],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("FES_TOOLCHAIN_LOCKFILE=", result.stdout)
        self.assertIn("cores/fes-coleco/toolchain.lock", result.stdout)
        self.assertIn("FES_TOOLCHAIN_ROOT=", result.stdout)
        self.assertIn("build/toolchain/fes-coleco", result.stdout)
        self.assertIn("FES_TOOLCHAIN_GPU_ROUTER=HIP", result.stdout)
        self.assertIn("FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201'", result.stdout)

    def test_oss_simulation_entrypoint_compiles_conditional_branches(self) -> None:
        result = subprocess.run(
            ["make", "-n", "sim-fes-coleco-oss"],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("-DFES_COLECO_OSS=1", result.stdout)
        self.assertIn('-CFLAGS "-DFES_COLECO_OSS=1"', result.stdout)
        self.assertIn("fes-coleco-machine-oss", result.stdout)
        self.assertIn("fes-coleco-board-oss", result.stdout)
        makefile = (ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertIn("sim-fes-coleco-oss", makefile)
        self.assertIn("coleco-sprite-diagnostic", makefile)
        self.assertIn("--sprites", result.stdout)
        self.assertIn("sprites-16k.rom", result.stdout)

    def test_oss_commands_use_verilog_tv80_and_coleco_memory_shapes(self) -> None:
        yosys, nextpnr = build_commands(
            ROOT,
            ROOT / OSS_OUTPUT,
            "00112233445566778899aabbccddeeff",
            {"yosys": Path("/tmp/yosys"), "nextpnr-mistral": Path("/tmp/nextpnr-mistral")},
        )
        program = yosys[2]
        self.assertIn("tv80_core.v", program)
        self.assertIn("t80pa.v", program)
        self.assertIn("coleco_vdp.sv", program)
        self.assertIn("cores/fes-coleco/rtl/coleco_reset_rom.hex", PINNED_INPUTS)
        self.assertIn("-DFES_COLECO_OSS=1", program)
        self.assertIn("-DTV80_REFRESH=1", program)
        self.assertIn("synth_intel_alm -nolutram -nodsp -top top", program)
        self.assertNotIn("T80pa.vhd", program)
        self.assertIn("cores/fes-coleco/rtl/top.v", program)
        self.assertIn("--freq", nextpnr)
        self.assertIn("74.25", nextpnr)
        self.assertIn("--seed", nextpnr)
        self.assertEqual(nextpnr[nextpnr.index("--seed") + 1], "3")
        self.assertIn("--router", nextpnr)
        self.assertEqual(nextpnr[nextpnr.index("--router") + 1], "gpu")
        self.assertNotIn("router1", nextpnr)
        self.assertNotIn("--tmg-ripup", nextpnr)
        joined = " ".join(nextpnr)
        self.assertIn("cores/fes-coleco/constraints-oss.qsf", joined)
        self.assertIn("cores/fes-coleco/clocks-oss.sdc", joined)
        self.assertNotIn("cores/fes-coleco/clocks.sdc", joined)

        record = create_build_record(
            ROOT,
            "https://example.invalid/misteross.git",
            "a" * 40,
            {"yosys": "test"},
        )
        self.assertIn(b'"seed":3', record)
        self.assertIn(b'"router":"gpu"', record)
        self.assertIn(b'"gpu_architectures":"gfx1100;gfx1201"', record)
        self.assertIn(b'"gpu_backend":"hip"', record)

    def test_gpu_route_keeps_the_registered_sprite_ram_mapper_pair(self) -> None:
        pins = load_lock(ROOT / COLECO_TOOLCHAIN_LOCK)
        self.assertEqual(pins["yosys"].commit, "da6373c0d7565f36036051efc7895fb0d9ac13c3")
        self.assertEqual(pins["nextpnr"].commit, "2d3c216afb7051d2e2070cbf678a50f274b3f786")

    def test_coleco_uses_a_core_local_toolchain_without_downgrading_main(self) -> None:
        self.assertEqual(COLECO_TOOLCHAIN_LOCK, "cores/fes-coleco/toolchain.lock")
        self.assertEqual(COLECO_TOOLCHAIN_ROOT, "build/toolchain/fes-coleco")
        self.assertIn(COLECO_TOOLCHAIN_LOCK, PINNED_INPUTS)
        global_pins = load_lock(ROOT / "toolchain.lock")
        self.assertEqual(global_pins["yosys"].commit, "ec34fcf38986217af9b5558936044b7197d968a7")
        self.assertEqual(global_pins["nextpnr"].commit, "9cbbf7353dd2b818ab73031fcf30d9993578c783")

    def test_oss_rejects_a_cpu_only_gpu_router_binary(self) -> None:
        with self.assertRaisesRegex(BuildError, "device backend"):
            _require_gpu_backend(
                "Info: GPU router: nextpnr was built without a GPU device backend; "
                "falling back to the CPU reference backend.\n"
                "Info: backend cpu-reference ready\n"
            )

    def test_oss_accepts_a_live_hip_gpu_router_backend(self) -> None:
        self.assertEqual(
            _require_gpu_backend(
                "Info: GPU devices: hip:AMD Radeon RX 7900 XTX\n"
                "Info: backend hip:AMD Radeon RX 7900 XTX ready\n"
            ),
            "hip",
        )

    def test_oss_rejects_a_live_cuda_gpu_router_backend(self) -> None:
        with self.assertRaisesRegex(BuildError, "HIP"):
            _require_gpu_backend(
                "Info: backend cuda:NVIDIA RTX 4090 ready\n"
            )

    def test_oss_top_and_ram_keep_the_open_source_boundaries(self) -> None:
        top = (ROOT / "cores/fes-coleco/rtl/top.v").read_text(encoding="utf-8")
        self.assertIn("MISTRAL_IO hdmi_scl_pad", top)
        self.assertIn('BEL = "cyclonev_hps_interface_peripheral_i2c.52.60.0"', top)
        self.assertIn("`ifdef QUARTUS", top)
        self.assertIn("hdmi_scl_low ? 1'b0 : 1'bz", top)
        dpram = (ROOT / "cores/fes-coleco/rtl/coleco_dpram.v").read_text(encoding="utf-8")
        self.assertIn("`ifdef FES_COLECO_OSS", dpram)
        self.assertIn('ram_style = "m10k_tdp"', dpram)
        self.assertIn("assign q_a = q_a_r", dpram)
        vdp = (ROOT / "cores/fes-coleco/rtl/coleco_vdp.sv").read_text(encoding="utf-8")
        self.assertIn('ramstyle = "M10K"', vdp)
        self.assertIn("raster_y", vdp)
        self.assertIn("DATAWIDTH(4)", vdp)
        self.assertIn("SPRITE_RENDER_READ", vdp)

    def test_quartus_vdp_uses_the_registered_multi_read_shape(self) -> None:
        vdp = (ROOT / "cores/fes-coleco/rtl/coleco_vdp.sv").read_text(encoding="utf-8")
        self.assertIn("`elsif QUARTUS", vdp)
        self.assertIn("FES_COLECO_REGISTERED_VDP", vdp)
        self.assertIn("vram_name_block", vdp)
        self.assertIn("vram_pattern_block", vdp)
        self.assertIn("vram_color_block", vdp)
        self.assertIn("vram_sprite_block", vdp)

    def test_oss_rejects_wrong_output_directory(self) -> None:
        with self.assertRaises(BuildError):
            build_commands(
                ROOT,
                ROOT / "build/other",
                "00112233445566778899aabbccddeeff",
                {"yosys": Path("/tmp/yosys"), "nextpnr-mistral": Path("/tmp/nextpnr-mistral")},
            )

    def test_oss_rejects_symlinked_output_directory(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            build_root = root / "build"
            build_root.mkdir()
            target = root / "outside"
            target.mkdir()
            (build_root / OSS_OUTPUT.name).symlink_to(target, target_is_directory=True)
            with self.assertRaisesRegex(BuildError, "symlink"):
                _prepare_output(root)

    def test_oss_rejects_non_directory_build_path(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "build").write_text("not a directory\n", encoding="utf-8")
            with self.assertRaisesRegex(BuildError, "directory"):
                _prepare_output(root)

    def test_quartus_project_lists_only_verilog_sources_and_reset_image(self) -> None:
        qsf = project_qsf(ROOT, ROOT / QUARTUS_OUTPUT / "project", "00112233445566778899aabbccddeeff")
        self.assertIn('VERILOG_MACRO "QUARTUS=1"', qsf)
        self.assertIn('VERILOG_MACRO "FES_COLECO_BUILD_ID=', qsf)
        self.assertIn("cores/fes-coleco/rtl/coleco_machine.sv", qsf)
        self.assertIn("cores/fes-coleco/rtl/tv80/tv80_core.v", qsf)
        self.assertIn("cores/fes-coleco/rtl/coleco_vdp.sv", qsf)
        self.assertNotIn("VHDL_FILE", qsf)
        self.assertIn("cores/fes-coleco/rtl/coleco_reset_rom.hex", QUARTUS_PINNED_INPUTS)
        self.assertIn("cores/fes-coleco/rtl/coleco_reset_rom.mif", QUARTUS_PINNED_INPUTS)

        mif = (ROOT / "cores/fes-coleco/rtl/coleco_reset_rom.mif").read_text(encoding="utf-8")
        self.assertIn("DEPTH = 8192;", mif)
        self.assertIn("0000: C3 00 80 00 00;", mif)

    def test_video_framebuffer_uses_dual_clock_ram_boundary(self) -> None:
        video = (ROOT / "cores/fes-coleco/rtl/coleco_video_720p.v").read_text(encoding="utf-8")
        quartus_recipe = (ROOT / "scripts/build_fes_coleco.py").read_text(encoding="utf-8")
        oss_recipe = (ROOT / "scripts/build_fes_coleco_oss.py").read_text(encoding="utf-8")
        self.assertIn("coleco_video_dpram", video)
        self.assertIn("clock_a", video)
        self.assertIn("clock_b", video)
        self.assertIn("cores/fes-coleco/rtl/coleco_video_dpram.v", quartus_recipe)
        self.assertIn("cores/fes-coleco/rtl/coleco_video_dpram.v", oss_recipe)
        dpram = (ROOT / "cores/fes-coleco/rtl/coleco_video_dpram.v").read_text(encoding="utf-8")
        self.assertIn('read_during_write_mode_port_a = "NEW_DATA_NO_NBE_READ"', dpram)
        self.assertNotIn('read_during_write_mode_port_a = "OLD_DATA"', dpram)

    def test_manifest_carries_coleco_identity(self) -> None:
        record = (
            b'{"format":1,"repository":"https://github.com/DeanoC/misteross.git",'
            b'"revision":"' + (b"a" * 40) + b'","recipe":"scripts/build_fes_coleco_oss.py",'
            b'"recipe_sha256":"' + (b"b" * 64) + b'","abi_definition":"x",'
            b'"abi_definition_sha256":"' + (b"c" * 64) + b'","dependencies":{},'
            b'"tools":{},"parameters":{}}'
        )
        evidence = {
            "build_id": "d" * 32,
            "rbf": {"size": 16, "sha256": "e" * 64},
        }
        tools = {
            "mistral": "commit=" + "f" * 40 + "; sha256=" + "1" * 64,
            "nextpnr-mistral": "commit=" + "2" * 40 + "; sha256=" + "3" * 64,
            "yosys": "commit=" + "4" * 40 + "; sha256=" + "5" * 64,
        }
        manifest = _manifest(
            record,
            evidence,
            "https://github.com/DeanoC/misteross.git",
            "a" * 40,
            tools,
        )
        self.assertIn(b"id = \"fes.coleco\"", manifest)
        self.assertIn(b"FES ColecoVision", manifest)
        self.assertNotIn(b"recipe = ", manifest)

    def test_manifest_carries_gpu_route_configuration(self) -> None:
        record = (
            b'{"format":1,"repository":"https://github.com/DeanoC/misteross.git",'
            b'"revision":"' + (b"a" * 40) + b'","recipe":"scripts/build_fes_coleco_oss.py",'
            b'"recipe_sha256":"' + (b"b" * 64) + b'","abi_definition":"x",'
            b'"abi_definition_sha256":"' + (b"c" * 64) + b'","dependencies":{},'
            b'"tools":{},"parameters":{}}'
        )
        manifest = _manifest(
            record,
            {"build_id": "d" * 32, "rbf": {"size": 16, "sha256": "e" * 64}},
            "https://github.com/DeanoC/misteross.git",
            "a" * 40,
            {
                "mistral": "commit=" + "f" * 40 + "; sha256=" + "1" * 64,
                "nextpnr-mistral": (
                    "commit=" + "2" * 40 + "; sha256=" + "3" * 64
                    + "; " + COLECO_TOOLCHAIN_CONFIGURATION
                ),
                "yosys": "commit=" + "4" * 40 + "; sha256=" + "5" * 64,
            },
        )
        self.assertIn(b"gpu-router=HIP", manifest)
        self.assertIn(COLECO_GPU_ARCHITECTURES.encode(), manifest)

    def test_coleco_gpu_configuration_constants_are_consistent(self) -> None:
        self.assertEqual(COLECO_GPU_BACKEND, "hip")
        self.assertEqual(COLECO_GPU_ARCHITECTURES, "gfx1100;gfx1201")
        self.assertEqual(
            COLECO_TOOLCHAIN_CONFIGURATION,
            "gpu-router=HIP; hip-architectures=gfx1100;gfx1201",
        )

    def test_docs_record_scope_builds_and_workarounds(self) -> None:
        readme = (ROOT / "cores/fes-coleco/README.md").read_text(encoding="utf-8")
        architecture = (ROOT / "docs/architecture.md").read_text(encoding="utf-8")
        for text in (readme, architecture):
            self.assertIn("fes-coleco", text)
            self.assertIn("16 KiB", text)
            self.assertIn("fes.simple-computer", text)
            self.assertIn("build-fes-coleco-quartus", text)
            self.assertIn("build-fes-coleco", text)
            self.assertIn("TV80_REFRESH", text)
            self.assertIn("M10K", text)
            self.assertIn("MISTRAL_IO", text)
            self.assertIn("52.60.0", text)
        self.assertNotIn("/home/deano/", readme)
        self.assertNotIn(".../usr/", readme)


if __name__ == "__main__":
    unittest.main()
