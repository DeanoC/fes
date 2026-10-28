#!/usr/bin/env python3
"""Classify whether a pull request needs a full image for hardware testing."""

import argparse
import ast
import json
from pathlib import Path
import re
import subprocess
import sys


RULES = [
    ('scripts/parent_affected.py', 'none', None),
    # Development/checking tools produce no deployed bytes. Keep this list
    # explicit: build, image and package producers below still need their
    # artifact-specific hardware plan.
    *[(path, 'none', None) for path in (
        'scripts/affected.py', 'scripts/test_changed.py', 'scripts/ci_gate.py',
        'scripts/ci_simulations.py', 'scripts/ci_verilator.sh',
        'scripts/test_policy.py', 'scripts/parent_tests.py', 'scripts/host_tests.py',
        'scripts/check_deadcode.py', 'scripts/check_diff.py',
        'scripts/check_doc_links.py', 'scripts/coordination.py', 'scripts/de25_runtime.py',
        'scripts/de25_boot_contract.py', 'scripts/de25_dma_descriptor.py', 'scripts/de25_kernel_test.py',
        'scripts/hil_plan.py', 'scripts/consistency.py', 'scripts/generate.py',
        'scripts/host_affected.py',
    )],
    # This driver is exercised only in a private QEMU kernel. No appliance,
    # image or software bundle installs or enables it.
    ('image/kernel/de25-dma/**', 'none', None),
    # This separate headless image needs a DE25 serial/SSH boot check. It is
    # never installed as a DE10 kit software bundle or native-integration image.
    ('image/de25-nano/**', 'full', 'de25-nano-linux'),
    ('image/**', 'full', 'image'), ('boot-media.lock.toml', 'full', 'boot-media'),
    ('containers/**', 'full', 'boot-media'), ('profiles/**', 'full', 'profile'),
    ('config/**', 'full', 'config'), ('Makefile', 'full', 'build-system'),
    ('scripts/build.py', 'full', 'build-system'),
    ('scripts/environment.py', 'full', 'build-system'),
    ('scripts/media*.py', 'full', 'boot-media'), ('platform/**', 'full', 'platform'),
    ('sources/FogCast/build/**', 'full', 'image'),
    ('sources/FogCast/Makefile', 'full', 'build-system'),
    ('docs/**', 'none', None), ('**/docs/**', 'none', None), ('**/*.md', 'none', None),
    ('tests/**', 'none', None), ('sources/*/tests/**', 'none', None),
    ('**/*_test.go', 'none', None), ('**/testdata/**', 'none', None),
    ('examples/**', 'none', None), ('AGENTS.md', 'none', None),
    ('.github/**', 'none', None), ('.superpowers/**', 'none', None),
    ('**/.superpowers/**', 'none', None),
    ('sources/misteross/cores/*/sim/**', 'none', None),
    ('sources/libmister-runtime/tests/**', 'none', None),
    ('sources/misteross/scripts/sim_*', 'none', None),
    # Standalone DE25-Nano bring-up has no image, bundle or package consumer.
    # Its manual DE25 JTAG smoke check is separate from deployed DE10 kit HIL.
    ('sources/mister-packages/packages/board/de25_nano.yaml', 'none', None),
    ('sources/mister-packages/packages/fabric/fes_fabric_de25_ddr_probe.yaml', 'none', None),
    ('sources/misteross/boards/de25nano/**', 'none', None),
    # These exact validation/generation tools have no deployed binary consumer.
    # A changed emitted artifact is classified through its actual consumer path.
    ('sources/mister-packages/cmd/mister-packages/main.go', 'none', None),
    ('sources/mister-packages/internal/pack/board.go', 'none', None),
    ('sources/mister-packages/internal/pack/load.go', 'none', None),
    ('sources/mister-packages/**', 'full', 'package-format'),
    ('sources/libmister-runtime/**', 'overlay', 'mister-runtime'),
    ('sources/FogCast/cmd/mister-agent/**', 'overlay', 'mister-agent'),
    ('sources/FogCast/cmd/fogcast-kit/**', 'overlay', 'fogcast-kit'),
    ('sources/FogCast/cmd/fogcast-tenfoot/**', 'overlay', 'fogcast-tenfoot'),
    ('sources/FogCast/cmd/fogcast-api/**', 'overlay', 'host:fogcast-api'),
    ('sources/FogCast/cmd/fogcast/**', 'overlay', 'host:fogcast'),
    # The Spectrum simulations' restore-stream and disk-image writers; no
    # image, bundle or package ships them, and the host converts snapshots and
    # disks in fogcast-api.
    ('sources/FogCast/cmd/fes-spectrum-restore/**', 'none', None),
    ('sources/FogCast/cmd/fes-spectrum-disk/**', 'none', None),
    ('sources/FogCast/cmd/*/**', 'full', 'fogcast-unmapped-command'),
    # ui/** is compiled into fogcast-kit, fogcast-tenfoot and fogcast-api, so it
    # falls through to kit-go+host below.
    # Imported only by cmd/target-image-lock, which no overlay artifact runs.
    ('sources/FogCast/internal/targetimage/**', 'full', 'image-selector'),
    # Imported only by cmd/fes-update, the host appliance update client, to read
    # a release image's software base record. No overlay artifact runs it.
    ('sources/FogCast/internal/ext4image/**', 'full', 'fogcast-unmapped-command'),
    # The immutable bundle producer and stable base supervisor share this code;
    # replacing the Go kit services cannot validate or replace those consumers.
    ('sources/FogCast/appliance/software/**', 'full', 'kit-software'),
    # Release confirm and the boot-ticket rewrite. On a trial boot the image's
    # mister-agent runs these; fes-kit-software does not open the bundle store.
    ('sources/FogCast/appliance/store/**', 'full', 'appliance-trial'),
    # The agent's confirm reconcile and boot-ticket repair, linked into that
    # same image agent (and into /usr/sbin/fes-kit-data for the ticket read).
    ('sources/FogCast/internal/applianceupdate/**', 'full', 'appliance-trial'),
    # FAT splash publish. On an fes-update trial boot the image's mister-agent
    # refreshes idle.rbf when the trial is confirmed and writes the restored
    # image's splash when a rollback is confirmed. The store prunes that staged
    # splash with the image. fes-kit-software does not start the bundle agent
    # for that boot, so a bundle install cannot exercise this package.
    ('sources/FogCast/internal/appliancesplash/**', 'full', 'appliance-trial'),
    # The fes-kit-data storage gate. It runs as the image's /usr/sbin/fes-kit-data,
    # which refuses the bundle store while the boot is still a trial.
    ('sources/FogCast/internal/appliancedata/**', 'full', 'storage-gate'),
    # Appliance packages linked into an image-only binary. A bundle cannot
    # replace fes-boot (platform/cmd/fes-boot), fes-kit-data
    # (sources/FogCast/cmd/fes-kit-data), or the image's trial-startup
    # supervisor (platform/cmd/fes-kit-software). go list -deps on those three
    # reports the imports below. store and software are already full above.
    # Overlay code also imports these packages; missing the image binary is
    # worse than retesting an overlay edit on a full image. *_test.go is none.
    #   github.com/DeanoC/FogCast/appliance            fes-boot, fes-kit-data
    #     (sources/FogCast/appliance/*.go; release.go is the only non-test file)
    #   github.com/DeanoC/FogCast/appliance/blockprobe fes-boot, fes-kit-data,
    #                                                  fes-kit-software
    #   github.com/DeanoC/FogCast/appliance/softwarecontrol
    #                                                  fes-kit-software, and
    #                                                  fes-kit-data via
    #                                                  applianceupdate -> agent -> protocol
    ('sources/FogCast/appliance/*.go', 'full', 'appliance-trial'),
    ('sources/FogCast/appliance/blockprobe/**', 'full', 'appliance-trial'),
    ('sources/FogCast/appliance/softwarecontrol/**', 'full', 'kit-software'),
    ('sources/FogCast/**', 'overlay', 'kit-go+host'),
    ('sources/misteross/toolchain.lock', 'overlay', 'core:ALL'),
    ('sources/misteross/toolchains/*.lock', 'overlay', 'core:{lock}'),
    ('sources/misteross/cores/fes-common/**', 'overlay', 'core:ALL'),
    # The splash produces the boot /idle.rbf, not a .fcore.
    ('sources/misteross/cores/fes-splash/**', 'full', 'image-splash'),
    ('sources/misteross/scripts/build_fes_splash*', 'full', 'image-splash'),
    # Video parts ship as separate part archives the overlay cannot attest.
    ('sources/misteross/scripts/*video_part*', 'full', 'video-part'),
    ('sources/misteross/cores/*/**', 'overlay', 'core:{core}'),
    ('sources/misteross/scripts/build_fes_*', 'overlay', 'core:{script}'),
    ('sources/misteross/scripts/**', 'overlay', 'core:ALL'),
    ('sources/misteross/boards/**', 'overlay', 'core:ALL'),
    # The sealed splash is the boot /idle.rbf, owned by the image, not a core package.
    ('sources/misteross/sealed/**', 'full', 'image-splash'),
    ('sources/misteross/Makefile', 'overlay', 'core:ALL'),
]
RANK = {'none': 0, 'overlay': 1, 'full': 2}
SHA40 = re.compile(r'^[0-9a-fA-F]{40}$')


