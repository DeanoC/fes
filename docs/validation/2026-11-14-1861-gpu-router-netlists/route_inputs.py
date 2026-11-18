#!/usr/bin/env python3
"""Are each artefact's route-deciding inputs byte-identical across two trees?

D12's merge-forward clause and the check_level rule both turn on the same set:
"its toolchain lock, its timing and pin constraints, and its placement recipe".
Comparing build_commands() is not enough to cover that set, because several
producers (spectrum, ramtest, c64, coleco) ignore the route tuple
build_commands returns and call route_after_synth with their own arguments
read from pnr_recipe. So this reads the recipe through the producer's own
pnr_recipe digests instead of restating any of it.

  python3 route_inputs.py --tree-a <dir> --tree-b <dir> [--json out.json]
"""
from __future__ import annotations

import argparse
import hashlib
import importlib
import json
import os
import subprocess
import sys
from pathlib import Path

MISTEROSS = "sources/misteross"


def _git(tree: Path, *args: str) -> str:
    return subprocess.run(("git", "-C", str(tree), *args),
                          capture_output=True, text=True, check=True).stdout.strip()


def _tree_identity(tree: Path, measured: set[str]) -> dict:
    """What was measured, pinned: the revision, the sources tree, cleanliness.

    Codex raised this on #1862 as a P1, and it was right: this report named
    only `tree_a` and `tree_b` as local paths, which are mutable. Two
    checkouts at different revisions, or matching uncommitted edits in both,
    would produce the same passing verdict while the README attributed it to a
    particular pair of commits. The adjacent design report already recorded
    its revisions; this one did not, and it is half of what carries conditions
    1 and 2 across a base move.

    `dirty` lists the measured input paths that differ from the commit, under
    `sources/misteross` and relative to it, so it names the files whose bytes
    decide the comparison rather than any stray file in the tree. A non-empty
    list makes the run a refusal, not a pass: the digests would be real and
    would belong to no commit.
    """
    # Parse each line by its first whitespace run, not by a fixed column.
    # `_git` strips the output, which eats the leading space of an unstaged
    # " M path" line, and a fixed `line[3:]` then cut one character off every
    # such path -- so nothing ever matched a measured input and every tree
    # reported clean. The counterexample caught it; reasoning about the format
    # had not. A rename reads "R  old -> new", so the new name is what counts.
    status = _git(tree, "status", "--porcelain", "--", MISTEROSS)
    changed = set()
    for line in status.splitlines():
        fields = line.strip().split(None, 1)
        if len(fields) < 2:
            continue
        name = fields[1].split(" -> ")[-1].strip().strip('"')
        changed.add(name.removeprefix(MISTEROSS + "/"))
    return {"path": str(tree), "revision": _git(tree, "rev-parse", "HEAD"),
            "sources_tree": _git(tree, "rev-parse", "HEAD:" + MISTEROSS),
            "measured_inputs": len(measured),
            "dirty": sorted(changed & measured)}

# artefact -> (producer module, core_id or None, attribute names for lock/qsf/sdc)
CASES = (
    ("fes.menu", "build_fes_menu_package", "fes.menu"),
    ("splash", "build_fes_splash", None),
    ("fes.pong", "build_fes_pong", "fes.pong"),
    ("fes.ramtest", "build_fes_ramtest", "fes.ramtest"),
    ("fes.riscv", "build_fes_riscv", "fes.riscv"),
    ("fes.coleco", "build_fes_coleco_socket_v2", "fes.coleco"),
    ("fes.c64", "build_fes_c64_oss", "fes.c64"),
    ("fes.spectrum", "build_fes_spectrum_oss", "fes.spectrum"),
    ("fes.spectrum128", "build_fes_spectrum_oss", "fes.spectrum128"),
    ("fes.spectrum-plus3", "build_fes_spectrum_oss", "fes.spectrum-plus3"),
    ("fes.pentagon", "build_fes_spectrum_oss", "fes.pentagon"),
    ("fes.zx81", "build_fes_zx81_quartus", "fes.zx81"),
    ("fes.zx80", "build_fes_zx81_quartus", "fes.zx80"),
    ("fes.ts1000", "build_fes_zx81_quartus", "fes.ts1000"),
    ("fes.ts1500", "build_fes_zx81_quartus", "fes.ts1500"),
)


def _load(tree: Path, module: str):
    for stale in [n for n in sys.modules if n == "scripts" or n.startswith("scripts.")]:
        del sys.modules[stale]
    root = tree / MISTEROSS
    sys.path.insert(0, str(root))
    os.chdir(root)
    try:
        return importlib.import_module(f"scripts.{module}")
    finally:
        sys.path.remove(str(root))


def _file(root: Path, relative) -> str | None:
    if relative is None:
        return None
    path = root / str(relative)
    if not path.is_file():
        return f"absent:{relative}"
    return hashlib.sha256(path.read_bytes()).hexdigest()


def _declared(producer) -> tuple[str, ...]:
    """The paths the producer itself says decide its build."""
    for name in ("PINNED_INPUTS", "INPUTS", "SOURCE_ROOTS"):
        value = getattr(producer, name, None)
        if value:
            return tuple(str(item) for item in value)
    if hasattr(producer, "inputs_for"):
        return tuple(str(item) for item in producer.inputs_for())
    return ()


