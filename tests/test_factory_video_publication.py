"""Host-only factory publication checks using real encoded FPGA/package readers."""
import copy
import hashlib
import json
from pathlib import Path
import runpy
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))
import core_catalog
import core_dev_accept
from scripts import factory_video_parts as video


def sha(data):
    return hashlib.sha256(data).hexdigest()


_ARCHIVE = r'''
import json, sys
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from scripts.export_core_package import _archive_bytes
from scripts.core_package import read_package
directory, destination = Path(sys.argv[2]), Path(sys.argv[3])
destination.write_bytes(_archive_bytes((directory/'manifest.toml').read_bytes(),
                                     (directory/'core.rbf').read_bytes(), None))
package = read_package(destination)
print(json.dumps(dict(package_id=package.package_id,
                      core_id=package.fields['core']['id'])))
'''


_CONSUMER = r'''
package main
import (
    "bytes"
    "context"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "os"
    "github.com/DeanoC/FogCast/corecatalog"
    "github.com/DeanoC/FogCast/corepackage"
)
func check() error {
    catalog, err := corecatalog.Load(os.Args[1])
    if err != nil { return err }
    reader, entry, err := catalog.OpenPackage("fes.coleco")
    if err != nil { return err }
    archive, err := io.ReadAll(reader)
    _ = reader.Close()
    if err != nil { return err }
    root, err := os.MkdirTemp("", "factory-video-consumer-")
    if err != nil { return err }
    defer os.RemoveAll(root)
    staged, err := corepackage.Stage(context.Background(), root, int64(len(archive)), bytes.NewReader(archive))
    if err != nil { return err }
    defer staged.Cleanup()
    shell, err := corepackage.CanonicalArchive(staged.Directory)
    if err != nil { return err }
    parts, err := catalog.ReadVideoParts(context.Background(), entry, shell)
    if err != nil { return err }
    references := make([]corepackage.FactoryVideoReference, 0, len(parts))
    for _, part := range parts {
        sum := sha256.Sum256(part.Archive)
        reference := part.Reference
        if hex.EncodeToString(sum[:]) != reference.ArchiveSHA256 || int64(len(part.Archive)) != reference.ArchiveSize {
            return fmt.Errorf("consumer changed authoritative part archive")
        }
        references = append(references, reference)
    }
    return json.NewEncoder(os.Stdout).Encode(map[string]any{"package_id": staged.PackageID, "video_parts": references})
}
func main() {
    if err := check(); err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
}
'''


