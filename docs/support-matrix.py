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
import subprocess
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
QUARTUS_PRIMARY_IDS = frozenset({'i386', 'fes.pc386', 'fes.pcxt', 'fes.pcxt-dos'})
BOARDS = frozenset({'de10', 'device', 'none'})
# Human-table feature words. The long `features` string stays in the JSON.
HIGHLIGHTS = {
    'fes.menu': 'HDMI',
    'splash': 'HDMI',
    'fes.pong': 'pad, HDMI',
    'fes.catch': 'pad, HDMI',
    'fes.ramtest': 'pad, HDMI',
    'fes.zx81': 'tape, keys, HDMI',
    'fes.zx80': 'tape, keys, HDMI',
    'fes.ts1000': 'tape, keys, HDMI',
    'fes.ts1500': 'tape, keys, HDMI',
    'fes.coleco': 'cart, pad, HDMI',
    'fes.sms': 'cart, keys, HDMI',
    'fes.sg1000': 'cart, keys, HDMI',
    'fes.apple2': 'disk, keys, HDMI',
    'fes.c64': 'disk, keys, HDMI',
    'fes.spectrum': 'tape, keys, HDMI',
    'fes.spectrum128': 'tape, keys, HDMI',
    'fes.spectrum-plus3': 'tape, disk, keys, HDMI',
    'fes.pentagon': 'tape, disk, keys, HDMI',
    'fes.c64-ntsc': '—',
    'fes.atari-st': 'disk, keys, mouse, HDMI',
    'fes.atari-ste': '—',
    'fes.riscv': 'pad, HDMI',
    'fes.demo': 'pad, HDMI',
    'sim.pong': '—',
    'fes.pcxt': 'disk, HDMI',
    'fes.pcxt-dos': 'disk, HDMI',
    'fes.pc386': 'sim',
    'i386': '—',
    'z80': '—',
    'cpu6502': '—',
    'fx68k': '—',
    'tv80': '—',
}
# Repo path of the core's own doc, or a shared status page when it has none.
DETAILS = {
    'fes.menu': ('sources/misteross/cores/fes-menu/README.md', 'README'),
    'splash': ('sources/misteross/cores/fes-splash/README.md', 'README'),
    'fes.pong': ('sources/misteross/docs/cores.md', 'cores'),
    'fes.catch': ('sources/misteross/docs/cores.md', 'cores'),
    'fes.ramtest': ('sources/misteross/cores/fes-ramtest/README.md', 'README'),
    'fes.zx81': ('sources/misteross/docs/cores.md', 'cores'),
    'fes.zx80': ('sources/misteross/docs/cores.md', 'cores'),
    'fes.ts1000': ('sources/misteross/docs/cores.md', 'cores'),
    'fes.ts1500': ('sources/misteross/docs/cores.md', 'cores'),
    'fes.coleco': ('sources/misteross/cores/fes-coleco/README.md', 'README'),
    'fes.sms': ('sources/misteross/cores/fes-sms/README.md', 'README'),
    'fes.sg1000': ('sources/misteross/cores/fes-sg1000/README.md', 'README'),
    'fes.apple2': ('sources/misteross/cores/fes-apple2/README.md', 'README'),
    'fes.c64': ('sources/misteross/cores/fes-c64/README.md', 'README'),
    'fes.c64-ntsc': ('sources/misteross/cores/fes-c64/README.md', 'README'),
    'fes.spectrum': ('sources/misteross/cores/fes-spectrum/README.md', 'README'),
    'fes.spectrum128': ('sources/misteross/cores/fes-spectrum/README.md', 'README'),
    'fes.spectrum-plus3': ('sources/misteross/cores/fes-spectrum/README.md', 'README'),
    'fes.pentagon': ('sources/misteross/cores/fes-spectrum/README.md', 'README'),
    'fes.atari-st': ('sources/misteross/cores/fes-atari-st/README.md', 'README'),
    'fes.atari-ste': ('sources/misteross/cores/fes-atari-st/README.md', 'README'),
    'fes.riscv': ('sources/misteross/cores/fes-riscv/README.md', 'README'),
    'fes.demo': ('sources/misteross/docs/cores.md', 'cores'),
    'sim.pong': ('sources/misteross/docs/cores.md', 'cores'),
    'fes.pcxt': ('sources/misteross/cores/fes-pcxt/README.md', 'README'),
    'fes.pcxt-dos': ('sources/misteross/cores/fes-pcxt/README.md', 'README'),
    'fes.pc386': ('sources/misteross/cores/fes-pc386/README.md', 'README'),
    'i386': ('sources/misteross/docs/i386-cpu-design.md', 'design'),
    'z80': ('sources/misteross/cores/fes-common/rtl/z80/README.md', 'README'),
    'cpu6502': ('sources/misteross/cores/fes-common/rtl/cpu6502/README.md', 'README'),
    'fx68k': ('sources/misteross/cores/fes-common/rtl/fx68k/README.md', 'README'),
    'tv80': ('docs/core-alignment.md', 'alignment'),
}
AS_OF = '2026-10-29'
# Main's first-parent tip at the end of AS_OF. Keep the history snapshot fixed
# so later merges with backdated author dates cannot change the matrix.
AS_OF_COMMIT = '8f06961a9ec899963eda280c96af4d7149feadcb'
WINDOW_START = '2026-08-30'
RECENT_CUTOFF = '2026-10-16'
MEDIA_GENS = frozenset({'live', 'linked-rom', 'old', 'none', 'sim', 'diagnostic'})
TIER_ROLES = frozenset({'firmware', 'shared', 'demo', 'reference', 'bring-up', 'play'})
OVERRIDE_REASON = 'Deano 2026-10-29'
VARIANT_KINDS = frozenset({'model', 'rom'})
ROLE_TIER = {
    'firmware': 'firmware',
    'shared': 'shared',
    'demo': 'demo',
    'reference': 'reference',
    'bring-up': 'bring-up',
}


