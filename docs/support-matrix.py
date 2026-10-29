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
BOARDS = {
    'de10': (
        f'MiSTer/DE10-Nano, Cyclone V `{DE10_DEVICE}` '
        '(`sources/misteross/boards/de10nano`). DE25-Nano Agilex 5: no shell.'
    ),
    'device': (
        f'Cyclone V `{DE10_DEVICE}` spike only. No DE10-Nano shell for this block. '
        'DE25-Nano Agilex 5: no shell.'
    ),
    'none': 'No board bitstream. DE25-Nano Agilex 5: no shell.',
}
DE25_TOOLCHAIN = (
    'DE25-Nano: unverified. No shell; policy is Quartus-only because Agilex 5 '
    'is not an OSS target. Per-core bring-up is TBD.'
)


def load_json(root: Path):
    return json.loads((root / DATA).read_text())


def cell(text: str) -> str:
    return ' '.join(text.replace('|', '/').split())


def toolchain_text(row: dict) -> str:
    lock = row.get('oss_lock')
    if lock and row['board'] == 'de10':
        where = 'DE10-Nano primary **oss** (Yosys + nextpnr-mistral).'
    elif lock and row['board'] == 'device':
        where = 'Cyclone V spike primary **oss** (Yosys + nextpnr-mistral). Not a board package.'
    elif lock:
        where = 'No board package. Lock pins:'
    else:
        where = ''
    if lock:
        oss = (
            f"{where} Yosys `{lock['yosys']}`, nextpnr `{lock['nextpnr']}`, "
            f"Mistral `{lock['mistral']}` (`{lock['path']}`). "
            f"{row['router']} Quartus: {row['quartus']}"
        )
    else:
        oss = f"{row['oss_note']} {row['router']} Quartus: {row['quartus']}"
    return f'{oss} {DE25_TOOLCHAIN}'


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
    if (root / DE25).exists():
        errors.append(f'{DE25} exists; the matrix still says there is no DE25 shell')
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
