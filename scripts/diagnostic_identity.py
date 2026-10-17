"""Shared active software identity checks for host API v1 diagnostics.

Source metadata is provenance only. The host inventory endpoint validates release
manifest content IDs; consumers pin that immutable ID and its provided contracts.
"""
import re

DIGEST = re.compile(r"[0-9a-f]{64}\Z")
INTERFACES = {"runtime-protocol": "2", "maintenance": "fes-kit-maintenance-v1",
              "software-admission": "fes-kit-software-v1"}
# Same ceiling the host already verified: targetclient.maxKitSoftwareResponse
# and the agent proxy maxSoftwareControlResponse. Each installed manifest may
# approach software.MaxManifestSize (1 MiB), and the kit returns every retained
# release, so this list is larger than an ordinary health or session body.
INVENTORY_RESPONSE_LIMIT = 32 << 20


def observe_health(value, target_id, bundle_id, base_id, *, require_ready=True):
    if not all(isinstance(v, str) and DIGEST.fullmatch(v) for v in (bundle_id, base_id)):
        raise ValueError("expected active bundle and base image must be SHA-256 identities")
    if not isinstance(value, dict):
        raise ValueError("host health is not an object")
    target = value.get("target")
    if not isinstance(target, dict) or target.get("reachable") is not True:
        raise ValueError("target is not reachable through the host")
    if require_ready and (value.get("ready") is not True or target.get("ready") is not True):
        raise ValueError("host or target is not ready")
    connection = target.get("connection")
    if not isinstance(connection, dict) or connection.get("target_id") != target_id:
        raise ValueError("target_id does not match expected target")
    if target.get("api_version") != "v1":
        raise ValueError("target API contract must be v1")
    software = target.get("software")
    if not isinstance(software, dict):
        raise ValueError("target health has no active software identity")
    if software.get("active_bundle") != bundle_id:
        raise ValueError("active bundle identity does not match expectation")
    if software.get("base_identity") != base_id:
        raise ValueError("base image identity does not match expectation")
    host = value.get("host")
    host = host if isinstance(host, dict) else {}
    artifacts = target.get("artifacts")
    artifacts = artifacts if isinstance(artifacts, dict) else {}
    return {"host_revision": host.get("revision"), "base_artifacts": artifacts}


def active_manifest(inventory, bundle_id):
    if not isinstance(inventory, list):
        raise ValueError("software release inventory is not an array")
    matches = [row for row in inventory if isinstance(row, dict) and row.get("id") == bundle_id]
    if len(matches) != 1 or not isinstance(matches[0].get("manifest"), dict):
        raise ValueError("active bundle manifest is missing or duplicated")
    manifest = matches[0]["manifest"]
    provided = manifest.get("interfaces")
    if not isinstance(provided, dict) or any(provided.get(k) != v for k, v in INTERFACES.items()):
        raise ValueError("active bundle provided interfaces are incompatible")
    return {"interfaces": dict(provided), "provenance": manifest.get("provenance", {})}


def expected_identity(args):
    return {"active_bundle": args.expected_bundle_id,
            "base_identity": args.expected_base_image_sha256,
            "host_api": "v1", "target_api": "v1"}
