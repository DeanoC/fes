import json
from pathlib import Path
import shutil
import subprocess
import sys
from types import SimpleNamespace
import tempfile
import unittest
from unittest.mock import patch

from scripts import build_fes_coleco_oss as coleco
from scripts.export_core_package import build_identity, PackageExportError
from scripts.functional_execution import execution_environment, execution_digest
from scripts.fes_build_common import _run_tool
from scripts.search_placer_qor import route_after_synth, SearchError


class FunctionalColecoTests(unittest.TestCase):
    def test_real_producer_closure_and_invalidation(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = Path(__file__).resolve().parents[1]
            for relative in ("scripts", "cores/fes-coleco", "cores/fes-common", "toolchains"):
                shutil.copytree(source / relative, root / relative,
                                ignore=shutil.ignore_patterns("__pycache__"))
            def git(*args):
                return subprocess.check_output(["git", "-C", str(root), *args], text=True).strip()
            git("init", "-q")
            git("config", "user.name", "Test")
            git("config", "user.email", "test@example.invalid")
            git("add", ".")
            git("commit", "-qm", "source")
            execution = {"gpu_device": 0, "version": 1, "environment": {"LANG": "C"}}
            def record():
                return coleco.create_build_record(root, "https://example.invalid/source", git("rev-parse", "HEAD"),
                    {"yosys": "test"}, identity_version=2, execution=execution)
            before = record()
            self.assertLess(len(before), 65536)
            (root / "README.md").write_text("Unrelated docs")
            git("add", "README.md")
            git("commit", "-qm", "docs")
            after = record()
            self.assertNotEqual(json.loads(before)["revision"], json.loads(after)["revision"])
            self.assertEqual(build_identity(before), build_identity(after))
            (root / "apps/ui").mkdir(parents=True)
            (root / "apps/ui/view.js").write_text("// unrelated UI change\n")
            git("add", "apps")
            git("commit", "-qm", "UI")
            self.assertEqual(build_identity(before), build_identity(record()))
            for relative in ("scripts/fes_build_common.py", "cores/fes-common/rtl/fes_application_gp.v",
                             "cores/fes-coleco/rtl/top.v", "cores/fes-coleco/clocks-oss.sdc",
                             "toolchains/registered-memory.lock"):
                with self.subTest(relative=relative):
                    path = root / relative
                    original = path.read_bytes()
                    path.write_bytes(original + b"\n")
                    self.assertNotEqual(build_identity(before), build_identity(record()))
                    path.write_bytes(original)
            execution["gpu_device"] = 1
            self.assertNotEqual(build_identity(before), build_identity(record()))
            (root / "scripts/untracked_helper.py").write_text("unexpected")
            with self.assertRaisesRegex(PackageExportError, "untracked"):
                record()

    def test_all_producers_support_real_module_root_without_relabeling(self):
        from scripts import build_fes_pong, build_fes_zx81_oss, build_fes_sms_oss, build_fes_sg1000_oss
        producers = (build_fes_pong, build_fes_zx81_oss, coleco, build_fes_sms_oss, build_fes_sg1000_oss)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = Path(__file__).resolve().parents[1]
            for relative in ("scripts", "cores", "boards", "toolchains"):
                shutil.copytree(source / relative, root / relative, ignore=shutil.ignore_patterns("__pycache__"))
            shutil.copy2(source / "toolchain.lock", root / "toolchain.lock")
            def git(*args):
                return subprocess.check_output(["git", "-C", str(root), *args], text=True).strip()
            git("init", "-q")
            git("config", "user.name", "Test")
            git("config", "user.email", "test@example.invalid")
            git("remote", "add", "origin", "https://example.invalid/fes")
            git("add", ".")
            git("commit", "-qm", "standalone")
            execution = {"gpu_device": 0, "version": 1}
            old = {producer: producer.create_build_record(root, "https://example.invalid/fes", git("rev-parse", "HEAD"),
                   {"yosys": "test"}, identity_version=2, execution=execution) for producer in producers}
            module = root / "sources/misteross"
            module.mkdir(parents=True)
            for name in ("scripts", "cores", "boards", "toolchains", "toolchain.lock"):
                (root / name).rename(module / name)
            git("add", "-A")
            git("commit", "-qm", "import")
            (root / "unrelated-user-work").write_text("preserve")
            for producer in producers:
                with self.subTest(producer=producer.__name__):
                    repository, revision = producer._require_clean_source(module, identity_version=2)
                    self.assertEqual(repository, "https://example.invalid/fes")
                    self.assertEqual(revision, git("rev-parse", "HEAD"))
                    record = producer.create_build_record(module, repository, revision, {"yosys": "test"},
                                                         identity_version=2, execution=execution)
                    self.assertEqual(json.loads(record)["source_path"], "sources/misteross")
                    self.assertEqual(build_identity(old[producer]), build_identity(record))
                    self.assertEqual(producer._require_clean_source(module), (repository, revision))
                    for relative in (("scripts/fes_build_common.py", "cores/fes-common/rtl/tv80/tv80_core.v",
                                      "toolchains/registered-memory.lock")
                                     if producer in (coleco, build_fes_sms_oss, build_fes_sg1000_oss)
                                     else ("scripts/fes_build_common.py",)):
                        path = module / relative
                        original = path.read_bytes()
                        try:
                            path.write_bytes(original + b"\n")
                            changed = producer.create_build_record(module, repository, revision,
                                {"yosys": "test"}, identity_version=2, execution=execution)
                            self.assertNotEqual(build_identity(record), build_identity(changed))
                        finally:
                            path.write_bytes(original)


    def test_execution_hashes_compiler_abc_and_required_support_bytes(self):
        from scripts.functional_execution import execution_inputs, _REQUIRED_YOSYS_SUPPORT
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            binary = root / "install/bin/yosys"
            binary.parent.mkdir(parents=True)
            binary.write_bytes(b"compiler one")
            binary.chmod(0o755)
            abc = binary.parent / "yosys-abc"
            abc.write_bytes(b"ABC one")
            abc.chmod(0o755)
            support = root / "install/share/yosys"
            for relative in _REQUIRED_YOSYS_SUPPORT:
                path = support / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("support")
            tools = {"yosys": binary}
            env = execution_environment(root, tools)
            with patch("scripts.functional_execution._machine_input_files", return_value=[]), \
                 patch("scripts.functional_execution.subprocess.run", return_value=SimpleNamespace(returncode=0, stdout="", stderr="")):
                before = execution_inputs(tools, env, 0)
                binary.write_bytes(b"compiler two with same version string")
                changed = execution_inputs(tools, env, 0)
                self.assertNotEqual(execution_digest(before), execution_digest(changed))
                abc.write_bytes(b"ABC two")
                self.assertNotEqual(execution_digest(changed), execution_digest(execution_inputs(tools, env, 0)))
                self.assertIn(str(abc), before["files"])
                (support / _REQUIRED_YOSYS_SUPPORT[0]).unlink()
                with self.assertRaisesRegex(ValueError, "support data"):
                    execution_inputs(tools, env, 0)
                abc.unlink()
                with self.assertRaisesRegex(ValueError, "yosys-abc"):
                    execution_inputs(tools, env, 0)

    def test_synthesis_and_route_receive_controlled_environment(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            env = execution_environment(root, {"yosys": Path(sys.executable)})
            with patch.dict("os.environ", {"YOSYS_DATDIR": "ambient", "HIP_VISIBLE_DEVICES": "99"}):
                _run_tool((sys.executable, "-c", "import os,json; print(json.dumps(dict(os.environ)))"),
                          root, root / "env.log", env=env, output_relative=Path("build/test"))
            observed = json.loads((root / "env.log").read_text())
            self.assertNotIn("YOSYS_DATDIR", observed)
            self.assertNotIn("HIP_VISIBLE_DEVICES", observed)
            with patch("scripts.search_placer_qor.subprocess.run", return_value=SimpleNamespace(returncode=1)) as run:
                with self.assertRaises(SearchError):
                    route_after_synth(nextpnr=root / "nextpnr", fixture=root / "synth.json",
                        dest=root, device="test", qsf=root / "test.qsf", sdc=None, freq=None,
                        seeds=(1,), weights=(10,), critexp=5, budget=1, mode="first-pass",
                        extra=(), gpu_devices=(2,), env=env)
                self.assertEqual(run.call_args.kwargs["env"], env)
                self.assertEqual(run.call_args.args[0][-2:], ["--gpu-device", "2"])

    def test_environment_is_explicit_and_home_is_not_ambient(self):
        tools = {"yosys": Path("/qualified/install/bin/yosys")}
        with patch.dict("os.environ", {"LD_PRELOAD": "unsafe", "YOSYS_DATDIR": "/ambient", "HOME": "/user"}):
            env = execution_environment(Path("/private/home"), tools)
        self.assertEqual(env["HOME"], "/private/home")
        self.assertEqual(env["LC_ALL"], "C")
        self.assertNotIn("LD_PRELOAD", env)
        self.assertNotIn("YOSYS_DATDIR", env)
        self.assertNotEqual(execution_digest({"gpu_device": 0}), execution_digest({"gpu_device": 1}))


if __name__ == "__main__":
    unittest.main()
