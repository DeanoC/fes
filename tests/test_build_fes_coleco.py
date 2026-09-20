from __future__ import annotations

import hashlib
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

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
    PLACER_CRITICALITY_EXPONENT,
    PLACER_SEEDS,
    PLACER_TIMING_WEIGHT,
    SEED,
    BuildError,
    COLECO_GPU_ARCHITECTURES,
    COLECO_GPU_BACKEND,
    COLECO_GPU_ROUTER,
    COLECO_TOOLCHAIN_CONFIGURATION,
    _require_gpu_backend,
    _prepare_output,
    _manifest,
    build_commands,
    create_build_record,
)
from scripts import build_fes_coleco_oss, toolchain_cache
from scripts.lockfile import load_lock
from tests.test_build_fes_pong import publish_shared_toolchain


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

    def test_oss_auth_selects_verified_core_hip_lane_directly(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request, manifest = publish_shared_toolchain(
                base,
                lock_path=ROOT / COLECO_TOOLCHAIN_LOCK,
                gpu_router=COLECO_GPU_ROUTER,
                hip_architectures=COLECO_GPU_ARCHITECTURES,
            )
            self.assertEqual(request.gpu_router, COLECO_GPU_ROUTER)
            self.assertEqual(request.hip_architectures, COLECO_GPU_ARCHITECTURES)
            self.assertEqual(
                manifest.configuration,
                {"gpu-router": COLECO_GPU_ROUTER, "hip-architectures": COLECO_GPU_ARCHITECTURES},
            )
            with patch.dict(
                os.environ,
                {"PATH": str(base), "FES_TOOLCHAIN_CACHE_ROOT": str(base / "ignored-cache")},
                clear=True,
            ), patch.object(
                toolchain_cache,
                "host_identity",
                return_value={"system": "Linux", "release": "test", "machine": "x86_64"},
            ), patch.object(
                toolchain_cache,
                "compiler_inventory",
                return_value={"commands": {"cc": {"path": "/test/cc"}}},
            ):
                authenticated = build_fes_coleco_oss._authenticate_coleco_tools(
                    ROOT, cache_root=request.cache_root
                )

            self.assertEqual(authenticated["yosys"].path, manifest.install / "bin/yosys")
            self.assertEqual(
                authenticated["nextpnr-mistral"].path,
                manifest.install / "bin/nextpnr-mistral",
            )
            self.assertIn(COLECO_TOOLCHAIN_CONFIGURATION, authenticated["nextpnr-mistral"].identity)

    def test_oss_auth_rejects_mismatched_staged_hip_configuration(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request, manifest = publish_shared_toolchain(
                base,
                lock_path=ROOT / COLECO_TOOLCHAIN_LOCK,
                gpu_router=COLECO_GPU_ROUTER,
                hip_architectures=COLECO_GPU_ARCHITECTURES,
            )
            commit = manifest.tools["nextpnr"]["commit"]
            configuration_path = manifest.evidence / "nextpnr" / f".config-{commit}.txt"
            configuration_path.chmod(0o644)
            configuration_path.write_text("gpu-router=OFF; hip-architectures=unused\n", encoding="utf-8")
            configuration_path.chmod(0o444)
            ready = toolchain_cache.ready_path(request)
            ready.chmod(0o644)
            data = json.loads(ready.read_text(encoding="utf-8"))
            entry = data["files"][f"evidence/nextpnr/{configuration_path.name}"]
            entry["sha256"] = hashlib.sha256(configuration_path.read_bytes()).hexdigest()
            entry["size"] = configuration_path.stat().st_size
            ready.write_text(json.dumps(data, sort_keys=True) + "\n", encoding="utf-8")
            ready.chmod(0o444)

            with patch.dict(
                os.environ,
                {"PATH": str(base), "FES_TOOLCHAIN_CACHE_ROOT": str(base / "ignored-cache")},
                clear=True,
            ), patch.object(
                toolchain_cache,
                "host_identity",
                return_value={"system": "Linux", "release": "test", "machine": "x86_64"},
            ), patch.object(
                toolchain_cache,
                "compiler_inventory",
                return_value={"commands": {"cc": {"path": "/test/cc"}}},
            ):
                with self.assertRaisesRegex(BuildError, "configuration"):
                    build_fes_coleco_oss._authenticate_coleco_tools(
                        ROOT, cache_root=request.cache_root
                    )

    def test_oss_auth_rejects_a_verified_off_lane_for_coleco(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request, _ = publish_shared_toolchain(
                base,
                lock_path=ROOT / COLECO_TOOLCHAIN_LOCK,
                gpu_router="OFF",
                hip_architectures=COLECO_GPU_ARCHITECTURES,
            )
            with patch.dict(
                os.environ,
                {
                    "PATH": str(base),
                    "FES_TOOLCHAIN_CACHE_ROOT": str(base / "ignored-cache"),
                    "FES_TOOLCHAIN_GPU_ROUTER": "OFF",
                },
                clear=True,
            ), patch.object(
                toolchain_cache,
                "host_identity",
                return_value={"system": "Linux", "release": "test", "machine": "x86_64"},
            ), patch.object(
                toolchain_cache,
                "compiler_inventory",
                return_value={"commands": {"cc": {"path": "/test/cc"}}},
            ):
                with self.assertRaisesRegex(BuildError, "shared toolchain|configuration"):
                    build_fes_coleco_oss._authenticate_coleco_tools(
                        ROOT, cache_root=request.cache_root
                    )

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
        self.assertEqual(SEED, 4)
        self.assertEqual(PLACER_SEEDS[0], SEED)
        self.assertEqual(PLACER_TIMING_WEIGHT, 300)
        self.assertEqual(PLACER_CRITICALITY_EXPONENT, 5)
        self.assertIn("--seed", nextpnr)
        self.assertEqual(nextpnr[nextpnr.index("--seed") + 1], str(SEED))
        self.assertEqual(nextpnr[nextpnr.index("--placer-heap-timingweight") + 1], "300")
        self.assertEqual(nextpnr[nextpnr.index("--placer-heap-critexp") + 1], "5")
        self.assertIn("--router", nextpnr)
        self.assertEqual(nextpnr[nextpnr.index("--router") + 1], "gpu")
        self.assertIn("--timing-allow-fail", nextpnr)
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
        self.assertIn(f'"seed":{SEED}'.encode(), record)
        self.assertIn(b'"seed_order":"4,1,2,3,5,12,7,10"', record)
        self.assertIn(b'"placer_heap_timingweight":300', record)
        self.assertIn(b'"placer_heap_timingweights":"10,100,300,1000,2000"', record)
        self.assertIn(b'"placer_heap_critexp":5', record)
        self.assertIn(b'"placer_qor_mode":"first-pass"', record)
        staged = create_build_record(
            ROOT,
            "https://example.invalid/misteross.git",
            "a" * 40,
            {"yosys": "test"},
            qor_mode="staged",
        )
        self.assertIn(b'"placer_qor_mode":"staged"', staged)
        self.assertIn(b'"router":"gpu"', record)
        self.assertIn(b'"gpu_architectures":"gfx1100;gfx1201"', record)
        self.assertIn(b'"gpu_backend":"hip"', record)

    def test_gpu_route_keeps_the_registered_sprite_ram_mapper_pair(self) -> None:
        pins = load_lock(ROOT / COLECO_TOOLCHAIN_LOCK)
        self.assertEqual(pins["yosys"].commit, "e2d425dee148cc60c50f4e9b354a10d90eab15f4")
        self.assertEqual(pins["nextpnr"].commit, "0fad53a75a0218941c417ec6bb58bdede9070987")

    def test_coleco_uses_a_core_local_toolchain_without_downgrading_main(self) -> None:
        self.assertEqual(COLECO_TOOLCHAIN_LOCK, "cores/fes-coleco/toolchain.lock")
        self.assertEqual(COLECO_TOOLCHAIN_ROOT, "build/toolchain/fes-coleco")
        self.assertIn(COLECO_TOOLCHAIN_LOCK, PINNED_INPUTS)
        global_pins = load_lock(ROOT / "toolchain.lock")
        self.assertEqual(global_pins["yosys"].commit, "ec34fcf38986217af9b5558936044b7197d968a7")
        self.assertEqual(global_pins["nextpnr"].commit, "d672fade461e8a1eba4d3f95895902d86f43b882")

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
        import tomllib
        fields = tomllib.loads(manifest.decode())
        self.assertEqual(fields["abi"], {"id": "fes.application", "major": 1, "minor": 0})
        self.assertEqual({i["id"] for i in fields["interfaces"]},
                         {"fes.gamepad.ports", "fes.keypad.ports", "fes.media.blob", "fes.media.blob-stream", "fes.video.fixed-720p60"})
        self.assertTrue(all(i["required"] for i in fields["interfaces"]))
        self.assertIn("cores/fes-common/rtl/fes_application_gp.v", RTL_SOURCES)
        self.assertIn("cores/fes-coleco/rtl/coleco_application_gp.v", RTL_SOURCES)
        self.assertNotIn("cores/fes-coleco/rtl/fes_computer_gp.v", RTL_SOURCES)

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
            self.assertIn("fes.application", text)
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
