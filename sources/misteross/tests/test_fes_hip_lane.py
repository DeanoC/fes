from __future__ import annotations

import hashlib
import json
import os
import re
import subprocess
import tempfile
import unittest
from tests.producer_fixture import clean_module, init_source, EXECUTION, FakeInvocation
from pathlib import Path
from types import SimpleNamespace
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
CUDA_PRODUCERS = (
    "build_fes_apple2_oss",
    "build_fes_atari_st_oss",
    "build_fes_c64_oss",
    "build_fes_coleco_socket_v2",
    "build_fes_sg1000_oss",
    "build_fes_sms_oss",
    "build_fes_spectrum_oss",
    "build_fes_zx81_oss",
)


def _write_local_pong_tools(
    root: Path,
    *,
    nextpnr_config: str | None,
    nextpnr_commit: str | None = None,
) -> Path:
    lock = root / build_fes_pong.TOOLCHAIN_LOCK
    lock.parent.mkdir(parents=True, exist_ok=True)
    lock_text = (ROOT / build_fes_pong.TOOLCHAIN_LOCK).read_text(encoding="utf-8")
    if nextpnr_commit is not None:
        original_commit = load_lock(ROOT / build_fes_pong.TOOLCHAIN_LOCK)["nextpnr"].commit
        old = f'commit = "{original_commit}"'
        if lock_text.count(old) != 1:
            raise AssertionError("expected one nextpnr commit in the fixture lock")
        lock_text = lock_text.replace(old, f'commit = "{nextpnr_commit}"', 1)
    lock.write_text(lock_text, encoding="utf-8")
    install = root / build_fes_pong.TOOLCHAIN_ROOT / "install/bin"
    install.mkdir(parents=True)
    pins = load_lock(lock)
    for lock_name, binary_name in (
        ("yosys", "yosys"),
        ("mistral", "mistral-cv"),
        ("nextpnr", "nextpnr-mistral"),
    ):
        binary = install / binary_name
        _write_fake_tool(binary, lock_name)
        evidence = root / build_fes_pong.TOOLCHAIN_ROOT / "build" / lock_name
        evidence.mkdir(parents=True)
        commit = pins[lock_name].commit
        (evidence / f".built-{commit}").write_text(f"commit={commit}\n", encoding="utf-8")
        digest = hashlib.sha256(binary.read_bytes()).hexdigest()
        (evidence / f".digest-{commit}.sha256").write_text(f"{digest}\n", encoding="utf-8")
        if lock_name == "nextpnr" and nextpnr_config is not None:
            (evidence / f".config-{commit}.txt").write_text(f"{nextpnr_config}\n", encoding="utf-8")
    return install


def _write_local_zx81_tools(root: Path, *, nextpnr_config: str | None) -> Path:
    lock = root / build_fes_zx81_oss.SOCKET_TOOLCHAIN_LOCK
    lock.parent.mkdir(parents=True, exist_ok=True)
    lock.write_bytes((ROOT / build_fes_zx81_oss.SOCKET_TOOLCHAIN_LOCK).read_bytes())
    install = root / "build/toolchain/zx81-expansion/install/bin"
    install.mkdir(parents=True)
    pins = load_lock(lock)
    for lock_name, binary_name in (
        ("yosys", "yosys"),
        ("mistral", "mistral-cv"),
        ("nextpnr", "nextpnr-mistral"),
    ):
        binary = install / binary_name
        _write_fake_tool(binary, lock_name)
        evidence = root / "build/toolchain/zx81-expansion/build" / lock_name
        evidence.mkdir(parents=True)
        commit = pins[lock_name].commit
        (evidence / f".built-{commit}").write_text(f"commit={commit}\n")
        digest = hashlib.sha256(binary.read_bytes()).hexdigest()
        (evidence / f".digest-{commit}.sha256").write_text(f"{digest}\n")
        if lock_name == "nextpnr" and nextpnr_config is not None:
            (evidence / f".config-{commit}.txt").write_text(f"{nextpnr_config}\n")
    return install


