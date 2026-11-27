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
                 "run(root / 'frozen', 'request')", LAUNCHER],
                cwd=root,
            )
            self.assertEqual(result, b"frozen/frozen")
