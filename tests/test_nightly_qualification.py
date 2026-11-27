from dataclasses import replace
from contextlib import redirect_stderr, redirect_stdout
import io
import json
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
import nightly_qualification as nightly


SOURCE_KEY = nightly.cell_key


class NightlyTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.store = Path(temporary.name)
        self.sha = "1" * 40
        self.release = dict(board="de10-nano", cores=["fes.menu", "fes.pong"], software_sha256="a" * 64)
        self.config = {"releases": {"kit": self.release}}
        self.keys = {core: core for core in self.release["cores"]}
        self.built = []
        quiet_output = redirect_stdout(io.StringIO())
        quiet_output.__enter__()
        self.addCleanup(quiet_output.__exit__, None, None, None)
        quiet = redirect_stderr(io.StringIO())
        quiet.__enter__()
        self.addCleanup(quiet.__exit__, None, None, None)
        self.patch(nightly, "git", side_effect=lambda *args: self.sha if args[0] == "rev-parse" else "")
        self.patch(nightly.subprocess, "run", wraps=subprocess.run,
                   side_effect=lambda argv, **kw: subprocess.CompletedProcess(argv, 0)
                   if argv[0] == "git" else self.fail("unexpected external command"))
        self.patch(nightly.recipes, "load_recipes", side_effect=lambda board: {
            core: nightly.recipes.recipe_for(core) for core in self.keys} if board == "de10-nano" else {})
        self.patch(nightly, "cell_key", side_effect=lambda source, recipe: self.keys[recipe.core_id])
        self.patch(nightly.core_dev, "prepare", side_effect=self.prepare)

    def patch(self, obj, name, **kwargs):
        p = patch.object(obj, name, **kwargs)
        self.addCleanup(p.stop)
        return p.start()

    def prepare(self, core, folder, board):
        self.built.append((self.sha, core))
        folder.mkdir()
        members = {"manifest.toml": core.encode(), "core.rbf": self.keys[core].encode()}
        archive = folder / "core.fcore"
        with tarfile.open(archive, "w") as stream:
            for name, data in members.items():
                info = tarfile.TarInfo(name)
                info.size = len(data)
                stream.addfile(info, io.BytesIO(data))
        package = nightly.digest(archive.read_bytes())
        payload = nightly.digest(members["core.rbf"])
        selection = (f'format = 2\nkind = "core-package"\ncore_id = "{core}"\npackage_id = "{package}"\n'
                     f'payload_sha256 = "{payload}"\nmisteross_revision = "{self.sha}"\n'
                     f'mister_packages_revision = "{self.sha}"\n').encode()
        (folder / "selection.toml").write_bytes(selection)
        receipt = dict(format=1, core_id=core, package_id=package,
                       archive=dict(path=archive.name, size=archive.stat().st_size, sha256=package),
                       sources=dict.fromkeys(["FogCast", "misteross", "mister-packages", "libmister-runtime"], self.sha),
                       selection=dict(path="selection.toml", sha256=nightly.digest(selection),
                                      manifest_sha256=nightly.digest(members["manifest.toml"]), payload_sha256=payload))
        (folder / "prepared.json").write_bytes(nightly.encoded(receipt))
        return receipt

    def run_batch(self):
        receipt = self.store / "simulation-fixture.json"
        receipt.write_bytes(nightly.encoded(dict(main=self.sha, full_simulations_and_producers_sha=self.sha,
                                                 pcxt_com="success",
                                                 run_url="https://github.com/DeanoC/fes/actions/runs/1/attempts/1")))
        return nightly.batch(self.store, self.sha, self.config, receipt)

    def requests(self):
        return [(p.name, nightly.read(p)["request"]) for p in (self.store / "requests").iterdir()]

    def answer(self, identity, outcome="PASS", kit="B"):
        evidence = self.store / "test-evidence"
        evidence.write_text("synthetic evidence for " + identity)
        verdict = self.store / "test-verdict"
        verdict.write_bytes(nightly.encoded(dict(request_sha256=identity, verdict=outcome, kit=kit,
                                                 restored=True, evidence_sha256=nightly.digest(evidence.read_bytes()))))
        nightly.record_verdict(self.store, verdict, evidence)

    def test_superseded_gate_reuses_only_the_exact_staged_candidate(self):
        self.run_batch()
        binding, original = next((b, r) for b, r in self.requests() if r["core"] == "fes.menu")
        recipe = nightly.recipes.recipe_for("fes.menu")
        args = self.store, self.sha, recipe, None, original["inputs"], None
        builds = list(self.built)
        replacement = nightly.qualify(*args, replaces=binding, spool_only=True)
        request = nightly.read(self.store / "requests" / replacement)["request"]
        self.assertEqual(request, dict(original, replaces=binding))
        self.assertNotEqual(binding, replacement)
        self.answer(replacement)
        obj = nightly.qualify(*args, replaces=binding)
        self.assertEqual(nightly.read(self.store / "objects" / obj / "qualification.json")["request"], request)
        with self.assertRaisesRegex(ValueError, "candidate missing"):
            nightly.qualify(*args[:4], "missing", None, replaces=binding)
        self.assertEqual(self.built, builds)

    def seed(self):
        self.assertEqual(self.run_batch(), 1)
        for identity, request in self.requests():
            self.assertEqual(request["main"], self.sha)
            self.answer(identity)
        self.assertEqual(self.run_batch(), 1)
        for identity, request in self.requests():
            if "release" in request:
                self.answer(identity)
        self.assertEqual(self.run_batch(), 0)
        return nightly.read(self.store / "state.json")["releases"]["kit"]["current"]

    def test_release_worker_can_await_matching_kit_and_import_verdict(self):
        self.run_batch()
        request = dict(release=self.release, packages=[])
        runner = nightly.subprocess.run
        runner.side_effect = lambda argv, **kwargs: subprocess.CompletedProcess(argv, 4)
        with self.assertRaises(nightly.AwaitingEvidence):
            nightly.gate(self.store, "/worker", request)
        self.answer(nightly.digest(nightly.encoded(request)))
        runner.reset_mock()
        self.assertEqual(nightly.gate(self.store, "/worker", request)["verdict"], "PASS")
        runner.assert_not_called()

    def test_batch_failure_retry_and_compatible_fallback(self):
        previous = self.seed()
        self.sha = "2" * 40
        self.keys["fes.pong"] = "changed shared RTL"
        self.assertEqual(self.run_batch(), 1)
        identity = next(i for i, r in self.requests() if r.get("main") == self.sha)
        self.answer(identity, outcome="FAIL")
        self.sha = "3" * 40
        self.assertEqual(self.run_batch(), 1)
        state = nightly.read(self.store / "state.json")
        self.assertEqual(state["cells"]["de10-nano/fes.pong"]["pending"]["main"], self.sha)
        self.assertEqual(state["releases"]["kit"]["current"], previous)
        self.answer(identity)
        self.assertEqual(self.run_batch(), 0)  # Old set still admitted while new tuple awaits evidence.
        latest = next(i for i, r in self.requests() if "release" in r and r["packages"][1]["main"] == "2"*40)
        self.answer(latest, outcome="INCOMPATIBLE")
        self.assertEqual(self.run_batch(), 0)
        self.assertEqual(nightly.read(self.store / "state.json")["releases"]["kit"]["current"], previous)
        self.answer(latest)
        self.assertEqual(self.run_batch(), 0)
        self.assertNotEqual(nightly.read(self.store / "state.json")["releases"]["kit"]["current"], previous)
        self.assertEqual(self.built, [("1"*40,"fes.menu"), ("1"*40,"fes.pong"), ("2"*40,"fes.pong")])

    def test_new_software_needs_its_own_admission_and_corruption_fails_closed(self):
        previous = self.seed()
        self.release["software_sha256"] = "b"*64
        self.assertEqual(self.run_batch(), 1)
        track = nightly.read(self.store / "state.json")["releases"]["kit"]
        self.assertIn("pending", track)
        self.assertEqual(track["current"], previous)
        lock = nightly.load_lock(self.store, previous)
        (self.store / lock["packages"][0]["prepared"]).write_bytes(b"damaged")
        self.assertEqual(self.run_batch(), 1)
        self.assertIn("corrupt", nightly.read(self.store / "state.json")["releases"]["kit"]["pending"]["error"])

    def test_release_command_pins_software_and_checks_retained_bytes(self):
        identity = self.seed()
        args = ["nightly", "--store", str(self.store), "select", "--release", "kit",
                "--software-sha256", self.release["software_sha256"]]
        with patch.object(sys, "argv", args), redirect_stdout(io.StringIO()) as output:
            self.assertEqual(nightly.main(), 0)
        self.assertEqual(Path(output.getvalue().strip()), self.store / "sets" / identity)
        with patch.object(sys, "argv", args[:-1] + ["b"*64]):
            with self.assertRaisesRegex(ValueError, "different release"):
                nightly.main()
        lock = nightly.load_lock(self.store, identity)
        (self.store / "evidence" / lock["admission"]["evidence_sha256"]).write_text("tampered")
        with patch.object(sys, "argv", args), self.assertRaises(ValueError):
            nightly.main()

    def test_release_preparation_exposes_verified_menu_for_image_pack(self):
        identity = self.seed()
        menu = nightly.load_lock(self.store, identity)["packages"][0]
        root = (self.store / menu["prepared"]).parent
        receipt = nightly.read(root / "prepared.json")
        self.assertEqual(nightly.digest((root / "menu-package/core.rbf").read_bytes()),
                         receipt["selection"]["payload_sha256"])

    def test_record_digest_does_not_trigger_a_rebuild_on_unrelated_commit(self):
        recipe = nightly.recipes.recipe_for("fes.menu")
        record = {"repository": "fes", "source_path": "sources/misteross", "revision": "1"*40, "identities": {"design":"v1", "record": "a"*64}, "source_inputs": {"rtl": "v1"}}
        with patch.object(nightly.bundle, "canonical_package_record", side_effect=lambda *a, **kw: nightly.encoded(record)):
            # cell_key is replaced with a fixture for the batch tests.
            original = SOURCE_KEY
            first = original(self.store, recipe)
            record.update(revision="2"*40, identities={"design":"v1", "record":"b"*64})
            self.assertEqual(first, original(self.store, recipe))
            record["identities"]["design"] = "v2"
            self.assertNotEqual(first, original(self.store, recipe))

    def test_changed_hil_requires_new_kit_verdict_for_identical_archive(self):
        self.seed()
        original = nightly.recipes.recipe_for("fes.pong")
        self.keys["fes.pong"] = "fes.pong"  # keep the test producer's archive identical
        changed = replace(original, hil_kits=("A",), hil_check="scripts/pcxt_dos_hil_acceptance.py")
        self.patch(nightly.recipes, "load_recipes", side_effect=lambda board: {
            "fes.pong": changed} if board == "de10-nano" else {})
        self.patch(nightly, "cell_key", return_value="new acceptance requirements")
        self.assertEqual(self.run_batch(), 1)
        binding, request = next((i, r) for i, r in self.requests() if r.get("inputs") == "new acceptance requirements")
        self.assertEqual(request["hil"]["kits"], ["A"])
        self.answer(binding, kit="A")
        self.assertEqual(self.run_batch(), 1)
        pending = nightly.read(self.store / "state.json")["cells"]["de10-nano/fes.pong"]["pending"]
        self.assertIn("missing registered HIL check", pending["error"])

    def test_hil_checker_bytes_and_kit_order_change_input_key(self):
        script = self.store / "registered.py"
        recipe = replace(nightly.recipes.recipe_for("fes.menu"), hil_check=script.name, hil_kits=("B", "A"))
        with patch.object(nightly, "ROOT", self.store), patch.object(nightly.bundle, "canonical_package_record",
                return_value=nightly.encoded({"identities": {"design": "same", "record": "unchanged"}})):
            for path in (nightly.HIL_WORKER_PATH, nightly.DE10_SMOKE_ADAPTER_PATH,
                         nightly.MENU_SMOKE_ADAPTER_PATH):
                target = self.store / path
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text("old")
            script.write_text("old")
            original = SOURCE_KEY(self.store, recipe)
            self.assertNotEqual(original, SOURCE_KEY(self.store, replace(recipe, hil_kits=("A",))))
            script.write_text("new")
            self.assertNotEqual(original, SOURCE_KEY(self.store, recipe))

    def test_hil_worker_and_smoke_adapter_bytes_change_input_key(self):
        recipe = nightly.recipes.recipe_for("fes.menu")
        with patch.object(nightly, "ROOT", self.store), patch.object(nightly.bundle, "canonical_package_record",
                return_value=nightly.encoded({"identities": {"design": "same", "record": "unchanged"}})):
            for path in (nightly.HIL_WORKER_PATH, nightly.DE10_SMOKE_ADAPTER_PATH,
                         nightly.MENU_SMOKE_ADAPTER_PATH):
                target = self.store / path
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text("old")
            original = SOURCE_KEY(self.store, recipe)
            for path in (nightly.HIL_WORKER_PATH, nightly.DE10_SMOKE_ADAPTER_PATH,
                         nightly.MENU_SMOKE_ADAPTER_PATH):
                target = self.store / path
                target.write_text("new " + path)
                self.assertNotEqual(original, SOURCE_KEY(self.store, recipe), path)
                target.write_text("old")

    def test_release_admission_identity_tracks_worker_and_policy_bytes(self):
        paths = (nightly.ADMISSION_WORKER_PATH, *nightly.ADMISSION_POLICY_PATHS)
        with patch.object(nightly, "ROOT", self.store):
            for path in paths:
                target = self.store / path
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text("old")
            original = nightly.release_admission_identity()
            (self.store / nightly.ADMISSION_WORKER_PATH).write_text("new worker")
            changed_worker = nightly.release_admission_identity()
            self.assertNotEqual(original["worker_sha256"], changed_worker["worker_sha256"])
            self.assertEqual(original["policy_sha256"], changed_worker["policy_sha256"])
            (self.store / nightly.ADMISSION_WORKER_PATH).write_text("old")
            (self.store / "scripts/image_artifacts.py").write_text("new admission policy")
            changed_policy = nightly.release_admission_identity()
            self.assertEqual(original["worker_sha256"], changed_policy["worker_sha256"])
            self.assertNotEqual(original["policy_sha256"], changed_policy["policy_sha256"])

    def test_changed_release_admission_identity_requires_a_new_verdict(self):
        previous = {"worker_sha256": "a" * 64, "policy_sha256": "b" * 64}
        current = {"worker_sha256": "c" * 64, "policy_sha256": "d" * 64}
        with patch.object(nightly, "release_admission_identity", return_value=previous):
            published = self.seed()
        with patch.object(nightly, "release_admission_identity", return_value=current):
            self.assertEqual(self.run_batch(), 1)
            identity, request = next((identity, request) for identity, request in self.requests()
                                     if request.get("release") and request.get("admission_policy") == current)
            self.assertEqual(request["admission_policy"], current)
            self.answer(identity)
            self.assertEqual(self.run_batch(), 0)
        updated = nightly.read(self.store / "state.json")["releases"]["kit"]["current"]
        self.assertNotEqual(published, updated)

    def test_wrong_kit_and_unbound_verdict_do_not_qualify(self):
        self.run_batch()
        for identity, request in self.requests():
            self.answer(identity, kit="TRWU8J8G")
        self.run_batch()
        state = nightly.read(self.store / "state.json")
        self.assertTrue(all("object" not in cell for cell in state["cells"].values()))
        path = next((self.store / "verdicts").iterdir())
        verdict = nightly.read(path)
        verdict["request_sha256"] = "f"*64
        path.write_bytes(nightly.encoded(verdict))
        self.run_batch()
        self.assertTrue(any("different request" in cell["pending"]["error"]
                            for cell in nightly.read(self.store / "state.json")["cells"].values()))
