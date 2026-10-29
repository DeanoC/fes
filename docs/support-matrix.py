#!/usr/bin/env python3
"""Render and check the core support matrix.

`docs/support-matrix.json` is the maintained source. This script renders
`docs/support-matrix.md` and checks the structured fields against the recipe
registry, library standings, bootstrap image, toolchain locks and core
directories. Narrative cells stay in the JSON. Run from the repository root:

    python3 docs/support-matrix.py --check
    python3 docs/support-matrix.py --write
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path
import tomllib

ROOT_FILES = {
    'recipes': Path('config/core-recipes.toml'),
    'library': Path('config/core-library.toml'),
    'profile': Path('profiles/native-integration-dev.toml'),
    'image_inputs': Path('image/build/native-inputs.toml'),
    'de10_readme': Path('sources/misteross/boards/de10nano/README.md'),
}
DATA = Path('docs/support-matrix.json')
MARKDOWN = Path('docs/support-matrix.md')
CORES = Path('sources/misteross/cores')
DE25 = Path('sources/misteross/boards/de25nano')
SKIP_CORE_DIRS = frozenset({'fes-common'})
DE10_DEVICE = '5CSEBA6U23I7'
POLICY_CITE = 'policy: Deano 2026-10-29'
QUARTUS_PRIMARY = 'Quartus (primary, policy)'
DE25_CELL = 'Quartus-only (policy); shell pending'
# i386, pc386 and PC/XT. nextpnr on these rows is advisory.
QUARTUS_PRIMARY_IDS = frozenset({'i386', 'fes.pc386', 'fes.pcxt'})
BOARDS = {
    'de10': (
        f'MiSTer/DE10-Nano, Cyclone V `{DE10_DEVICE}` '
        f'(`sources/misteross/boards/de10nano`). DE25-Nano: {DE25_CELL}.'
    ),
    'device': (
        f'Cyclone V `{DE10_DEVICE}` spike only. No DE10-Nano shell for this block. '
        f'DE25-Nano: {DE25_CELL}.'
    ),
    'none': f'No board bitstream. DE25-Nano: {DE25_CELL}.',
}
DE25_TOOLCHAIN = (
    f'DE25-Nano: {DE25_CELL}. {POLICY_CITE}. '
    'Agilex 5 has no OSS reverse-engineering. '
    'Per-core cells stay on this line until the shell is on this branch.'
)


def load_json(root: Path):
    return json.loads((root / DATA).read_text())


def cell(text: str) -> str:
    return ' '.join(text.replace('|', '/').split())


def toolchain_text(row: dict) -> str:
    lock = row.get('oss_lock')
    pins = ''
    if lock:
        pins = (
            f"Yosys `{lock['yosys']}`, nextpnr `{lock['nextpnr']}`, "
            f"Mistral `{lock['mistral']}` (`{lock['path']}`). "
        )
    if row.get('primary_toolchain') == 'quartus':
        # The Quartus field carries the primary label and the citation.
        # The lock pins are the advisory nextpnr build.
        body = f"{row['quartus']} Advisory nextpnr: {pins}{row['router']}"
    elif lock and row['board'] == 'de10':
        body = (
            f"DE10-Nano primary **oss** (Yosys + nextpnr-mistral). {pins}"
            f"{row['router']} Quartus: {row['quartus']}"
        )
    elif lock and row['board'] == 'device':
        body = (
            'Cyclone V spike primary **oss** (Yosys + nextpnr-mistral). '
            f"Not a board package. {pins}{row['router']} Quartus: {row['quartus']}"
        )
    elif lock:
        body = f"No board package. Lock pins: {pins}{row['router']} Quartus: {row['quartus']}"
    else:
        body = f"{row['oss_note']} {row['router']} Quartus: {row['quartus']}"
    return f'{body} {DE25_TOOLCHAIN}'


def render(data: dict) -> str:
    lines = [
        '<!-- Generated from docs/support-matrix.json by docs/support-matrix.py. Edit the JSON, then run python3 docs/support-matrix.py --write -->',
        '',
        '# Core support matrix',
        '',
        'Maintained source: [`support-matrix.json`](support-matrix.json). '
        'Regenerate this page with `python3 docs/support-matrix.py --write` from the repository root. '
        'ABI, interfaces and package standing stay in [core status](core-status.md). '
        'How to build a core stays in [misteross cores](../sources/misteross/docs/cores.md).',
        '',
    ]
    for paragraph in data['intro']:
        lines.append(paragraph)
        lines.append('')
    lines += ['## Tiers', '']
    lines.append('| Tier | Meaning |')
    lines.append('| --- | --- |')
    for tier in data['tiers']:
        lines.append(f"| {cell(tier['label'])} | {cell(tier['meaning'])} |")
    lines.append('')
    lines += ['## Toolchain policy', '']
    for paragraph in data['policy']:
        lines.append(paragraph)
        lines.append('')
    lines += [
        '## Matrix',
        '',
        'Bluetooth and mesh are host services. A paired Bluetooth HID or HoG pad is an evdev node on the kit '
        '([FogCast README](../sources/FogCast/README.md)); no per-core Bluetooth contract was found. '
        'Mesh and FogCast launch are also host-wide ([mesh under kit-as-host](mesh-kit-host.md)); '
        'no per-core mesh flag was found. Those two facts are not repeated on every row. '
        'A feature cell says `per-core unverified` where the host fact is the only evidence.',
        '',
        '| Core | Tier | Boards | RTL toolchain | Key features | Notes |',
        '| --- | --- | --- | --- | --- | --- |',
    ]
    labels = {tier['id']: tier['label'] for tier in data['tiers']}
    for row in data['cores']:
        lines.append('| ' + ' | '.join((
            cell(row['label']),
            cell(labels[row['tier']]),
            cell(BOARDS[row['board']]),
            cell(toolchain_text(row)),
            cell(row['features']),
            cell(row['notes']),
        )) + ' |')
    lines += ['', '## Unverified cells', '']
    lines.append(
        'These cells are deliberately not filled. The surrounding text is what the tree actually shows.'
    )
    lines.append('')
    for item in data['unverified']:
        lines.append(f"- **{item['where']} — {item['cell']}.** {item['detail']}")
    lines += ['', '## Evidence', '']
    lines.append('| Core | Paths |')
    lines.append('| --- | --- |')
    for row in data['cores']:
        links = []
        for path in row['evidence']:
            target = path[len('docs/'):] if path.startswith('docs/') else '../' + path
            links.append(f'[`{path}`]({target})')
        lines.append(f"| {cell(row['label'])} | {cell('; '.join(links))} |")
    lines.append('')
    return '\n'.join(lines)


def _pins(path: Path) -> dict:
    data = tomllib.loads(path.read_text())
    tools = data['tool']
    return {name: tools[name]['commit'][:8] for name in ('yosys', 'mistral', 'nextpnr')}


def validate(root: Path, data: dict) -> list[str]:
    errors = []
    recipes = {
        row['core_id']: row
        for row in tomllib.loads((root / ROOT_FILES['recipes']).read_text())['recipes']
    }
    library = {
        row['core_id']: row['standing']
        for row in tomllib.loads((root / ROOT_FILES['library']).read_text())['cores']
    }
    profile = tomllib.loads((root / ROOT_FILES['profile']).read_text())
    image_ids = {row['core_id'] for row in profile['fpga_packages']}
    image_text = (root / ROOT_FILES['image_inputs']).read_text()
    de10 = (root / ROOT_FILES['de10_readme']).read_text()
    if DE10_DEVICE not in de10:
        errors.append(f'{ROOT_FILES["de10_readme"]} no longer names {DE10_DEVICE}')
    # DE25-Nano landing is expected (policy: Deano 2026-10-29). The directory
    # may be absent (shell pending) or present. Either state is valid.
    # Per-core DE25 cells are re-verified by hand only after the shell is on
    # this branch; this check does not inspect them, and the rendered line
    # stays "Quartus-only (policy); shell pending" until that review.
    (root / DE25).is_dir()
    if 'unverified' in DE25_TOOLCHAIN or DE25_CELL not in DE25_TOOLCHAIN:
        errors.append('DE25 toolchain line must stay Quartus-only (policy); shell pending')
    if not (root / 'sources/misteross/boards/de10nano').is_dir():
        errors.append('sources/misteross/boards/de10nano is missing')

    tier_ids = {tier['id'] for tier in data['tiers']}
    rows = data['cores']
    ids = [row['id'] for row in rows]
    if len(ids) != len(set(ids)):
        errors.append('duplicate core ids in support-matrix.json')
    by_id = {row['id']: row for row in rows}

    for core_id, recipe in recipes.items():
        row = by_id.get(core_id)
        if row is None:
            errors.append(f'recipe core {core_id} has no matrix row')
            continue
        if row.get('recipe') is not True:
            errors.append(f'{core_id} is a recipe but the matrix says recipe is false')
        if bool(recipe.get('optional', False)) != bool(row.get('optional')):
            errors.append(f'{core_id} optional flag does not match config/core-recipes.toml')
        if recipe.get('quartus_role') != row.get('quartus_role'):
            errors.append(f'{core_id} quartus_role does not match config/core-recipes.toml')
        if recipe.get('gpu_router') != row.get('recipe_gpu_router'):
            errors.append(f'{core_id} gpu_router does not match config/core-recipes.toml')
        if recipe.get('producer_script') != row.get('producer_script'):
            errors.append(f'{core_id} producer_script does not match config/core-recipes.toml')
        producer = root / 'sources/misteross' / recipe['producer_script']
        if not producer.is_file():
            errors.append(f'{core_id} producer is missing: {producer}')
        lock = root / 'sources/misteross' / recipe['lock_path']
        recorded = row.get('oss_lock') or {}
        if recorded.get('path') != str(Path('sources/misteross') / recipe['lock_path']):
            errors.append(f'{core_id} lock path does not match config/core-recipes.toml')
        elif lock.is_file():
            pins = _pins(lock)
            for name, pin in pins.items():
                if recorded.get(name) != pin:
                    errors.append(f'{core_id} {name} pin {recorded.get(name)} != lock {pin}')
        else:
            errors.append(f'{core_id} lock file is missing: {lock}')

    for row in rows:
        if row['tier'] not in tier_ids:
            errors.append(f"{row['id']} uses unknown tier {row['tier']}")
        if row['board'] not in BOARDS:
            errors.append(f"{row['id']} uses unknown board class {row['board']}")
        if row.get('recipe') is True and row['id'] not in recipes:
            errors.append(f"{row['id']} claims a recipe that is not in config/core-recipes.toml")
        if row.get('recipe') is not True and row['id'] in recipes:
            errors.append(f"{row['id']} is registered but recipe is not true")
        standing = row.get('library_standing')
        if standing != library.get(row['id']):
            errors.append(
                f"{row['id']} library standing {standing!r} != {library.get(row['id'])!r}"
            )
        lock = row.get('oss_lock')
        if lock:
            path = root / lock['path']
            if not path.is_file():
                errors.append(f"{row['id']} lock missing: {lock['path']}")
            else:
                pins = _pins(path)
                for name, pin in pins.items():
                    if lock.get(name) != pin:
                        errors.append(f"{row['id']} {name} pin {lock.get(name)} != {pin}")
            for name in ('yosys', 'nextpnr', 'mistral'):
                if lock[name] not in toolchain_text(row):
                    errors.append(f"{row['id']} rendered toolchain omits {name}")
        elif not row.get('oss_note'):
            errors.append(f"{row['id']} needs oss_lock or oss_note")
        for path in row.get('paths') or []:
            if not (root / path).exists():
                errors.append(f"{row['id']} path missing: {path}")
        for path in row['evidence']:
            if not (root / path).exists():
                errors.append(f"{row['id']} evidence missing: {path}")
        if not row['evidence']:
            errors.append(f"{row['id']} has no evidence paths")

    claimed_image = {row['id'] for row in rows if row.get('in_bootstrap_image')}
    if claimed_image != image_ids:
        errors.append(f'bootstrap image ids {sorted(image_ids)} != matrix {sorted(claimed_image)}')
    for row in rows:
        pin = row.get('image_pin_path')
        if pin and pin not in image_text:
            errors.append(f"{row['id']} image pin {pin} is not in {ROOT_FILES['image_inputs']}")

    covered = set()
    named_rtl = set()
    rtl_root = 'sources/misteross/cores/fes-common/rtl'
    for row in rows:
        covered.update(row.get('covers') or [])
        for path in row.get('paths') or []:
            relative = Path(path)
            if relative.parent.as_posix() == rtl_root:
                named_rtl.add(relative.name)
    actual = {path.name for path in (root / CORES).iterdir() if path.is_dir()} - SKIP_CORE_DIRS
    missing = actual - covered
    extra = covered - actual
    if missing:
        errors.append('core directories missing from the matrix: ' + ', '.join(sorted(missing)))
    if extra:
        errors.append('matrix covers directories that are not cores: ' + ', '.join(sorted(extra)))
    rtl_dirs = {path.name for path in (root / rtl_root).iterdir() if path.is_dir()}
    if named_rtl != rtl_dirs:
        errors.append(
            'fes-common/rtl directories '
            f'missing {sorted(rtl_dirs - named_rtl)} extra {sorted(named_rtl - rtl_dirs)}'
        )

    for core_id in library:
        if core_id not in by_id:
            errors.append(f'library core {core_id} has no matrix row')

    claimed_primary = {row['id'] for row in rows if row.get('primary_toolchain') == 'quartus'}
    if claimed_primary != QUARTUS_PRIMARY_IDS:
        errors.append(
            'Quartus-primary rows '
            f'{sorted(claimed_primary)} != {sorted(QUARTUS_PRIMARY_IDS)}'
        )
    for core_id in sorted(QUARTUS_PRIMARY_IDS & set(by_id)):
        rendered_row = toolchain_text(by_id[core_id])
        if QUARTUS_PRIMARY not in rendered_row or POLICY_CITE not in rendered_row:
            errors.append(f'{core_id} toolchain cell is missing the Quartus-primary policy')
        if 'primary **oss**' in rendered_row:
            errors.append(f'{core_id} still renders an OSS-primary toolchain')
    policy = '\n'.join(data.get('policy') or [])
    for phrase in (POLICY_CITE, QUARTUS_PRIMARY, DE25_CELL, '0 titles'):
        if phrase not in policy:
            errors.append(f'policy section is missing {phrase!r}')
    coleco = by_id.get('fes.coleco') or {}
    coleco_notes = coleco.get('notes') or ''
    for phrase in (POLICY_CITE, '0 titles', 'low priority', 'Adam'):
        if phrase not in coleco_notes:
            errors.append(f'fes.coleco notes are missing {phrase!r}')
    return errors


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path.cwd())
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument('--check', action='store_true')
    group.add_argument('--write', action='store_true')
    args = parser.parse_args()
    root = args.root.resolve()
    data = load_json(root)
    errors = validate(root, data)
    rendered = render(data)
    target = root / MARKDOWN
    if errors:
        parser.exit(1, 'support matrix:\n' + '\n'.join(errors) + '\n')
    if args.write:
        target.write_text(rendered)
        print(f'wrote {MARKDOWN}')
        return
    current = target.read_text() if target.is_file() else ''
    if current != rendered:
        parser.exit(1, f'{MARKDOWN} is stale; run python3 docs/support-matrix.py --write\n')
    print('support matrix: ok')


if __name__ == '__main__':
    main()
