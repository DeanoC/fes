from __future__ import annotations

import json
import os
import hashlib
import shutil
import subprocess
import tempfile
import tomllib
import unittest
from pathlib import Path
from unittest.mock import patch

from scripts import build_fes_pong
from scripts import toolchain_cache
from scripts.build_fes_pong import (
    AuthenticatedTool,
    BuildError,
    build,
    build_commands,
    create_build_record,
    validate_build_evidence,
)
from scripts.export_core_package import build_identity
from scripts.lockfile import load_lock


ROOT = Path(__file__).resolve().parents[1]
PLL_PARAMETERS = {
    "duty_cycle0": "00000000000000000000000000110010",
    "fractional_vco_multiplier": "true",
    "number_of_clocks": "00000000000000000000000000000001",
    "operation_mode": "direct",
    "output_clock_frequency0": "74.25 MHz",
    "phase_shift0": "0 ps",
    "reference_clock_frequency": "50.0 MHz",
}


def _write_fake_tool(
    path: Path,
    tool_name: str,
    *,
    include_target: bool = True,
    mutation_target: Path | None = None,
) -> None:
    target_output = "5CSEBA6U23I7 fake database" if include_target else "other device database"
    mutation = ""
    if mutation_target is not None:
        chmod_command = shutil.which("chmod") or "/bin/chmod"
        quoted_chmod = chmod_command.replace("'", "'\"'\"'")
        quoted_target = str(mutation_target).replace("'", "'\"'\"'")
        mutation = (
            "if [ \"${1:-}\" = '--version' ]; then\n"
            f"  '{quoted_chmod}' u+w '{quoted_target}'\n"
            f"  printf '%s\\n' '# tampered by identity probe' >> '{quoted_target}'\n"
            f"  '{quoted_chmod}' u-w '{quoted_target}'\n"
            "fi\n"
        )
    path.write_text(
        "#!/bin/sh\n"
        + mutation
        + "case \"${1:-}\" in\n"
        f"  models) printf '%s\\n' '{target_output}' ;;\n"
        f"  *) printf '%s\\n' 'fake {tool_name} identity' ;;\n"
        "esac\n",
        encoding="utf-8",
    )
    path.chmod(0o755)


def publish_shared_toolchain(
    base: Path,
    *,
    lock_path: Path | None = None,
    gpu_router: str = "OFF",
    hip_architectures: str = toolchain_cache.DEFAULT_HIP_ARCHITECTURES,
    mistral_target: bool = True,
    mutate_after_probe: bool = False,
) -> tuple[toolchain_cache.ToolchainRequest, toolchain_cache.ToolchainManifest]:
    """Publish a complete five-tool lane through the real cache API."""

    cache = base / "shared-cache"
    selected_lock = ROOT / "toolchain.lock" if lock_path is None else Path(lock_path)
    host = {"system": "Linux", "release": "test", "machine": "x86_64"}
    compiler = {"commands": {"cc": {"path": "/test/cc"}}}
    environment = {
        "PATH": str(base / "decoy-bin"),
        "FES_TOOLCHAIN_CACHE_ROOT": str(cache),
        "FES_TOOLCHAIN_GPU_ROUTER": gpu_router,
        "FES_TOOLCHAIN_HIP_ARCHITECTURES": hip_architectures,
    }
    with patch.dict(os.environ, environment, clear=True), patch.object(
        toolchain_cache, "host_identity", return_value=host
    ), patch.object(toolchain_cache, "compiler_inventory", return_value=compiler):
        request = toolchain_cache.request_from_environment(
            ROOT,
            selected_lock,
            gpu_router=gpu_router,
            hip_architectures=hip_architectures,
        )

    cache.mkdir(mode=0o700)
    cache.chmod(0o700)
    slot = toolchain_cache.slot_path(request)
    for directory in (slot / "src", slot / "build", slot / "install" / "bin", slot / "evidence"):
        directory.mkdir(parents=True, exist_ok=True)
    pins = load_lock(selected_lock)
    tools = {}
    for lock_name, binary_name in toolchain_cache.TOOL_BINARY_NAMES.items():
        binary = slot / "install" / "bin" / binary_name
        _write_fake_tool(
            binary,
            lock_name,
            include_target=mistral_target,
            mutation_target=slot / "install" / "bin/mistral-cv"
            if mutate_after_probe and lock_name == "yosys"
            else None,
        )
        tool_evidence = slot / "evidence" / lock_name
        tool_evidence.mkdir(parents=True)
        commit = pins[lock_name].commit
        digest = hashlib.sha256(binary.read_bytes()).hexdigest()
        (tool_evidence / f".built-{commit}").write_text(f"commit={commit}\n", encoding="utf-8")
        (tool_evidence / f".digest-{commit}.sha256").write_text(f"{digest}\n", encoding="utf-8")
        (tool_evidence / f".identity-{commit}.txt").write_text(
            f"fake {lock_name} identity\n", encoding="utf-8"
        )
        if lock_name == "nextpnr":
            lane_architectures = hip_architectures if gpu_router.upper() == "HIP" else "unused"
            (tool_evidence / f".config-{commit}.txt").write_text(
                f"gpu-router={gpu_router.upper()}; hip-architectures={lane_architectures}\n",
                encoding="utf-8",
            )
        tools[lock_name] = {
            "binary": f"bin/{binary_name}",
            "commit": commit,
            "identity": f"fake {lock_name} identity",
        }

    with toolchain_cache.acquire_build_lock(request):
        manifest = toolchain_cache.publish_ready(request, tools=tools)
    return request, toolchain_cache.verify_ready(request)


