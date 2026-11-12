#!/usr/bin/env python3
"""Classify whether a pull request needs a full image for hardware testing."""

import argparse
import ast
import fnmatch
import json
import os
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
        'scripts/check_deadcode.py', 'scripts/check_diff.py', 'scripts/changed_paths.py',
        'scripts/check_doc_links.py', 'scripts/coordination.py', 'scripts/de25_runtime.py',
        'scripts/de25_dma_descriptor.py',
        'scripts/hil_plan.py', 'scripts/hil_go_inventory.py', 'scripts/hil_go_imports.go',
        'scripts/consistency.py', 'scripts/generate.py',
        'scripts/host_affected.py', 'scripts/runtime_tests.py', 'scripts/input_diagnostic.py',
    )],
    # The native permanent-owner kernel now compiles this same DMA driver.
    # Only its disabled KUnit translation unit remains software-test-only.
    ('image/kernel/de25-dma/de25_dma_test.c', 'none', None),
    ('image/kernel/de25-dma/*.md', 'none', None),
    ('image/kernel/de25-dma/**', 'full', 'de25-nano-linux'),
    # These helpers are inputs of the native owner-image/kernel recipe too,
    # despite their original off-board/test-oriented names.
    *[(path, 'full', 'de25-nano-linux') for path in (
        'scripts/de25_kernel_test.py', 'scripts/de25_kernel_platform.py',
        'scripts/de25_owner_image.py', 'scripts/de25_boot_contract.py',
        'scripts/de25_first_boot.py',
    )],
    # This separate headless image needs a DE25 serial/SSH boot check. It is
    # never installed as a DE10 kit software bundle or native-integration image.
    ('image/de25-nano/**', 'full', 'de25-nano-linux'),
    ('image/**', 'full', 'image'), ('boot-media.lock.toml', 'full', 'boot-media'),
    ('containers/**', 'full', 'boot-media'), ('profiles/**', 'full', 'profile'),
    # Display metadata for the separately published host catalog; it is not
    # embedded in the image, kit software bundle or core package.
    ('config/core-library.toml', 'none', None),
    # Checked-in development inventory; it is not a deployed image input.
    ('config/hil-go-consumers.json', 'none', None),
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
    # Definitions are checked through their generated deployed consumers below.
    ('sources/mister-packages/packages/board/de25_nano.yaml', 'none', None),
    ('sources/mister-packages/packages/fabric/fes_fabric_de25_ddr_probe.yaml', 'none', None),
    ('sources/mister-packages/packages/fabric/fes_fabric_de25_scanout.yaml', 'none', None),
    ('sources/mister-packages/packages/fabric/fes_fabric_de25_computer.yaml', 'none', None),
    ('sources/mister-packages/packages/fabric/fes_fabric_de25_ramtest.yaml', 'none', None),
    ('sources/mister-packages/packages/fabric/fes_fabric_de25_pr_control.yaml', 'none', None),
    # DE25 now has deployed MENU and ZX81 packages. Keep board-qualified names:
    # a DE10 package/HIL run cannot satisfy these consumers. Shared board code
    # conservatively selects both DE25 packages; it does not require an OS image.
    ('sources/misteross/boards/de25nano/producers/build_menu_package.py', 'overlay', 'core:de25-menu'),
    ('sources/misteross/boards/de25nano/producers/build_zx81_package.py', 'overlay', 'core:de25-zx81'),
    ('sources/misteross/boards/de25nano/producers/review_audio_clocks.py', 'overlay', 'core:de25-zx81'),
    ('sources/misteross/boards/de25nano/**/test_*.py', 'none', None),
    ('sources/misteross/boards/de25nano/**/*_tb.cpp', 'none', None),
    ('sources/misteross/boards/de25nano/**/*_harness.v', 'none', None),
    ('sources/misteross/boards/de25nano/**/sim.py', 'none', None),
    ('sources/misteross/boards/de25nano/**/*_sim.py', 'none', None),
    ('sources/misteross/boards/de25nano/zx81-hps/**', 'overlay', 'core:de25-zx81'),
    # This helper is also imported by the canonical computer producer.
    ('sources/misteross/boards/de25nano/zx81/build.py', 'overlay', 'core:de25-zx81'),
    ('sources/misteross/boards/de25nano/zx81/firmware.py', 'overlay', 'core:de25-zx81'),
    # These remaining diagnostics have no deployed package consumer.
    *[('sources/misteross/boards/de25nano/' + path, 'none', None) for path in (
        'blinky.v', 'blinky.sv', 'blinky_tb.cpp', 'pins.qsf', 'clocks.sdc',
        'Makefile', 'test_build.py', 'test_de25nano.py', 'sim/**',
        'ramtest/**', 'link-proof/**', 'zx81/**',
    )],
    ('sources/misteross/boards/de25nano/**', 'overlay', 'core:de25-ALL'),
    # These exact validation/generation tools have no deployed binary consumer.
    # A changed emitted artifact is classified through its actual consumer path.
    ('sources/mister-packages/cmd/mister-packages/main.go', 'none', None),
    ('sources/mister-packages/internal/pack/board.go', 'none', None),
    ('sources/mister-packages/internal/pack/load.go', 'none', None),
    ('sources/mister-packages/internal/emitc/emitc.go', 'none', None),
    ('sources/mister-packages/**', 'full', 'package-format'),
    ('sources/libmister-runtime/**', 'overlay', 'mister-runtime'),
    ('sources/FogCast/cmd/mister-agent/**', 'overlay', 'mister-agent'),
    ('sources/FogCast/cmd/fogcast-tenfoot/**', 'overlay', 'fogcast-tenfoot'),
    # The kit bundle's bin/fogcast-kit is cmd/fogcast-api built under the kit's
    # name (#863 slice 5), so a change ships in both the bundle and the host.
    ('sources/FogCast/cmd/fogcast-api/**', 'overlay', 'kit-go+host'),
    ('sources/FogCast/cmd/fogcast/**', 'overlay', 'host:fogcast'),
    # These operator clients run on the host and are not installed in an image.
    ('sources/FogCast/cmd/fes-update/**', 'overlay', 'host:fes-update'),
    ('sources/FogCast/cmd/fes-software/**', 'overlay', 'host:fes-software'),
    ('sources/FogCast/cmd/fes-kit-bundle/**', 'overlay', 'host:fes-kit-bundle'),
    ('sources/FogCast/cmd/target-image-lock/**', 'overlay', 'host:target-image-lock'),
    # Built on request but not installed by the kit image or software bundle.
    ('sources/FogCast/cmd/mister-bridge/**', 'none', None),
    ('sources/FogCast/cmd/fes-parts/**', 'none', None),
    # The Spectrum simulations' restore-stream and disk-image writers; no
    # image, bundle or package ships them, and the host converts snapshots and
    # disks in fogcast-api.
    ('sources/FogCast/cmd/fes-spectrum-restore/**', 'none', None),
    ('sources/FogCast/cmd/fes-spectrum-disk/**', 'none', None),
    ('sources/FogCast/cmd/*/**', 'full', 'fogcast-unmapped-command'),
    # ui/** is compiled into fogcast-kit, fogcast-tenfoot and fogcast-api, so it
    # falls through to kit-go+host below.
    # Imported only by cmd/target-image-lock, which no overlay artifact runs.
    ('sources/FogCast/internal/targetimage/**', 'overlay', 'host:target-image-lock'),
    # Imported only by cmd/fes-update, the host appliance update client, to read
    # a release image's software base record. No overlay artifact runs it.
    ('sources/FogCast/internal/ext4image/**', 'overlay', 'host:fes-update'),
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
    # Expansion archives are independent of the core packages an overlay loads.
    # The full-image plan escalates the change; the archive still needs its own
    # build and artifact-specific validation (the default image does not ship it).
    ('sources/misteross/scripts/*slot_card*', 'full', 'expansion-archive'),
    ('sources/misteross/scripts/build_coleco_bus_diagnostic.py', 'full', 'expansion-archive'),
    ('sources/misteross/scripts/build_coleco_sgm.py', 'full', 'expansion-archive'),
    ('sources/misteross/scripts/build_zx81_bus_validation_cart.py', 'full', 'expansion-archive'),
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
    # DE25 package inputs live under the board namespace so edits cannot alter
    # DE10 source closures. Keep their deployed package consumers explicit.
    de25 = {
        'sources/misteross/boards/de25nano/producers/build_menu_package.py':
            ('core:de25-menu', ('core:de25-zx81',)),
        'sources/misteross/boards/de25nano/producers/build_zx81_package.py':
            ('core:de25-zx81', ()),
        'sources/misteross/boards/de25nano/producers/build_zx81_static_package.py':
            ('core:de25-zx81', ()),
        'sources/misteross/boards/de25nano/producers/export_static_persona.py':
            ('core:de25-zx81', ()),
        'sources/misteross/boards/de25nano/producers/review_audio_clocks.py':
            ('core:de25-zx81', ()),
        'sources/misteross/boards/de25nano/package/core_package.py':
            ('core:de25-menu', ('core:de25-zx81',)),
        'sources/misteross/boards/de25nano/package/export_core_package.py':
            ('core:de25-menu', ('core:de25-zx81',)),
        'sources/misteross/boards/de25nano/shared-rtl/fes_menu_reader.v':
            ('core:de25-menu', ('core:de25-zx81',)),
        'sources/misteross/boards/de25nano/shared-rtl/fes_menu_video.v':
            ('core:de25-menu', ('core:de25-zx81',)),
    }
    local = de25.get(path)
    if local is None and path.startswith('sources/misteross/boards/de25nano/shared-rtl/'):
        name = Path(path).name
        if name in ('fes_menu_control.v', 'fes_menu_ddr.v',
                    'fes_menu_endpoint.v', 'fes_menu_top.v'):
            local = ('core:de25-menu', ())
        elif name in ('fes_computer_mailbox.v', 'fes_display_plane.v',
                      'fes_zx81_core.v', 'zx81_top.v', 'zx81_machine.sv',
                      'zx81_machine_clock.v'):
            local = ('core:de25-zx81', ())
    if local is not None:
        component, consumers = local
        row = {'path': path, 'class': 'overlay', 'component': component,
               'rule': 'DE25 board-local package input'}
        if consumers:
            row['consumers'] = list(consumers)
        return row
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
    candidates = {'/'.join(parts[:index]) for index in range(2, len(parts) + 1)}
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


