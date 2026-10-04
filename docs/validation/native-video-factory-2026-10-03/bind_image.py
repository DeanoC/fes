#!/usr/bin/env python3
"""Read-only binding of the verified local image to the independent FPGA proof."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tomllib

output = Path(__file__).resolve().parent
root = Path.cwd()
image_output = root / "out/native-integration-dev"
expected_image = "4c2de9f3edb28d71fd920eee66157b94b56d9b2dd9cf7adfea92aabd1c8c2628"
expected_version = "0.2.0-dev.native-video-3ffe989f"
preparation = json.loads((output / "preparation.json").read_bytes())
revision = preparation["expected_source_revision"]
argv = preparation["verification"]["argv"]
source = Path(argv[argv.index("--source") + 1])
snapshot = source.parent.parent
sys.path.insert(0, str(snapshot))
from scripts import factory_video_parts

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


def clean_source():
    actual = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=snapshot, text=True).strip()
    status = subprocess.check_output(["git", "status", "--porcelain"], cwd=snapshot, text=True).strip()
    require(actual == revision and not status, "reader source is not the clean selected revision")


clean_source()
proof = json.loads(read(output / "build.json"))
binding = json.loads(read(output / "prepared-binding.json"))
require(proof["source"]["revision"] == binding["source_revision"] == revision, "proof source differs")
require(binding["all_bytes_match"] and binding["build_proof"] == info(output / "build.json"),
        "prepared proof binding differs")
package_id = proof["shell"]["package_id"]
image = image_output / "linux.img"
image_identity = info(image)
require(image_identity["sha256"] == expected_image and image_identity["size"] == 128 << 20,
        "local image differs from the designated two-pass build")
receipt = json.loads(read(image_output / "image.json"))
inputs = json.loads(read(image_output / "inputs.json"))
require(receipt["fes_revision"] == revision and inputs["sources"] == {
    name: revision for name in ("FogCast", "misteross", "libmister-runtime", "mister-packages")},
    "image source differs from the proven revision")
require(receipt["inputs"] == inputs["image_fingerprint"], "image input fingerprint differs")
manifest_path = image_output / "manifest.tsv"
manifest = {}
for line_number, line in enumerate(read(manifest_path).decode().splitlines(), 1):
    path, kind, digest = line.split("\t")
    require(path not in manifest, "duplicate installed-manifest path")
    manifest[path] = {"kind": kind, "sha256": digest, "line": line_number}
repro = dict(line.split("=", 1) for line in read(image_output / "reproducibility.txt").decode().splitlines())
require(repro["run_1_sha256"] == repro["run_2_sha256"] == expected_image, "build passes differ")
verification = json.loads(read(image_output / "verification.json"))
require(verification["image_sha256"] == expected_image and all(verification[key] == "pass" for key in
    ("two_pass_reproducibility", "structural", "qemu_packaging")), "image verification did not pass")
qemu = info(image_output / "qemu-smoke.log")
require(verification["qemu_log_sha256"] == qemu["sha256"], "QEMU evidence differs")

index_path = image_output / "core-video-parts/index.json"
index_bytes = read(index_path, sealed=True)
index = factory_video_parts.read_index(index_path.parent)
require(index_bytes == read(binding["video_inventory"]["path"], sealed=True)
        and info(index_path)["sha256"] == binding["video_inventory"]["sha256"],
        "image inventory differs from the proven prepared inventory")
require(read(image_output / "fes-core-video-parts.json") == index_bytes, "image inventories differ")
require(len(index["packages"]) == 1 and index["packages"][0]["package_id"] == package_id,
        "image inventory selects a different package")
original_manifest = Path(proof["discovered_artifacts"]["shell_directory"]) / "manifest.toml"
original_selection = Path(binding["prepared_receipt"]["path"]).parent / "fes-coleco.package-selection.toml"
targets = [
    (f"core-packages/{package_id}/core.rbf", f"usr/share/mister-runtime/core-packages/{package_id}/core.rbf", proof["shell"]["payload"]["path"]),
    (f"core-packages/{package_id}/manifest.toml", f"usr/share/mister-runtime/core-packages/{package_id}/manifest.toml", str(original_manifest)),
    ("core-video-parts/index.json", "usr/share/mister-runtime/core-video-parts/index.json", binding["video_inventory"]["path"]),
    ("fes-core-video-parts.json", "usr/share/mister-runtime/selections/fes-core-video-parts.json", binding["video_publication"]["path"]),
    ("fes-coleco.package-selection.toml", "usr/share/mister-runtime/selections/fes-coleco.package.toml", str(original_selection)),
]
for part in index["packages"][0]["parts"]:
    original = proof["parts"][part["profile"]]
    require(part["part_id"] == original["part_id"], "image profile selects another part")
    targets.append(("core-video-parts/" + part["archive_path"],
                    "usr/share/mister-runtime/core-video-parts/" + part["archive_path"],
                    original["archive"]["path"]))
for prefix in (f"usr/share/mister-runtime/core-packages/{package_id}/",
               "usr/share/mister-runtime/core-video-parts/"):
    require({name for name in manifest if name.startswith(prefix)} ==
            {installed for _, installed, _ in targets if installed.startswith(prefix)},
            "installed manifest has an unexpected member in the selected package/part tree")

debugfs = Path("/usr/sbin/debugfs")
installed_files = []
for external, installed, original in targets:
    data = read(image_output / external)
    require(data == read(original), "external artifact differs from proven bytes: " + external)
    prior_identity = proof["observed_files"].get(original) or binding["observed_files"].get(original)
    require(prior_identity is not None and all(observed[original][key] == prior_identity[key]
            for key in ("sha256", "size")), "original artifact differs from retained proof: " + original)
    digest = hashlib.sha256(data).hexdigest()
    require(receipt["files"][external] == digest, "image receipt hash differs: " + external)
    require(manifest[installed]["kind"] == "file" and manifest[installed]["sha256"] == digest,
            "installed manifest hash differs: " + installed)
    command = [str(debugfs), "-R", "cat /" + installed, str(image)]
    result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True)
    require(result.stdout == data, "installed ext4 bytes differ: " + installed)
    installed_files.append({"path": "/" + installed, "sha256": digest, "size": len(data),
        "manifest_line": manifest[installed]["line"], "external": info(image_output / external),
        "original": info(original), "read_command": command, "reader_stderr": result.stderr.decode(),
        "full_bytes_equal": True})

selected = [package for package in inputs["fpga_packages"] if package["selection"]["core_id"] == "fes.coleco"]
require(len(selected) == 1, "image inputs do not select exactly one Coleco package")
selected = selected[0]
require(selected["selection"] == tomllib.loads(read(image_output / "fes-coleco.package-selection.toml").decode())
        and selected["selection"]["package_id"] == package_id
        and selected["core_rbf_sha256"] == proof["shell"]["payload"]["sha256"]
        and selected["manifest_sha256"] == info(original_manifest)["sha256"], "image package inputs differ")
provenance = json.loads(read(image_output / "fes-coleco.package-selection.provenance.json"))
require(provenance == selected["source_selection"]
        and provenance["original_revision"] == provenance["selected_revision"] == revision
        and provenance["original_record_sha256"] == provenance["selected_record_sha256"] == proof["shell"]["build_record"]["sha256"],
        "image source provenance differs from original proven source")
require(selected["video_parts"]["index"] == index
        and selected["video_parts"]["index_sha256"] == info(index_path)["sha256"], "image video input index differs")
for part in selected["video_parts"]["parts"]:
    original = proof["parts"][part["profile"]]
    require(part["part_id"] == original["part_id"] and part["revision"] == revision
            and part["recipe_sha256"] == original["manifest"]["recipe_sha256"]
            and part["archive_sha256"] == original["archive"]["sha256"]
            and part["build_summary_sha256"] == original["build_summary"]["sha256"],
            "image video input provenance differs")
for name in ("linux.img", "inputs.json", "manifest.tsv", "reproducibility.txt"):
    require(receipt["files"][name] == info(image_output / name)["sha256"], "image receipt differs: " + name)

releases = []
for path in (image_output / "appliance/releases" / expected_image).glob("*/release.json"):
    candidate = json.loads(read(path, sealed=True))
    if candidate.get("version") == expected_version:
        releases.append((path.parent, candidate))
require(len(releases) <= 1, "the designated immutable release is ambiguous")
if releases:
    release_dir, release = releases[0]
    factory_video_parts._plain(release_dir, directory=True, sealed=True)
    require({path.name for path in release_dir.iterdir()} == {"rootfs.img", "release.json", "evidence.json"},
            "immutable release has unexpected members")
    require(release["image_sha256"] == expected_image and release["image_size"] == image_identity["size"]
            and all(release[field] == revision for field in ("fes_revision", "fogcast_revision", "runtime_revision")),
            "release identity differs")
    release_image = info(release_dir / "rootfs.img", sealed=True)
    require(release_image["sha256"] == expected_image and release_image["size"] == image_identity["size"],
            "release image differs from local image")
    release_evidence = json.loads(read(release_dir / "evidence.json", sealed=True))
    require(release_evidence["hardware"] == "not-run"
            and release_evidence["manifest_sha256"] == info(release_dir / "release.json")["sha256"],
            "release evidence differs")
    for field, filename in (("image_receipt_sha256", "image.json"),
                            ("verification_sha256", "verification.json"), ("qemu_log_sha256", "qemu-smoke.log")):
        require(release_evidence["provenance"][field] == info(image_output / filename)["sha256"],
                "release provenance receipt differs: " + field)
    release_details = {"status": "exported", "directory": str(release_dir), "manifest": release,
                       "image": release_image, "evidence": observed[str(release_dir / "evidence.json")]}
else:
    failure_path = output.parent / "release.log"
    failure = read(failure_path).decode()
    require(expected_version in failure and "locked boot payload cache is absent" in failure,
            "release is pending without a completed failure receipt")
    release_details = {"status": "not-exported", "version": expected_version,
                       "reason": "locked boot payload cache is absent",
                       "failure_log": observed[str(failure_path)]}
info(debugfs)
info(Path(__file__).resolve())
for path in list(observed):
    read(path)
clean_source()
result = {
    "format": 1,
    "classification": "independent host-only binding of verified local image to proven FPGA artifacts; release status recorded separately",
    "source_revision": revision,
    "package_id": package_id,
    "image": image_identity,
    "build_proof": observed[str(output / "build.json")],
    "prepared_binding": observed[str(output / "prepared-binding.json")],
    "image_receipt": observed[str(image_output / "image.json")],
    "installed_manifest": observed[str(manifest_path)],
    "video_inventory": observed[str(index_path)],
    "verification": {"receipt": observed[str(image_output / "verification.json")], "result": verification},
    "release": release_details,
    "installed_files": installed_files,
    "all_proven_bytes_match": True,
    "hardware": {"status": "not-run", "pending": True, "target_access": False,
                 "reason": "Kit 2 remains assigned to the active rb429-hil task; this phase is a verified local-image handoff."},
    "limitations": ["Only the seven listed installed files were independently read with debugfs; whole-image structural and QEMU results are bound from the parent verification receipt.",
                    "This image binding does not establish hardware video/audio behavior or make SGM a factory-installed companion."],
    "invocation": {"argv": sys.orig_argv, "cwd": str(root)},
    "observed_files": observed,
}
with (output / "image-binding.json").open("x") as stream:
    stream.write(json.dumps(result, sort_keys=True, indent=2) + "\n")
print("Verified local image and seven installed files match the proven artifacts; release status: "
      + release_details["status"] + ".")