class BuildFesPongTests(unittest.TestCase):
    def test_make_entrypoint_uses_the_fixed_recipe(self) -> None:
        result = subprocess.run(
            ["make", "-n", "build-fes-pong"],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.stdout.strip(),
            f'python3 scripts/build_fes_pong.py --root "{ROOT}"',
        )

    def test_commands_pin_exact_design_build_id_clock_and_outputs(self) -> None:
        build_id = "00112233445566778899aabbccddeeff"
        tools = {
            "yosys": Path("/authenticated/bin/yosys"),
            "nextpnr-mistral": Path("/authenticated/bin/nextpnr-mistral"),
        }
        yosys, nextpnr = build_commands(ROOT, ROOT / "build/fes-pong", build_id, tools)

        self.assertEqual(yosys[0], "/authenticated/bin/yosys")
        self.assertEqual(yosys[1], "-p")
        program = yosys[2]
        self.assertIn("read_verilog -sv -I cores/fes-pong/generated", program)
        for source in build_fes_pong.RTL_SOURCES:
            self.assertIn(source, program)
        self.assertIn("chparam -set BUILD_ID 128'h00112233445566778899aabbccddeeff top", program)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", program)
        self.assertIn("write_json build/fes-pong/synth.json", program)

        self.assertEqual(nextpnr[0], "/authenticated/bin/nextpnr-mistral")
        self.assertEqual(nextpnr[nextpnr.index("--device") + 1], "5CSEBA6U23I7")
        self.assertEqual(nextpnr[nextpnr.index("--json") + 1], "build/fes-pong/synth.json")
        self.assertEqual(nextpnr[nextpnr.index("--qsf") + 1], "cores/fes-pong/constraints.qsf")
        self.assertEqual(nextpnr[nextpnr.index("--sdc") + 1], "boards/de10nano/clocks.sdc")
        self.assertEqual(nextpnr[nextpnr.index("--freq") + 1], "74.25")
        self.assertEqual(nextpnr[nextpnr.index("--seed") + 1], "1")
        self.assertEqual(nextpnr[nextpnr.index("--router") + 1], "gpu")
        self.assertEqual(nextpnr[nextpnr.index("--rbf") + 1], "build/fes-pong/core.rbf")
        self.assertEqual(nextpnr[nextpnr.index("--write") + 1], "build/fes-pong/routed.json")
        self.assertEqual(nextpnr[nextpnr.index("--report") + 1], "build/fes-pong/timing.json")
        self.assertIn("--compress-rbf", nextpnr)
        self.assertIn("--detailed-timing-report", nextpnr)

    def test_pll_and_pin_constraints_are_exact(self) -> None:
        pll = (ROOT / "cores/fes-pong/rtl/pixel_pll.v").read_text(encoding="utf-8")
        for declaration in (
            '.reference_clock_frequency("50.0 MHz")',
            ".number_of_clocks(1)",
            '.output_clock_frequency0("74.25 MHz")',
            '.phase_shift0("0 ps")',
            ".duty_cycle0(50)",
            '.operation_mode("direct")',
            '.fractional_vco_multiplier("true")',
        ):
            self.assertEqual(pll.count(declaration), 1, declaration)

        qsf = (ROOT / "cores/fes-pong/constraints.qsf").read_text(encoding="utf-8")
        expected_pins = {
            "FPGA_CLK1_50": "PIN_V11",
            "HDMI_TX_CLK": "PIN_AG5",
            "HDMI_TX_DE": "PIN_AD19",
            "HDMI_TX_HS": "PIN_T8",
            "HDMI_TX_VS": "PIN_V13",
            "HDMI_TX_D[0]": "PIN_AD12",
            "HDMI_TX_D[1]": "PIN_AE12",
            "HDMI_TX_D[2]": "PIN_W8",
            "HDMI_TX_D[3]": "PIN_Y8",
            "HDMI_TX_D[4]": "PIN_AD11",
            "HDMI_TX_D[5]": "PIN_AD10",
            "HDMI_TX_D[6]": "PIN_AE11",
            "HDMI_TX_D[7]": "PIN_Y5",
            "HDMI_TX_D[8]": "PIN_AF10",
            "HDMI_TX_D[9]": "PIN_Y4",
            "HDMI_TX_D[10]": "PIN_AE9",
            "HDMI_TX_D[11]": "PIN_AB4",
            "HDMI_TX_D[12]": "PIN_AE7",
            "HDMI_TX_D[13]": "PIN_AF6",
            "HDMI_TX_D[14]": "PIN_AF8",
            "HDMI_TX_D[15]": "PIN_AF5",
            "HDMI_TX_D[16]": "PIN_AE4",
            "HDMI_TX_D[17]": "PIN_AH2",
            "HDMI_TX_D[18]": "PIN_AH4",
            "HDMI_TX_D[19]": "PIN_AH5",
            "HDMI_TX_D[20]": "PIN_AH6",
            "HDMI_TX_D[21]": "PIN_AG6",
            "HDMI_TX_D[22]": "PIN_AF9",
            "HDMI_TX_D[23]": "PIN_AE8",
            "HDMI_I2C_SCL": "PIN_U10",
            "HDMI_I2C_SDA": "PIN_AA4",
        }
        for signal, pin in expected_pins.items():
            self.assertEqual(qsf.count(f"set_location_assignment {pin} -to {signal}\n"), 1)
        self.assertEqual(qsf.count('set_instance_assignment -name IO_STANDARD "3.3-V LVTTL"'), 8)

        game = (ROOT / "cores/pong/rtl/pong_game.sv").read_text(encoding="utf-8")
        self.assertIn("output logic signed [9:0] ball_x, ball_y, player_y, ai_y", game)
        self.assertIn("logic signed [2:0] vertical_speed", game)
        self.assertIn("logic signed [9:0] next_x, next_y", game)
        self.assertNotIn("output integer ball_x", game)

    def test_build_record_is_canonical_and_self_contained(self) -> None:
        identities = {
            "mistral": "commit=" + "b" * 40 + "; sha256=" + "1" * 64,
            "nextpnr-mistral": "commit=" + "c" * 40 + "; sha256=" + "2" * 64,
            "yosys": "commit=" + "d" * 40 + "; sha256=" + "3" * 64,
        }
        record = create_build_record(
            ROOT,
            "https://github.com/DeanoC/misteross.git",
            "a" * 40,
            identities,
        )
        fields = json.loads(record)

        self.assertEqual(record, json.dumps(fields, ensure_ascii=False, separators=(",", ":"), sort_keys=True).encode() + b"\n")
        self.assertEqual(fields["format"], 1)
        self.assertEqual(fields["dependencies"], {})
        self.assertEqual(fields["recipe"], "scripts/build_fes_pong.py")
        self.assertEqual(fields["abi_definition"], "cores/fes-pong/generated/fes_gp.vh")
        self.assertEqual(fields["tools"], identities)
        self.assertEqual(
            fields["parameters"],
            {
                "device": "5CSEBA6U23I7",
                "gpu_architectures": "gfx1100;gfx1201",
                "gpu_backend": "hip",
                "pixel_clock_hz": 74250000,
                "pll_fractional_vco_multiplier": True,
                "reference_clock_hz": 50000000,
                "router": "gpu",
                "seed": 1,
                "top": "top",
            },
        )

    def test_shared_lane_uses_verified_direct_tools_without_local_or_path_fallback(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            decoy = base / "decoy-bin"
            decoy.mkdir()
            marker = base / "path-used"
            for name in ("yosys", "mistral-cv", "nextpnr-mistral"):
                _write_fake_tool(decoy / name, f"decoy {name}")
                (decoy / name).write_text(
                    "#!/bin/sh\nprintf '%s\\n' invoked > %s\nexit 99\n" % (name, marker),
                    encoding="utf-8",
                )
                (decoy / name).chmod(0o755)
            request, manifest = publish_shared_toolchain(base, gpu_router="HIP")

            with patch.dict(
                os.environ,
                {"PATH": str(decoy), "FES_TOOLCHAIN_CACHE_ROOT": str(base / "ignored-cache")},
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
                authenticated = build_fes_pong._authenticate_tools(
                    ROOT, cache_root=request.cache_root
                )

            self.assertEqual(
                {name: tool.path for name, tool in authenticated.items()},
                {
                    "yosys": manifest.install / "bin/yosys",
                    "mistral": manifest.install / "bin/mistral-cv",
                    "nextpnr-mistral": manifest.install / "bin/nextpnr-mistral",
                },
            )
            self.assertFalse(marker.exists(), "shared authentication must never execute PATH decoys")
            for lock_name, record in manifest.tools.items():
                if lock_name in {"yosys", "mistral", "nextpnr"}:
                    self.assertIn(f"commit={record['commit']}", authenticated[{
                        "yosys": "yosys",
                        "mistral": "mistral",
                        "nextpnr": "nextpnr-mistral",
                    }[lock_name]].identity)

            identities = {name: tool.identity for name, tool in authenticated.items()}
            shared_record = create_build_record(
                ROOT,
                "https://github.com/DeanoC/misteross.git",
                "a" * 40,
                identities,
            )
            fields = json.loads(shared_record)
            self.assertEqual(fields["format"], 1)
            self.assertEqual(fields["tools"], identities)
            local_tools = {
                name: AuthenticatedTool(Path("/legacy/install") / name, tool.identity)
                for name, tool in authenticated.items()
            }
            local_record = create_build_record(
                ROOT,
                "https://github.com/DeanoC/misteross.git",
                "a" * 40,
                {name: tool.identity for name, tool in local_tools.items()},
            )
            self.assertEqual(shared_record, local_record)
            self.assertEqual(build_identity(shared_record), build_identity(local_record))

    def test_shared_lane_runs_the_mistral_target_database_probe(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request, _ = publish_shared_toolchain(base, gpu_router="HIP", mistral_target=False)
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
                with self.assertRaisesRegex(BuildError, "Mistral database"):
                    build_fes_pong._authenticate_tools(ROOT, cache_root=request.cache_root)

    def test_shared_lane_rechecks_the_closure_after_identity_probes(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request, _ = publish_shared_toolchain(base, gpu_router="HIP", mutate_after_probe=True)
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
                with self.assertRaisesRegex(
                    BuildError,
                    "shared toolchain verification failed after identity probes: .*closure differs",
                ):
                    build_fes_pong._authenticate_tools(ROOT, cache_root=request.cache_root)

    def test_shared_lane_rejects_tampered_ready_provenance_without_fallback(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request, _ = publish_shared_toolchain(base, gpu_router="HIP")
            ready = toolchain_cache.ready_path(request)
            ready.chmod(0o644)
            data = json.loads(ready.read_text(encoding="utf-8"))
            data["tools"]["yosys"]["commit"] = "0" * 40
            ready.write_text(json.dumps(data, sort_keys=True) + "\n", encoding="utf-8")
            ready.chmod(0o444)

            with patch.dict(
                os.environ,
                {"PATH": str(base), "FES_TOOLCHAIN_CACHE_ROOT": str(base / "ignored-cache")},
                clear=True,
            ), patch.object(toolchain_cache, "host_identity", return_value={"system": "Linux"}), patch.object(
                toolchain_cache,
                "compiler_inventory",
                return_value={"commands": {"cc": {"path": "/test/cc"}}},
            ):
                with self.assertRaisesRegex(BuildError, "shared toolchain"):
                    build_fes_pong._authenticate_tools(ROOT, cache_root=request.cache_root)

    def test_shared_lane_rejects_partial_slot_without_fallback(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request, _ = publish_shared_toolchain(base, gpu_router="HIP")
            toolchain_cache.ready_path(request).unlink()

            with patch.dict(
                os.environ,
                {"PATH": str(base), "FES_TOOLCHAIN_CACHE_ROOT": str(base / "ignored-cache")},
                clear=True,
            ), patch.object(toolchain_cache, "host_identity", return_value={"system": "Linux"}), patch.object(
                toolchain_cache,
                "compiler_inventory",
                return_value={"commands": {"cc": {"path": "/test/cc"}}},
            ):
                with self.assertRaisesRegex(BuildError, "shared toolchain"):
                    build_fes_pong._authenticate_tools(ROOT, cache_root=request.cache_root)

    def test_local_lane_keeps_legacy_paths_when_shared_opt_in_is_absent(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "toolchain.lock").write_bytes((ROOT / "toolchain.lock").read_bytes())
            install = root / "build/toolchain/install/bin"
            build_root = root / "build/toolchain/build"
            install.mkdir(parents=True)
            definitions = {
                "yosys": ("yosys", "--version"),
                "mistral": ("mistral-cv", "models"),
                "nextpnr": ("nextpnr-mistral", "--version"),
            }
            pins = load_lock(root / "toolchain.lock")
            for lock_name, (binary_name, _) in definitions.items():
                binary = install / binary_name
                _write_fake_tool(binary, lock_name)
                evidence = build_root / lock_name
                evidence.mkdir(parents=True)
                commit = pins[lock_name].commit
                (evidence / f".built-{commit}").write_text(f"commit={commit}\n", encoding="utf-8")
                digest = hashlib.sha256(binary.read_bytes()).hexdigest()
                (evidence / f".digest-{commit}.sha256").write_text(f"{digest}\n", encoding="utf-8")
                if lock_name == "nextpnr":
                    (evidence / f".config-{commit}.txt").write_text(
                        "gpu-router=HIP; hip-architectures=gfx1100;gfx1201\n",
                        encoding="utf-8",
                    )

            with patch.dict(os.environ, {}, clear=True):
                authenticated = build_fes_pong._authenticate_tools(root)
            self.assertEqual(authenticated["yosys"].path, install / "yosys")
            self.assertEqual(authenticated["mistral"].path, install / "mistral-cv")
            self.assertEqual(authenticated["nextpnr-mistral"].path, install / "nextpnr-mistral")

    def _write_passing_outputs(self, output: Path, *, achieved: float = 90.0) -> None:
        output.mkdir(parents=True, exist_ok=True)
        (output / "synth.json").write_text(
            json.dumps(
                {
                    "modules": {
                        "top": {
                            "cells": {
                                "pll": {
                                    "type": "altera_pll",
                                    "parameters": dict(PLL_PARAMETERS),
                                },
                                "hps": {"type": "cyclonev_hps_interface_mpu_general_purpose"},
                            }
                        }
                    }
                }
            ),
            encoding="utf-8",
        )
        (output / "routed.json").write_text(
            json.dumps(
                {
                    "modules": {
                        "top": {
                            "cells": {
                                "pll": {
                                    "type": "altera_pll",
                                    "parameters": dict(PLL_PARAMETERS),
                                }
                            }
                        }
                    }
                }
            )
            + "\n",
            encoding="utf-8",
        )
        for filename in ("synth.json", "routed.json"):
            design = json.loads((output / filename).read_text())
            cells = design["modules"]["top"]["cells"]
            design["modules"]["top"]["ports"] = {
                "HDMI_I2C_SCL": {"direction": "inout", "bits": [20]},
                "HDMI_I2C_SDA": {"direction": "inout", "bits": [21]},
            }
            cells["hdmi_i2c"] = {
                "type": "cyclonev_hps_interface_peripheral_i2c",
                "attributes": {"NEXTPNR_BEL" if filename == "routed.json" else "BEL":
                               "cyclonev_hps_interface_peripheral_i2c.52.60.0"},
                "connections": {"out_clk": [10], "out_data": [11], "scl": [12], "sda": [13]},
            }
            for name, enable, feedback, pin, bel in (
                ("hdmi_scl_pad", 10, 12, "PIN_U10", "MISTRAL_IO.6.0.0"),
                ("hdmi_sda_pad", 11, 13, "PIN_AA4", "MISTRAL_IO.4.0.2"),
            ):
                cells[name] = {"type": "MISTRAL_IO",
                    "attributes": {"LOC": pin, "NEXTPNR_BEL": bel},
                    "connections": {"I": ["0"], "OE": [enable], "O": [feedback], "PAD": [20 if enable == 10 else 21]}}
            (output / filename).write_text(json.dumps(design))
        (output / "core.rbf").write_bytes(b"rbf\n")
        (output / "nextpnr.log").write_text(
            "Info: constraining clock net 'FPGA_CLK1_50' to 50.00 MHz\n"
            "Info: PLL 'video_clock.pll': fractional-N requested 74250000.000000 Hz, "
            "achieved 74249999.832439542 Hz, error -0.00225670649 ppm.\n"
            "Info: PLL 'video_clock.pll': 50 MHz -> 74.25 MHz, direct, M=8 N=1 C6=6, "
            "bel altera_pll.0.14.0\n"
            "Info: backend hip:AMD Radeon RX 7900 XTX ready\n"
            "Info: Program finished normally.\n",
            encoding="utf-8",
        )
        (output / "timing.json").write_text(
            json.dumps(
                {
                    "fmax": {
                        "pixel_clk": {"constraint": 74.25, "achieved": achieved},
                    },
                    "utilization": {
                        "MISTRAL_COMB": {"used": 100, "available": 83820},
                        "MISTRAL_FF": {"used": 80, "available": 167640},
                        "MISTRAL_IO": {"used": 30, "available": 472},
                        "MISTRAL_BUF": {"used": 8, "available": 0},
                        "MISTRAL_CLKENA": {"used": 2, "available": 2},
                        "altera_pll": {"used": 1, "available": 2},
                        "cyclonev_hps_interface_mpu_general_purpose": {"used": 1, "available": 1},
                        "cyclonev_oscillator": {"used": 0, "available": 1},
                        "cyclonev_hps_interface_peripheral_i2c": {"used": 1, "available": 4},
                        "MISTRAL_M10K": {"used": 0, "available": 553},
                        "MISTRAL_MUL9X9": {"used": 0, "available": 112},
                        "MISTRAL_MUL18X18": {"used": 0, "available": 112},
                        "MISTRAL_MUL18X19": {"used": 0, "available": 112},
                        "MISTRAL_MUL18X19_COMBINED": {"used": 0, "available": 112},
                        "MISTRAL_MUL27X27": {"used": 0, "available": 112},
                    },
                }
            ),
            encoding="utf-8",
        )

    def test_evidence_rejects_unsafe_i2c_wiring(self) -> None:
        mutations = (
            ("hdmi_scl_pad", "connections", "I", ["1"]),
            ("hdmi_scl_pad", "connections", "PAD", [21]),
            ("hdmi_scl_pad", "connections", "OE", [11]),
            ("hdmi_sda_pad", "connections", "O", [12]),
            ("hdmi_scl_pad", "attributes", "LOC", "PIN_AA4"),
            ("hdmi_i2c", "attributes", "NEXTPNR_BEL", "cyclonev_hps_interface_peripheral_i2c.52.59.0"),
        )
        for cell, group, key, value in mutations:
            with self.subTest(cell=cell, key=key), tempfile.TemporaryDirectory() as directory:
                output = Path(directory)
                self._write_passing_outputs(output)
                path = output / "routed.json"
                design = json.loads(path.read_text())
                design["modules"]["top"]["cells"][cell][group][key] = value
                path.write_text(json.dumps(design))
                with self.assertRaisesRegex(BuildError, "I2C"):
                    validate_build_evidence(output)

    def test_evidence_rejects_missing_hdmi_i2c(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            self._write_passing_outputs(output)
            for filename in ("synth.json", "routed.json"):
                design = json.loads((output / filename).read_text())
                design["modules"]["top"]["cells"].pop("hdmi_i2c", None)
                (output / filename).write_text(json.dumps(design))
            with self.assertRaisesRegex(BuildError, "I2C"):
                validate_build_evidence(output)

    def test_evidence_requires_exact_resources_route_and_both_clocks(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            self._write_passing_outputs(output)
            summary = validate_build_evidence(output)
            self.assertEqual(summary["status"], "pass")
            self.assertEqual(summary["timing"]["pixel"]["requested_mhz"], 74.25)
            self.assertEqual(
                summary["timing"]["reference"],
                {
                    "clock": "FPGA_CLK1_50",
                    "constraint_mhz": 50.0,
                    "evidence": "boards/de10nano/clocks.sdc and routed PLL",
                    "requested_mhz": 50.0,
                    "status": "pass",
                },
            )
            self.assertEqual(summary["resources"]["altera_pll"]["used"], 1)
            self.assertEqual(summary["resources"]["cyclonev_oscillator"]["used"], 0)

            self._write_passing_outputs(output, achieved=74.249)
            with self.assertRaisesRegex(BuildError, "74.25"):
                validate_build_evidence(output)

            self._write_passing_outputs(output, achieved=74.25001)
            timing = json.loads((output / "timing.json").read_text(encoding="utf-8"))
            timing["fmax"]["pixel_clk"]["constraint"] = 74.25005
            (output / "timing.json").write_text(json.dumps(timing), encoding="utf-8")
            with self.assertRaisesRegex(BuildError, "74.25005"):
                validate_build_evidence(output)

            self._write_passing_outputs(output)
            timing = json.loads((output / "timing.json").read_text(encoding="utf-8"))
            timing["utilization"]["MISTRAL_M10K"]["used"] = 1
            (output / "timing.json").write_text(json.dumps(timing), encoding="utf-8")
            with self.assertRaisesRegex(BuildError, "MISTRAL_M10K"):
                validate_build_evidence(output)

            self._write_passing_outputs(output)
            timing = json.loads((output / "timing.json").read_text(encoding="utf-8"))
            timing["utilization"]["cyclonev_oscillator"]["used"] = 1
            (output / "timing.json").write_text(json.dumps(timing), encoding="utf-8")
            with self.assertRaisesRegex(BuildError, "cyclonev_oscillator"):
                validate_build_evidence(output)

            self._write_passing_outputs(output)
            timing = json.loads((output / "timing.json").read_text(encoding="utf-8"))
            del timing["utilization"]["cyclonev_oscillator"]
            (output / "timing.json").write_text(json.dumps(timing), encoding="utf-8")
            with self.assertRaisesRegex(BuildError, "cyclonev_oscillator"):
                validate_build_evidence(output)

            self._write_passing_outputs(output)
            (output / "routed.json").write_text("{}\n", encoding="utf-8")
            with self.assertRaisesRegex(BuildError, "routed design"):
                validate_build_evidence(output)

    def test_reference_and_pll_evidence_is_complete_and_consistent(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            self._write_passing_outputs(output)
            route_log = output / "nextpnr.log"
            route_log.write_text(
                "Info: backend hip:AMD Radeon RX 7900 XTX ready\n"
                "Info: Program finished normally.\n",
                encoding="utf-8",
            )
            with self.assertRaisesRegex(BuildError, "50.00 MHz"):
                validate_build_evidence(output)

            for filename, label in (("synth.json", "synthesized"), ("routed.json", "routed")):
                with self.subTest(pll_evidence=label):
                    self._write_passing_outputs(output)
                    design = json.loads((output / filename).read_text(encoding="utf-8"))
                    design["modules"]["top"]["cells"]["pll"]["parameters"][
                        "output_clock_frequency0"
                    ] = "75 MHz"
                    (output / filename).write_text(json.dumps(design), encoding="utf-8")
                    with self.assertRaisesRegex(BuildError, f"{label} PLL"):
                        validate_build_evidence(output)

            self._write_passing_outputs(output)
            route_log.write_text(
                "Info: constraining clock net 'FPGA_CLK1_50' to 50.00 MHz\n"
                "Info: backend hip:AMD Radeon RX 7900 XTX ready\n"
                "Info: Program finished normally.\n",
                encoding="utf-8",
            )
            with self.assertRaisesRegex(BuildError, "fractional PLL mapping"):
                validate_build_evidence(output)

            self._write_passing_outputs(output)
            timing = json.loads((output / "timing.json").read_text(encoding="utf-8"))
            timing["fmax"]["invented_reference_domain"] = {
                "constraint": 50.0,
                "achieved": 120.0,
            }
            (output / "timing.json").write_text(json.dumps(timing), encoding="utf-8")
            with self.assertRaisesRegex(BuildError, "single pixel"):
                validate_build_evidence(output)

            source = output / "source"
            sdc = source / "boards/de10nano/clocks.sdc"
            sdc.parent.mkdir(parents=True)
            sdc.write_text(
                "create_clock -name FPGA_CLK1_50 -period 19.999 "
                "[get_ports {FPGA_CLK1_50}]\n",
                encoding="utf-8",
            )
            self._write_passing_outputs(output)
            with self.assertRaisesRegex(BuildError, "20.000 ns"):
                validate_build_evidence(output, source)

    def test_timing_frequencies_must_be_finite(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            for field in ("constraint", "achieved"):
                for value in (float("nan"), float("inf"), float("-inf")):
                    with self.subTest(field=field, value=value):
                        self._write_passing_outputs(output)
                        timing = json.loads(
                            (output / "timing.json").read_text(encoding="utf-8")
                        )
                        timing["fmax"]["pixel_clk"][field] = value
                        (output / "timing.json").write_text(
                            json.dumps(timing), encoding="utf-8"
                        )
                        with self.assertRaisesRegex(BuildError, "finite"):
                            validate_build_evidence(output)

            self._write_passing_outputs(output, achieved=74.25005)
            timing = json.loads((output / "timing.json").read_text(encoding="utf-8"))
            timing["fmax"]["pixel_clk"]["constraint"] = 74.25005
            (output / "timing.json").write_text(json.dumps(timing), encoding="utf-8")
            self.assertEqual(
                validate_build_evidence(output)["timing"]["pixel"]["achieved_mhz"],
                74.25005,
            )

    def test_forbidden_synthesis_cells_are_rejected_without_utilization_rows(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            for resource in (
                "MISTRAL_MLAB",
                "MISTRAL_M10K",
                "MISTRAL_MUL9X9",
                "MISTRAL_MUL18X18",
                "MISTRAL_MUL18X19",
                "MISTRAL_MUL18X19_COMBINED",
                "MISTRAL_MUL27X27",
            ):
                with self.subTest(resource=resource):
                    self._write_passing_outputs(output)
                    synthesis = json.loads(
                        (output / "synth.json").read_text(encoding="utf-8")
                    )
                    synthesis["modules"]["top"]["cells"]["forbidden"] = {
                        "type": resource
                    }
                    (output / "synth.json").write_text(
                        json.dumps(synthesis), encoding="utf-8"
                    )
                    with self.assertRaisesRegex(BuildError, resource):
                        validate_build_evidence(output)

    def test_unused_mul18x19_timing_rows_are_known(self) -> None:
        self.assertLessEqual(
            {"MISTRAL_MUL18X19", "MISTRAL_MUL18X19_COMBINED"},
            set(build_fes_pong.FORBIDDEN_RESOURCES),
        )
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            self._write_passing_outputs(output)
            summary = validate_build_evidence(output)
            self.assertEqual(summary["status"], "pass")
            self.assertEqual(summary["resources"]["MISTRAL_MUL18X19"]["used"], 0)
            self.assertEqual(summary["resources"]["MISTRAL_MUL18X19_COMBINED"]["used"], 0)

            for resource in ("MISTRAL_MUL18X19", "MISTRAL_MUL18X19_COMBINED"):
                with self.subTest(resource=resource):
                    self._write_passing_outputs(output)
                    timing = json.loads((output / "timing.json").read_text(encoding="utf-8"))
                    timing["utilization"][resource]["used"] = 1
                    (output / "timing.json").write_text(json.dumps(timing), encoding="utf-8")
                    with self.assertRaisesRegex(BuildError, resource):
                        validate_build_evidence(output)

            self._write_passing_outputs(output)
            timing = json.loads((output / "timing.json").read_text(encoding="utf-8"))
            timing["utilization"]["MISTRAL_NOT_A_REAL_RESOURCE"] = {
                "used": 0,
                "available": 1,
            }
            (output / "timing.json").write_text(json.dumps(timing), encoding="utf-8")
            with self.assertRaisesRegex(BuildError, "unknown resources"):
                validate_build_evidence(output)

    def test_recipe_pins_the_integrated_fractional_pll_toolchain(self) -> None:
        self.assertEqual(
            build_fes_pong.EXPECTED_TOOL_COMMITS,
            {
                "mistral": "b28e30a36b5139aaed5a5d361a30b542e6b7c758",
                "nextpnr": "d672fade461e8a1eba4d3f95895902d86f43b882",
                "yosys": "ec34fcf38986217af9b5558936044b7197d968a7",
            },
        )

    def test_record_exists_before_synthesis_and_export_waits_for_validation(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for relative in set(build_fes_pong.PINNED_INPUTS):
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(
                    build_fes_pong.REFERENCE_SDC_BYTES
                    if relative == build_fes_pong.SDC
                    else f"fixture {relative}\n".encode()
                )
            output = root / "build/fes-pong"
            package_store = root / "build/packages"
            revision = "a" * 40
            tools = {
                "mistral": AuthenticatedTool(Path("/tool/mistral-cv"), "mistral identity"),
                "nextpnr-mistral": AuthenticatedTool(Path("/tool/nextpnr-mistral"), "nextpnr identity"),
                "yosys": AuthenticatedTool(Path("/tool/yosys"), "yosys identity"),
            }
            events: list[str] = []

            def run_tool(command: tuple[str, ...], cwd: Path, log: Path, **kwargs) -> None:
                self.assertTrue((output / "build-inputs.json").is_file())
                events.append(Path(command[0]).name)
                if Path(command[0]).name == "yosys":
                    self.assertIn(
                        build_identity((output / "build-inputs.json").read_bytes()),
                        " ".join(command),
                    )
                    log.write_text("ok\n", encoding="utf-8")
                    self._write_passing_outputs(output)

            def exporter(manifest: bytes, payload: Path, destination: Path) -> Path:
                self.assertEqual(events, ["yosys", "nextpnr-mistral"])
                self.assertEqual(validate_build_evidence(output)["status"], "pass")
                decoded = tomllib.loads(manifest.decode("utf-8"))
                self.assertEqual(decoded["build"]["id"], build_identity((output / "build-inputs.json").read_bytes()))
                self.assertEqual(decoded["core"]["version"], "1.1.0")
                self.assertEqual(decoded["abi"], {"id": "fes.simple-game", "major": 1, "minor": 0})
                self.assertEqual(decoded["interfaces"], [
                    {"id": "fes.gamepad", "major": 1, "minor": 0, "required": True},
                    {"id": "fes.video.fixed-720p60", "major": 1, "minor": 0, "required": True},
                    {"id": "fes.persistence.words", "major": 1, "minor": 0, "required": True},
                    {"id": "fes.pong.progress", "major": 1, "minor": 0, "required": True},
                ])
                self.assertEqual(payload.resolve(), (output / "core.rbf").resolve())
                self.assertEqual(destination.resolve(), package_store.resolve())
                return package_store / ("f" * 64)

            with (
                patch.object(build_fes_pong, "_require_clean_source", return_value=("https://github.com/DeanoC/misteross.git", revision)),
                patch.object(build_fes_pong, "_authenticate_tools", return_value=tools),
                patch.object(build_fes_pong, "_run_tool", side_effect=run_tool),
                patch.object(build_fes_pong, "export_package", side_effect=exporter) as export_mock,
            ):
                result = build(root, package_store)

            self.assertEqual(result, package_store / ("f" * 64))
            export_mock.assert_called_once()

    def test_failed_timing_never_exports(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for relative in set(build_fes_pong.PINNED_INPUTS):
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(
                    build_fes_pong.REFERENCE_SDC_BYTES
                    if relative == build_fes_pong.SDC
                    else f"fixture {relative}\n".encode()
                )
            output = root / "build/fes-pong"
            tools = {
                "mistral": AuthenticatedTool(Path("/tool/mistral-cv"), "mistral identity"),
                "nextpnr-mistral": AuthenticatedTool(Path("/tool/nextpnr-mistral"), "nextpnr identity"),
                "yosys": AuthenticatedTool(Path("/tool/yosys"), "yosys identity"),
            }

            def run_tool(command: tuple[str, ...], cwd: Path, log: Path, **kwargs) -> None:
                if Path(command[0]).name == "yosys":
                    log.write_text("ok\n", encoding="utf-8")
                    self._write_passing_outputs(output, achieved=70.0)

            with (
                patch.object(build_fes_pong, "_require_clean_source", return_value=("https://github.com/DeanoC/misteross.git", "a" * 40)),
                patch.object(build_fes_pong, "_authenticate_tools", return_value=tools),
                patch.object(build_fes_pong, "_run_tool", side_effect=run_tool),
                patch.object(build_fes_pong, "export_package") as export_mock,
            ):
                with self.assertRaises(BuildError):
                    build(root, root / "build/packages")
            export_mock.assert_not_called()
            self.assertTrue((output / "build-inputs.json").is_file())
            self.assertFalse((output / "core.rbf").exists())
            self.assertFalse((output / "manifest.toml").exists())
            self.assertFalse((output / "build-summary.json").exists())

    def test_nextpnr_retry_exit_one_is_accepted_when_route_finished(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fake = root / "nextpnr-mistral"
            fake.write_text(
                "#!/bin/sh\n"
                "printf '%s\\n' 'ERROR: Max frequency for clock x: 73.43 MHz (FAIL at 74.25 MHz)'\n"
                "printf '%s\\n' 'Info: Max frequency for clock x: 78.27 MHz (PASS at 74.25 MHz)'\n"
                "printf '%s\\n' 'Info: Program finished normally.'\n"
                "exit 1\n",
                encoding="utf-8",
            )
            fake.chmod(0o755)
            payload = root / "build/fes-pong/core.rbf"
            payload.parent.mkdir(parents=True)
            payload.write_bytes(b"rbf")
            log = root / "build/fes-pong/nextpnr.log"
            build_fes_pong._run_tool((str(fake),), root, log, output_relative=build_fes_pong.OUTPUT_RELATIVE)
            self.assertIn("Program finished normally", log.read_text(encoding="utf-8"))

    def test_nextpnr_nonzero_exit_without_finished_route_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fake = root / "nextpnr-mistral"
            fake.write_text("#!/bin/sh\nprintf '%s\\n' 'ERROR: crashed'\nexit 1\n", encoding="utf-8")
            fake.chmod(0o755)
            log = root / "nextpnr.log"
            with self.assertRaisesRegex(BuildError, "tool failed with exit 1"):
                build_fes_pong._run_tool((str(fake),), root, log, output_relative=build_fes_pong.OUTPUT_RELATIVE)


if __name__ == "__main__":
    unittest.main()
