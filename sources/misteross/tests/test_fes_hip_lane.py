from __future__ import annotations

import hashlib
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from scripts import build_fes_coleco_oss, build_fes_pong, build_fes_zx81_oss, toolchain_cache
from scripts.build_fes_coleco_oss import COLECO_TOOLCHAIN_LOCK
from scripts.build_fes_pong import AuthenticatedTool, BuildError, build_commands, create_build_record
from scripts.build_fes_zx81_oss import OUTPUT_RELATIVE as ZX81_OUTPUT
from scripts.build_fes_zx81_oss import build_commands as zx81_build_commands
from scripts.build_fes_zx81_oss import create_build_record as zx81_create_build_record
from scripts.lockfile import load_lock
from tests.test_build_fes_pong import _write_fake_tool, publish_shared_toolchain


ROOT = Path(__file__).resolve().parents[1]
HIP = "HIP"
HIP_ARCHITECTURES = "gfx1100;gfx1201"
HIP_CONFIGURATION = f"gpu-router={HIP}; hip-architectures={HIP_ARCHITECTURES}"
CPU_FALLBACK_LOG = (
    "Info: GPU router: nextpnr was built without a GPU device backend; "
    "falling back to the CPU reference backend.\n"
    "Info: backend cpu-reference ready\n"
    "Info: Program finished normally.\n"
)
LIVE_HIP_LOG = (
    "Info: GPU devices: hip:AMD Radeon RX 7900 XTX\n"
    "Info: backend hip:AMD Radeon RX 7900 XTX ready\n"
    "Info: Program finished normally.\n"
)
OFF_CONFIGURATION = "gpu-router=OFF; hip-architectures=unused"


def _write_local_root_tools(root: Path, *, nextpnr_config: str | None) -> Path:
    (root / "toolchain.lock").write_bytes((ROOT / "toolchain.lock").read_bytes())
    install = root / "build/toolchain/install/bin"
    install.mkdir(parents=True)
    pins = load_lock(root / "toolchain.lock")
    for lock_name, binary_name in (
        ("yosys", "yosys"),
        ("mistral", "mistral-cv"),
        ("nextpnr", "nextpnr-mistral"),
    ):
        binary = install / binary_name
        _write_fake_tool(binary, lock_name)
        evidence = root / "build/toolchain/build" / lock_name
        evidence.mkdir(parents=True)
        commit = pins[lock_name].commit
        (evidence / f".built-{commit}").write_text(f"commit={commit}\n", encoding="utf-8")
        digest = hashlib.sha256(binary.read_bytes()).hexdigest()
        (evidence / f".digest-{commit}.sha256").write_text(f"{digest}\n", encoding="utf-8")
        if lock_name == "nextpnr" and nextpnr_config is not None:
            (evidence / f".config-{commit}.txt").write_text(f"{nextpnr_config}\n", encoding="utf-8")
    return install


