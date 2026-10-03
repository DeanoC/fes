#!/usr/bin/env python3
"""Bind a verified corrected image to the unchanged, originally proven FPGA bytes."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tomllib

IMAGE_REVISION = "6ed1dad495b0c836b611e4e7b331c3a7d4801c63"
ARTIFACT_REVISION = "3ffe989fbba6857b74f149112d1965156face870"
output = Path(__file__).resolve().parent
root = output.parents[3]
snapshot = root / "out/work" / ("misteross-" + IMAGE_REVISION)
sys.path.insert(0, str(snapshot))
# core_catalog is also a CLI module with sibling absolute imports. Bind its
# actual serializer rather than the newline-bearing video-index serializer.
sys.path.insert(0, str(snapshot / "scripts"))
from scripts import bundle, core_catalog, factory_video_parts

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--expected-image", required=True, help="SHA-256 explicitly reported after parent build/verify")
parser.add_argument("--image-output", type=Path, default=root / "out/native-integration-dev")
parser.add_argument("--release-directory", type=Path)
parser.add_argument("--expected-release-version")
args = parser.parse_args()
if not factory_video_parts.HEX64.fullmatch(args.expected_image):
    parser.error("--expected-image must be a full lowercase SHA-256")
if bool(args.release_directory) != bool(args.expected_release_version):
    parser.error("release directory and expected version must be supplied together")
image_output = args.image_output.absolute()
observed = {}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def read(path, *, sealed=False):
    path = Path(path)
    data = factory_video_parts._read(path, sealed=sealed, limit=256 << 20)
    identity = {"path": str(path), "sha256": hashlib.sha256(data).hexdigest(), "size": len(data)}
    require(str(path) not in observed or observed[str(path)] == identity, "changed file: " + str(path))
    observed[str(path)] = identity
    return data


def info(path, **kwargs):
    read(path, **kwargs)
    return observed[str(path)]


def matches(path, expected, **kwargs):
    actual = info(path, **kwargs)
    require(all(actual[key] == expected[key] for key in ("sha256", "size")),
            "retained evidence changed: " + str(path))
    return actual


def clean_source():
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=snapshot, text=True).strip()
    status = subprocess.check_output(["git", "status", "--porcelain"], cwd=snapshot, text=True).strip()
    require(revision == IMAGE_REVISION and not status, "reader snapshot is not the clean selected image source")


clean_source()
source_binding_path = output / "source-cache-binding.json"
source_binding = json.loads(read(source_binding_path))
require(source_binding["image_source_revision"] == IMAGE_REVISION and
        source_binding["artifact_source_revision"] == ARTIFACT_REVISION and
        source_binding["all_cache_bytes_equal_original_proof"] and
        source_binding["source_closure"]["all_inputs_identical"], "source/cache preparation differs")
for path, identity in source_binding["observed_files"].items():
    matches(path, identity)
proof_path = Path(source_binding["original_build_proof"]["path"])
proof = json.loads(read(proof_path))
matches(proof_path, source_binding["original_build_proof"])
prepared_path = Path(source_binding["original_prepared_binding"]["path"])
prepared = json.loads(read(prepared_path))
matches(prepared_path, source_binding["original_prepared_binding"])
require(proof["source"]["revision"] == prepared["source_revision"] == ARTIFACT_REVISION,
        "original FPGA proof provenance differs")
require(prepared["all_bytes_match"] and prepared["build_proof"] == info(proof_path),
        "original prepared proof differs")
package_id = proof["shell"]["package_id"]
require(source_binding["package_id"] == package_id, "source/cache package differs")
image_path = image_output / "linux.img"
image_identity = info(image_path)
require(image_identity["sha256"] == args.expected_image and image_identity["size"] == 128 << 20,
        "image differs from the explicitly designated two-pass build")
receipt = json.loads(read(image_output / "image.json"))
inputs = json.loads(read(image_output / "inputs.json"))
require(receipt["fes_revision"] == IMAGE_REVISION and inputs["sources"] == {
    name: IMAGE_REVISION for name in ("FogCast", "misteross", "libmister-runtime", "mister-packages")},
    "corrected image does not select the expected source")
require(receipt["inputs"] == inputs["image_fingerprint"], "image input fingerprint differs")
repro = dict(line.split("=", 1) for line in read(image_output / "reproducibility.txt").decode().splitlines())
require(repro["run_1_sha256"] == repro["run_2_sha256"] == args.expected_image, "image build passes differ")
verification = json.loads(read(image_output / "verification.json"))
require(verification["image_sha256"] == args.expected_image and all(verification[key] == "pass" for key in
        ("two_pass_reproducibility", "structural", "qemu_packaging")), "image verification did not pass")
qemu = info(image_output / "qemu-smoke.log")
require(verification["qemu_log_sha256"] == qemu["sha256"], "QEMU evidence differs")
manifest_path = image_output / "manifest.tsv"
manifest = {}
for number, line in enumerate(read(manifest_path).decode().splitlines(), 1):
    path, kind, digest = line.split("\t")
    require(path not in manifest, "duplicate installed manifest path")
    manifest[path] = {"kind": kind, "sha256": digest, "line": number}

index_path = image_output / "core-video-parts/index.json"
index_bytes = read(index_path, sealed=True)
index = factory_video_parts.read_index(index_path.parent)
require(index_bytes == read(prepared["video_inventory"]["path"], sealed=True) and
        info(index_path)["sha256"] == prepared["video_inventory"]["sha256"],
        "corrected image inventory differs from the originally proven inventory")
require(read(image_output / "fes-core-video-parts.json") == index_bytes, "image inventories differ")
require(len(index["packages"]) == 1 and index["packages"][0]["package_id"] == package_id,
        "inventory selects another shell")

# The local catalog is reused as originally produced. It is a hash-bound
# publication, not a new catalog generation or a filesystem-readonly store.
publication_dir = output.parent / "publication"
publication_reuse_path = output.parent / "publication-reuse.json"
publication_reuse = json.loads(read(publication_reuse_path))
original_publication = proof_path.parent.parent / "publication"
require(publication_reuse["origin"] == str(original_publication) and
        publication_reuse["origin_artifact_source_commit"] == ARTIFACT_REVISION and
        publication_reuse["selected_executable_commit"] == IMAGE_REVISION,
        "local publication reuse provenance differs")
catalog_bytes = read(publication_dir / "catalog.json")
catalog = json.loads(catalog_bytes)
require(set(catalog) == {"version", "source_id", "entries", "catalog_sha256"} and
        catalog["version"] == 1 and len(catalog["entries"]) == 1,
        "local catalog shape differs")
unsigned_catalog = {key: value for key, value in catalog.items() if key != "catalog_sha256"}
require(catalog["catalog_sha256"] == hashlib.sha256(core_catalog.canonical(unsigned_catalog)).hexdigest() and
        catalog_bytes == core_catalog.canonical(catalog) + b"\n", "local catalog digest differs")
entry = catalog["entries"][0]
package_archive = proof["shell"]["archive"]
require(entry["core_id"] == "fes.coleco" and entry["package_id"] == package_id and
        entry["archive_path"] == f"packages/{package_id}.fcore" and
        entry["archive_sha256"] == package_archive["sha256"] and
        entry["archive_size"] == package_archive["size"], "catalog package differs from original proof")
expected_parts = [dict(part, archive_path="video-parts/" + part["archive_path"])
                  for part in index["packages"][0]["parts"]]
require(entry["video_parts"] == expected_parts, "catalog companions differ from proven image inventory")
publication_expected = {"catalog.json": None, entry["archive_path"]: package_archive}
for part in expected_parts:
    publication_expected[part["archive_path"]] = proof["parts"][part["profile"]]["archive"]
require(len(publication_reuse["files"]) == 4 and
        {row["path"] for row in publication_reuse["files"]} == set(publication_expected),
        "publication reuse receipt has unexpected files")
expected_tree = set(publication_expected) | {"packages", "video-parts", "video-parts/" + package_id}
for directory in (publication_dir, original_publication):
    factory_video_parts._plain(directory, directory=True)
    require({path.relative_to(directory).as_posix() for path in directory.rglob("*")} == expected_tree,
            "local publication tree has unexpected members")
    for relative in expected_tree - set(publication_expected):
        factory_video_parts._plain(directory / relative, directory=True)
publication_files = []
for row in publication_reuse["files"]:
    path = publication_dir / row["path"]
    original_path = original_publication / row["path"]
    data = read(path)
    require(data == read(original_path), "reused catalog publication differs from original bytes")
    require(info(path)["sha256"] == row["sha256"] and info(path)["size"] == row["size"],
            "publication reuse receipt hash differs")
    proven = publication_expected[row["path"]]
    if proven is not None:
        matches(proven["path"], proven)
        require(data == read(proven["path"]), "catalog archive differs from original FPGA proof")
    publication_files.append({"publication": info(path), "original_publication": info(original_path),
                              "proven_archive": proven, "full_bytes_equal": True})
publication_binding = {"receipt": info(publication_reuse_path), "directory": str(publication_dir),
    "catalog": info(publication_dir / "catalog.json"), "catalog_id": catalog["catalog_sha256"],
    "files": publication_files, "all_original_bytes_match": True,
    "classification": "Existing local catalog bytes reused; package and companions match the original FPGA proof."}

original_shell = Path(proof["discovered_artifacts"]["shell_directory"])
original_selection = Path(prepared["prepared_receipt"]["path"]).parent / "fes-coleco.package-selection.toml"
original_selection_bytes = read(original_selection)
matches(original_selection, prepared["observed_files"][str(original_selection)])
selection_expectation = tomllib.loads(original_selection_bytes.decode())
require(selection_expectation["misteross_revision"] == selection_expectation["mister_packages_revision"] == ARTIFACT_REVISION,
        "original selection provenance differs")
selection_expectation["mister_packages_revision"] = IMAGE_REVISION
selection_bytes = bundle._selection_bytes(selection_expectation)
require(read(image_output / "fes-coleco.package-selection.toml") == selection_bytes,
        "selection changed beyond its selected mister-packages revision")
targets = [
    (f"core-packages/{package_id}/core.rbf", f"usr/share/mister-runtime/core-packages/{package_id}/core.rbf", proof["shell"]["payload"]["path"]),
    (f"core-packages/{package_id}/manifest.toml", f"usr/share/mister-runtime/core-packages/{package_id}/manifest.toml", str(original_shell / "manifest.toml")),
    ("core-video-parts/index.json", "usr/share/mister-runtime/core-video-parts/index.json", prepared["video_inventory"]["path"]),
    ("fes-core-video-parts.json", "usr/share/mister-runtime/selections/fes-core-video-parts.json", prepared["video_publication"]["path"]),
    ("fes-coleco.package-selection.toml", "usr/share/mister-runtime/selections/fes-coleco.package.toml", None),
]
for part in index["packages"][0]["parts"]:
    original = proof["parts"][part["profile"]]
    require(part["part_id"] == original["part_id"], "image profile selects another part")
    targets.append(("core-video-parts/" + part["archive_path"],
                    "usr/share/mister-runtime/core-video-parts/" + part["archive_path"], original["archive"]["path"]))
require(len(targets) == 7, "unexpected installed target count")
for prefix in (f"usr/share/mister-runtime/core-packages/{package_id}/", "usr/share/mister-runtime/core-video-parts/"):
    require({name for name in manifest if name.startswith(prefix)} ==
            {installed for _, installed, _ in targets if installed.startswith(prefix)},
            "unexpected installed package/video members")

debugfs = Path("/usr/sbin/debugfs")
installed_files = []
for external, installed, original in targets:
    data = read(image_output / external)
    if original is not None:
        require(data == read(original), "external FPGA/inventory artifact changed: " + external)
        prior_identity = proof["observed_files"].get(original) or prepared["observed_files"].get(original)
        require(prior_identity is not None, "original file is absent from retained proof")
        matches(original, prior_identity)
        expected = {"basis": "unchanged original proven bytes", "original": info(original)}
    else:
        require(data == selection_bytes, "selected revision metadata differs")
        expected = {"basis": "canonical current selection metadata", "original": info(original_selection),
                    "changed_fields": {"mister_packages_revision": {"before": ARTIFACT_REVISION, "after": IMAGE_REVISION}}}
    digest = hashlib.sha256(data).hexdigest()
    require(receipt["files"][external] == digest and manifest[installed]["kind"] == "file" and
            manifest[installed]["sha256"] == digest, "receipt/installed manifest differs: " + installed)
    command = [str(debugfs), "-R", "cat /" + installed, str(image_path)]
    result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True)
    require(result.stdout == data, "installed ext4 bytes differ: " + installed)
    installed_files.append({"path": "/" + installed, "sha256": digest, "size": len(data),
        "manifest_line": manifest[installed]["line"], "external": info(image_output / external),
        "expected": expected, "read_command": command, "reader_stderr": result.stderr.decode(),
        "full_bytes_equal_expected": True})

selected = [item for item in inputs["fpga_packages"] if item["selection"]["core_id"] == "fes.coleco"]
require(len(selected) == 1, "image does not select one Coleco package")
selected = selected[0]
require(selected["selection"] == selection_expectation and
        selected["selection_sha256"] == hashlib.sha256(selection_bytes).hexdigest() and
        selected["core_rbf_sha256"] == proof["shell"]["payload"]["sha256"] and
        selected["manifest_sha256"] == info(original_shell / "manifest.toml")["sha256"],
        "image package input identity differs")
original_record = json.loads(read(source_binding["original_record"]["path"]))
expected_provenance = {
    "format": 1, "selected_repository": original_record["repository"], "selected_revision": IMAGE_REVISION,
    "selected_source_path": original_record["source_path"], "original_repository": original_record["repository"],
    "original_revision": ARTIFACT_REVISION, "original_source_path": original_record["source_path"],
    "functional_inputs_sha256": source_binding["functional_inputs_sha256"],
    "original_record_sha256": source_binding["original_record"]["sha256"],
    "selected_record_sha256": source_binding["selected_record_expectation"]["sha256"],
    "package_id": package_id, "core_rbf_sha256": proof["shell"]["payload"]["sha256"],
}
provenance_path = image_output / "fes-coleco.package-selection.provenance.json"
provenance = json.loads(read(provenance_path))
require(provenance == selected["source_selection"] == expected_provenance,
        "parent cache selection does not preserve original provenance or match selected functional inputs")
video_inputs = selected["video_parts"]
require(video_inputs["index"] == index and video_inputs["index_sha256"] == info(index_path)["sha256"] and
        video_inputs["index_size"] == len(index_bytes) and
        video_inputs["functional_inputs_sha256"] == source_binding["video_functional_inputs_sha256"] and
        video_inputs["shell"] == {"package_id": package_id, "build_record_sha256": source_binding["original_record"]["sha256"]},
        "video functional cache identity differs")
require(len(video_inputs["parts"]) == 2 and {part["profile"] for part in video_inputs["parts"]} == {"direct", "scanlines"},
        "video input companion set differs")
for part in video_inputs["parts"]:
    original = proof["parts"][part["profile"]]
    cram_path = str(Path(original["build_summary"]["path"]).parent / "cram-diff.json")
    require(part["part_id"] == original["part_id"] and part["revision"] == ARTIFACT_REVISION and
            part["recipe_sha256"] == original["manifest"]["recipe_sha256"] and
            part["archive_sha256"] == original["archive"]["sha256"] and part["archive_size"] == original["archive"]["size"] and
            part["build_summary_sha256"] == original["build_summary"]["sha256"] and
            part["cram_report_sha256"] == proof["observed_files"][cram_path]["sha256"],
            "video archive or original build provenance differs")
for name in ("linux.img", "inputs.json", "manifest.tsv", "reproducibility.txt"):
    require(receipt["files"][name] == info(image_output / name)["sha256"], "image receipt differs: " + name)

release_details = {"status": "not-bound", "reason": "No release directory was supplied; this receipt does not assert export status."}
if args.release_directory:
    release_dir = args.release_directory.absolute()
    factory_video_parts._plain(release_dir, directory=True, sealed=True)
    require({path.name for path in release_dir.iterdir()} == {"rootfs.img", "release.json", "evidence.json"},
            "release has unexpected members")
    release = json.loads(read(release_dir / "release.json", sealed=True))
    require(release["image_sha256"] == args.expected_image and release["image_size"] == image_identity["size"] and
            release["version"] == args.expected_release_version and
            all(release[field] == IMAGE_REVISION for field in ("fes_revision", "fogcast_revision", "runtime_revision")),
            "corrected release identity differs")
    require(release_dir.name == info(release_dir / "release.json")["sha256"] and
            release_dir.parent.name == args.expected_image, "release path differs from immutable identity")
    release_image = info(release_dir / "rootfs.img", sealed=True)
    require(release_image["sha256"] == args.expected_image and release_image["size"] == image_identity["size"],
            "release image differs")
    release_evidence = json.loads(read(release_dir / "evidence.json", sealed=True))
    require(release_evidence["manifest_sha256"] == info(release_dir / "release.json")["sha256"],
            "release manifest binding differs")
    for field, name in (("image_receipt_sha256", "image.json"), ("verification_sha256", "verification.json"),
                        ("qemu_log_sha256", "qemu-smoke.log")):
        require(release_evidence["provenance"][field] == info(image_output / name)["sha256"],
                "release evidence differs: " + field)
    release_details = {"status": "exported", "directory": str(release_dir), "manifest": release,
        "image": release_image, "evidence": info(release_dir / "evidence.json"),
        "recorded_hardware_status": release_evidence["hardware"]}

info(debugfs)
info(Path(__file__).resolve())
for path in list(observed):
    read(path)
clean_source()
result = {
    "format": 1,
    "classification": "independent host-only binding of corrected image to original unchanged FPGA proof; no new FPGA build",
    "image_source_revision": IMAGE_REVISION,
    "artifact_source_revision": ARTIFACT_REVISION,
    "package_id": package_id,
    "image": image_identity,
    "source_cache_binding": info(source_binding_path),
    "original_build_proof": info(proof_path),
    "original_prepared_binding": info(prepared_path),
    "image_receipt": info(image_output / "image.json"),
    "installed_manifest": info(manifest_path),
    "video_inventory": info(index_path),
    "local_catalog_publication": publication_binding,
    "selected_package_provenance": {"receipt": info(provenance_path), "result": provenance},
    "verification": {"receipt": info(image_output / "verification.json"), "result": verification},
    "release": release_details,
    "installed_files": installed_files,
    "all_fpga_inventory_bytes_match_original_proof": True,
    "selection_metadata_matches_current_source": True,
    "hardware": {"classification": "not established by this host binding", "target_access": False},
    "limitations": ["Six listed FPGA/inventory files were read directly from ext4 and equal original proven bytes; the seventh selection file changes only the selected mister-packages revision.",
                    "Whole-image structural and QEMU results are bound from the parent verification receipt.",
                    "The four-case 3ffe composition proof is retained unchanged, not rerun or relabelled as a 6ed FPGA build. SGM remains a separately imported optional part.",
                    "This receipt does not establish physical video/audio, library lifecycle, or image hardware acceptance."],
    "invocation": {"argv": sys.orig_argv, "cwd": str(Path.cwd())},
    "observed_files": observed,
}
with (output / "image-binding.json").open("x") as stream:
    stream.write(json.dumps(result, sort_keys=True, indent=2) + "\n")
print("Corrected image binds original FPGA proof: six unchanged installed files, one canonical updated selection; release: " + release_details["status"])
