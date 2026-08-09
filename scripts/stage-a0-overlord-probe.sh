#!/bin/sh
set -eu

usage() {
    printf '%s\n' 'usage: stage-a0-overlord-probe.sh --overlord DIR --resources DIR --lock FILE --output FILE' >&2
    exit 2
}

overlord=
resources=
lock=
output=
while [ "$#" -gt 0 ]; do
    [ "$#" -ge 2 ] || usage
    key=$1
    value=$2
    shift 2
    [ -n "$value" ] || usage
    case "$key" in
        --overlord)
            [ -z "$overlord" ] || usage
            overlord=$value
            ;;
        --resources)
            [ -z "$resources" ] || usage
            resources=$value
            ;;
        --lock)
            [ -z "$lock" ] || usage
            lock=$value
            ;;
        --output)
            [ -z "$output" ] || usage
            output=$value
            ;;
        *)
            usage
            ;;
    esac
done
[ -n "$overlord" ] && [ -n "$resources" ] && [ -n "$lock" ] && [ -n "$output" ] || usage

exec python3 - "$overlord" "$resources" "$lock" "$output" <<'PY'
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys

overlord_arg, resources_arg, lock_arg, output_arg = sys.argv[1:]

def input_error(message):
    print(message, file=sys.stderr)
    raise SystemExit(2)

def clean_absolute(value, label):
    path = Path(value)
    if not path.is_absolute() or path != Path(os.path.normpath(value)):
        input_error(f"{label} must be a clean absolute path")
    try:
        info = path.lstat()
    except OSError:
        input_error(f"{label} is unavailable")
    if not stat.S_ISDIR(info.st_mode) or stat.S_ISLNK(info.st_mode):
        input_error(f"{label} must be a non-symlink directory")
    return path

overlord = clean_absolute(overlord_arg, "overlord checkout")
resources = clean_absolute(resources_arg, "resource checkout")
lock = Path(lock_arg)
if not lock.is_absolute() or lock != Path(os.path.normpath(lock_arg)):
    input_error("lock must be a clean absolute path")
try:
    lock_info = lock.lstat()
except OSError:
    input_error("lock is unavailable")
if not stat.S_ISREG(lock_info.st_mode) or stat.S_ISLNK(lock_info.st_mode):
    input_error("lock must be a non-symlink regular file")
output = Path(output_arg)
if not output.is_absolute() or output != Path(os.path.normpath(output_arg)):
    input_error("output must be a clean absolute path")
try:
    output.lstat()
except FileNotFoundError:
    pass
else:
    input_error("output already exists")

git_env = dict(os.environ)
git_env["GIT_CONFIG_NOSYSTEM"] = "1"

def git_value(root, *args):
    try:
        value = subprocess.check_output(
            ["git", "-C", str(root), *args],
            env=git_env,
            stderr=subprocess.DEVNULL,
            text=True,
        )
    except (OSError, subprocess.CalledProcessError):
        input_error("checkout identity cannot be observed")
    return value.rstrip("\n")

def lock_identity(section):
    try:
        text = lock.read_text(encoding="utf-8")
    except OSError:
        input_error("lock cannot be read")
    match = re.search(r"(?ms)^\[" + re.escape(section) + r"\]\s*\n(.*?)(?=^\[|\Z)", text)
    if not match:
        input_error("lock section is missing: " + section)
    values = {}
    for key in ("commit", "tree"):
        value = re.search(r"(?m)^" + key + r"\s*=\s*\"([0-9a-f]{40})\"\s*$", match.group(1))
        if not value:
            input_error("lock identity is incomplete: " + section)
        values[key] = value.group(1)
    return values

expected_overlord = lock_identity("overlord")
expected_resources = lock_identity("resources")

def checkout_identity(root, repository_id, expected):
    if git_value(root, "rev-parse", "--is-inside-work-tree") != "true":
        input_error("checkout identity cannot be observed")
    if git_value(root, "status", "--porcelain", "--untracked-files=all"):
        input_error("checkout is dirty")
    identity = {
        "repository_id": repository_id,
        "commit": git_value(root, "rev-parse", "HEAD"),
        "tree": git_value(root, "rev-parse", "HEAD^{tree}"),
    }
    if identity["commit"] != expected["commit"] or identity["tree"] != expected["tree"]:
        input_error("checkout identity differs from pinned lock")
    return identity

def logical_files(root):
    raw = git_value(root, "ls-tree", "-r", "--name-only", "HEAD")
    return [line for line in raw.splitlines() if line and ".git" not in Path(line).parts]

def tracked_size(root, name):
    try:
        return int(git_value(root, "cat-file", "-s", "HEAD:" + name))
    except (ValueError, SystemExit):
        return 0

def first_match(root, files, expected_paths, markers):
    available = set(files)
    for name in expected_paths:
        if name not in available or tracked_size(root, name) <= 0:
            continue
        try:
            content = git_value(root, "show", "HEAD:" + name).lower()
        except SystemExit:
            continue
        if all(marker in content for marker in markers):
            return name
    return ""

resource_files = logical_files(resources)
capabilities = []

def capability(identifier, path):
    if path:
        capabilities.append({"id": identifier, "status": "present", "path": path})
    else:
        capabilities.append({"id": identifier, "status": "missing", "path": ""})

board = first_match(resources, resource_files, ["boards/de10_nano.yaml"], ["board", "de10_nano"])
soc = first_match(resources, resource_files, ["socs/cyclone_v.yaml"], ["soc", "cyclone_v"])
registers = first_match(resources, resource_files, ["registers/cyclone_v.yaml"], ["register"])
toolchain = first_match(resources, resource_files, ["toolchains/arm-none-linux-gnueabihf.yaml"], ["target", "arm-none-linux-gnueabihf"])
software = first_match(resources, resource_files, ["software/main_mister.yaml"], ["program", "main_mister"])

capability("board-de10-nano", board)
capability("soc-cyclone-v", soc)
capability("registers-cyclone-v", registers)
capability("toolchain-arm-none-linux-gnueabihf", toolchain)
capability("software-main-mister", software)

blocker_by_id = {
    "board-de10-nano": "OVERLORD_BOARD_DE10_NANO_MISSING",
    "soc-cyclone-v": "OVERLORD_SOC_CYCLONE_V_MISSING",
    "registers-cyclone-v": "OVERLORD_REGISTERS_CYCLONE_V_MISSING",
    "toolchain-arm-none-linux-gnueabihf": "OVERLORD_TOOLCHAIN_ARM_NONE_LINUX_GNUEABIHF_MISSING",
    "software-main-mister": "OVERLORD_SOFTWARE_MAIN_MISTER_MISSING",
}
blockers = sorted(blocker_by_id[item["id"]] for item in capabilities if item["status"] == "missing")

report = {
    "format": 1,
    "schema": "fogcast.stage-a0.overlord-probe.v1",
    "status": "ready-for-generation" if not blockers else "blocked",
    "generation": "not-run",
    "overlord": checkout_identity(overlord, "deanoc-overlord", expected_overlord),
    "resources": checkout_identity(resources, "deanoc-ikuy-std-resources", expected_resources),
    "capabilities": capabilities,
    "blockers": blockers,
}
raw = (json.dumps(report, sort_keys=True, separators=(",", ":")) + "\n").encode("utf-8")
parent = output.parent
parent.mkdir(mode=0o700, parents=True, exist_ok=True)
try:
    fd = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
except OSError:
    input_error("output cannot be created without replacement")
with os.fdopen(fd, "wb") as handle:
    handle.write(raw)
print("overlord probe: " + report["status"])
sys.exit(0 if not blockers else 1)
PY