def glob_matches(path, pattern):
    """Match the small glob syntax used by the classifier rules."""
    pieces = []
    index = 0
    while index < len(pattern):
        if pattern.startswith('**/', index):
            pieces.append('(?:.*/)?')
            index += 3
        elif pattern.startswith('**', index):
            pieces.append('.*')
            index += 2
        elif pattern[index] == '*':
            pieces.append('[^/]*')
            index += 1
        elif pattern[index] == '?':
            pieces.append('[^/]')
            index += 1
        else:
            pieces.append(re.escape(pattern[index]))
            index += 1
    return re.fullmatch(''.join(pieces), path) is not None


def classify_path(path):
    path = path.replace('\\', '/')
    for pattern, category, component in RULES:
        if glob_matches(path, pattern):
            if component and '{' in component:
                if component == 'core:{lock}':
                    name = Path(path).stem
                elif component == 'core:{core}':
                    name = path.split('/')[3].removeprefix('fes-')
                else:
                    name = Path(path).stem.removeprefix('build_').removeprefix('fes_')
                    name = re.sub(r'_video_part.*$', '', name)
                    name = re.sub(r'_(?:socket_dev|socket_v\d+|quartus|package|oss)$', '', name)
                    name = name.replace('_', '-')
                component = 'core:' + name
            return {'path': path, 'class': category, 'component': component,
                    'rule': pattern}
    return {'path': path, 'class': 'full', 'component': 'UNRECOGNISED',
            'rule': '(no rule; fail-safe)'}