def expansion_input_consumers(path, texts):
    """Find expansion archive producers that declare this path in INPUTS."""
    relative = path.removeprefix(MISTEROSS)
    modules = script_modules(texts)
    found = []
    for stem, module in sorted(modules.items()):
        row = classify_path(SCRIPTS_DIR + stem + '.py')
        if row['component'] != 'expansion-archive':
            continue
        inputs = module.defs.get('INPUTS')
        if inputs is None:
            continue
        if relative in inputs[0]:
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
        # A full-image producer (splash, video part, expansion) or an unresolved import.
        row.update({'class': 'full', 'component': full[0]['component'],
                    'rule': row['rule'] + ' <- ' + full[0]['path']})
        return
    if row['class'] == 'none' and consumers:
        row.update({'class': 'overlay', 'component': consumers[0]['component'],
                    'rule': row['rule'] + ' <- ' + consumers[0]['path']})
    if row['class'] == 'overlay' and row['component'] != 'core:ALL':
        row['consumers'] = sorted({item['component'] for item in consumers} - {row['component']})


GO_INVENTORY = 'config/hil-go-consumers.json'
GO_ROOTS = ('sources/FogCast/', 'sources/misteross/expansion/')
# Go 1.26 go/build.fileListForExt, plus Go source.
GO_SOURCE_SUFFIXES = ('.go', '.c', '.cc', '.cpp', '.cxx', '.m', '.h', '.hh',
                      '.hpp', '.hxx', '.f', '.F', '.for', '.f90', '.s', '.S',
                      '.sx', '.swig', '.swigcxx', '.syso')
