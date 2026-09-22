from __future__ import annotations

import subprocess
import json
import os
import tempfile
import tomllib
import unittest
from tests.producer_fixture import clean_module, init_source, EXECUTION, FakeInvocation
from pathlib import Path
from unittest.mock import patch

from scripts import build_fes_zx81_oss, toolchain_cache
from scripts.build_fes_zx81_oss import (
    OUTPUT_RELATIVE,
    PLACER_SEEDS,
    PLACER_TIMING_WEIGHT,
    PLACER_CRITICALITY_EXPONENT,
    RTL_SOURCES,
    BuildError,
    _manifest,
    build_commands,
)
from tests.test_build_fes_pong import publish_shared_toolchain


ROOT = Path(__file__).resolve().parents[1]


class BuildFesZx81OssTests(unittest.TestCase):
    def test_record_seals_effective_search_policy(self) -> None:
        for mode in ("first-pass", "staged"):
            with self.subTest(mode=mode):
                weights, budget = build_fes_zx81_oss.placement_policy(mode)
                record = json.loads(build_fes_zx81_oss.create_build_record(
                    ROOT, "https://github.com/DeanoC/misteross.git", "a" * 40,
                    {"yosys": "test"}, qor_mode=mode,
                 execution=EXECUTION))
                parameters = record["parameters"]
                self.assertEqual(parameters["placer_heap_timingweights"], ",".join(map(str, weights)))
                self.assertEqual(parameters["placer_qor_budget"], budget)
                self.assertEqual(parameters["expansion_socket"], "zx81-bus-v1")
        with self.assertRaises(BuildError):
            build_fes_zx81_oss.placement_policy("unknown")

    def test_first_pass_reaches_fallback_and_stops_after_timing_closes(self) -> None:
        from scripts.search_placer_qor import search
        from tests.test_search_placer_qor import _candidate
        weights, budget = build_fes_zx81_oss.placement_policy("first-pass")
        calls = []

        def route(seed, weight):
            calls.append((seed, weight))
            return _candidate(seed, weight, 52.4 if (seed, weight) == (34, 300) else 51.5)

        ranked = search(
            nextpnr=Path("unused"), fixture=Path("unused"), output=Path("unused"),
            device=build_fes_zx81_oss.TARGET, qsf=Path("unused"), sdc=None, freq=None,
            seeds=PLACER_SEEDS, weights=weights, critexp=PLACER_CRITICALITY_EXPONENT,
            budget=budget, mode="first-pass", extra=(), timeout=1, run_one=route,
        )
        self.assertEqual(calls, [(seed, weight) for weight in (1000, 300) for seed in PLACER_SEEDS])
        self.assertTrue(ranked[0].passing)
        self.assertEqual((ranked[0].seed, ranked[0].weight), (34, 300))
        self.assertEqual(budget, 70)

    def test_make_entrypoint_uses_the_oss_recipe(self) -> None:
        result = subprocess.run(
            ["make", "-n", "build-fes-zx81"],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.stdout.strip(),
            f'python3 scripts/build_fes_zx81_oss.py --root "{ROOT}"',
        )
        phony = (ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertRegex(phony, r"\.PHONY:.*\bbuild-fes-zx81\b")

    def test_oss_auth_uses_verified_shared_tools_directly(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request, manifest = publish_shared_toolchain(
                base, lock_path=ROOT / build_fes_zx81_oss.SOCKET_TOOLCHAIN_LOCK, gpu_router="HIP")
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
                authenticated = build_fes_zx81_oss._authenticate_tools(
                    ROOT, cache_root=request.cache_root
                )

            self.assertEqual(authenticated["yosys"].path, manifest.install / "bin/yosys")
            self.assertEqual(
                authenticated["nextpnr-mistral"].path,
                manifest.install / "bin/nextpnr-mistral",
            )

    def test_oss_commands_use_tv80_m10k_and_not_vhdl_t80(self) -> None:
        yosys, nextpnr = build_commands(
            ROOT,
            ROOT / OUTPUT_RELATIVE,
            "00112233445566778899aabbccddeeff",
            {"yosys": Path("/tmp/yosys"), "nextpnr-mistral": Path("/tmp/nextpnr-mistral")},
        )
        program = yosys[2]
        self.assertIn("tv80_core.v", program)
        self.assertIn("t80pa.v", program)
        self.assertNotIn("-DFES_ZX81_OSS=1", program)
        self.assertIn("synth_intel_alm -nolutram -nodsp -top top", program)
        self.assertNotIn("-nobram", program)
        self.assertNotIn("T80pa.vhd", program)
        self.assertIn("cores/fes-zx81/rtl/top.v", program)
        self.assertIn("chparam -set EXPANSION_SOCKET 1 top", program)
        self.assertIn("--freq", nextpnr)
        self.assertIn("74.25", nextpnr)
        self.assertIn("--seed", nextpnr)
        self.assertIn(str(PLACER_SEEDS[0]), nextpnr)
        self.assertIn("--placer-heap-timingweight", nextpnr)
        self.assertIn(str(PLACER_TIMING_WEIGHT), nextpnr)
        self.assertIn("--placer-heap-critexp", nextpnr)
        self.assertIn(str(PLACER_CRITICALITY_EXPONENT), nextpnr)
        self.assertIn("--router", nextpnr)
        self.assertIn("gpu", nextpnr)
        self.assertNotIn("router1", nextpnr)
        self.assertIn("--timing-allow-fail", nextpnr)
        self.assertNotIn("--tmg-ripup", nextpnr)
        joined = " ".join(nextpnr)
        self.assertIn("build/fes-zx81-oss/socket.qsf", joined)
        self.assertIn("cores/fes-zx81/clocks-oss.sdc", joined)
        self.assertNotIn("cores/fes-zx81/clocks.sdc", joined)
        self.assertNotIn("cores/fes-zx81/constraints-oss.qsf", joined)

    def test_standard_authentication_uses_the_socket_toolchain(self) -> None:
        with patch.object(build_fes_zx81_oss, "_authenticate_oss_tools") as authenticate:
            build_fes_zx81_oss._authenticate_tools(ROOT, cache_root=Path("/cache"))
        authenticate.assert_called_once()
        kwargs = authenticate.call_args.kwargs
        self.assertEqual(kwargs["lock_path"], ROOT / build_fes_zx81_oss.SOCKET_TOOLCHAIN_LOCK)
        self.assertEqual(kwargs["toolchain_root"], ROOT / "build/toolchain/zx81-expansion")

    def test_oss_top_uses_mistral_io_without_quartus(self) -> None:
        top = (ROOT / "cores/fes-zx81/rtl/top.v").read_text(encoding="utf-8")
        self.assertIn("MISTRAL_IO hdmi_scl_pad", top)
        self.assertIn('BEL = "cyclonev_hps_interface_peripheral_i2c.52.60.0"', top)
        self.assertIn("`ifdef QUARTUS", top)
        self.assertIn("hdmi_scl_low ? 1'b0 : 1'bz", top)
        dpram = (ROOT / "cores/fes-zx81/rtl/zx81_dpram.v").read_text(encoding="utf-8")
        self.assertNotIn("FES_ZX81_OSS", dpram)
        self.assertIn('ramstyle = "M10K"', dpram)
        self.assertIn("assign q_a = ram[address_a]", dpram)
        sys_pll = (ROOT / "cores/fes-zx81/rtl/sys_pll.v").read_text(encoding="utf-8")
        self.assertIn('.output_clock_frequency0("52.0 MHz")', sys_pll)
        self.assertNotIn('.output_clock_frequency0("50.0 MHz")', sys_pll)

    def test_oss_rejects_wrong_output_directory(self) -> None:
        with self.assertRaises(BuildError):
            build_commands(
                ROOT,
                ROOT / "build/other",
                "00112233445566778899aabbccddeeff",
                {"yosys": Path("/tmp/yosys"), "nextpnr-mistral": Path("/tmp/nextpnr-mistral")},
            )

    def test_oss_manifest_carries_source_identity(self) -> None:
        record = (
            b'{"format":1,"repository":"https://github.com/DeanoC/misteross.git",'
            b'"revision":"' + (b"a" * 40) + b'","recipe":"scripts/build_fes_zx81_oss.py",'
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
        self.assertIn(b"repository = ", manifest)
        self.assertIn(b"revision = \"" + (b"a" * 40) + b"\"", manifest)
        self.assertNotIn(b"recipe = ", manifest)

    def test_socket_manifest_exposes_bus_not_a_cart_type(self) -> None:
        record = (
            b'{"format":1,"repository":"https://github.com/DeanoC/misteross.git",'
            b'"revision":"' + (b"a" * 40) + b'","recipe":"scripts/build_fes_zx81_oss.py",'
            b'"recipe_sha256":"' + (b"b" * 64) + b'","abi_definition":"x",'
            b'"abi_definition_sha256":"' + (b"c" * 64) + b'","dependencies":{},'
            b'"tools":{},"parameters":{"expansion_socket":"zx81-bus-v1"}}'
        )
        manifest = _manifest(
            record,
            {"build_id": "d" * 32, "rbf": {"size": 16, "sha256": "e" * 64}},
            "https://github.com/DeanoC/misteross.git", "a" * 40,
            {"mistral": "m", "nextpnr-mistral": "n", "yosys": "y"},
        )
        fields = tomllib.loads(manifest.decode())
        self.assertEqual(fields["core"]["version"], "1.2.0")
        interfaces = {item["id"] for item in fields["interfaces"]}
        self.assertIn("fes.expansion.zx81-bus", interfaces)
        self.assertNotIn("fes.expansion.zx81-ram", interfaces)


def setUpModule():
    global ROOT, _source_fixture
    _source_fixture, ROOT = clean_module(ROOT)

def tearDownModule():
    _source_fixture.cleanup()