def label_tokens(label: str) -> list[str]:
    start = label.find('(')
    end = label.rfind(')')
    if start < 0 or end <= start:
        return []
    return [part.strip() for part in label[start + 1:end].split(',') if part.strip()]


def derive_tier(row: dict) -> str:
    """Tier from role and signals. Catalog standing and overrides are not inputs."""
    role = row['tier_role']
    if role in ROLE_TIER:
        return ROLE_TIER[role]
    rtl = (row.get('signals') or {}).get('last_rtl_change')
    media = (row.get('signals') or {}).get('media_gen')
    if media == 'old' and rtl and rtl >= RECENT_CUTOFF:
        return 'unresolved'
    if rtl and rtl >= RECENT_CUTOFF:
        return 'current'
    if rtl and rtl >= WINDOW_START:
        return 'aging'
    return 'stale'


def _git(root: Path, *args: str) -> str:
    return subprocess.run(['git', '-C', str(root), *args], check=True,
                          capture_output=True, text=True).stdout


def owned_rtl(root: Path, row: dict) -> tuple[str, str, str] | None:
    """Newest owned-RTL commit in the window for a shared row, as tier_rules defines it.

    Owned: the subject names the core, or the core holds at least half of the
    commit's RTL paths and at least two files. A one-file edit holding less
    than half of another commit's RTL paths is never owned. Returns
    (full sha, author date, subject), or None.
    """
    name = row['id'].removeprefix('fes.').lower()
    log = _git(root, 'log', AS_OF_COMMIT, '--format=%H%x00%as%x00%s', '--', *row['paths'])
    for line in log.splitlines():
        sha, date, subject = line.split('\0', 2)
        if not WINDOW_START <= date <= AS_OF:
            continue
        files = _git(root, 'show', '--format=', '--name-only', sha).split()
        rtl = [path for path in files if '/rtl/' in path]
        mine = [path for path in rtl if any(path.startswith(prefix + '/') for prefix in row['paths'])]
        half = 2 * len(mine) >= len(rtl)
        if len(mine) == 1 and not half:
            continue
        if name in subject.lower() or (half and len(mine) >= 2):
            return sha, date, subject
    return None


def effective_tier(row: dict) -> str:
    """Displayed tier. A recorded override replaces the derived result."""
    override = row.get('tier_override')
    if override:
        return override['tier']
    return derive_tier(row)


def load_json(root: Path):
    return json.loads((root / DATA).read_text())


def cell(text: str) -> str:
    return ' '.join(text.replace('|', '/').split())


def repo_href(path: str) -> str:
    if path.startswith('docs/'):
        return path[len('docs/'):]
    return '../' + path


def de10_cell(row: dict) -> str:
    if row.get('primary_toolchain') == 'quartus' and row['board'] == 'de10':
        return 'Quartus'
    if row['board'] == 'de10':
        return 'OSS'
    return '—'