GO_TARGETS = {'linux-amd64', 'linux-amd64-sdl3', 'linux-arm64', 'linux-arm64-sdl3',
              'linux-armv7', 'darwin-arm64', 'darwin-arm64-cgo', 'darwin-arm64-sdl3'}
GO_ROLES = {'kit', 'kit-and-host', 'host', 'image', 'host-tool', 'builder',
            'standalone', 'simulation'}
REQUIRED_GO_ROLES = {
    'fes-kit-data': 'image', 'fes-update': 'host-tool', 'fes-software': 'host-tool',
    'fes-kit-bundle': 'builder', 'target-image-lock': 'builder',
    'mister-bridge': 'standalone', 'fes-parts': 'standalone',
    'mister-agent': 'kit', 'fogcast-api': 'kit-and-host',
    'fogcast-tenfoot': 'kit-and-host', 'fogcast': 'host',
    'fes-spectrum-disk': 'simulation', 'fes-spectrum-restore': 'simulation',
    'fes-boot': 'image', 'fes-kit-software': 'image', 'fes-bluetooth': 'image',
}
PLATFORM_GO_COMMANDS = {'fes-boot', 'fes-kit-software', 'fes-bluetooth'}


def read_go_inventory(repo=None, revision=None):
    """Read the inventory from the requested source, never an unrelated checkout."""
    if repo is None:
        text = (Path(__file__).resolve().parents[1] / GO_INVENTORY).read_text()
    else:
        revision = git_commit(repo, revision)
        result = subprocess.run(['git', '-C', str(repo), 'show', revision + ':' + GO_INVENTORY],
                                text=True, capture_output=True)
        if result.returncode:
            raise ValueError(f'Go command inventory missing at {revision}; cannot classify safely')
        text = result.stdout
    try:
        data = json.loads(text)
        commands = data['commands']
        if data['version'] != 1 or not isinstance(commands, dict):
            raise ValueError('unsupported inventory format')
        for name, expected in REQUIRED_GO_ROLES.items():
            if commands[name]['role'] != expected:
                raise ValueError(f'{name} has wrong role')
        for name, entry in commands.items():
            if not re.fullmatch(r'[a-z0-9-]+', name) or entry['role'] not in GO_ROLES:
                raise ValueError(f'invalid command role: {name}')
            targets = []
            if not isinstance(entry['graphs'], list) or not entry['graphs']:
                raise ValueError(f'missing graphs for {name}')
            for group in entry['graphs']:
                if any(not isinstance(group[key], list) for key in ('targets', 'packages', 'embedded', 'embed_patterns')):
                    raise ValueError(f'invalid graph for {name}')
                targets.extend(group['targets'])
                for package in (*group['packages'], *group['embedded'], *group['embed_patterns']):
                    if not isinstance(package, str) or not any(
                            package == root.rstrip('/') or package.startswith(root)
                            for root in (*GO_ROOTS, 'platform/')) or '..' in Path(package).parts:
                        raise ValueError(f'invalid package path for {name}')
            expected_targets = {'linux-armv7'} if name in PLATFORM_GO_COMMANDS else GO_TARGETS
            if len(targets) != len(expected_targets) or set(targets) != expected_targets:
                raise ValueError(f'incomplete targets for {name}')
    except (KeyError, TypeError, json.JSONDecodeError, ValueError) as error:
        raise ValueError(f'invalid Go command inventory: {error}') from error
    return commands