def _pick(producer, *suffixes: str) -> str | None:
    """Find the one declared input with this role, or refuse to guess.

    Reading an attribute by a guessed name is how this program first went
    wrong: six producers keep their lock under a prefixed name
    (MENU_TOOLCHAIN_LOCK, C64_TOOLCHAIN_LOCK, SPECTRUM_TOOLCHAIN_LOCK) or in
    no constant at all, so a `getattr(..., "TOOLCHAIN_LOCK", None) or
    "toolchain.lock"` chain silently fell back to the repository-root lock and
    reported six different cores as sharing one digest. Selecting from the
    producer's own declared input list cannot drift that way, and returning
    None when the match is not unique makes the gap visible as UNANSWERED
    instead of as a passing comparison.
    """
    hits = sorted({path for path in _declared(producer)
                   if any(path.endswith(suffix) for suffix in suffixes)})
    return hits[0] if len(hits) == 1 else None


def measure(tree: Path, module: str, core_id: str | None) -> dict:
    producer = _load(tree, module)
    root = producer.ROOT
    lock = _pick(producer, "toolchain.lock", "quartus.lock")
    qsf = _pick(producer, ".qsf")
    sdc = _pick(producer, ".sdc")
    out = {
        "toolchain_lock_path": lock or "unavailable:not-uniquely-declared",
        "toolchain_lock": _file(root, lock) if lock else "unavailable:not-uniquely-declared",
        "qsf_path": qsf or "unavailable:not-uniquely-declared",
        "qsf": _file(root, qsf) if qsf else "unavailable:not-uniquely-declared",
        "sdc_path": sdc or "unavailable:not-uniquely-declared",
        "sdc": _file(root, sdc) if sdc else "unavailable:not-uniquely-declared",
    }
    if core_id is not None:
        from scripts import pnr_recipe
        for name in ("recipe_digest", "design_recipe_digest"):
            try:
                out[name] = getattr(pnr_recipe, name)(core_id)
            except Exception as exc:
                out[name] = f"unavailable:{type(exc).__name__}"
    return out


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--tree-a", type=Path, required=True)
    parser.add_argument("--tree-b", type=Path, required=True)
    parser.add_argument("--json", type=Path)
    args = parser.parse_args()
    # Resolve every output path before anything chdirs. These programs import
    # producers, and _load() does os.chdir(tree) so the producer sees its own
    # tree as cwd -- so a relative --json or --dump path would be written
    # *inside the tree being measured*. That happened: three summaries landed
    # in nb2/ and nh2/ and were saved only by sources/misteross/.gitignore
    # listing *.json, which kept those worktrees clean by luck rather than by
    # design. A stray file in a measured tree can change the very cleanliness
    # the records assert.
    if args.json is not None:
        args.json = args.json.resolve()
    a, b = args.tree_a.resolve(), args.tree_b.resolve()
    result, moved, unanswered, measured = {}, [], [], set()
    print(f'{"artefact":22} {"lock":6} {"qsf":6} {"sdc":6} {"recipe":8} {"design":8}')
    for artefact, module, core_id in CASES:
        try:
            ma, mb = measure(a, module, core_id), measure(b, module, core_id)
        except ModuleNotFoundError as exc:
            # A producer that exists on only one side cannot be compared. Say
            # so rather than skipping it, because a missing row here means the
            # artefact changed producer, which is itself a reason to re-measure.
            print(f"{artefact:22} PRODUCER ABSENT ON ONE SIDE: {exc.name}")
            unanswered.append(f"{artefact}: producer absent on one side")
            result[artefact] = {"verdict": "producer absent on one side"}
            continue
        row, bad = {}, []
        for key in sorted(set(ma) | set(mb)):
            if key.endswith("_path"):
                continue
            va, vb = ma.get(key), mb.get(key)
            if isinstance(va, str) and va.startswith("unavailable:"):
                unanswered.append(f"{artefact}.{key}")
                row[key] = "?"
                continue
            row[key] = "same" if va == vb else "MOVED"
            if va != vb:
                bad.append(key)
        if bad:
            moved.append(f"{artefact}: {', '.join(bad)}")
        for side in (ma, mb):
            for key, value in side.items():
                if key.endswith("_path") and isinstance(value, str) \
                        and not value.startswith("unavailable:"):
                    measured.add(value)
        result[artefact] = {"tree_a": ma, "tree_b": mb, "verdict": row}
        print(f'{artefact:22} {row.get("toolchain_lock","-"):6} {row.get("qsf","-"):6} '
              f'{row.get("sdc","-"):6} {row.get("recipe_digest","-"):8} '
              f'{row.get("design_recipe_digest","-"):8}')
    print()
    if unanswered:
        print("UNANSWERED: " + ", ".join(unanswered))
    identity = {"tree_a": _tree_identity(a, measured),
                "tree_b": _tree_identity(b, measured)}
    for name, side in identity.items():
        print(f'{name}: {side["revision"][:12]}  sources {side["sources_tree"][:12]}'
              + (f'  DIRTY: {", ".join(side["dirty"])}' if side["dirty"] else "  clean"))
    dirty = sorted({f'{name}:{path}' for name, side in identity.items()
                    for path in side["dirty"]})
    if dirty:
        # A measured input that differs from its commit makes the digests
        # belong to no revision, so the comparison cannot be attributed to the
        # pair of commits above. That is a refusal, not a footnote.
        print("REFUSED: measured inputs differ from their commit -- " + ", ".join(dirty))
    print("every route-deciding input is byte-identical" if not moved
          else "MOVED -- " + "; ".join(moved))
    if args.json:
        args.json.write_text(json.dumps(
            {**identity, "rows": result, "moved": moved,
             "unanswered": unanswered, "dirty": dirty},
            indent=2, sort_keys=True) + "\n")
        print(f"wrote {args.json}")
    return 1 if (moved or unanswered or dirty) else 0


if __name__ == "__main__":
    raise SystemExit(main())
