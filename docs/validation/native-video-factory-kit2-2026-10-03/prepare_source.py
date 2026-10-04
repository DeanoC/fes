#!/usr/bin/env python3
"""Bind unchanged FPGA inputs/cache bytes to a later image source; no builds."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys

ARTIFACT_REVISION = "3ffe989fbba6857b74f149112d1965156face870"
IMAGE_REVISION = "6ed1dad495b0c836b611e4e7b331c3a7d4801c63"
PROOF_SHA256 = "135f4445f1d2fa122c004bb1fcae4e3c692a9d5697a9bb0f92236796afe2dec0"
PREPARED_SHA256 = "5049e2f50269079284c365b79996e733e287ad8d6b69a44b0e88f3be5bf13724"
PRIOR_IMAGE_BINDING_SHA256 = "c82186d8afb1658da3e1006bb1ccb523b1483071f21e532c0254dd1ff0e0f45a"
output = Path(__file__).resolve().parent
root = output.parents[3]
snapshot = root / "out/work" / ("misteross-" + IMAGE_REVISION)
source = snapshot / "sources/misteross"
prior = root / "out/native-video-factory/candidate-3ffe989f/build-proof"
sys.path.insert(0, str(snapshot))
from scripts import artifact_cache, factory_video_parts

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--cache-root", type=Path, required=True,
                    help="Actual parent shared cache, explicit because snapshots have their own Git roots")
args = parser.parse_args()
if not args.cache_root.is_absolute():
    parser.error("--cache-root must be absolute")

observed = {}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def read(path, *, sealed=False):
    path = Path(path)
    data = factory_video_parts._read(path, sealed=sealed, limit=256 << 20)
    record = {"path": str(path), "sha256": hashlib.sha256(data).hexdigest(), "size": len(data)}
    require(str(path) not in observed or observed[str(path)] == record, "changed file: " + str(path))
    observed[str(path)] = record
    return data


def info(path, **kwargs):
    read(path, **kwargs)
    return observed[str(path)]


def git(*args, cwd=snapshot):
    return subprocess.check_output(["git", *args], cwd=cwd, text=True).strip()


def clean_source():
    require(git("rev-parse", "HEAD") == IMAGE_REVISION and not git("status", "--porcelain"),
            "selected image snapshot is not the clean expected revision")


clean_source()
proof = json.loads(read(prior / "build.json"))
prepared = json.loads(read(prior / "prepared-binding.json"))
prior_image = json.loads(read(prior / "image-binding.json"))
for path, digest in ((prior / "build.json", PROOF_SHA256),
                     (prior / "prepared-binding.json", PREPARED_SHA256),
                     (prior / "image-binding.json", PRIOR_IMAGE_BINDING_SHA256)):
    require(info(path)["sha256"] == digest, "original proof receipt differs")
require(proof["source"]["revision"] == prepared["source_revision"] == ARTIFACT_REVISION,
        "original artifact provenance differs")
require(prior_image["source_revision"] == ARTIFACT_REVISION and prior_image["all_proven_bytes_match"],
        "original image binding differs")
package_id = proof["shell"]["package_id"]
record_path = Path(proof["shell"]["build_record"]["path"])
record = read(record_path)
require(info(record_path) == proof["shell"]["build_record"], "original build record changed")
original = json.loads(record)
shell_summary_path = Path(proof["shell"]["build_summary"]["path"])
require(info(shell_summary_path) == proof["shell"]["build_summary"], "original shell summary changed")
part_summaries = []
for name in ("direct", "scanlines"):
    part = proof["parts"][name]
    path = Path(part["build_summary"]["path"])
    require(info(path) == part["build_summary"], "original part summary changed")
    part_summaries.append(path)

# Only Git/source readers and canonical record construction execute here. The
# original authenticated execution inputs are reused as an expectation; no
# compiler is invoked and no new FPGA build or provenance is asserted.
program = r'''
import hashlib,json,sys
from pathlib import Path
source=Path(sys.argv[1]); sys.path.insert(0,str(source))
from scripts import build_fes_coleco_socket_v2 as shell, build_video_part as video
from scripts import native_video_parts
from scripts.export_core_package import POLICY, source_input_closure, verify_record_source_at_revision
from scripts.functional_execution import source_roots_for_inputs, execution_digest
record=Path(sys.argv[2]).read_bytes()
old=verify_record_source_at_revision(source,record)
summary=json.loads(Path(sys.argv[3]).read_bytes())
repository,revision=shell._require_clean_source(source,native_video=True)
assert revision==sys.argv[6]
assert execution_digest(summary['execution'])==old['parameters']['execution_sha256']
selected=shell.create_build_record(source,repository,revision,old['tools'],summary['execution'],native_video=True)
fields=json.loads(selected)
assert fields['source_inputs']==old['source_inputs']
roots=source_roots_for_inputs(video.NATIVE_INPUTS)
closure=source_input_closure(source,roots,policy=POLICY)
parts=[json.loads(Path(path).read_bytes())['recipe'] for path in sys.argv[4:6]]
for recipe in parts:
    assert recipe['inputs']==closure and recipe['source_roots']==roots
    assert recipe['source_closure_policy']==POLICY and recipe['map']==native_video_parts.MAP
current={'inputs':closure,'source_roots':roots,'source_closure_policy':POLICY,
         'tools':parts[0]['tools'],'execution':parts[0]['execution'],'map':native_video_parts.MAP}
assert all(recipe['tools']==current['tools'] and recipe['execution']==current['execution'] for recipe in parts)
print(json.dumps({'selected_record':selected.decode(),'video_current':current,'closure_count':len(closure)}))
'''
command = [sys.executable, "-I", "-B", "-c", program, str(source), str(record_path),
           str(shell_summary_path), *map(str, part_summaries), IMAGE_REVISION]
derived = json.loads(subprocess.check_output(command, cwd=source))
selected_record = derived["selected_record"].encode()
selected_fields = json.loads(selected_record)
functional_key = artifact_cache.functional_key(record)
require(functional_key == artifact_cache.functional_key(selected_record), "functional shell inputs changed")
require(selected_fields["revision"] == IMAGE_REVISION and original["revision"] == ARTIFACT_REVISION,
        "source revisions were conflated")
for name in set(original) | set(selected_fields):
    if name != "revision":
        require(original.get(name) == selected_fields.get(name), "unexpected selected-record change: " + name)
video_key = hashlib.sha256(b"fes-factory-video-parts-v1\0" + factory_video_parts.canonical(
    {"package_id": package_id, "current": derived["video_current"]})).hexdigest()

cache_root = args.cache_root
cache_package_entry = artifact_cache.store_for(cache_root / "core-packages", record) / package_id
cache_package = cache_package_entry / package_id
factory_video_parts._plain(cache_package_entry, directory=True, sealed=True)
require({p.name for p in cache_package_entry.iterdir()} == {package_id, "build-inputs.json"},
        "unexpected functional package cache members")
require(read(cache_package_entry / "build-inputs.json", sealed=True) == record,
        "functional cache rewrote original source provenance")
cache_comparisons = []


def equal_proven(cache_path, original_path):
    original_path = Path(original_path)
    require(read(cache_path, sealed=True) == read(original_path), "cached artifact differs from proof: " + str(cache_path))
    require(all(info(original_path)[key] == proof["observed_files"][str(original_path)][key]
                for key in ("sha256", "size")),
            "original proof input changed: " + str(original_path))
    cache_comparisons.append({"cached": info(cache_path), "original": info(original_path), "full_bytes_equal": True})


shell = Path(proof["discovered_artifacts"]["shell_directory"])
factory_video_parts._closed(cache_package, ("manifest.toml", "core.rbf"), sealed=True)
for name in ("manifest.toml", "core.rbf"):
    equal_proven(cache_package / name, shell / name)
cache_shell = cache_root / "core-video-shells" / package_id
factory_video_parts._closed(cache_shell, factory_video_parts.SHELL_MEMBERS, sealed=True)
for name in factory_video_parts.SHELL_MEMBERS:
    equal_proven(cache_shell / name, shell / name)
cache_parts = cache_root / "core-video-parts" / video_key
for profile in ("direct", "scanlines"):
    part = proof["parts"][profile]
    slot = cache_parts / profile
    members = (*factory_video_parts.PART_MEMBERS, "cart-synth.json", "archive.tar")
    factory_video_parts._closed(slot, members, sealed=True)
    directory = Path(part["build_summary"]["path"]).parent
    for name in members:
        original_path = part["archive"]["path"] if name == "archive.tar" else directory / name
        equal_proven(slot / name, original_path)

subtrees = {}
paths = ("sources/FogCast", "sources/libmister-runtime", "sources/mister-packages", "sources/misteross",
         "sources/misteross/cores/fes-coleco", "sources/misteross/cores/fes-common",
         "sources/misteross/scripts", "sources/misteross/toolchains", "config", "scripts", "image")
for path in paths:
    previous = git("rev-parse", ARTIFACT_REVISION + ":" + path)
    current = git("rev-parse", IMAGE_REVISION + ":" + path)
    subtrees[path] = {"artifact_source_tree": previous, "image_source_tree": current, "unchanged": previous == current}
require(all(subtrees[path]["unchanged"] for path in paths if path not in
            ("sources/libmister-runtime", "sources/misteross")), "unexpected changed functional subtree")
require(git("diff", "--name-only", ARTIFACT_REVISION, IMAGE_REVISION, "--", "sources/misteross") ==
        "sources/misteross/docs/cores.md", "unexpected producer source changes")
require(not subtrees["sources/libmister-runtime"]["unchanged"], "corrected runtime subtree is absent")
info(Path(__file__).resolve())
for path in list(observed):
    read(path)
clean_source()
receipt = {
    "format": 1,
    "classification": "source and immutable-cache preparation for corrected image; no new FPGA build and no image verification yet",
    "image_source_revision": IMAGE_REVISION,
    "artifact_source_revision": ARTIFACT_REVISION,
    "selected_snapshot": str(snapshot),
    "package_id": package_id,
    "original_build_proof": info(prior / "build.json"),
    "original_prepared_binding": info(prior / "prepared-binding.json"),
    "original_image_binding": info(prior / "image-binding.json"),
    "original_record": info(record_path),
    "selected_record_expectation": {"sha256": hashlib.sha256(selected_record).hexdigest(), "size": len(selected_record),
        "method": "Selected producer canonical record from unchanged source closure and original authenticated tools/execution; expectation must match the parent selected_record_sha256. Original records are never rewritten."},
    "functional_inputs_sha256": functional_key,
    "video_functional_inputs_sha256": video_key,
    "source_closure": {"shell_input_count": len(original["source_inputs"]), "video_input_count": derived["closure_count"],
        "all_inputs_identical": True, "roots": original["source_roots"], "changed_record_fields": ["revision"]},
    "subtrees": subtrees,
    "changed_component_paths": git("diff", "--name-only", ARTIFACT_REVISION, IMAGE_REVISION, "--", "sources", "config", "scripts", "image").splitlines(),
    "cache_root": str(cache_root),
    "cache_comparisons": cache_comparisons,
    "all_cache_bytes_equal_original_proof": True,
    "image_verification": "pending explicit root readiness",
    "hardware": "not accessed by this preparation",
    "invocation": {"argv": sys.orig_argv, "cwd": str(Path.cwd())},
    "observed_files": observed,
}
with (output / "source-cache-binding.json").open("x") as stream:
    stream.write(json.dumps(receipt, sort_keys=True, indent=2) + "\n")
print(json.dumps({"shell_functional_key": functional_key, "video_functional_key": video_key,
    "closure_files": derived["closure_count"], "equal_cache_members": len(cache_comparisons),
    "selected_record_expectation": receipt["selected_record_expectation"]["sha256"]}))