class FesHipLaneTests(unittest.TestCase):
    def test_producer_cli_defaults_functional_identity_and_rejects_v1(self):
        from scripts import build_fes_sms_oss, build_fes_sg1000_oss
        for producer in (build_fes_pong, build_fes_zx81_oss, build_fes_coleco_oss,
                         build_fes_sms_oss, build_fes_sg1000_oss):
            with self.subTest(producer=producer.__name__), patch.object(producer, "build", return_value=Path("/pkg")) as built:
                self.assertEqual(producer.main([]), 0)
                self.assertEqual(built.call_args.kwargs["identity_version"], 2)
                with self.assertRaises(SystemExit):
                    producer.main(["--identity-version", "1"])
        for producer in (build_fes_sms_oss, build_fes_sg1000_oss):
            with patch.object(producer, "synth", return_value={"synthesis_cells": {}}) as synth:
                self.assertEqual(producer.main(["--synth-only"]), 0)
                synth.assert_called_once()

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

    def test_one_shared_cache_gives_each_core_its_own_lock_slot(self) -> None:
        # The image build authenticates every selected core against one cache
        # root. A core whose lock moves to another nextpnr gets that lock's
        # slot; a core on the old lock keeps the old slot.
        from scripts import build_fes_catch
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / "misteross"
            for relative in ("scripts/bootstrap.sh", "scripts/lockfile.py", "scripts/toolchain_cache.py",
                             build_fes_pong.TOOLCHAIN_LOCK, build_fes_catch.TOOLCHAIN_LOCK):
                (root / relative).parent.mkdir(parents=True, exist_ok=True)
                (root / relative).write_bytes((ROOT / relative).read_bytes())
            # Keep the two cache requests distinct even when current locks
            # happen to pin the same nextpnr commit. The synthetic pin also
            # confirms recipe lookup is independent of a commit allowlist.
            pong_pin = "a" * 40
            catch_pin = load_lock(ROOT / build_fes_catch.TOOLCHAIN_LOCK)["nextpnr"].commit
            for relative, pin in ((build_fes_pong.TOOLCHAIN_LOCK, pong_pin),
                                  (build_fes_catch.TOOLCHAIN_LOCK, catch_pin)):
                text, count = re.subn(r'(\[tool\.nextpnr\][^\[]*?commit = ")[0-9a-f]{40}"',
                                      rf'\g<1>{pin}"', (root / relative).read_text())
                self.assertEqual(count, 1)
                (root / relative).write_text(text)
            pong_request, pong_manifest = publish_shared_toolchain(
                base, lock_path=root / build_fes_pong.TOOLCHAIN_LOCK, gpu_router=HIP)
            catch_request, catch_manifest = publish_shared_toolchain(
                base, lock_path=root / build_fes_catch.TOOLCHAIN_LOCK, gpu_router=HIP)
            self.assertEqual(pong_request.cache_root, catch_request.cache_root)
            self.assertNotEqual(pong_manifest.slot, catch_manifest.slot)
            with patch.dict(os.environ, {"PATH": str(base)}, clear=True), patch.object(
                toolchain_cache, "host_identity",
                return_value={"system": "Linux", "release": "test", "machine": "x86_64"},
            ), patch.object(
                toolchain_cache, "compiler_inventory",
                return_value={"commands": {"cc": {"path": "/test/cc"}}},
            ):
                pong = build_fes_pong._authenticate_tools(root, cache_root=pong_request.cache_root)
                catch = build_fes_catch._authenticate_tools(root, cache_root=catch_request.cache_root)
            self.assertEqual(pong["nextpnr-mistral"].path, pong_manifest.install / "bin/nextpnr-mistral")
            self.assertEqual(catch["nextpnr-mistral"].path, catch_manifest.install / "bin/nextpnr-mistral")
            self.assertIn(f"commit={pong_pin};", pong["nextpnr-mistral"].identity)
            self.assertIn(f"commit={catch_pin};", catch["nextpnr-mistral"].identity)

    def test_ambient_cache_root_does_not_select_shared_lane(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request, _ = publish_shared_toolchain(base, gpu_router=HIP)
            local = base / "local-tree"
            local.mkdir()
            lock = local / build_fes_pong.TOOLCHAIN_LOCK
            lock.parent.mkdir(parents=True, exist_ok=True)
            lock.write_bytes((ROOT / build_fes_pong.TOOLCHAIN_LOCK).read_bytes())
            install = local / build_fes_pong.TOOLCHAIN_ROOT / "install/bin"
            install.mkdir(parents=True)
            from tests.test_build_fes_pong import _write_fake_tool
            from scripts.lockfile import load_lock
            pins = load_lock(lock)
            for lock_name, binary_name in (("yosys", "yosys"), ("mistral", "mistral-cv"),
                                           ("nextpnr", "nextpnr-mistral")):
                binary = install / binary_name
                _write_fake_tool(binary, lock_name)
                evidence = local / build_fes_pong.TOOLCHAIN_ROOT / "build" / lock_name
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

    def test_root_and_coleco_share_the_hip_slot(self) -> None:
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
            self.assertEqual(COLECO_TOOLCHAIN_LOCK, "toolchain.lock")
            self.assertEqual(toolchain_cache.cache_key(pong), toolchain_cache.cache_key(zx81))
            self.assertEqual(toolchain_cache.cache_key(pong), toolchain_cache.cache_key(coleco))
            self.assertNotEqual(toolchain_cache.cache_key(pong), toolchain_cache.cache_key(off))

    def test_pong_and_zx81_commands_and_records_use_hip_gpu_router(self) -> None:
        tools = {"yosys": Path("/yosys"), "nextpnr-mistral": Path("/nextpnr-mistral")}
        _, pong_nextpnr = build_commands(ROOT, ROOT / "build/fes-pong", "00112233445566778899aabbccddeeff", tools)
        self.assertEqual(pong_nextpnr[pong_nextpnr.index("--router") + 1], "gpu")
        pong_record = json.loads(create_build_record(ROOT, "https://example.invalid/m.git", "a" * 40, {"yosys": "x"}, execution=EXECUTION))
        self.assertEqual(pong_record["parameters"]["router"], "gpu")
        self.assertEqual(pong_record["parameters"]["gpu_backend"], "hip")
        self.assertEqual(pong_record["parameters"]["gpu_architectures"], HIP_ARCHITECTURES)

        _, zx81_nextpnr = zx81_build_commands(
            ROOT, ROOT / ZX81_OUTPUT, "00112233445566778899aabbccddeeff", tools
        )
        self.assertEqual(zx81_nextpnr[zx81_nextpnr.index("--router") + 1], "gpu")
        zx81_record = json.loads(zx81_create_build_record(ROOT, "https://example.invalid/m.git", "a" * 40, {"yosys": "x"}, execution=EXECUTION))
        self.assertEqual(zx81_record["parameters"]["router"], "gpu")
        self.assertEqual(zx81_record["parameters"]["gpu_backend"], "hip")
        self.assertEqual(zx81_record["parameters"]["gpu_architectures"], HIP_ARCHITECTURES)

    def test_route_proof_rejects_cpu_fallback_and_requires_live_hip(self) -> None:
        for module in (build_fes_pong, build_fes_zx81_oss, build_fes_coleco_oss):
            with self.subTest(module=module.__name__):
                with self.assertRaisesRegex(BuildError, "device backend"):
                    module._require_gpu_backend(CPU_FALLBACK_LOG, "hip")
                self.assertEqual(module._require_gpu_backend(LIVE_HIP_LOG, "hip"), "hip")
                with self.assertRaisesRegex(BuildError, "authenticated backend hip"):
                    module._require_gpu_backend(
                        "Info: backend cuda:NVIDIA RTX 4090 ready\n", "hip")

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

            def run_tool(command, cwd, log, **kwargs):
                log.parent.mkdir(parents=True, exist_ok=True)
                log.write_text("ok\n", encoding="utf-8")
                if log.name == "yosys.log":
                    (log.parent / "synth.json").write_text("{}\n", encoding="utf-8")

            init_source(root)
            with (
                patch.object(build_fes_pong, "FunctionalInvocation", FakeInvocation),
                patch.object(build_fes_pong, "_require_clean_source",
                             return_value=("https://github.com/DeanoC/misteross.git", "a" * 40)),
                patch.object(build_fes_pong, "_authenticate_tools", side_effect=authenticate),
                patch.object(build_fes_pong, "_run_tool", side_effect=run_tool),
                patch.object(build_fes_pong, "route_after_synth",
                             return_value=SimpleNamespace(seed=1, weight=1)),
                patch.object(
                    build_fes_pong,
                    "validate_build_evidence",
                    return_value={"rbf": {"sha256": "a" * 64, "size": 1}, "route": {}},
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
                        (_write_local_zx81_tools if module is build_fes_zx81_oss else _write_local_pong_tools)(
                            root, nextpnr_config=stamp)
                        with patch.dict(os.environ, {}, clear=True):
                            target = "toolchain-fes-zx81" if module is build_fes_zx81_oss else "toolchain-fes-pong"
                            hint = rf"make {target}"
                            with self.assertRaisesRegex(BuildError, hint):
                                module._authenticate_tools(root)

    def test_local_hip_stamp_from_toolchain_fes_lane_authenticates_pong_and_zx81(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            install = _write_local_zx81_tools(root, nextpnr_config=HIP_CONFIGURATION)
            with patch.dict(os.environ, {}, clear=True):
                authenticated = build_fes_zx81_oss._authenticate_tools(root)
                self.assertEqual(authenticated["nextpnr-mistral"].path, install / "nextpnr-mistral")
                self.assertIn(HIP_CONFIGURATION, authenticated["nextpnr-mistral"].identity)
            root = Path(directory) / "pong"
            root.mkdir()
            install = _write_local_pong_tools(root, nextpnr_config=HIP_CONFIGURATION)
            with patch.dict(os.environ, {}, clear=True):
                authenticated = build_fes_pong._authenticate_tools(root)
                self.assertEqual(authenticated["nextpnr-mistral"].path, install / "nextpnr-mistral")
                self.assertIn(HIP_CONFIGURATION, authenticated["nextpnr-mistral"].identity)

    def test_local_per_core_lock_commit_is_the_authentication_authority(self) -> None:
        commit = "a" * 40
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            install = _write_local_pong_tools(
                root,
                nextpnr_config=HIP_CONFIGURATION,
                nextpnr_commit=commit,
            )
            with patch.dict(os.environ, {}, clear=True):
                authenticated = build_fes_pong._authenticate_tools(root)
            self.assertEqual(authenticated["nextpnr-mistral"].path, install / "nextpnr-mistral")
            self.assertIn(f"commit={commit};", authenticated["nextpnr-mistral"].identity)

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
        self.assertNotIn("toolchains/registered-memory.lock", result.stdout)
        self.assertNotIn("build/toolchain/fes-coleco", result.stdout)
        makefile = (ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertRegex(makefile, r"\.PHONY:.*\btoolchain-fes\b")
        self.assertIn("toolchain-fes  Build the repository-local HIP toolchain from toolchain.lock", makefile)
        self.assertIn("toolchain-fes-zx81  Build the repository-local HIP toolchain for the standard FES ZX81 socket", makefile)
        readme = (ROOT / "README.md").read_text(encoding="utf-8")
        self.assertIn("make toolchain-fes", readme)
        self.assertIn("make toolchain", readme)

        zx81 = subprocess.run(
            ["make", "-n", "toolchain-fes-zx81"],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(zx81.returncode, 0, zx81.stderr)
        self.assertIn(str(ROOT / "cores/fes-zx81/toolchain.lock"), zx81.stdout)
        self.assertNotIn(f'FES_TOOLCHAIN_LOCKFILE="{ROOT}/toolchain.lock"', zx81.stdout)
        self.assertIn("build/toolchain/zx81-expansion", zx81.stdout)

    def test_each_registered_core_toolchain_target_selects_its_lock(self) -> None:
        targets = {
            "fes.menu": "toolchain-fes-menu",
            "fes.catch": "toolchain-fes-catch",
            "fes.pong": "toolchain-fes-pong",
            "fes.zx81": "toolchain-fes-zx81",
            "fes.coleco": "toolchain-fes-coleco",
            "fes.sms": "toolchain-fes-sms",
            "fes.sg1000": "toolchain-fes-sg1000",
            "fes.apple2": "toolchain-fes-apple2",
            "fes.c64": "toolchain-fes-c64",
            "fes.spectrum": "toolchain-fes-spectrum",
            "fes.riscv": "toolchain-fes-riscv",
            "fes.ramtest": "toolchain-fes-ramtest",
            "fes.atari-st": "toolchain-fes-atari-st",
        }
        for core_id, target in targets.items():
            core = core_id.removeprefix("fes.")
            with self.subTest(core=core_id, target=target):
                result = subprocess.run(
                    ["make", "-n", target],
                    cwd=ROOT,
                    text=True,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.PIPE,
                    check=False,
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn(
                    str(ROOT / f"cores/fes-{core}/toolchain.lock"),
                    result.stdout,
                )

    def test_make_forwards_cache_root_to_fes_producers(self) -> None:
        cache = "/tmp/fes-explicit-cache"
        producers = (
            ("build-fes-pong", "scripts/build_fes_pong.py"),
            ("build-fes-zx81", "scripts/build_fes_zx81_oss.py"),
            ("build-fes-coleco", "scripts/build_fes_coleco_socket_v2.py"),
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


class CudaLaneConfigurationTests(unittest.TestCase):
    def test_cuda_producers_authenticate_the_default_cuda_architectures(self) -> None:
        import importlib

        from scripts import fes_build_common

        self.assertEqual(
            fes_build_common.FES_CUDA_ARCHITECTURES,
            toolchain_cache.DEFAULT_CUDA_ARCHITECTURES,
        )
        expected = toolchain_cache.configuration_text(
            {
                "gpu-router": "CUDA",
                "hip-architectures": "unused",
                "cuda-architectures": toolchain_cache.DEFAULT_CUDA_ARCHITECTURES,
            }
        )
        self.assertEqual(expected, "gpu-router=CUDA; hip-architectures=unused; cuda-architectures=86")
        self.assertEqual(fes_build_common.FES_CUDA_TOOLCHAIN_CONFIGURATION, expected)
        with patch.dict(os.environ, {}, clear=False):
            os.environ.pop("FES_TOOLCHAIN_CUDA_ARCHITECTURES", None)
            self.assertEqual(fes_build_common.cuda_toolchain_configuration(), expected)
            for name in CUDA_PRODUCERS:
                with self.subTest(producer=name):
                    producer = importlib.import_module(f"scripts.{name}")
                    self.assertEqual(producer.gpu_choice("CUDA"), ("cuda", "unused", expected))

    def test_custom_cuda_slot_authenticates_and_changes_build_identity(self) -> None:
        import importlib

        from scripts import fes_build_common
        from scripts.build_fes_spectrum_oss import create_build_record
        from scripts.export_core_package import build_identity

        selected = "89"
        configuration = fes_build_common.cuda_toolchain_configuration(selected)
        self.assertEqual(
            configuration,
            "gpu-router=CUDA; hip-architectures=unused; cuda-architectures=89",
        )
        with patch.dict(os.environ, {"FES_TOOLCHAIN_CUDA_ARCHITECTURES": "75;86"}, clear=False):
            multi = fes_build_common.cuda_toolchain_configuration()
        self.assertEqual(
            multi,
            "gpu-router=CUDA; hip-architectures=unused; cuda-architectures=75;86",
        )
        self.assertEqual(
            fes_build_common._cuda_architectures_from_configuration(multi),
            "75;86",
        )
        with patch.dict(os.environ, {"FES_TOOLCHAIN_CUDA_ARCHITECTURES": "native"}, clear=False):
            with self.assertRaises(BuildError):
                fes_build_common.cuda_toolchain_configuration()

        host = {"system": "Linux", "release": "test", "machine": "x86_64"}
        compiler = {"commands": {"cc": {"path": "/test/cc"}}}
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            spectrum = importlib.import_module("scripts.build_fes_spectrum_oss")
            request, _manifest = publish_shared_toolchain(
                base, lock_path=ROOT / spectrum.SPECTRUM_TOOLCHAIN_LOCK,
                gpu_router="CUDA", cuda_architectures=selected
            )
            with patch.dict(
                os.environ,
                {"FES_TOOLCHAIN_CUDA_ARCHITECTURES": selected, "PATH": "/bin"},
                clear=True,
            ), patch.object(toolchain_cache, "host_identity", return_value=host), patch.object(
                toolchain_cache, "compiler_inventory", return_value=compiler
            ):
                for name in CUDA_PRODUCERS:
                    with self.subTest(producer=name):
                        producer = importlib.import_module(f"scripts.{name}")
                        self.assertEqual(
                            producer.gpu_choice("CUDA"),
                            ("cuda", "unused", configuration),
                        )
                authenticated = spectrum._authenticate_spectrum_tools(
                    ROOT, cache_root=request.cache_root, gpu_router="CUDA"
                )
            identity = authenticated["nextpnr-mistral"].identity
            self.assertIn(configuration, identity)
            self.assertEqual(authenticated["nextpnr-mistral"].path.parent.name, "bin")

        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            publish_shared_toolchain(base, lock_path=ROOT / spectrum.SPECTRUM_TOOLCHAIN_LOCK,
                                     gpu_router="CUDA", cuda_architectures="86")
            with patch.dict(
                os.environ,
                {"FES_TOOLCHAIN_CUDA_ARCHITECTURES": selected, "PATH": "/bin"},
                clear=True,
            ), patch.object(toolchain_cache, "host_identity", return_value=host), patch.object(
                toolchain_cache, "compiler_inventory", return_value=compiler
            ):
                with self.assertRaises(BuildError):
                    spectrum._authenticate_spectrum_tools(
                        ROOT, cache_root=base / "shared-cache", gpu_router="CUDA"
                    )

        def record(tool_identity: str) -> bytes:
            return create_build_record(
                ROOT,
                "https://example.invalid/m.git",
                "a" * 40,
                {
                    "yosys": "commit=y; sha256=" + "b" * 64,
                    "mistral": "commit=m; sha256=" + "c" * 64,
                    "nextpnr-mistral": tool_identity,
                },
                execution=EXECUTION,
                gpu_backend="cuda",
                gpu_architectures="unused",
            )

        default_identity = "commit=n; sha256=" + "d" * 64 + "; " + fes_build_common.FES_CUDA_TOOLCHAIN_CONFIGURATION
        custom_identity = "commit=n; sha256=" + "d" * 64 + "; " + configuration
        default_bytes = record(default_identity)
        custom_bytes = record(custom_identity)
        custom_record = json.loads(custom_bytes)
        self.assertIn(configuration, custom_record["tools"]["nextpnr-mistral"])
        self.assertNotEqual(build_identity(default_bytes), build_identity(custom_bytes))
        self.assertNotEqual(json.loads(default_bytes)["tools"], custom_record["tools"])


if __name__ == "__main__":
    unittest.main()


def setUpModule():
    global ROOT, _source_fixture
    _source_fixture, ROOT = clean_module(ROOT)

def tearDownModule():
    _source_fixture.cleanup()
