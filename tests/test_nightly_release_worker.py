import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

SCRIPTS = Path(__file__).resolve().parents[1] / "scripts"
sys.path.insert(0, str(SCRIPTS))
spec = importlib.util.spec_from_file_location("worker", SCRIPTS / "powerboat/nightly-release-admit.py")
worker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(worker)


class PlayableAdmissionTest(unittest.TestCase):
    def test_exact_runtime_tenfoot_and_menu_required(self):
        with tempfile.TemporaryDirectory() as tmp:
            store = Path(tmp) / "store"
            (store / "evidence").mkdir(parents=True)
            (store / "objects/test").mkdir(parents=True)
            software = store.parent / "software" / ("a" * 64)
            software.mkdir(parents=True)
            binaries = {"mister-runtime": "b" * 64, "fogcast-tenfoot": "c" * 64}
            (software / "artifacts.json").write_text(json.dumps({"binaries": binaries}))
            release = dict(board="de10-nano", cores=["fes.menu", "fes.zx81"], software_sha256=software.name)
            menu = dict(core="fes.menu", package_id="d" * 64)
            game = dict(core="fes.zx81", package_id="e" * 64, prepared="objects/test/prepared.json")
            (store / game["prepared"]).write_text(json.dumps(game))
            selection = dict(release=release, packages=[menu, game])
            runtime = binaries["mister-runtime"]
            tenfoot = binaries["fogcast-tenfoot"]
            lines = [f"{runtime}  mister-runtime", f"{tenfoot}  fogcast-tenfoot", f"{runtime} active-mister-runtime",
                     f"{tenfoot} active-fogcast-tenfoot", "--menu-package",
                     "/usr/share/mister-runtime/core-packages/" + menu["package_id"], menu["package_id"]]
            with patch.object(worker, "bootstrap_admission"), patch.object(worker, "publish", return_value={"catalog_sha256": "f"*64}):
                for missing in [None, 3, 6]:
                    report = dict(request_sha256="1"*64, request=game, smoke=game, restored=True,
                                  software="\n".join(v for i, v in enumerate(lines) if i != missing))
                    raw = worker.encoded(report)
                    digest = worker.digest(raw)
                    (store / "evidence" / digest).write_bytes(raw)
                    (store / "objects/test/qualification.json").write_text(json.dumps({"receipt": {
                        "evidence_sha256": digest, "request_sha256": report["request_sha256"]}}))
                    if missing is None:
                        self.assertEqual(worker.admit(selection, store)["verdict"], "PASS")
                    else:
                        with self.assertRaises(SystemExit) as error:
                            worker.admit(selection, store)
                        self.assertEqual(error.exception.code, 4)
