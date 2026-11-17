#!/usr/bin/env python3
"""Collect each condition-3 gated build's identity, margins and seal verdict.

Reads the build-summary.json each producer wrote, so the numbers in the record
are the producer's own and not re-derived here. A margin is printed as the
ratio of achieved to constrained, because a core that clears by under one per
cent is the case a later failure would be misread as a regression.
"""
from __future__ import annotations
import json, sys
from pathlib import Path

TREE = Path("/data/fes1861-netlist/identity/c3head/sources/misteross")
OUT = Path("/data/fes1861-netlist/cond3")
LOGS = Path("/data/fes1861-netlist/cond3-logs")
# The revision condition 3 must have been taken at. A summary from any other
# revision is a stale directory, not this run, and is refused rather than
# collected: a build-summary.json sitting at a deterministic path is exactly
# the kind of artefact that gets attributed to the wrong run.
EXPECTED_REVISION = None  # set from --revision
OSS = {"menu": "build/oss/fes-menu-package", "splash": "build/fes-splash",
       "c64": "build/fes-c64-oss", "spectrum": "build/fes-spectrum-oss",
       "spectrum128": "build/fes-spectrum128-oss",
       "spectrum-plus3": "build/fes-spectrum-plus3-oss",
       "pentagon": "build/fes-pentagon-oss"}


def margins(timing) -> dict:
    """The lane's own margin figures, in the lane's own units.

    The two lanes do not measure the same quantity and are not made to look as
    though they do. nextpnr reports an achieved and a constrained frequency per
    clock, so its margin is a ratio. Quartus reports *slack in nanoseconds* per
    analysis type (Setup, Hold, Recovery, Removal, Minimum Pulse Width), so its
    margin is a slack, and the worst of those is not comparable to a frequency
    ratio. Forcing one number over both would state something neither report
    says: a 0.121 ns hold slack is ordinary, while a 0.121x frequency ratio
    would be a catastrophic miss.
    """
    if isinstance(timing, list):
        rows = {row.get("type", f"row{i}"): row for i, row in enumerate(timing)
                if isinstance(row, dict) and isinstance(row.get("slack_ns"), (int, float))}
        if not rows:
            return {}
        worst = min(rows.items(), key=lambda kv: kv[1]["slack_ns"])
        return {"kind": "quartus-slack-ns",
                "slack_ns": {name: row["slack_ns"] for name, row in rows.items()},
                "tns_ns": {name: row.get("tns_ns") for name, row in rows.items()},
                "worst_analysis": worst[0], "worst_slack_ns": worst[1]["slack_ns"],
                "all_non_negative": all(r["slack_ns"] >= 0 for r in rows.values())}
    out = {}
    for name, row in (timing or {}).items():
        if not isinstance(row, dict):
            continue
        a, c = row.get("achieved_mhz"), row.get("constraint_mhz")
        if isinstance(a, (int, float)) and isinstance(c, (int, float)) and c:
            out[name] = {"achieved_mhz": a, "constraint_mhz": c,
                         "ratio": a / c, "status": row.get("status")}
    if out:
        return {"kind": "nextpnr-frequency-ratio", "clocks": out,
                "worst_ratio": min(v["ratio"] for v in out.values())}
    return {}


def announced_output(key: str) -> Path | None:
    """The output directory the Quartus producer named in its own log.

    build_fes_zx81_quartus prints "<core_id> Quartus build <build_id>: <dir>"
    before it compiles, so the run says where it wrote rather than this
    program guessing from a glob. Globbing picked the last path in sort order,
    which is not the newest and need not be this run at all.
    """
    log = LOGS / f"{key}.log"
    if not log.is_file():
        return None
    for line in log.read_text(errors="replace").splitlines():
        if " Quartus build " in line and ": " in line:
            return Path(line.rsplit(": ", 1)[1].strip())
    return None


def derived_build_id(key: str) -> str | None:
    """Re-derive this artefact's BUILD_ID at the head, for a summary with no revision.

    build_fes_splash writes a format-1 record and its build-summary.json
    carries no `revision` field -- but the *record* does, and BUILD_ID is taken
    over the whole record. So the sealed build_id commits to the revision
    cryptographically, which is a stronger tie than the missing field would
    have been. Deriving it costs no build: it authenticates the tools and
    encodes the record, nothing more.
    """
    if key != "splash":
        return None
    import os
    saved = sys.path[:], os.getcwd(), os.environ.get("FES_TOOLCHAIN_CACHE_ROOT")
    try:
        os.chdir(TREE)
        sys.path.insert(0, str(TREE))
        # splash refuses a shared cache root and uses its tree-local install.
        os.environ.pop("FES_TOOLCHAIN_CACHE_ROOT", None)
        from scripts import build_fes_splash as sp
        from scripts.export_core_package import build_identity
        repository, revision = sp.board._require_clean_source(
            sp.ROOT, pinned_inputs=sp.PINNED_INPUTS, identity_version=sp.IDENTITY_VERSION)
        auth = sp.authenticate_oss_tools(sp.ROOT)
        ids = {n: t.identity for n, t in auth.items()}
        return build_identity(sp.create_build_record(sp.ROOT, repository, revision, ids))
    except Exception as exc:
        print(f"  (could not derive {key}'s BUILD_ID: {type(exc).__name__}: {exc})")
        return None
    finally:
        sys.path[:], cwd, cache = saved
        os.chdir(cwd)
        if cache is not None:
            os.environ["FES_TOOLCHAIN_CACHE_ROOT"] = cache


