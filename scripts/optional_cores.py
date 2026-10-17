"""Optional FPGA cores for image assembly.

A required core still fails the image. An optional core that misses timing, fails
its producer, or is skipped up front is omitted. The omission is recorded once
and installed into the image. A failed producer is never replaced by a package
from a different functional key: a cache hit is only the package built from
the same sources, and that lookup happens before the producer runs.
"""
import json
import math
import os
from pathlib import Path
import re
import stat
import sys

from recipes import recipe_for

PRODUCER_REPORT_NAME = "fes-core-unavailable.json"
MANIFEST_NAME = "unavailable-cores.json"
REASONS = frozenset({"timing-not-met", "build-failed", "skipped"})
MAX_BYTES = 65536
DISPLAY_NAMES = {"fes.atari-st": "Atari ST"}
_CORE_ID = re.compile(r"fes\.[a-z0-9]+(?:[._-][a-z0-9]+)*\Z")
_CLOCK = re.compile(r"[A-Za-z_][A-Za-z0-9_.-]{0,63}\Z")


def _environment(env):
    return os.environ if env is None else env


def require_all(env=None):
    """True when a release build must contain every selected core."""
    return _environment(env).get("FES_REQUIRE_ALL_CORES") == "1"


def skip_all_optional(env=None):
    """True when every selected optional core is omitted before its producer."""
    return _environment(env).get("FES_SKIP_OPTIONAL_CORES") == "1"


def parse_skip_list(env, selected):
    """Return the optional core ids named by FES_SKIP_CORES.

    ``selected`` is the profile's recipe objects. A required, unknown, or
    unselected id is an error. Either skip form together with
    FES_REQUIRE_ALL_CORES=1 is an error.
    """
    env = _environment(env)
    raw = env.get("FES_SKIP_CORES") or ""
    parts = tuple(part.strip() for part in raw.split(",")) if raw.strip() else ()
    if require_all(env) and (skip_all_optional(env) or parts):
        raise ValueError("FES_REQUIRE_ALL_CORES=1 conflicts with skipping optional cores")
    if not parts:
        return frozenset()
    if any(part == "" for part in parts):
        raise ValueError("FES_SKIP_CORES contains an empty core id")
    if len(parts) != len(set(parts)):
        raise ValueError("FES_SKIP_CORES repeats a core id")
    selected_ids = {recipe.core_id for recipe in selected}
    for core_id in parts:
        try:
            recipe = recipe_for(core_id)
        except ValueError as error:
            raise ValueError(f"FES_SKIP_CORES names an unknown core {core_id}") from error
        if core_id not in selected_ids:
            raise ValueError(f"FES_SKIP_CORES names an unselected core {core_id}")
        if not recipe.optional:
            raise ValueError(f"FES_SKIP_CORES names a required core {core_id}")
    return frozenset(parts)


def core_is_skipped(recipe, env, skip_ids):
    """An optional core is skipped before its producer and its cache."""
    if not recipe.optional:
        return False
    if skip_all_optional(env):
        return True
    return recipe.core_id in skip_ids


def _number(value):
    if type(value) is bool or type(value) not in (int, float):
        raise ValueError("unavailable figure is not a finite number")
    if type(value) is float and not math.isfinite(value):
        raise ValueError("unavailable figure is not a finite number")
    return value


def _clocks(value):
    if not isinstance(value, dict) or len(value) > 16:
        raise ValueError("unavailable figure clocks are invalid")
    cleaned = {}
    for name in sorted(value):
        if not isinstance(name, str) or _CLOCK.fullmatch(name) is None:
            raise ValueError("unavailable figure clock name is invalid")
        pair = value[name]
        if not isinstance(pair, dict) or not {"achieved_mhz", "constraint_mhz"} <= set(pair):
            raise ValueError("unavailable figure clocks are invalid")
        cleaned[name] = {
            "achieved_mhz": _number(pair["achieved_mhz"]),
            "constraint_mhz": _number(pair["constraint_mhz"]),
        }
    return cleaned