class FesHipLaneTests(unittest.TestCase):
    def test_producer_clis_forward_cache_root(self) -> None:
        pong = build_fes_pong._parser().parse_args(["--cache-root", "/tmp/fes-cache"])
        self.assertEqual(pong.cache_root, Path("/tmp/fes-cache"))
        with patch.object(build_fes_pong, "build", return_value=Path("/pkg")) as built:
            self.assertEqual(
                build_fes_pong.main(["--root", str(ROOT), "--cache-root", "/tmp/fes-cache"]),
                0,
            )
        built.assert_called_once()
        self.assertEqual(built.call_args.args[0], Path(ROOT))
        self.assertEqual(built.call_args.kwargs.get("cache_root") or built.call_args.args[2], Path("/tmp/fes-cache"))

        for module, label in (
            (build_fes_zx81_oss, "build-fes-zx81-oss"),
            (build_fes_coleco_oss, "build-fes-coleco-oss"),
        ):
            with self.subTest(producer=label), patch.object(module, "build", return_value=Path("/pkg")) as built:
                self.assertEqual(
                    module.main(["--root", str(ROOT), "--cache-root", "/tmp/fes-cache"]),
                    0,
                )
            built.assert_called_once()
            kwargs = built.call_args.kwargs
            args = built.call_args.args
            cache = kwargs.get("cache_root")
            if cache is None and len(args) >= 3:
                cache = args[2]
            self.assertEqual(cache, Path("/tmp/fes-cache"))

    def test_shared_lane_uses_explicit_cache_root_not_environment(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            selected, manifest = publish_shared_toolchain(base, gpu_router=HIP)
            other = base / "other-cache"
            other.mkdir(mode=0o700)
            with patch.dict(
                os.environ,
                {"PATH": str(base), "FES_TOOLCHAIN_CACHE_ROOT": str(other)},
                clear=True,
            ), patch.object(
                toolchain_cache, "host_identity",
                return_value={"system": "Linux", "release": "test", "machine": "x86_64"},
            ), patch.object(
                toolchain_cache, "compiler_inventory",
                return_value={"commands": {"cc": {"path": "/test/cc"}}},
            ):
                authenticated = build_fes_pong._authenticate_tools(
                    ROOT, cache_root=selected.cache_root
                )
            self.assertEqual(authenticated["yosys"].path, manifest.install / "bin/yosys")
            self.assertIn(HIP_CONFIGURATION, authenticated["nextpnr-mistral"].identity)

    def test_ambient_cache_root_does_not_select_shared_lane(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request, _ = publish_shared_toolchain(base, gpu_router=HIP)
            local = base / "local-tree"
            local.mkdir()
            (local / "toolchain.lock").write_bytes((ROOT / "toolchain.lock").read_bytes())
            install = local / "build/toolchain/install/bin"
            install.mkdir(parents=True)
            from tests.test_build_fes_pong import _write_fake_tool
            from scripts.lockfile import load_lock
            pins = load_lock(local / "toolchain.lock")
            for lock_name, binary_name in (("yosys", "yosys"), ("mistral", "mistral-cv"),
                                           ("nextpnr", "nextpnr-mistral")):
                binary = install / binary_name
                _write_fake_tool(binary, lock_name)
                evidence = local / "build/toolchain/build" / lock_name
                evidence.mkdir(parents=True)
                commit = pins[lock_name].commit
                (evidence / f".built-{commit}").write_text(f"commit={commit}\n", encoding="utf-8")
                digest = __import__("hashlib").sha256(binary.read_bytes()).hexdigest()
                (evidence / f".digest-{commit}.sha256").write_text(f"{digest}\n", encoding="utf-8")
                if lock_name == "nextpnr":
                    (evidence / f".config-{commit}.txt").write_text(HIP_CONFIGURATION + "\n", encoding="utf-8")
            with patch.dict(
                os.environ,
                {"FES_TOOLCHAIN_CACHE_ROOT": str(request.cache_root)},
                clear=True,
            ):
                authenticated = build_fes_pong._authenticate_tools(local)
            self.assertEqual(authenticated["yosys"].path, install / "yosys")
            self.assertNotEqual(authenticated["yosys"].path, request.cache_root / "slots")

    def test_root_lock_hip_slot_differs_from_coleco_lock_slot(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            cache = Path(directory) / "cache"
            cache.mkdir(mode=0o700)
            host = {"system": "Linux", "release": "test", "machine": "x86_64"}
            compiler = {"commands": {"cc": {"path": "/test/cc"}}}
            with patch.object(toolchain_cache, "host_identity", return_value=host), patch.object(
                toolchain_cache, "compiler_inventory", return_value=compiler
            ), patch.dict(os.environ, {"PATH": "/bin"}, clear=True):
                pong = toolchain_cache.request_from_environment(
                    ROOT, ROOT / "toolchain.lock", cache_root=cache,
                    gpu_router=HIP, hip_architectures=HIP_ARCHITECTURES,
                )
                zx81 = toolchain_cache.request_from_environment(
                    ROOT, ROOT / "toolchain.lock", cache_root=cache,
                    gpu_router=HIP, hip_architectures=HIP_ARCHITECTURES,
                )
                coleco = toolchain_cache.request_from_environment(
                    ROOT, ROOT / COLECO_TOOLCHAIN_LOCK, cache_root=cache,
                    gpu_router=HIP, hip_architectures=HIP_ARCHITECTURES,
                )
                off = toolchain_cache.request_from_environment(
                    ROOT, ROOT / "toolchain.lock", cache_root=cache,
                    gpu_router="OFF", hip_architectures=HIP_ARCHITECTURES,
                )
            self.assertEqual(toolchain_cache.cache_key(pong), toolchain_cache.cache_key(zx81))
            self.assertNotEqual(toolchain_cache.cache_key(pong), toolchain_cache.cache_key(coleco))
            self.assertNotEqual(toolchain_cache.cache_key(pong), toolchain_cache.cache_key(off))

    def test_pong_and_zx81_commands_and_records_use_hip_gpu_router(self) -> None:
        tools = {"yosys": Path("/yosys"), "nextpnr-mistral": Path("/nextpnr-mistral")}
        _, pong_nextpnr = build_commands(ROOT, ROOT / "build/fes-pong", "00112233445566778899aabbccddeeff", tools)
        self.assertEqual(pong_nextpnr[pong_nextpnr.index("--router") + 1], "gpu")
        pong_record = json.loads(create_build_record(ROOT, "https://example.invalid/m.git", "a" * 40, {"yosys": "x"}))
        self.assertEqual(pong_record["parameters"]["router"], "gpu")
        self.assertEqual(pong_record["parameters"]["gpu_backend"], "hip")
        self.assertEqual(pong_record["parameters"]["gpu_architectures"], HIP_ARCHITECTURES)

        _, zx81_nextpnr = zx81_build_commands(
            ROOT, ROOT / ZX81_OUTPUT, "00112233445566778899aabbccddeeff", tools
        )
        self.assertEqual(zx81_nextpnr[zx81_nextpnr.index("--router") + 1], "gpu")
        zx81_record = json.loads(zx81_create_build_record(ROOT, "https://example.invalid/m.git", "a" * 40, {"yosys": "x"}))
        self.assertEqual(zx81_record["parameters"]["router"], "gpu")
        self.assertEqual(zx81_record["parameters"]["gpu_backend"], "hip")
        self.assertEqual(zx81_record["parameters"]["gpu_architectures"], HIP_ARCHITECTURES)

    def test_route_proof_rejects_cpu_fallback_and_requires_live_hip(self) -> None:
        for module in (build_fes_pong, build_fes_zx81_oss, build_fes_coleco_oss):
            with self.subTest(module=module.__name__):
                with self.assertRaisesRegex(BuildError, "device backend"):
                    module._require_gpu_backend(CPU_FALLBACK_LOG)
                self.assertEqual(module._require_gpu_backend(LIVE_HIP_LOG), "hip")

    def test_build_reauthenticates_with_the_same_explicit_cache_root(self) -> None:
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
            tools = {
                "mistral": AuthenticatedTool(Path("/tool/mistral-cv"), "mistral identity"),
                "nextpnr-mistral": AuthenticatedTool(Path("/tool/nextpnr-mistral"), "nextpnr identity"),
                "yosys": AuthenticatedTool(Path("/tool/yosys"), "yosys identity"),
            }
            cache = Path("/explicit-cache")
            seen: list[Path | None] = []

            def authenticate(tree, cache_root=None, **kwargs):
                seen.append(cache_root)
                return tools

            def run_tool(command, cwd, log):
                log.parent.mkdir(parents=True, exist_ok=True)
                log.write_text("ok\n", encoding="utf-8")
                if log.name == "yosys.log":
                    (log.parent / "synth.json").write_text("{}\n", encoding="utf-8")

            with (
                patch.object(build_fes_pong, "_require_clean_source",
                             return_value=("https://github.com/DeanoC/misteross.git", "a" * 40)),
                patch.object(build_fes_pong, "_authenticate_tools", side_effect=authenticate),
                patch.object(build_fes_pong, "_run_tool", side_effect=run_tool),
                patch.object(
                    build_fes_pong,
                    "validate_build_evidence",
                    return_value={"rbf": {"sha256": "a" * 64, "size": 1}},
                ),
                patch.object(build_fes_pong, "export_package", return_value=root / "pkg"),
            ):
                build_fes_pong.build(root, root / "build/packages", cache_root=cache)
            self.assertEqual(seen, [cache, cache])

    def test_local_off_or_unstamped_nextpnr_is_rejected_for_pong_and_zx81(self) -> None:
        for module in (build_fes_pong, build_fes_zx81_oss):
            for stamp in (OFF_CONFIGURATION, None):
                with self.subTest(module=module.__name__, stamp=stamp):
                    with tempfile.TemporaryDirectory() as directory:
                        root = Path(directory)
                        _write_local_root_tools(root, nextpnr_config=stamp)
                        with patch.dict(os.environ, {}, clear=True):
                            with self.assertRaisesRegex(BuildError, r"make toolchain-fes"):
                                module._authenticate_tools(root)

    def test_local_hip_stamp_from_toolchain_fes_lane_authenticates_pong_and_zx81(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            install = _write_local_root_tools(root, nextpnr_config=HIP_CONFIGURATION)
            with patch.dict(os.environ, {}, clear=True):
                for module in (build_fes_pong, build_fes_zx81_oss):
                    with self.subTest(module=module.__name__):
                        authenticated = module._authenticate_tools(root)
                        self.assertEqual(authenticated["nextpnr-mistral"].path, install / "nextpnr-mistral")
                        self.assertIn(HIP_CONFIGURATION, authenticated["nextpnr-mistral"].identity)

    def test_toolchain_fes_provisions_root_lock_hip_lane(self) -> None:
        result = subprocess.run(
            ["make", "-n", "toolchain-fes"],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("FES_TOOLCHAIN_GPU_ROUTER=HIP", result.stdout)
        self.assertIn("FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201'", result.stdout)
        self.assertIn("scripts/bootstrap.sh", result.stdout)
        self.assertNotIn("cores/fes-coleco/toolchain.lock", result.stdout)
        self.assertNotIn("build/toolchain/fes-coleco", result.stdout)
        makefile = (ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertRegex(makefile, r"\.PHONY:.*\btoolchain-fes\b")
        self.assertIn("toolchain-fes  Build the repository-local HIP toolchain for FES Pong/ZX81", makefile)
        readme = (ROOT / "README.md").read_text(encoding="utf-8")
        self.assertIn("make toolchain-fes", readme)
        self.assertIn("make toolchain", readme)

    def test_make_forwards_cache_root_to_fes_producers(self) -> None:
        cache = "/tmp/fes-explicit-cache"
        producers = (
            ("build-fes-pong", "scripts/build_fes_pong.py"),
            ("build-fes-zx81", "scripts/build_fes_zx81_oss.py"),
            ("build-fes-coleco", "scripts/build_fes_coleco_oss.py"),
        )
        makefile = (ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertIn("CACHE_ROOT=", makefile)
        self.assertNotIn("optional --cache-root", makefile)
        for target, script in producers:
            with self.subTest(target=target, forwarded=True):
                result = subprocess.run(
                    ["make", "-n", target, f"CACHE_ROOT={cache}"],
                    cwd=ROOT,
                    text=True,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.PIPE,
                    check=False,
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn(f'{script} --root "{ROOT}" --cache-root "{cache}"', result.stdout)
            with self.subTest(target=target, forwarded=False):
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
                    f'python3 {script} --root "{ROOT}"',
                )
                self.assertNotIn("--cache-root", result.stdout)


if __name__ == "__main__":
    unittest.main()