def de25_cell(row: dict) -> str:
    if row['board'] == 'de10':
        return 'Quartus'
    return '—'


def detail_cell(row: dict) -> str:
    path, title = DETAILS[row['id']]
    return f'[{title}]({repo_href(path)})'


def render(data: dict) -> str:
    lines = [
        '<!-- Generated from docs/support-matrix.json by docs/support-matrix.py. Edit the JSON, then run python3 docs/support-matrix.py --write -->',
        '',
        '# Core support matrix',
        '',
        'Short view of cores, boards and toolchains. Locks, evidence, feature notes and the full policy wording stay in [`support-matrix.json`](support-matrix.json). Edit that file, then run `python3 docs/support-matrix.py --write`.',
        '',
        'Package ABI and standing: [core status](core-status.md). Build steps: [misteross cores](../sources/misteross/docs/cores.md).',
        '',
        '## Legend',
        '',
        f'Tiers are derived from signals in the JSON (as of {AS_OF}). Catalog standing is recorded and is not a tier. Owned RTL is a commit that changes that core\'s `rtl/` and names the core, or that core holds at least half of the commit\'s RTL files and at least two files. A one-file edit inside another core\'s commit does not count. A recorded override from {OVERRIDE_REASON} replaces the derived tier.',
        '',
        'Coleco is Aging: work toward Current is in progress and still needs work. Spectrum and Atari ST stay Current; their kit HIL pass on the latest RTL is pending and expected soon. C64 NTSC and Atari STE are Bring-up (queued).',
        '',
        '| Tier | Meaning |',
        '| --- | --- |',
    ]
    for tier in data['tiers']:
        lines.append(f"| {cell(tier['label'])} | {cell(tier['meaning'])} |")
    lines += [
        '',
        f'**DE10-Nano** (MiSTer, Cyclone V) **OSS** is Yosys + nextpnr-mistral, the installed toolchain. i386, pc386 and PC/XT are {QUARTUS_PRIMARY} ({POLICY_CITE}). nextpnr builds there are advisory; a miss is filed as a nextpnr issue, not a blocker. A dash means this row has no DE10-Nano bitstream. pc386 and i386 are Cyclone V spikes; Quartus is still their primary toolchain.',
        '',
        f'**DE25-Nano** (Agilex 5) follows {DE25_CELL} ({POLICY_CITE}). Rows with a board bitstream show Quartus; rows without one show a dash. Agilex 5 has no OSS reverse-engineering. Per-core ports are re-checked only after the shell is on this branch.',
        '',
        '`oss` and `quartus` are the Debian-style labels in [Quartus-built cores](design/quartus-cores.md). Both labelled builds ship only when results differ by the 15% rule. No registered recipe ships both today. Bluetooth and mesh are host services, so they are not columns.',
        '',
        f'ColecoVision still uses the old media handling. Its example arcade room shows an empty-library status when no matching titles are available. Updating it is low priority and is folded into the wider Coleco work, including Adam support ({POLICY_CITE}).',
        '',
        'Atari ST is still the only optional recipe (`FES_SKIP_OPTIONAL_CORES`). That flag is not its tier.',
        '',
        '## Cores',
        '',
        '| Core | Tier | DE10-Nano | DE25-Nano | Features | Details |',
        '| --- | --- | --- | --- | --- | --- |',
    ]
    labels = {tier['id']: tier['label'] for tier in data['tiers']}
    groups = {cid: group for group in data.get('display_groups') or [] for cid in group['ids']}
    emitted = set()
    for row in data['cores']:
        group = groups.get(row['id'])
        if group:
            if group['ids'][0] != row['id'] or row['id'] in emitted:
                continue
            emitted.add(row['id'])
            members = [by for by in data['cores'] if by['id'] in group['ids']]
            lines.append('| ' + ' | '.join((
                cell(group['label']),
                labels[members[0]['tier']],
                de10_cell(members[0]),
                de25_cell(members[0]),
                feature_union(group['ids']),
                detail_cell(members[0]),
            )) + ' |')
            continue
        lines.append('| ' + ' | '.join((
            cell(row.get('display') or row['label']),
            labels[row['tier']],
            de10_cell(row),
            de25_cell(row),
            HIGHLIGHTS[row['id']],
            detail_cell(row),
        )) + ' |')
    lines += [
        '',
        '## Release scope',
        '',
        'The first release lists only v1 cores in tenfoot Home, the rooms and the core catalog view. The tenfoot Settings row Show all cores lists the rest, for development and HIL. The scope is not a tier, and v1 does not mean the exact release package has passed kit HIL.',
        '',
    ]
    for scope, title in RELEASE_SCOPES.items():
        names = [row['label'] for row in data['cores'] if row.get('release') == scope]
        lines.append(f"- **{title}:** {', '.join(names)}")
    lines += [
        '',
        '## Open',
        '',
        'These cells are still open. The wording is in [`support-matrix.json`](support-matrix.json).',
        '',
    ]
    for item in data['unverified']:
        lines.append(f"- {item['where']} — {item['cell']}")
    lines += ['', '## Needs Deano', '']
    for item in data.get('needs_deano') or []:
        lines.append(f"- {item}")
    lines.append('')
    return '\n'.join(lines)