class FactoryVideoPublicationTests(unittest.TestCase):
    lane = "native"
    @classmethod
    def setUpClass(cls):
        cls.fixture_temp = tempfile.TemporaryDirectory()
        cls.fixture = Path(cls.fixture_temp.name)
        # Reuse the producer reader fixture: these are valid full-device encoded
        # frames and Python USTAR archives, not compiler or hardware acceptance.
        fixture_program = runpy.run_path(str(ROOT / "tests/test_factory_video_parts.py"))["_FIXTURE"]
        # Publication only consumes archives. The separate producer suite checks
        # routing/timing evidence; avoid recalculating its full-device diff here.
        fixture_program, marker, _ = fixture_program.partition("    cells={} if profile=='direct'")
        if not marker:
            raise AssertionError("producer fixture archive boundary changed")
        fixture_program += "    cases[case]={'package':str(package),'current':current,'archive':str(archive),'part_id':part_id}\n"
        fixture_program += "(output/'cases.json').write_bytes(enc(cases))\n"
        subprocess.run([sys.executable, "-I", "-B", "-c", fixture_program,
                        str(ROOT / "sources/misteross"), str(cls.fixture), cls.lane, "archives"], check=True)
        cls.cases = json.loads((cls.fixture / "cases.json").read_bytes())
        cls.package = Path(cls.cases["direct"]["package"])
        cls.package_id = cls.package.name
        cls.archive = cls.fixture / "core.fcore"
        identity = json.loads(subprocess.check_output(
            [sys.executable, "-I", "-B", "-c", _ARCHIVE,
             str(ROOT / "sources/misteross"), str(cls.package), str(cls.archive)], text=True))
        if identity != {"core_id": "fes.coleco", "package_id": cls.package_id}:
            raise AssertionError(identity)
        cls.revision = cls.cases["direct"]["current"]["revision"]
        helper = cls.fixture / "consumer.go"
        helper.write_text(_CONSUMER)
        cls.consumer = cls.fixture / "consumer"
        subprocess.run(["go", "build", "-o", str(cls.consumer), str(helper)],
                       cwd=ROOT / "sources/FogCast", check=True)

    @classmethod
    def tearDownClass(cls):
        cls.fixture_temp.cleanup()

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.candidate = self.root / "candidate"
        self.candidate.mkdir()
        self.tree = self.candidate / "core-video-parts"
        self.addCleanup(lambda: video._remove(self.tree))
        self.metadata = self.root / "metadata.toml"
        self.metadata.write_text('version = 1\nsource_id = "fes-first-party"\n'
                                 '[[cores]]\ncore_id = "fes.coleco"\nlabel = "ColecoVision"\n'
                                 'system = "coleco"\nstanding = "supported"\n')
        self.files, parts = {}, []
        for profile in video.PROFILES:
            archive = Path(self.cases[profile]["archive"]).read_bytes()
            part_id = self.cases[profile]["part_id"]
            relative = f"{self.package_id}/{part_id}.tar"
            self.files[relative] = archive
            parts.append(dict(profile=profile, part_id=part_id, archive_path=relative,
                              archive_sha256=sha(archive), archive_size=len(archive)))
        self.index = dict(version=1, packages=[dict(package_id=self.package_id, parts=parts)])
        self.write_inventory(self.index, self.files)
        shell = self.archive.read_bytes()
        (self.candidate / "core.fcore").write_bytes(shell)
        payload_sha = sha((self.package / "core.rbf").read_bytes())
        selection = ('format = 2\nkind = "core-package"\ncore_id = "fes.coleco"\n'
                     f'package_id = "{self.package_id}"\nmisteross_revision = "{self.revision}"\n'
                     f'mister_packages_revision = "{self.revision}"\npayload_sha256 = "{payload_sha}"\n').encode()
        selection_name = "fes-coleco.package-selection.toml"
        (self.candidate / selection_name).write_bytes(selection)
        provenance = dict(format=1, selected_repository="https://github.com/DeanoC/fes.git",
                          selected_revision=self.revision, selected_source_path="sources/misteross",
                          original_repository="https://github.com/DeanoC/fes.git",
                          original_revision=self.revision, original_source_path="sources/misteross",
                          functional_inputs_sha256="f" * 64, original_record_sha256="1" * 64,
                          selected_record_sha256="2" * 64, package_id=self.package_id,
                          core_rbf_sha256=payload_sha)
        encoded = video.canonical(provenance)
        sidecar_name = Path(selection_name).with_suffix(".provenance.json").name
        (self.candidate / sidecar_name).write_bytes(encoded)
        self.receipt = dict(format=2, core_id="fes.coleco", package_id=self.package_id,
                            archive=dict(path="core.fcore", sha256=sha(shell), size=len(shell)),
                            sources={name: self.revision for name in
                                     ("FogCast", "libmister-runtime", "misteross", "mister-packages")},
                            selection=dict(path=selection_name, sha256=sha(selection),
                                           manifest_sha256=sha((self.package / "manifest.toml").read_bytes()),
                                           payload_sha256=payload_sha),
                            source_selection=dict(path=sidecar_name, sha256=sha(encoded)))
        self.prepared = self.candidate / "prepared.json"
        self.save_receipt()

    def write_inventory(self, index, files):
        video._remove(self.tree)
        encoded = video.canonical(index)
        video._publish(self.tree, files | {"index.json": encoded})
        self.index_path = self.candidate / "fes-core-video-parts.json"
        if self.index_path.exists():
            self.index_path.chmod(0o644)
        self.index_path.write_bytes(encoded)
        self.index_path.chmod(0o444)

    def save_receipt(self):
        encoded = self.index_path.read_bytes()
        self.receipt["video_parts"] = dict(path=self.index_path.name, sha256=sha(encoded), size=len(encoded))
        self.prepared.write_bytes(video.canonical(self.receipt))

    def publish(self, output=None):
        return core_catalog.publish(ROOT, self.metadata, {"fes.coleco": self.prepared}, output or self.root / "out")

    def assert_no_publication(self):
        self.assertFalse((self.root / "out").exists())
        self.assertEqual(list(self.root.glob(".core-catalog-*")), [])

    def test_canonical_publication_preserves_exact_producer_archives_and_consumer_admission(self):
        provenance = {}
        core_dev_accept.candidate_arguments(self.prepared, provenance)
        self.assertEqual(provenance["video_parts"], self.index["packages"][0]["parts"])
        value = self.publish()
        unsigned = {key: data for key, data in value.items() if key != "catalog_sha256"}
        self.assertEqual(value["catalog_sha256"], sha(core_catalog.canonical(unsigned)))
        self.assertEqual((self.root / "out/catalog.json").read_bytes(), core_catalog.canonical(value) + b"\n")
        row = value["entries"][0]
        expected = [dict(part, archive_path="video-parts/" + part["archive_path"])
                    for part in self.index["packages"][0]["parts"]]
        self.assertEqual(row["video_parts"], expected)
        self.assertEqual((self.root / "out" / row["archive_path"]).read_bytes(), self.archive.read_bytes())
        for original, published in zip(self.index["packages"][0]["parts"], expected):
            self.assertEqual((self.root / "out" / published["archive_path"]).read_bytes(),
                             self.files[original["archive_path"]])
            self.assertEqual(original["archive_size"] % 10240, 0)  # Python producer record padding.
        consumed = json.loads(subprocess.check_output([str(self.consumer), str(self.root / "out/catalog.json")], text=True))
        self.assertEqual(consumed, dict(package_id=self.package_id, video_parts=expected))
        self.assertEqual(value, self.publish(self.root / "again"))

    def test_corrupt_second_companion_rejects_before_publication(self):
        second = self.tree / self.index["packages"][0]["parts"][1]["archive_path"]
        damaged = bytearray(second.read_bytes())
        damaged[-1] ^= 1
        second.chmod(0o644)
        second.write_bytes(damaged)
        second.chmod(0o444)
        with self.assertRaisesRegex(ValueError, "digest or size differs"):
            self.publish()
        self.assert_no_publication()

    def test_valid_closed_inventory_bound_to_another_shell_rejects(self):
        changed = copy.deepcopy(self.index)
        changed["packages"][0]["package_id"] = "a" * 64
        rebound = {}
        for part in changed["packages"][0]["parts"]:
            original = part["archive_path"]
            part["archive_path"] = "a" * 64 + "/" + Path(original).name
            rebound[part["archive_path"]] = self.files[original]
        self.write_inventory(changed, rebound)
        self.save_receipt()
        self.assertEqual(video.read_index(self.tree), changed)
        with self.assertRaisesRegex(ValueError, "differ from selected package"):
            self.publish()
        self.assert_no_publication()

    def test_second_companion_change_after_candidate_admission_rejects(self):
        snapshot = core_catalog.snapshot
        second = self.tree / self.index["packages"][0]["parts"][1]["archive_path"]
        def changed_snapshot(path, *args, **kwargs):
            if Path(path) == second:
                second.chmod(0o644)
                second.write_bytes(b"changed after receipt admission")
                second.chmod(0o444)
            return snapshot(path, *args, **kwargs)
        with patch.object(core_catalog, "snapshot", changed_snapshot), self.assertRaisesRegex(ValueError, "SHA-256 mismatch"):
            self.publish()
        self.assert_no_publication()

    def test_video_inventory_requires_source_bound_receipt(self):
        self.receipt["format"] = 1
        del self.receipt["source_selection"]
        self.save_receipt()
        with self.assertRaisesRegex(ValueError, "require a source-bound"):
            self.publish()
        self.assert_no_publication()

    def test_marked_factory_shell_requires_inventory_reference(self):
        self.save_receipt()
        del self.receipt["video_parts"]
        self.prepared.write_bytes(video.canonical(self.receipt))
        with self.assertRaisesRegex(ValueError, "video"):
            self.publish()
        self.assert_no_publication()

    def test_marked_factory_shell_requires_inventory_tree(self):
        video._remove(self.tree)
        with self.assertRaises((ValueError, OSError)):
            self.publish()
        self.assert_no_publication()


class RasterFactoryVideoPublicationTests(FactoryVideoPublicationTests):
    lane = "raster"


if __name__ == "__main__":
    unittest.main()
