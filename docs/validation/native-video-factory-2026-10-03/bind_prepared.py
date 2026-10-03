#!/usr/bin/env python3
"""Bind a completed independent proof to the immutable prepared publication."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tomllib

output = Path(__file__).resolve().parent
preparation = json.loads((output / "preparation.json").read_bytes())
revision = preparation["expected_source_revision"]
arguments = preparation["verification"]["argv"]
source = Path(arguments[arguments.index("--source") + 1])
snapshot = source.parent.parent
prepared = Path(preparation["prepared_inventory"]).parent.parent
sys.path.insert(0, str(snapshot))
from scripts import factory_video_parts

observed = {}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def read(path, *, sealed=False, limit=128 << 20):
    path = Path(path)
    data = factory_video_parts._read(path, sealed=sealed, limit=limit)
    record = {"path": str(path), "sha256": hashlib.sha256(data).hexdigest(), "size": len(data)}
    require(str(path) not in observed or observed[str(path)] == record,
            "file changed during prepared binding: " + str(path))
    observed[str(path)] = record
    return data


def info(path, **kwargs):
    read(path, **kwargs)
    return observed[str(path)]


def clean_source():
    actual = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=snapshot, text=True).strip()
    status = subprocess.check_output(["git", "status", "--porcelain"], cwd=snapshot, text=True).strip()
    require(actual == revision and not status, "binding reader is not from the clean selected source")


def match_proof(path, original, *, sealed=False):
    data = read(path, sealed=sealed)
    original_bytes = read(original["path"])
    require(observed[original["path"]] == original, "original proof file changed")
    require(data == original_bytes, "prepared artifact differs from original producer bytes: " + str(path))
    return info(path, sealed=sealed)


clean_source()
proof = json.loads(read(output / "build.json"))
verification = json.loads(read(output / "verification.json"))
require(verification["status"] == "passed" and verification["exit_code"] == 0,
        "independent proof did not pass")
require(verification["build_proof"] == info(output / "build.json"), "proof invocation binding changed")
require(proof["source"]["revision"] == revision, "proof source differs from preparation")
package_id = proof["shell"]["package_id"]
receipt = json.loads(read(prepared / "prepared.json"))
require(receipt["format"] == 2 and receipt["core_id"] == "fes.coleco"
        and receipt["package_id"] == package_id, "prepared package identity differs")
require(receipt["sources"] == {name: revision for name in
        ("FogCast", "libmister-runtime", "mister-packages", "misteross")},
        "prepared source selection differs")
prepared_archive = match_proof(prepared / "core.fcore", proof["shell"]["archive"])
require(receipt["archive"] == {"path": "core.fcore", **{
    key: prepared_archive[key] for key in ("sha256", "size")}}, "prepared archive receipt differs")

index_path = prepared / "core-video-parts/index.json"
index_bytes = read(index_path, sealed=True, limit=64 << 10)
require(hashlib.sha256(index_bytes).hexdigest() ==
        "2c5cacb765a1afd71837dbba0f6344473a3c35ac600325128b4a14f9fe82f66a",
        "prepared inventory differs from the root readiness identity")
index = factory_video_parts.read_index(index_path.parent)
require(read(index_path, sealed=True, limit=64 << 10) == index_bytes,
        "prepared inventory changed while its tree was validated")
publication = read(prepared / "fes-core-video-parts.json")
require(publication == index_bytes, "prepared publication and sealed inventory differ")
require(receipt["video_parts"] == {"path": "fes-core-video-parts.json", **{
    key: observed[str(index_path)][key] for key in ("sha256", "size")}},
    "prepared inventory receipt differs")
require(len(index["packages"]) == 1 and index["packages"][0]["package_id"] == package_id,
        "prepared inventory must contain exactly the proven package")
parts = []
for part in index["packages"][0]["parts"]:
    original = proof["parts"][part["profile"]]
    require(part["part_id"] == original["part_id"], "prepared profile selects another part")
    archive = match_proof(index_path.parent / part["archive_path"], original["archive"], sealed=True)
    require(archive["sha256"] == part["archive_sha256"] and archive["size"] == part["archive_size"],
            "prepared archive differs from inventory")
    parts.append({"profile": part["profile"], "part_id": part["part_id"], "archive": archive})

selection_path = prepared / "fes-coleco.package-selection.toml"
selection = tomllib.loads(read(selection_path).decode())
require(selection["package_id"] == package_id
        and selection["payload_sha256"] == proof["shell"]["payload"]["sha256"]
        and selection["misteross_revision"] == revision
        and selection["mister_packages_revision"] == revision, "prepared selection differs")
require(receipt["selection"]["sha256"] == observed[str(selection_path)]["sha256"],
        "prepared selection receipt differs")
provenance_path = prepared / "fes-coleco.package-selection.provenance.json"
provenance = json.loads(read(provenance_path))
require(provenance["package_id"] == package_id and provenance["original_revision"] == revision
        and provenance["selected_revision"] == revision
        and provenance["original_record_sha256"] == proof["shell"]["build_record"]["sha256"]
        and provenance["selected_record_sha256"] == proof["shell"]["build_record"]["sha256"],
        "prepared provenance differs from original source proof")
require(receipt["source_selection"]["sha256"] == observed[str(provenance_path)]["sha256"],
        "prepared provenance receipt differs")
info(Path(__file__).resolve())
info(Path(factory_video_parts.__file__).resolve())
for path in list(observed):
    read(path)
clean_source()
result = {
    "format": 1,
    "classification": "host-only exact-byte binding to prepared package and inventory; no image or hardware acceptance",
    "source_revision": revision,
    "package_id": package_id,
    "all_bytes_match": True,
    "build_proof": observed[str(output / "build.json")],
    "prepared_receipt": observed[str(prepared / "prepared.json")],
    "prepared_archive": prepared_archive,
    "video_inventory": observed[str(index_path)],
    "video_publication": observed[str(prepared / "fes-core-video-parts.json")],
    "parts": parts,
    "invocation": {"argv": sys.orig_argv, "cwd": str(Path.cwd())},
    "observed_files": observed,
}
with (output / "prepared-binding.json").open("x") as stream:
    stream.write(json.dumps(result, sort_keys=True, indent=2) + "\n")
print("Prepared package and both sealed companion archives match the original proven bytes.")