def feature_union(ids: list[str]) -> str:
    """Group feature cell. A feature that only some models have is marked (some);
    the row's JSON features text says which."""
    order = ('tape', 'disk', 'cart', 'pad', 'keys', 'mouse', 'HDMI', 'sim')
    holders = {}
    for core_id in ids:
        for word in HIGHLIGHTS[core_id].split(', '):
            if word != '—':
                holders[word] = holders.get(word, 0) + 1
    chosen = [word if holders[word] == len(ids) else word + ' (some)'
              for word in order if word in holders]
    return ', '.join(chosen) if chosen else '—'


def _pins(path: Path) -> dict:
    data = tomllib.loads(path.read_text())
    tools = data['tool']
    return {name: tools[name]['commit'][:8] for name in ('yosys', 'mistral', 'nextpnr')}


RELEASE_SCOPES = {'v1': 'v1', 'v2': 'v2', 'held-back': 'Held back'}


def release_row(row: dict) -> bool:
    """A package row other than the idle menu records its release scope."""
    return row['id'].startswith('fes.') and row['id'] != 'fes.menu'


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
    # this branch; this check does not inspect them.
    (root / DE25).is_dir()
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
        recorded = row.get('quartus_lock' if recipe.get('toolchain') == 'quartus' else 'oss_lock') or {}
        if recorded.get('path') != str(Path('sources/misteross') / recipe['lock_path']):
            errors.append(f'{core_id} lock path does not match config/core-recipes.toml')
        elif lock.is_file() and recipe.get('toolchain') == 'quartus':
            descriptor = tomllib.loads(lock.read_text())
            if descriptor.get('version') != recipe['quartus']['version']:
                errors.append(f'{core_id} Quartus version differs from its lock')
        elif lock.is_file():
            pins = _pins(lock)
            for name, pin in pins.items():
                if recorded.get(name) != pin:
                    errors.append(f'{core_id} {name} pin {recorded.get(name)} != lock {pin}')
        else:
            errors.append(f'{core_id} lock file is missing: {lock}')

    for row in rows:
        if release_row(row) and row.get('release') not in RELEASE_SCOPES:
            errors.append(f"{row['id']} release {row.get('release')!r} is not one of {', '.join(RELEASE_SCOPES)}")
        if not release_row(row) and 'release' in row:
            errors.append(f"{row['id']} is not a release package but records release")
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
        recorded = by_id[core_id].get('quartus') or ''
        if QUARTUS_PRIMARY not in recorded or POLICY_CITE not in recorded:
            errors.append(f'{core_id} JSON toolchain is missing the Quartus-primary policy')
    ids = {row['id'] for row in rows}
    if set(HIGHLIGHTS) != ids:
        errors.append(
            'highlight keys '
            f'missing {sorted(ids - set(HIGHLIGHTS))} extra {sorted(set(HIGHLIGHTS) - ids)}'
        )
    if set(DETAILS) != ids:
        errors.append(
            'detail keys '
            f'missing {sorted(ids - set(DETAILS))} extra {sorted(set(DETAILS) - ids)}'
        )
    if len(data['tiers']) != len(tier_ids):
        errors.append('duplicate tier ids')
    for row in rows:
        if row.get('tier_role') not in TIER_ROLES:
            errors.append(f"{row['id']} tier_role is missing or unknown")
        sig = row.get('signals') or {}
        if sig.get('as_of') != AS_OF or sig.get('window_start') != WINDOW_START or sig.get('recent_cutoff') != RECENT_CUTOFF:
            errors.append(f"{row['id']} signal window does not match the checker")
        if sig.get('media_gen') not in MEDIA_GENS:
            errors.append(f"{row['id']} media_gen is missing or unknown")
        if sig.get('catalog_weight') != 'weak':
            errors.append(f"{row['id']} catalog standing must stay a weak signal")
        if sig.get('catalog_standing') != row.get('library_standing'):
            errors.append(f"{row['id']} catalog signal does not match library_standing")
        derived = derive_tier(row)
        override = row.get('tier_override')
        if override:
            if override.get('reason') != OVERRIDE_REASON:
                errors.append(f"{row['id']} tier override reason is not {OVERRIDE_REASON}")
            if override.get('tier') not in tier_ids:
                errors.append(f"{row['id']} tier override {override.get('tier')} is not a displayed tier")
            if not str(override.get('note') or '').strip():
                errors.append(f"{row['id']} tier override has no note")
            if row.get('tier') != override.get('tier'):
                errors.append(f"{row['id']} tier {row.get('tier')} != override {override.get('tier')}")
        elif row.get('tier') != derived:
            errors.append(f"{row['id']} tier {row.get('tier')} != derived {derived}")
        if derived == 'unresolved' and not override:
            errors.append(f"{row['id']} derives to unresolved and has no override")
        expected = effective_tier(row)
        flipped = dict(row)
        flipped['library_standing'] = 'supported' if row.get('library_standing') != 'supported' else 'experimental'
        flipped_sig = dict(sig)
        flipped_sig['catalog_standing'] = flipped['library_standing']
        flipped['signals'] = flipped_sig
        if derive_tier(flipped) != derived:
            errors.append(f"{row['id']} derived tier changes when catalog standing changes")
        if effective_tier(flipped) != expected:
            errors.append(f"{row['id']} displayed tier changes when catalog standing changes")
        hil = sig.get('hil_note')
        if hil and not (root / hil).is_file():
            errors.append(f"{row['id']} hil note is missing: {hil}")
        image_role = sig.get('image_role')
        if row.get('in_bootstrap_image') and image_role != 'bootstrap':
            errors.append(f"{row['id']} is in the bootstrap image but image_role is {image_role}")
        if not row.get('in_bootstrap_image') and image_role == 'bootstrap':
            errors.append(f"{row['id']} claims the bootstrap image")
    seen_group = []
    for group in data.get('display_groups') or []:
        members = [by_id[cid] for cid in group['ids'] if cid in by_id]
        if len(members) != len(group['ids']):
            errors.append(f"display group {group.get('label')} names an unknown core")
            continue
        tiers = {member['tier'] for member in members}
        boards = {de10_cell(member) for member in members}
        if len(tiers) != 1 or len(boards) != 1:
            errors.append(f"display group {group.get('label')} mixes tiers or DE10 cells")
        tokens = label_tokens(group.get('label') or '')
        named = set()
        for cid in group['ids']:
            if cid in seen_group:
                errors.append(f'{cid} is in two display groups')
            seen_group.append(cid)
            row_variant = by_id[cid].get('variant')
            if not row_variant:
                errors.append(f'{cid} is grouped but has no variant name')
            elif row_variant not in tokens:
                errors.append(f"{cid} variant {row_variant} is not in the group label")
            else:
                named.add(row_variant)
            for item in by_id[cid].get('variants') or []:
                name = item.get('name')
                kind = item.get('kind')
                if kind not in VARIANT_KINDS or not name:
                    errors.append(f'{cid} variant entry needs a name and kind')
                    continue
                named.add(name)
                if kind == 'rom' and name not in tokens:
                    errors.append(f'{cid} ROM variant {name} is not in the group label')
        for token in tokens:
            if token not in named:
                errors.append(f"{group.get('label')} lists {token} without a variant")
    for core_id, highlight in HIGHLIGHTS.items():
        if len(highlight) > 40 or '|' in highlight or '`' in highlight:
            errors.append(f'{core_id} highlight is not a short feature cell')
    for core_id, (path, _title) in DETAILS.items():
        if not (root / path).is_file():
            errors.append(f'{core_id} detail doc is missing: {path}')
    for row in rows:
        if row.get('tier_role') != 'shared' or not row.get('paths'):
            continue
        signals = row.get('signals') or {}
        try:
            owned = owned_rtl(root, row)
        except (OSError, subprocess.CalledProcessError) as error:
            errors.append(f"{row['id']} owned-RTL history is unreadable: {error}")
            continue
        recorded = signals.get('last_rtl_change')
        if owned is None:
            if recorded or signals.get('last_rtl_sha'):
                errors.append(f"{row['id']} records owned RTL {signals.get('last_rtl_sha')} but none qualifies in the window")
        else:
            recorded_sha = signals.get('last_rtl_sha') or ''
            recorded_subject = signals.get('last_rtl_subject')
            if (
                recorded != owned[1]
                or not recorded_sha
                or not owned[0].startswith(recorded_sha)
                or (recorded_subject and recorded_subject != owned[2])
            ):
                errors.append(
                    f"{row['id']} owned RTL is {owned[0]} on {owned[1]} ({owned[2]}), "
                    f"recorded {recorded_sha or '<missing>'} on "
                    f"{recorded or '<missing>'} ({recorded_subject or '<missing>'})"
                )
    try:
        rendered = render(data)
    except (IndexError, KeyError, TypeError) as error:
        rendered = None
        errors.append(f'human page cannot be rendered: {error}')
    if rendered is not None:
        if rendered.count(DE25_CELL) != 1:
            errors.append('DE25 policy line must appear once in the human page')
        if rendered.count(QUARTUS_PRIMARY) != 1:
            errors.append('Quartus-primary policy line must appear once in the human page')
        for phrase in (
            POLICY_CITE,
            'empty-library status',
            'shell pending',
            'Bring-up (queued)',
            'Spectrum (48K, 128K, +2, +2A/+3, Pentagon)',
            'C64 (NTSC)',
            'Atari ST (STE)',
        ):
            if phrase not in rendered:
                errors.append(f'human page is missing {phrase!r}')
        if 'Unresolved' in rendered:
            errors.append('human page still shows the Unresolved tier')
    if any(row['id'] == 'fes.spectrum-plus2' for row in rows):
        errors.append('fes.spectrum-plus2 is not a separate core')
    plus2 = next((
        item for item in (by_id.get('fes.spectrum128') or {}).get('variants') or []
        if item.get('name') == '+2'
    ), None)
    if not plus2 or plus2.get('kind') != 'rom':
        errors.append('fes.spectrum128 must record +2 as a ROM variant')
    matrix_text = json.dumps(data)
    for phrase in ('no distinct model', 'no plus2 model', 'has no model', 'Distinct model', 'lacks a model'):
        if (rendered is not None and phrase in rendered) or phrase in matrix_text:
            errors.append(f'support matrix still says {phrase!r}')
    if rendered is not None and ('toolchain.lock' in rendered or '## Evidence' in rendered):
        errors.append('human page still carries locks or an evidence table')
    for row in rows:
        lock = row.get('oss_lock') or {}
        for name in ('yosys', 'nextpnr', 'mistral'):
            pin = lock.get(name)
            if pin and rendered is not None and pin in rendered:
                errors.append(f"{row['id']} {name} pin leaked into the human page")
        if de10_cell(row) == 'Quartus' and row['id'] not in QUARTUS_PRIMARY_IDS:
            errors.append(f"{row['id']} DE10 cell says Quartus without the primary policy")
        if row['id'] in QUARTUS_PRIMARY_IDS and row['board'] == 'de10' and de10_cell(row) != 'Quartus':
            errors.append(f"{row['id']} DE10 cell should say Quartus")
    for item in data.get('unverified') or []:
        detail = item.get('detail') or ''
        if len(detail) > 80 and rendered is not None and detail[:80] in rendered:
            errors.append(f"open cell {item.get('cell')!r} still pastes its long detail")
    if rendered is not None:
        for line in rendered.splitlines():
            if line.startswith('| ') and not line.startswith('| ---') and len(line) > 180:
                errors.append(f'human table row is too long ({len(line)}): {line[:80]}')
    policy = '\n'.join(data.get('policy') or [])
    for phrase in (POLICY_CITE, QUARTUS_PRIMARY, DE25_CELL, 'empty-library status'):
        if phrase not in policy:
            errors.append(f'policy section is missing {phrase!r}')
    coleco = by_id.get('fes.coleco') or {}
    coleco_notes = coleco.get('notes') or ''
    for phrase in (POLICY_CITE, 'empty-library status', 'low priority', 'Adam'):
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
    if errors:
        parser.exit(1, 'support matrix:\n' + '\n'.join(errors) + '\n')
    rendered = render(data)
    target = root / MARKDOWN
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