def go_input(row):
    """Exclude docs/tests, but include source and embedded assets in Go modules."""
    path = row['path']
    source = path.endswith(GO_SOURCE_SUFFIXES) and not path.endswith('_test.go')
    return path.startswith(GO_ROOTS) and (row['class'] != 'none' or source)



def matches_embed(path, group):
    if path in group['embedded']:
        return True
    # A pattern may name a directory, in which case Go embeds its descendants.
    # Conservatively include hidden files and broad wildcard matches; the graph
    # guard resolves the actual head's EmbedFiles before any evidence is accepted.
    parts = path.split('/')
    return any(fnmatch.fnmatchcase('/'.join(parts[:end]), pattern)
               for pattern in group['embed_patterns']
               for end in range(1, len(parts) + 1))


def go_commands(rows, inventories):
    """Identify candidate consumers; import reachability is not byte proof."""
    found = {}
    unknown = []
    for row in rows:
        path = row['path']
        if not path.startswith(GO_ROOTS):
            continue
        fogcast_module = path in ('sources/FogCast/go.mod', 'sources/FogCast/go.sum')
        expansion_module = path in ('sources/misteross/expansion/go.mod',
                                    'sources/misteross/expansion/go.sum')
        appliance_module = path in ('sources/FogCast/appliance/go.mod', 'sources/FogCast/appliance/go.sum')
        matched = False
        for inventory in inventories:
            for command, entry in inventory.items():
                embedded_targets = {target for group in entry['graphs'] if matches_embed(path, group)
                                    for target in group['targets']}
                if not go_input(row) and not embedded_targets:
                    continue
                if fogcast_module:
                    targets = {target for group in entry['graphs']
                               if any(package.startswith('sources/FogCast/') and
                                      not (package == 'sources/FogCast/appliance' or
                                           package.startswith('sources/FogCast/appliance/'))
                                      for package in group['packages']) for target in group['targets']}
                elif expansion_module:
                    targets = {target for group in entry['graphs']
                               if any(package == 'sources/misteross/expansion' or
                                      package.startswith('sources/misteross/expansion/')
                                      for package in group['packages']) for target in group['targets']}
                elif appliance_module:
                    targets = {target for group in entry['graphs']
                               if any(package == 'sources/FogCast/appliance' or
                                      package.startswith('sources/FogCast/appliance/')
                                      for package in group['packages']) for target in group['targets']}
                else:
                    directory = path.rsplit('/', 1)[0]
                    targets = {target for group in entry['graphs']
                               if any(directory == package if path.endswith(GO_SOURCE_SUFFIXES)
                                      else path.startswith(package + '/')
                                      for package in group['packages'])
                               for target in group['targets']}
                targets |= embedded_targets
                if not targets:
                    continue
                matched = True
                item = found.setdefault(command, {'command': command, 'role': entry['role'],
                                                   'paths': set(), 'targets': set(), 'embedded_paths': set()})
                if item['role'] != entry['role']:
                    raise ValueError(f'command role changed across revisions: {command}')
                item['paths'].add(path)
                item['targets'].update(targets)
                if embedded_targets:
                    item['embedded_paths'].add(path)
        if go_input(row) and path.endswith(GO_SOURCE_SUFFIXES) and not matched:
            unknown.append(path)
    return ([{'command': item['command'], 'role': item['role'],
              'paths': sorted(item['paths']), 'targets': sorted(item['targets'])}
             | {'embedded_paths': sorted(item['embedded_paths'])}
             for item in sorted(found.values(), key=lambda x: x['command'])], sorted(unknown))