def _figure(value):
    if not isinstance(value, dict):
        raise ValueError("unavailable figure is invalid")
    if not {"seed", "weight", "passing", "worst_ratio", "clocks"} <= set(value):
        raise ValueError("unavailable figure is invalid")
    if type(value["seed"]) is not int or type(value["weight"]) is not int:
        raise ValueError("unavailable figure seed is invalid")
    if type(value["passing"]) is not bool:
        raise ValueError("unavailable figure passing flag is invalid")
    return {
        "seed": value["seed"],
        "weight": value["weight"],
        "passing": value["passing"],
        "worst_ratio": _number(value["worst_ratio"]),
        "clocks": _clocks(value["clocks"]),
    }


def _name(core_id, value):
    if (isinstance(value, str) and 1 <= len(value) <= 80
            and all(32 <= ord(char) < 127 for char in value)):
        return value
    return DISPLAY_NAMES.get(core_id, core_id)


def canonical_cores(records):
    """Return omission rows sorted by core id, with producer-only fields dropped."""
    if isinstance(records, (str, bytes, dict)) or records is None:
        raise ValueError("unavailable cores must be a list")
    rows = []
    seen = set()
    for record in records:
        if not isinstance(record, dict):
            raise ValueError("unavailable core record is invalid")
        core_id = record.get("core_id")
        if not isinstance(core_id, str) or _CORE_ID.fullmatch(core_id) is None:
            raise ValueError("unavailable core id is invalid")
        try:
            recipe = recipe_for(core_id)
        except ValueError as error:
            raise ValueError(f"unavailable core {core_id} is unknown") from error
        if not recipe.optional:
            raise ValueError(f"unavailable core {core_id} is required")
        if core_id in seen:
            raise ValueError(f"unavailable core {core_id} is duplicated")
        seen.add(core_id)
        reason = record.get("reason")
        if reason not in REASONS:
            raise ValueError("unavailable core reason is invalid")
        seeds = record.get("seeds") or []
        figures = record.get("figures") or []
        if reason != "timing-not-met":
            if seeds or figures:
                raise ValueError("only a timing miss records seeds and figures")
            seeds, figures = [], []
        else:
            if (not isinstance(seeds, (list, tuple)) or len(seeds) > 64
                    or any(type(seed) is not int for seed in seeds)):
                raise ValueError("unavailable core seeds are invalid")
            if not isinstance(figures, (list, tuple)) or len(figures) > 32:
                raise ValueError("unavailable core figures are invalid")
            seeds = list(seeds)
            figures = [_figure(item) for item in figures]
        rows.append({
            "core_id": core_id,
            "name": _name(core_id, record.get("name")),
            "reason": reason,
            "seeds": seeds,
            "figures": figures,
        })
    rows.sort(key=lambda row: row["core_id"])
    return tuple(rows)


def unavailable_record(core_id, reason, *, seeds=(), figures=(), name=None):
    """One canonical omission row."""
    return canonical_cores([{
        "core_id": core_id,
        "name": name or DISPLAY_NAMES.get(core_id, core_id),
        "reason": reason,
        "seeds": list(seeds),
        "figures": list(figures),
    }])[0]


def failure_record(core_id, error):
    """Map a producer or video-part failure to an omission row.

    Structured timing evidence hangs on the ValueError or its direct cause.
    Any other failure is ``build-failed`` and carries no seed figures.
    """
    report = getattr(error, "unavailable", None)
    if report is None:
        report = getattr(error.__cause__, "unavailable", None)
    if (isinstance(report, dict) and report.get("reason") == "timing-not-met"
            and report.get("core_id") in (None, core_id)):
        return unavailable_record(
            core_id, "timing-not-met",
            seeds=report.get("seeds") or (), figures=report.get("figures") or ())
    return unavailable_record(core_id, "build-failed")


def encode_document(records):
    """Canonical manifest bytes, version 1."""
    payload = {"version": 1, "cores": list(canonical_cores(records))}
    return (json.dumps(payload, sort_keys=True, separators=(",", ":")) + "\n").encode()