def check(key: str, data: dict, path: Path, revision: str | None) -> str | None:
    """Why this summary must not be collected, or None if it is sound.

    An earlier version compared the summary's mtime against the run log's.
    That is a false positive by construction: the log is the build's *output
    stream*, so it keeps being written until after the summary exists. It
    refused fes.zx81's genuine seal over a 1.4-second difference.

    What actually ties a summary to this run needs no clock:

    - its `revision` must be the head condition 3 was taken at;
    - its `build_id` must match the one in its own path, so a directory
      cannot be credited to an identity it does not hold;
    - for the Quartus lane the directory must be the one the producer
      *announced* in its log before compiling, which identifies the run
      outright.

    A build_id is derived from the producer's closure at a revision, so a
    summary passing the first two cannot have come from a different revision
    whatever its timestamps say.
    """
    build_id = data.get("build_id")
    if build_id in (None, ""):
        return "summary has no build_id"
    if revision is not None and data.get("revision") not in (None, revision):
        return f'summary revision {str(data.get("revision"))[:9]} is not the head {revision[:9]}'
    if build_id not in path.parts and build_id not in str(path):
        # Only the Quartus lane nests the identity in the path; the OSS
        # producers write to a fixed directory, so this is informational there.
        if f"-quartus/" in str(path):
            return f"build_id {build_id[:12]} is not the identity its path names"
    if data.get("revision") is None and revision is not None:
        expected = derived_build_id(key)
        if expected is None:
            return "summary records no revision and no derivation is available for it"
        if expected != build_id:
            return (f"summary build_id {build_id[:12]} is not the {expected[:12]} this head "
                    f"derives, so it is not this run's seal")
        return None
    return None


def main() -> int:
    import argparse, subprocess
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--revision", help="the head condition 3 was taken at")
    parser.add_argument("--allow-unverified", action="store_true",
                        help="collect summaries that fail the staleness check, marking them")
    arguments = parser.parse_args()
    revision = arguments.revision
    if revision is None:
        revision = subprocess.run(("git", "-C", str(TREE), "rev-parse", "HEAD"),
                                  capture_output=True, text=True).stdout.strip() or None
    result = {}
    refused = []
    for key, relative in OSS.items():
        path = TREE / relative / "build-summary.json"
        if not path.is_file():
            continue
        data = json.loads(path.read_text())
        why = check(key, data, path, revision)
        if why and not arguments.allow_unverified:
            refused.append(f"{key}: {why}")
            continue
        result[key] = {"lane": "nextpnr", "build_id": data.get("build_id"),
                       "unverified": why, "revision": data.get("revision"),
                       "tied_to_head_by": ("revision field" if data.get("revision")
                                           else "BUILD_ID re-derived at the head"),
                       "placer_seed": data.get("route", {}).get("placer_seed"),
                       "margins": margins(data.get("timing")),
                       "source": str(path)}
    for key in ("zx81", "zx80", "ts1000", "ts1500"):
        announced = announced_output(key)
        provenance = "announced by the producer in its own log"
        if announced is not None and (announced / "build-summary.json").is_file():
            chosen = announced / "build-summary.json"
        else:
            provenance = "newest matching directory (the log announced none)"
            found = sorted((TREE / "build").glob(f"fes-{key}-quartus/*/attempt-*/build-summary.json"),
                           key=lambda p: p.stat().st_mtime)
            if not found:
                continue
            chosen = found[-1]
        data = json.loads(chosen.read_text())
        why = check(key, data, chosen, revision)
        if why and not arguments.allow_unverified:
            refused.append(f"{key}: {why}")
            continue
        runs = data.get("runs", [])
        result[key] = {"lane": "quartus-lite", "build_id": data.get("build_id"),
                       "unverified": why, "provenance": provenance,
                       "revision": data.get("revision"),
                       "seeds": data.get("seeds"),
                       "repeat_matches_signoff":
                           bool(runs) and runs[0].get("rbf_sha256") == data.get("repeat_rbf_sha256"),
                       "payload_sha256": data.get("payload_sha256"),
                       "margins": margins((runs[0] if runs else {}).get("timing")),
                       "source": str(chosen)}
    OUT.mkdir(parents=True, exist_ok=True)
    (OUT / "cond3.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print(f'{"artefact":18} {"lane":13} {"build_id":34} margin (lane units)')
    for key, row in sorted(result.items()):
        m = row.get("margins", {})
        if m.get("kind") == "nextpnr-frequency-ratio":
            shown = f'{m["worst_ratio"]:.4f}x worst clock'
        elif m.get("kind") == "quartus-slack-ns":
            shown = (f'{m["worst_slack_ns"]:+.3f} ns worst slack ({m["worst_analysis"]})'
                     + ("" if m["all_non_negative"] else "  **NEGATIVE SLACK**"))
        else:
            shown = "no margin figures in the summary"
        print(f'{key:18} {row["lane"]:13} {str(row["build_id"]):34} {shown}')
    if refused:
        print("\nREFUSED (not collected):")
        for line in refused:
            print("  " + line)
    print(f"\nwrote {OUT / 'cond3.json'} ({len(result)} artefacts collected"
          + (f", {len(refused)} refused)" if refused else ")"))
    return 1 if refused else 0


if __name__ == "__main__":
    raise SystemExit(main())