CORE_RULES = ('sources/misteross/cores/*/**', 'sources/misteross/cores/fes-common/**')
MISTEROSS = 'sources/misteross/'
SCRIPTS_DIR = MISTEROSS + 'scripts/'


def script_texts(repo=None, head=None):
    """Read misteross scripts at head (or this checkout) for consumer lookup."""
    if repo and head:
        names = subprocess.run(['git', '-C', str(repo), 'ls-tree', '--name-only', head, '--', SCRIPTS_DIR],
                               text=True, capture_output=True, check=True).stdout.split()
        return {name: subprocess.run(['git', '-C', str(repo), 'show', f'{head}:{name}'],
                                     text=True, capture_output=True, check=True).stdout
                for name in names if name.endswith('.py')}
    root = Path(__file__).resolve().parents[1]
    return {path.relative_to(root).as_posix(): path.read_text()
            for path in sorted((root / SCRIPTS_DIR).glob('*.py'))}


class ScriptModule:
    """Top-level names of one misteross script and what each one reads.

    Every top-level definition (constant, function, class) records the string
    literals inside it and the names it uses: local top-level names, and names
    from other repo scripts. Statements that are not definitions run at import
    time, so they belong to the module's own reads (MODULE)."""

    MODULE = '<module>'

    def __init__(self, name, text, modules):
        self.name = name
        self.unresolved = []
        self.aliases = {}
        self.defs = {self.MODULE: (set(), set())}
        self.dynamic = set()
        try:
            tree = ast.parse(text)
        except SyntaxError as error:
            self.unresolved.append(f'{name}: cannot parse ({error.msg})')
            return
        for node in ast.walk(tree):
            if isinstance(node, (ast.Import, ast.ImportFrom)):
                self._import(node, modules)
            elif isinstance(node, ast.Call) and self._dynamic_import(node):
                argument = node.args[0] if node.args else None
                if not (isinstance(argument, ast.Constant) and isinstance(argument.value, str)):
                    self.unresolved.append(f'{name}: dynamic import with a computed name')
                else:
                    module = self._module_name(argument.value, modules, 'dynamic import')
                    if module is not None:
                        self.dynamic.add(module)
        for statement in tree.body:
            names = self._defined(statement)
            for target in names or (self.MODULE,):
                literals, uses = self.defs.setdefault(target, (set(), set()))
                self._collect(statement, literals, uses)
        self.defs[self.MODULE][1].update(('module', module) for module in self.dynamic)

    def imported(self):
        """Every misteross script module this one imports, by any form."""
        result = set(self.dynamic)
        for alias in self.aliases.values():
            if alias[0] == 'package':
                result |= alias[1]
            else:
                result.add(alias[1])
        for _, uses in self.defs.values():
            result.update(use[1] for use in uses if use[0] != 'local')
        return result

    @staticmethod
    def _dynamic_import(node):
        function = node.func
        if isinstance(function, ast.Name):
            return function.id in ('__import__', 'import_module')
        return isinstance(function, ast.Attribute) and function.attr == 'import_module'

    @staticmethod
    def _defined(statement):
        if isinstance(statement, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
            return [statement.name]
        if isinstance(statement, ast.Assign) and all(isinstance(target, ast.Name) for target in statement.targets):
            return [target.id for target in statement.targets]
        if isinstance(statement, ast.AnnAssign) and isinstance(statement.target, ast.Name):
            return [statement.target.id]
        return []

    def _module_name(self, dotted, modules, kind):
        """Map an import to a repo script module, None for an outside module."""
        parts = dotted.split('.')
        if parts[0] == 'scripts':
            if len(parts) == 2 and parts[1] in modules:
                return parts[1]
            self.unresolved.append(f'{self.name}: {kind} {dotted} is not a misteross script module')
            return None
        if parts[0] in modules:
            if len(parts) == 1:
                return parts[0]
            self.unresolved.append(f'{self.name}: {kind} {dotted} is not a misteross script module')
        return None

    def _import(self, node, modules):
        if isinstance(node, ast.Import):
            for alias in node.names:
                module = self._module_name(alias.name, modules, 'import')
                if module is None:
                    continue
                if alias.asname:
                    self.aliases[alias.asname] = ('module', module)
                elif alias.name == module:
                    self.aliases[module] = ('module', module)
                else:
                    # import scripts.X binds "scripts"; scripts.X.NAME reads all of X.
                    self.aliases.setdefault('scripts', ('package', set()))[1].add(module)
            return
        if node.level > 1:
            self.unresolved.append(f'{self.name}: relative import from {"." * node.level}{node.module or ""}')
            return
        # The scripts run as the "scripts" package, so ".X" is scripts.X.
        dotted = ('scripts.' + node.module if node.module else 'scripts') if node.level else node.module
        if dotted == 'scripts':
            for alias in node.names:
                module = self._module_name('scripts.' + alias.name, modules, 'import')
                if module is not None:
                    self.aliases[alias.asname or alias.name] = ('module', module)
            return
        module = self._module_name(dotted, modules, 'import from')
        if module is None:
            return
        for alias in node.names:
            if alias.name == '*':
                self.defs[self.MODULE][1].add(('module', module))
            else:
                self.aliases[alias.asname or alias.name] = ('name', module, alias.name)

    def _collect(self, statement, literals, uses):
        attribute_bases = set()
        for node in ast.walk(statement):
            if isinstance(node, (ast.Import, ast.ImportFrom)):
                # Importing runs the module's top-level statements and binds names
                # even when nothing else here uses them.
                for alias in node.names:
                    bound = self.aliases.get(alias.asname or alias.name.split('.')[0])
                    if bound is None:
                        continue
                    if bound[0] == 'name':
                        uses.add(bound)
                    elif bound[0] == 'module':
                        uses.add(('name', bound[1], self.MODULE))
                    else:
                        uses.update(('name', module, self.MODULE) for module in bound[1])
            elif isinstance(node, ast.Constant) and isinstance(node.value, str):
                literals.add(node.value)
            elif isinstance(node, ast.Attribute) and isinstance(node.value, ast.Name):
                alias = self.aliases.get(node.value.id)
                if alias and alias[0] == 'module':
                    uses.add(('name', alias[1], node.attr))
                    attribute_bases.add(id(node.value))
        for node in ast.walk(statement):
            if not isinstance(node, ast.Name) or id(node) in attribute_bases:
                continue
            alias = self.aliases.get(node.id)
            if alias is None:
                uses.add(('local', node.id))
            elif alias[0] == 'package':
                uses.update(('module', module) for module in alias[1])
            else:
                # A bare module alias (passed around, getattr) may read any of its names.
                uses.add(alias)


def script_modules(texts):
    names = {Path(script).stem: script for script in texts}
    return {stem: ScriptModule(stem, texts[script], names) for stem, script in names.items()}


def script_reads(modules, stem):
    """Every literal a script can read through its own text and the repo scripts it imports."""
    literals = set()
    seen = set()
    pending = [('module', stem)]
    while pending:
        item = pending.pop()
        if item in seen:
            continue
        seen.add(item)
        module = modules.get(item[1])
        if module is None:
            continue
        if item[0] == 'module':
            names = list(module.defs)
        else:
            names = [ScriptModule.MODULE]
            if item[2] in module.defs:
                names.append(item[2])
            elif item[2] in module.aliases:
                # Re-exported import, e.g. demo.board.NAME.
                pending.append(module.aliases[item[2]])
            else:
                # Unknown name (e.g. set by a module-level loop): read the whole module.
                names = list(module.defs)
        for name in names:
            text_literals, uses = module.defs[name]
            literals |= text_literals
            for use in uses:
                if use[0] == 'local':
                    if use[1] in module.defs:
                        pending.append(('name', module.name, use[1]))
                    elif use[1] in module.aliases:
                        pending.append(module.aliases[use[1]])
                else:
                    pending.append(use)
    return literals


def core_consumers(path, texts):
    """Classify every script that reads this core file or one of its directories.

    A script reads a path when the path is a string literal in its own text or in
    any repo script name it uses through an import, transitively. An import that
    cannot be resolved to a misteross script fails closed as a full image."""
    relative = path.removeprefix(MISTEROSS)
    parts = relative.split('/')
    candidates = {'/'.join(parts[:index]) for index in range(3, len(parts) + 1)}
    modules = script_modules(texts)
    found = []
    for stem, module in sorted(modules.items()):
        row = classify_path(SCRIPTS_DIR + stem + '.py')
        if row['class'] == 'none':
            continue
        if module.unresolved:
            found.append(unresolved_row(stem, module))
            continue
        if any(literal.rstrip('/') in candidates for literal in script_reads(modules, stem)):
            found.append(row)
    return found


def unresolved_row(stem, module):
    return {'path': SCRIPTS_DIR + stem + '.py', 'class': 'full',
            'component': 'unresolved-script-import', 'rule': module.unresolved[0]}


def script_importers(path, texts):
    """Classify every script that imports this script module, directly or transitively.

    Any change to a module can change what its importers produce, so this works on
    whole modules. An unresolvable import anywhere fails closed as a full image."""
    changed = Path(path).stem
    modules = script_modules(texts)
    found = []
    for stem, module in sorted(modules.items()):
        row = classify_path(SCRIPTS_DIR + stem + '.py')
        if stem == changed or row['class'] == 'none':
            continue
        if module.unresolved:
            found.append(unresolved_row(stem, module))
            continue
        seen, pending = set(), list(module.imported())
        while pending:
            current = pending.pop()
            if current not in seen and current in modules:
                seen.add(current)
                pending.extend(modules[current].imported())
        if changed in seen:
            found.append(row)
    return found


def is_script_module(path):
    name = path.removeprefix(SCRIPTS_DIR)
    return path.startswith(SCRIPTS_DIR) and '/' not in name and name.endswith('.py')


def expand(row, consumers):
    """Fold the scripts that consume a changed file into its plan row."""
    full = [item for item in consumers if item['class'] == 'full']
    if full:
        # A full-image producer (splash, video part) or an unresolved import.
        row.update({'class': 'full', 'component': full[0]['component'],
                    'rule': row['rule'] + ' <- ' + full[0]['path']})
        return
    if row['class'] == 'none' and consumers:
        row.update({'class': 'overlay', 'component': consumers[0]['component'],
                    'rule': row['rule'] + ' <- ' + consumers[0]['path']})
    if row['class'] == 'overlay' and row['component'] != 'core:ALL':
        row['consumers'] = sorted({item['component'] for item in consumers} - {row['component']})


def plan(paths, texts=None):
    rows = [classify_path(path) for path in paths if path.strip()]
    for row in rows:
        if row['class'] == 'overlay' and row['rule'] in CORE_RULES:
            if texts is None:
                texts = script_texts()
            expand(row, core_consumers(row['path'], texts))
        elif row['class'] != 'full' and is_script_module(row['path']):
            # A changed script also changes every producer that imports it.
            if texts is None:
                texts = script_texts()
            expand(row, script_importers(row['path'], texts))
    worst = max((row['class'] for row in rows), key=lambda item: RANK[item], default='none')
    components = sorted({component for row in rows
                         if row['class'] == 'overlay' and row['component']
                         for component in (row['component'], *row.get('consumers', ()))})
    if worst == 'full':
        decision = 'FULL_IMAGE'
    elif worst == 'overlay':
        decision = 'OVERLAY ' + ' '.join(components)
    else:
        decision = 'NO_DEPLOY_CHANGE'
    return {'rows': rows, 'class': worst, 'components': components,
            'decision': decision}


def full_sha(value, label):
    if not SHA40.fullmatch(value):
        raise ValueError(f'{label} must be a full 40-hex SHA')
    return value.lower()


def git_commit(repo, sha):
    sha = full_sha(sha, 'commit')
    result = subprocess.run(['git', '-C', str(repo), 'rev-parse', '--verify',
                             '--end-of-options', sha + '^{commit}'],
                            text=True, capture_output=True)
    if result.returncode:
        raise ValueError(f'commit does not resolve: {sha}')
    return result.stdout.strip()


def repo_paths(repo, base, head):
    base = git_commit(repo, base)
    head = git_commit(repo, head)
    result = subprocess.run(['git', '-C', str(repo), 'diff', '--name-only',
                             '--no-renames', base, head],
                            text=True, capture_output=True, check=True)
    return result.stdout.splitlines()


def optional_commit(repo, rev):
    """Return a commit SHA, or None when rev does not resolve."""
    result = subprocess.run(['git', '-C', str(repo), 'rev-parse', '--verify', '--quiet',
                             '--end-of-options', rev + '^{commit}'],
                            text=True, capture_output=True)
    if result.returncode:
        return None
    return result.stdout.strip()


def pull_request_base(repo, head):
    """Return a recoverable PR base, preferring origin/main over local main.

    Local main is a fallback when origin/main is unavailable or disconnected
    from head. If the preferred ref already contains head, there is no separate
    PR range to recover; callers then classify the image commit through head.
    """
    head = git_commit(repo, head)

    def merge_base(ref):
        result = subprocess.run(['git', '-C', str(repo), 'merge-base', ref, head],
                                text=True, capture_output=True)
        if result.returncode or not result.stdout.strip():
            return None
        return result.stdout.strip()

    remote_main = optional_commit(repo, 'origin/main')
    if remote_main is not None:
        base = merge_base(remote_main)
        if base == head:
            # When origin/main already contains the supplied PR head, that is
            # not a separate PR range; use the caller's image-to-head fallback.
            return None
        if base is not None:
            return base

    local_main = optional_commit(repo, 'main')
    if local_main is None:
        return None
    base = merge_base(local_main)
    return None if base is None or base == head else base


def image_gap(repo, image, pr_base):
    """Describe commits between the booted image and the pull-request base."""
    image = git_commit(repo, image)
    pr_base = git_commit(repo, pr_base)
    if image == pr_base:
        return None
    count = subprocess.run(['git', '-C', str(repo), 'rev-list', '--count',
                            image + '..' + pr_base],
                           text=True, capture_output=True, check=True)
    ancestor = subprocess.run(['git', '-C', str(repo), 'merge-base', '--is-ancestor',
                               image, pr_base],
                              text=True, capture_output=True)
    commits = int(count.stdout.strip() or '0')
    paths = repo_paths(repo, image, pr_base)
    if commits == 0 and not paths:
        return None
    return {'base_image_commit': image, 'pr_base': pr_base, 'commits': commits,
            'relation': 'behind' if ancestor.returncode == 0 else 'diverged',
            'paths': paths}


def show(plan_data, as_json=False):
    if as_json:
        print(json.dumps(plan_data, indent=2))
        return
    gap = plan_data.get('image_gap')
    if gap:
        if gap['relation'] == 'behind':
            print(f"WARNING: kit image {gap['base_image_commit']} is {gap['commits']} "
                  f"commit(s) behind PR base {gap['pr_base']}; these paths differ "
                  f"and are not part of the decision:")
        else:
            print(f"WARNING: kit image {gap['base_image_commit']} has diverged from "
                  f"PR base {gap['pr_base']}; these paths differ and are not part of the decision:")
        for path in gap['paths']:
            print('  ' + path)
    for row in plan_data['rows']:
        print(f"{row['class']:7} {row['component'] or '-':22} "
              f"{row['path']} [{row['rule']}]")
    print('DECISION: ' + plan_data['decision'])



def parser():
    root = argparse.ArgumentParser(description=__doc__)
    sub = root.add_subparsers(dest='command', required=True)
    classify = sub.add_parser('classify')
    classify.add_argument('--repo')
    classify.add_argument('--base-image-commit')
    classify.add_argument('--head')
    classify.add_argument('--paths-file')
    classify.add_argument('--strict', action='store_true',
                          help='classify --base-image-commit through --head, including commits before the PR base')
    classify.add_argument('--json', action='store_true')
    return root


def main(argv=None):
    args = parser().parse_args(argv)
    try:
        texts = None
        gap = None
        if args.paths_file:
            paths = Path(args.paths_file).read_text().splitlines()
        elif args.repo and args.base_image_commit and args.head:
            head = git_commit(args.repo, args.head)
            image = git_commit(args.repo, args.base_image_commit)
            base = image
            if not args.strict:
                pr_base = pull_request_base(args.repo, head)
                if pr_base is not None:
                    base = pr_base
                    gap = image_gap(args.repo, image, pr_base)
            paths = repo_paths(args.repo, base, head)
            texts = script_texts(args.repo, head)
        else:
            raise ValueError('provide --paths-file or all of --repo, --base-image-commit, --head')
        data = plan(paths, texts)
        if gap:
            data['image_gap'] = gap
        show(data, args.json)
        return 0
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        print(str(error), file=sys.stderr)
        return 2


if __name__ == '__main__':
    sys.exit(main())