def go_import_changes(repo, base, head, paths):
    """Compare package/import/build-tag headers at both exact revisions."""
    pairs = []
    for path in paths:
        if not path.endswith('.go'):
            continue
        values = []
        for revision in (base, head):
            result = subprocess.run(['git', '-C', str(repo), 'show', revision + ':' + path],
                                    text=True, capture_output=True)
            values.append(result.stdout if result.returncode == 0 else None)
        pairs.append({'path': path, 'before': values[0], 'after': values[1]})
    if not pairs:
        return []
    env = os.environ.copy()
    env.update(GOENV='off', GOWORK='off', GO111MODULE='off', GOPROXY='off',
               GOTOOLCHAIN='local', GOFLAGS='')
    helper = Path(__file__).resolve().with_name('hil_go_imports.go')
    try:
        result = subprocess.run(['go', 'run', str(helper)], input=json.dumps(pairs),
                                text=True, capture_output=True, env=env, timeout=60, check=True)
        changed = json.loads(result.stdout)
        if not isinstance(changed, list) or any(path not in paths for path in changed):
            raise ValueError('invalid Go import comparison output')
        return changed
    except (OSError, subprocess.SubprocessError, json.JSONDecodeError) as error:
        detail = (getattr(error, 'stderr', '') or '').strip()
        raise ValueError(f'Go import graph comparison failed: {error}' +
                         (f'\n{detail}' if detail else '')) from error