def read_producer_report(path):
    """Return cleaned timing evidence, or None when the report is unusable.

    A symlink is not followed. A report for a required core, a non-timing
    reason, or a body over 65536 bytes is ignored.
    """
    path = Path(path)
    try:
        metadata = path.lstat()
    except FileNotFoundError:
        return None
    if (stat.S_ISLNK(metadata.st_mode) or not stat.S_ISREG(metadata.st_mode)
            or metadata.st_size < 1 or metadata.st_size > MAX_BYTES):
        return None
    try:
        descriptor = os.open(path, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0))
    except OSError:
        return None
    try:
        with os.fdopen(descriptor, "rb") as stream:
            data = stream.read(MAX_BYTES + 1)
    except OSError:
        return None
    if len(data) != metadata.st_size:
        return None
    try:
        document = json.loads(data)
        if not isinstance(document, dict) or document.get("version") != 1:
            return None
        if document.get("reason") != "timing-not-met":
            return None
        row = canonical_cores([{
            "core_id": document.get("core_id"),
            "reason": "timing-not-met",
            "seeds": document.get("seeds") or [],
            "figures": document.get("figures") or [],
            "name": document.get("name"),
        }])[0]
    except (ValueError, json.JSONDecodeError, UnicodeDecodeError):
        return None
    return {
        "core_id": row["core_id"],
        "reason": "timing-not-met",
        "seeds": row["seeds"],
        "figures": row["figures"],
    }


def publish_manifest(output, records):
    """Write the omission manifest, or remove one left by an earlier build."""
    output = Path(output)
    path = output / MANIFEST_NAME
    rows = canonical_cores(records)
    if not rows:
        try:
            metadata = path.lstat()
        except FileNotFoundError:
            return None
        if stat.S_ISLNK(metadata.st_mode) or stat.S_ISREG(metadata.st_mode):
            path.unlink()
            return None
        raise ValueError("unavailable core manifest is not a regular file")
    data = encode_document(rows)
    output.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + ".tmp")
    temporary.unlink(missing_ok=True)
    temporary.write_bytes(data)
    temporary.chmod(0o444)
    temporary.replace(path)
    return path


def verify_manifest(output, records):
    """Require the manifest bytes to match the omission rows exactly."""
    path = Path(output) / MANIFEST_NAME
    rows = canonical_cores(records)
    try:
        metadata = path.lstat()
    except FileNotFoundError:
        metadata = None
    if not rows:
        if metadata is not None:
            raise ValueError("unavailable core manifest is present without omitted cores")
        return
    if (metadata is None or stat.S_ISLNK(metadata.st_mode)
            or not stat.S_ISREG(metadata.st_mode)):
        raise ValueError("unavailable core manifest is missing")
    if metadata.st_size > MAX_BYTES or path.read_bytes() != encode_document(rows):
        raise ValueError("unavailable core manifest differs from the omitted cores")


def recorded_cores(output):
    """Omission rows stored with the image inputs, keyed by core id.

    A missing inputs file means this output has no recorded omission. A
    present file with a malformed omission list fails verification.
    """
    path = Path(output) / "inputs.json"
    try:
        text = path.read_text()
    except FileNotFoundError:
        return {}
    document = json.loads(text)
    raw = document.get("unavailable_cores") if isinstance(document, dict) else None
    if not raw:
        return {}
    return {row["core_id"]: row for row in canonical_cores(raw)}


def warning_lines(records):
    """Loud final-build lines for every omitted optional core."""
    rows = canonical_cores(records)
    if not rows:
        return []
    lines = ["WARNING: optional cores were omitted from this image:"]
    for record in rows:
        seeds = ",".join(str(seed) for seed in record["seeds"]) or "-"
        lines.append(
            f"WARNING: {record['core_id']} ({record['name']}) "
            f"reason={record['reason']} seeds={seeds}")
        for figure in record["figures"]:
            clocks = ", ".join(
                f"{name} {clock['achieved_mhz']}/{clock['constraint_mhz']} MHz"
                for name, clock in figure["clocks"].items())
            lines.append(
                f"WARNING:   seed={figure['seed']} weight={figure['weight']} "
                f"passing={str(figure['passing']).lower()} "
                f"worst_ratio={figure['worst_ratio']} {clocks}".rstrip())
    lines.append(
        "WARNING: FES_REQUIRE_ALL_CORES=1 makes an optional miss fail the build. "
        "Use it for a release image that must contain every selected core.")
    lines.append(
        "WARNING: FES_SKIP_OPTIONAL_CORES=1 or FES_SKIP_CORES skips optional "
        "cores before place-and-route.")
    return lines


def warn(records, stream=None):
    stream = sys.stderr if stream is None else stream
    for line in warning_lines(records):
        print(line, file=stream, flush=True)
