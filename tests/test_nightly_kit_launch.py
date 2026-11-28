import hashlib
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

LAUNCHER = Path(__file__).resolve().parents[1] / "scripts/powerboat/nightly-kit-launch.py"


class FrozenWorkerTest(unittest.TestCase):
    def test_legacy_worker_loads_validator_and_lazy_import_from_frozen_tree(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for tree in ("ambient", "frozen"):
                scripts = root / tree / "scripts"
                scripts.mkdir(parents=True)
                (scripts / "marker.py").write_text(f"VALUE = {tree!r}\n")
                (scripts / "core_dev_accept.py").write_text(
                    f"def validate():\n    from marker import VALUE\n    return ({tree!r} + '/' + VALUE).encode()\n"
                )
            worker = root / "frozen/scripts/powerboat/nightly-kit-worker.py"
            worker.parent.mkdir()
            worker.write_text(
                f"import sys\nsys.path.insert(0, {str(root / 'ambient/scripts')!r})\n"
                "from core_dev_accept import validate\n"
                "def accept(binding):\n    return validate()\n"
            )
            result = subprocess.check_output(
                [sys.executable, "-I", "-B", "-c",
                 "import runpy, sys\nfrom pathlib import Path\n"
                 "run = runpy.run_path(sys.argv[1])['run_snapshot']\n"
                 "root = Path.cwd()\nrun.__globals__['REPO'] = root / 'ambient'\n"
                 "run(root / 'frozen', root / 'frozen', 'request')", LAUNCHER],
                cwd=root,
            )
            self.assertEqual(result, b"frozen/frozen")

    def test_replacement_runs_protected_policy_with_original_validator(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            repo = root / "fes"
            store = root / ".local/share/fes-qualification"
            store.mkdir(parents=True)
            env = dict(os.environ, HOME=directory, PATH=f"{root}:{os.environ['PATH']}")
            ssh = root / "ssh"
            ssh.write_text('#!/bin/sh\ncat "$HOME/request.json"\n')
            ssh.chmod(0o755)

            def git(*args):
                return subprocess.check_output(["git", "-C", repo, *args], env=env, text=True).strip()

            repo.mkdir()
            git("init", "-q", "-b", "main")
            git("config", "user.name", "test")
            git("config", "user.email", "test@example.com")
            worker_path = "scripts/powerboat/nightly-kit-worker.py"
            worker = repo / worker_path
            worker.parent.mkdir(parents=True)
            validator = repo / "scripts/core_dev_accept.py"
            validator.write_text("def validate():\n    from marker import VALUE\n    return VALUE\n")
            marker = repo / "scripts/marker.py"
            marker.write_text("VALUE = b'artifact'\n")
            worker.write_text("def accept(binding): return b'old worker'\n")
            git("add", ".")
            git("commit", "-qm", "artifact")
            head = git("rev-parse", "HEAD")
            marker.write_text("VALUE = b'changed'\n")
            worker.write_text("from core_dev_accept import validate\ndef accept(binding): return b'corrected:' + validate()\n")
            git("commit", "-qam", "corrected gate")
            policy = git("rev-parse", "HEAD")
            git("remote", "add", "origin", str(repo))
            request = dict(main=head, policy_main=policy, replaces="a"*64,
                           hil=dict(worker_sha256=hashlib.sha256(worker.read_bytes()).hexdigest()))

            def launch():
                data = (json.dumps(request, sort_keys=True, separators=(",", ":")) + "\n").encode()
                (root / "request.json").write_text(json.dumps(dict(request=request)))
                return subprocess.run([sys.executable, "-I", "-B", LAUNCHER, hashlib.sha256(data).hexdigest()],
                                      env=env, capture_output=True)

            result = launch()
            self.assertEqual((result.returncode, result.stdout), (0, b"corrected:artifact"), result.stderr)
            request["hil"]["worker_sha256"] = "0"*64
            self.assertIn(b"frozen HIL executable mismatch", launch().stderr)
            git("switch", "-qc", "unmerged")
            git("commit", "-q", "--allow-empty", "-m", "unmerged policy")
            request["policy_main"] = git("rev-parse", "HEAD")
            request["hil"]["worker_sha256"] = hashlib.sha256(worker.read_bytes()).hexdigest()
            self.assertNotEqual(launch().returncode, 0)