def go_embed_changes(repo, base, head, paths, inventories):
    """Detect additions/removals or stale EmbedFiles at either exact revision."""
    changed = []
    for path in paths:
        memberships = []
        uncertain = False
        for revision, inventory in zip((base, head), inventories):
            exact = {name for name, entry in inventory.items()
                     if any(path in group['embedded'] for group in entry['graphs'])}
            potential = any(matches_embed(path, group) for entry in inventory.values()
                            for group in entry['graphs'])
            exists = subprocess.run(['git', '-C', str(repo), 'cat-file', '-e', revision + ':' + path],
                                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0
            # A present new file matching a pattern but omitted from EmbedFiles
            # means the inventory may predate it. Deleted paths need no head entry.
            if potential and exists and not exact:
                uncertain = True
            memberships.append(exact)
        if uncertain or memberships[0] != memberships[1]:
            changed.append(path)
    return changed


def plan(paths, texts=None, go_inventories=None, go_graph_changes=None, inventory_errors=None):
    rows = [classify_path(path) for path in paths if path.strip()]
    for row in rows:
        if row['class'] == 'overlay' and row['rule'] in CORE_RULES:
            if texts is None:
                texts = script_texts()
            expand(row, core_consumers(row['path'], texts))
        elif row['path'] == 'sources/misteross/toolchain.lock' and row['class'] == 'overlay':
            if texts is None:
                texts = script_texts()
            expand(row, expansion_input_consumers(row['path'], texts))
        elif row['class'] != 'full' and is_script_module(row['path']):
            # A changed script also changes every producer that imports it.
            if texts is None:
                texts = script_texts()
            expand(row, script_importers(row['path'], texts))
    possible = [row for row in rows if row['path'].startswith(GO_ROOTS)]
    commands, unknown = [], []
    if possible:
        inventories = go_inventories if go_inventories is not None else [read_go_inventory()]
        commands, unknown = go_commands(possible, inventories)
        # Embedded files are deployed even when their extension (e.g. README.md)
        # would ordinarily select the documentation-only shortcut.
        for row in possible:
            if row['class'] != 'none':
                continue
            consumers = [command for command in commands
                         if row['path'] in command['embedded_paths'] or
                         (go_input(row) and row['path'].endswith(GO_SOURCE_SUFFIXES) and row['path'] in command['paths'])]
            if any(command['role'] in ('kit', 'kit-and-host') for command in consumers):
                row.update({'class': 'overlay', 'component': 'kit-go+host',
                            'rule': row['rule'] + ' <- Go source or embedded asset'})
            elif any(command['role'] == 'host' for command in consumers):
                row.update({'class': 'overlay', 'component': 'host:fogcast',
                            'rule': row['rule'] + ' <- Go source or embedded asset'})
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
    result = {'rows': rows, 'class': worst, 'components': components,
              'decision': decision}
    relevant = [row for row in rows if go_input(row)]
    if relevant or commands or inventory_errors:
        required = [command for command in commands
                    if command['role'] in ('image', 'host-tool', 'builder')]
        advisory = [command for command in commands
                    if command['role'] in ('standalone', 'simulation')]
        # Path-only callers cannot compare source headers. A missing/new Go
        # package or changed import/build constraints needs the CI graph guard.
        unverified = ([row['path'] for row in relevant if row['path'].endswith('.go')] +
                      [path for command in commands if command['role'] not in ('standalone', 'simulation')
                       for path in command['embedded_paths']]
                      if go_graph_changes is None else go_graph_changes)
        unverified = sorted(set(unverified + unknown + (inventory_errors or []) +
                            [row['path'] for row in relevant if row['path'].endswith(('/go.mod', '/go.sum'))]))
        result['command_evidence'] = required
        result['advisory_commands'] = advisory
        result['go_graph_unverified'] = unverified
        if required or unverified:
            result['deployment_class'] = worst
            result['deployment_decision'] = decision
            result['class'] = 'analysis'
            result['decision'] = 'COMMAND_EVIDENCE_REQUIRED'
    return result


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
    if plan_data.get('deployment_decision'):
        print('DEPLOYMENT: ' + plan_data['deployment_decision'])
    for command in plan_data.get('command_evidence', []):
        print(f"REQUIRE: {command['command']} ({command['role']}) "
              f"targets={','.join(command['targets'])}")
    for command in plan_data.get('advisory_commands', []):
        print(f"OPTIONAL: {command['command']} ({command['role']}) "
              f"targets={','.join(command['targets'])}")
    for path in plan_data.get('go_graph_unverified', []):
        print('VERIFY GO GRAPH: ' + path)
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
        inventories = None
        graph_changes = None
        rows = [classify_path(path) for path in paths if path.strip()]
        possible = [row['path'] for row in rows if row['path'].startswith(GO_ROOTS)]
        relevant = [row['path'] for row in rows if go_input(row)]
        inventory_errors = []
        if possible and args.repo and args.base_image_commit and args.head and not args.paths_file:
            inventories = []
            for revision in (base, head):
                try:
                    inventories.append(read_go_inventory(args.repo, revision))
                except ValueError as error:
                    inventory_errors.append(str(error))
            graph_changes = go_import_changes(args.repo, base, head, relevant)
            if len(inventories) == 2:
                graph_changes += go_embed_changes(args.repo, base, head, possible, inventories)
        data = plan(paths, texts, inventories, graph_changes, inventory_errors)
        if gap:
            data['image_gap'] = gap
        show(data, args.json)
        return 3 if data['decision'] == 'COMMAND_EVIDENCE_REQUIRED' else 0
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        print(str(error), file=sys.stderr)
        return 2


if __name__ == '__main__':
    sys.exit(main())
