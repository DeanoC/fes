"""Tests for HIL change classification."""

import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / 'scripts/hil_plan.py'
FIXTURES = ROOT / 'tests/fixtures/hil_plan'


def hil_module():
    import importlib.util
    spec = importlib.util.spec_from_file_location('hil_plan_under_test', SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def run(*args, cwd=None):
    return subprocess.run([sys.executable, str(SCRIPT), *map(str, args)],
                          cwd=cwd, text=True, capture_output=True)


def deployment(data):
    # An evidence-blocking decision preserves the underlying deployment plan.
    return data.get('deployment_decision', data['decision'])


class HilPlanTest(unittest.TestCase):
    def test_catalog_metadata_does_not_require_deployment(self):
        planner = hil_module()
        catalog = 'config/core-library.toml'
        result = planner.plan([catalog], texts={})
        self.assertEqual(result['decision'], 'NO_DEPLOY_CHANGE')
        self.assertEqual(result['rows'][0]['class'], 'none')
        # The exception is exact and must not mask deployed or unknown inputs.
        for path, decision in (('config/core-recipes.toml', 'FULL_IMAGE'),
                               ('config/other.toml', 'FULL_IMAGE'),
                               ('sources/FogCast/cmd/mister-agent/main.go',
                                'OVERLAY mister-agent')):
            with self.subTest(path=path):
                mixed = planner.plan([catalog, path], texts={}, go_graph_changes=[])
                self.assertEqual(deployment(mixed), decision)

    def test_de25_linux_requires_its_own_full_image_boot_check(self):
        planner = hil_module()
        paths = ['image/de25-nano/build.py', 'image/de25-nano/boot.py',
                 'image/de25-nano/vendor.lock.toml', 'image/de25-nano/rootfs/config']
        result = planner.plan(paths, texts={})
        self.assertEqual(result['decision'], 'FULL_IMAGE')
        self.assertTrue(all(row['component'] == 'de25-nano-linux' for row in result['rows']))
        self.assertEqual(planner.plan(['docs/de25-linux.md'], texts={})['decision'],
                         'NO_DEPLOY_CHANGE')
        for path, component in (('image/scripts/build-target-image.sh', 'image'),
                                ('image/de25-nano-other/build.py', 'image'),
                                ('boot-media.lock.toml', 'boot-media')):
            with self.subTest(path=path):
                mixed = planner.plan([*paths, path], texts={})
                self.assertEqual(mixed['decision'], 'FULL_IMAGE')
                self.assertEqual(mixed['rows'][-1]['component'], component)

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.work = Path(self.temp.name)

    def git(self, *args):
        return subprocess.check_output(['git', '-C', str(self.repo), *args], text=True).strip()

    def start_repo(self):
        self.repo = self.work / 'repo'
        self.repo.mkdir()
        self.git('init', '-q')
        self.git('config', 'user.email', 'test@example.com')
        self.git('config', 'user.name', 'Test')
        (self.repo / 'README').write_text('base\n')
        self.git('add', '.')
        self.git('commit', '-m', 'base')
        self.git('branch', '-M', 'main')
        return self.git('rev-parse', 'HEAD')

    def git_repo(self, paths=None):
        if hasattr(self, 'repo') and self.repo.exists():
            shutil.rmtree(self.repo)
        self.repo = self.work / 'repo'
        self.repo.mkdir()
        self.git('init', '-q')
        self.git('config', 'user.email', 'test@example.com')
        self.git('config', 'user.name', 'Test')
        (self.repo / 'docs').mkdir()
        (self.repo / 'docs/base.md').write_text('base\n')
        self.git('add', '.')
        self.git('commit', '-m', 'base')
        self.base = self.git('rev-parse', 'HEAD')
        for path in (paths if paths is not None else ['sources/libmister-runtime/change.cpp']):
            target = self.repo / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text('overlay\n')
        if paths is None or paths:
            self.git('add', '.')
            self.git('commit', '-m', 'overlay')
        self.head = self.git('rev-parse', 'HEAD')
        return self.base, self.head


    def test_real_pr_fixtures(self):
        expected = {'pr569.files': ('mister-runtime', 'kit-go+host'),
                    'pr565.files': ('core:ramtest',)}
        # #567 rebuilt the Atari ST video part archive, which the overlay cannot attest.
        result = run('classify', '--paths-file', FIXTURES / 'pr567.files', '--json')
        self.assertEqual(json.loads(result.stdout)['decision'], 'FULL_IMAGE')
        # #899's confirm, boot-ticket rewrite and storage gate run from the image
        # on a trial boot, so a bundle overlay cannot exercise them.
        result = run('classify', '--paths-file', FIXTURES / 'pr899.files', '--json')
        self.assertEqual(result.returncode, 3, result.stderr)
        data = json.loads(result.stdout)
        self.assertEqual(data['decision'], 'COMMAND_EVIDENCE_REQUIRED')
        self.assertEqual(deployment(data), 'FULL_IMAGE')
        by_path = {row['path']: row for row in data['rows']}
        for path in ('sources/FogCast/appliance/store/store.go',
                     'sources/FogCast/internal/appliancedata/gate.go',
                     'sources/FogCast/internal/applianceupdate/service.go'):
            self.assertEqual(by_path[path]['class'], 'full', path)
        for path in ('sources/FogCast/appliance/store/store_test.go',
                     'sources/FogCast/docs/ARCHITECTURE.md',
                     'sources/FogCast/internal/appliancedata/gate_test.go',
                     'sources/FogCast/internal/applianceupdate/service_test.go'):
            self.assertEqual(by_path[path]['class'], 'none', path)
        # #902's FAT splash refresh, rollback-confirm publish and staged-splash
        # prune run in the image's mister-agent during an fes-update trial boot.
        result = run('classify', '--paths-file', FIXTURES / 'pr902.files', '--json')
        self.assertEqual(result.returncode, 3, result.stderr)
        data = json.loads(result.stdout)
        self.assertEqual(data['decision'], 'COMMAND_EVIDENCE_REQUIRED')
        self.assertEqual(deployment(data), 'FULL_IMAGE')
        by_path = {row['path']: row for row in data['rows']}
        for path in ('sources/FogCast/appliance/store/store.go',
                     'sources/FogCast/internal/appliancesplash/splash.go',
                     'sources/FogCast/internal/applianceupdate/service.go'):
            self.assertEqual(by_path[path]['class'], 'full', path)
            self.assertEqual(by_path[path]['component'], 'appliance-trial', path)
        for path in ('docs/appliance-releases.md',
                     'docs/bootable-media.md',
                     'docs/hps-ddr.md',
                     'sources/FogCast/README.md',
                     'sources/FogCast/appliance/store/store_test.go',
                     'sources/FogCast/docs/ARCHITECTURE.md',
                     'sources/FogCast/docs/appliance-updates.md',
                     'sources/FogCast/docs/kit-resource-limits.md',
                     'sources/FogCast/internal/appliancesplash/splash_test.go',
                     'sources/FogCast/internal/applianceupdate/service_test.go'):
            self.assertEqual(by_path[path]['class'], 'none', path)
        # The mister-agent command, host diagnostics and the host status client
        # stay overlay. The splash package linked into the image agent is what
        # forces the full image. The shell test stays on the FogCast overlay rule.
        for path, kind in (('sources/FogCast/cmd/mister-agent/appliance.go', 'mister-agent'),
                           ('sources/FogCast/scripts/kit-diagnostics.sh', 'kit-go+host'),
                           ('sources/FogCast/scripts/tests/kit-diagnostics_test.sh', 'kit-go+host'),
                           ('sources/FogCast/targetclient/appliance_update.go', 'kit-go+host')):
            self.assertEqual(by_path[path]['class'], 'overlay', path)
            self.assertEqual(by_path[path]['component'], kind, path)
        for filename, components in expected.items():
            with self.subTest(filename=filename):
                result = run('classify', '--paths-file', FIXTURES / filename, '--json')
                data = json.loads(result.stdout)
                self.assertEqual(result.returncode, 3 if data['decision'] == 'COMMAND_EVIDENCE_REQUIRED' else 0, result.stderr)
                self.assertEqual(data.get('deployment_class', data['class']), 'overlay')
                for component in components:
                    self.assertIn(component, data['components'])
                if filename == 'pr565.files':
                    self.assertNotIn('core:ramtest-quartus', data['components'])

    def test_trial_image_paths_are_full_and_neighbours_stay_overlay(self):
        plan = hil_module()
        full = {
            'sources/FogCast/appliance/store/store.go': 'appliance-trial',
            'sources/FogCast/internal/applianceupdate/service.go': 'appliance-trial',
            'sources/FogCast/internal/applianceupdate/identity.go': 'appliance-trial',
            'sources/FogCast/internal/appliancesplash/splash.go': 'appliance-trial',
            'sources/FogCast/internal/appliancesplash/publish.go': 'appliance-trial',
            'sources/FogCast/internal/appliancedata/gate.go': 'storage-gate',
            'sources/FogCast/internal/appliancedata/bind.go': 'storage-gate',
            # Linked into fes-boot / fes-kit-data / the image trial supervisor.
            # release.go used to be pinned overlay; that missed the image binary.
            'sources/FogCast/appliance/release.go': 'appliance-trial',
            'sources/FogCast/appliance/blockprobe/probe.go': 'appliance-trial',
            'sources/FogCast/appliance/softwarecontrol/contract.go': 'kit-software',
            # Trial startup and trial boot already ship in the image.
            'platform/cmd/fes-kit-software/main_linux.go': 'platform',
            'platform/internal/kitsoftware/engine.go': 'platform',
            'platform/cmd/fes-boot/main_linux.go': 'platform',
            'platform/internal/applianceboot/boot.go': 'platform',
            'sources/FogCast/cmd/fes-kit-data/main.go': 'fogcast-unmapped-command',
        }
        for path, component in full.items():
            with self.subTest(path=path):
                row = plan.classify_path(path)
                self.assertEqual(row['class'], 'full')
                self.assertEqual(row['component'], component)
        # These rules do not pull neighbouring FogCast code out of the overlay.
        # Importers of the image-linked packages stay on their own rules.
        for path in ('sources/FogCast/cmd/mister-agent/appliance.go',
                     'sources/FogCast/internal/httpapi/update.go',
                     'sources/FogCast/fogcast/service.go',
                     'sources/FogCast/internal/example.go',
                     'sources/FogCast/catalog/scanner.go',
                     'sources/FogCast/ui/tenfoot/tape.go',
                     'sources/FogCast/protocol/types.go',
                     'sources/FogCast/targetclient/kit_software.go'):
            with self.subTest(path=path):
                self.assertEqual(plan.classify_path(path)['class'], 'overlay')
        for path in ('sources/FogCast/appliance/release_test.go',
                     'sources/FogCast/appliance/blockprobe/probe_test.go'):
            with self.subTest(path=path):
                self.assertEqual(plan.classify_path(path)['class'], 'none')

    def test_simulation_only_commands_are_none(self):
        plan = hil_module()
        for command in ('fes-spectrum-restore', 'fes-spectrum-disk'):
            row = plan.classify_path(f'sources/FogCast/cmd/{command}/main.go')
            self.assertEqual(row['class'], 'none', command)
        # Optional standalone tools are not installed by the image or bundle.
        self.assertEqual(plan.classify_path('sources/FogCast/cmd/fes-parts/main.go')['class'], 'none')

    def test_ci_selection_scripts_are_none(self):
        plan = hil_module()
        for name in ('affected', 'hil_plan', 'host_tests', 'runtime_tests', 'changed_paths',
                     'test_changed', 'input_diagnostic', 'de25_runtime', 'de25_dma_descriptor'):
            path = f'scripts/{name}.py'
            with self.subTest(path=path):
                row = plan.classify_path(path)
                self.assertEqual(row['class'], 'none', path)

    def test_image_binary_appliance_fixtures_are_full(self):
        expected = {
            'release.files': ('sources/FogCast/appliance/release.go', 'appliance-trial'),
            'blockprobe.files': ('sources/FogCast/appliance/blockprobe/probe.go', 'appliance-trial'),
            'softwarecontrol.files': ('sources/FogCast/appliance/softwarecontrol/contract.go', 'kit-software'),
        }
        for filename, (path, component) in expected.items():
            with self.subTest(filename=filename):
                result = run('classify', '--paths-file', FIXTURES / filename, '--json')
                self.assertEqual(result.returncode, 3, result.stderr)
                data = json.loads(result.stdout)
                self.assertEqual(data['decision'], 'COMMAND_EVIDENCE_REQUIRED')
                self.assertEqual(deployment(data), 'FULL_IMAGE')
                by_path = {row['path']: row for row in data['rows']}
                self.assertEqual(by_path[path]['class'], 'full')
                self.assertEqual(by_path[path]['component'], component)
                for row in data['rows']:
                    if row['path'].endswith('_test.go'):
                        self.assertEqual(row['class'], 'none', row['path'])
        # A mixed diff is a full image. Overlay importers of the same packages stay overlay.
        mixed = self.work / 'mixed-image-appliance.files'
        mixed.write_text('\n'.join((
            'sources/FogCast/appliance/release.go',
            'sources/FogCast/appliance/blockprobe/probe.go',
            'sources/FogCast/catalog/scanner.go',
            'sources/FogCast/internal/httpapi/update.go',
            'sources/FogCast/protocol/types.go',
        )) + '\n')
        result = run('classify', '--paths-file', mixed, '--json')
        self.assertEqual(result.returncode, 3, result.stderr)
        data = json.loads(result.stdout)
        self.assertEqual(data['decision'], 'COMMAND_EVIDENCE_REQUIRED')
        self.assertEqual(deployment(data), 'FULL_IMAGE')
        by_path = {row['path']: row for row in data['rows']}
        self.assertEqual(by_path['sources/FogCast/appliance/release.go']['class'], 'full')
        self.assertEqual(by_path['sources/FogCast/appliance/blockprobe/probe.go']['class'], 'full')
        for path in ('sources/FogCast/catalog/scanner.go',
                     'sources/FogCast/internal/httpapi/update.go',
                     'sources/FogCast/protocol/types.go'):
            self.assertEqual(by_path[path]['class'], 'overlay', path)

    def test_producer_script_suffixes_map_to_canonical_cores(self):
        path = self.work / 'producers.files'
        path.write_text('\n'.join((
            'sources/misteross/scripts/build_fes_sg1000_oss.py',
            'sources/misteross/scripts/build_fes_ramtest_quartus.py',
        )) + '\n')
        result = run('classify', '--paths-file', path, '--json')
        self.assertEqual(result.returncode, 0, result.stderr)
        components = set(json.loads(result.stdout)['components'])
        self.assertTrue({'core:sg1000', 'core:ramtest'} <= components, components)
        self.assertTrue({'core:sg1000-oss', 'core:ramtest-quartus'}.isdisjoint(components), components)
        # The selected Coleco producer escalates through its video-part import,
        # but its own rule must still name the deployed core.
        row = hil_module().classify_path('sources/misteross/scripts/build_fes_coleco_socket_v2.py')
        self.assertEqual(row['component'], 'core:coleco')

    def test_expansion_archives_and_their_inputs_require_full_plan(self):
        plan = hil_module()
        scripts = 'sources/misteross/scripts/'
        texts = plan.script_texts()
        producers = (
            'build_apple2_slot_card.py', 'build_atari_st_slot_card.py',
            'build_c64_slot_card.py', 'build_coleco_bus_diagnostic.py',
            'build_coleco_sgm.py', 'build_zx81_bus_validation_cart.py',
        )
        for name in producers:
            with self.subTest(producer=name):
                result = plan.plan([scripts + name], texts)
                self.assertEqual(result['decision'], 'FULL_IMAGE')
                self.assertEqual(result['rows'][0]['component'], 'expansion-archive')

        for path in (
            'cores/fes-apple2/expansions/probe_rom.vh',
            'cores/fes-atari-st/expansions/st_probe.sv',
            'cores/fes-c64/expansions/probe.v',
            'cores/fes-coleco/expansions/diagnostic.v',
            'cores/fes-coleco/expansions/sgm_ay.v',
            'cores/fes-zx81/expansions/zonx_ay.v',
        ):
            with self.subTest(source=path):
                result = plan.plan(['sources/misteross/' + path], texts)
                self.assertEqual(result['decision'], 'FULL_IMAGE')
                self.assertIn(' <- ', result['rows'][0]['rule'])

        toolchain_lock = 'sources/misteross/toolchain.lock'
        lock_consumers = plan.expansion_input_consumers(toolchain_lock, texts)
        self.assertEqual(
            {row['path'] for row in lock_consumers},
            {scripts + 'build_coleco_bus_diagnostic.py', scripts + 'build_coleco_sgm.py'},
        )
        result = plan.plan([toolchain_lock], texts)
        self.assertEqual(result['decision'], 'FULL_IMAGE')
        self.assertEqual(result['rows'][0]['component'], 'expansion-archive')
        self.assertIn(' <- ' + scripts + 'build_coleco_bus_diagnostic.py',
                      result['rows'][0]['rule'])

        # Shared helpers also affect the archive producer that imports them.
        for name, producer in (('apple2_slots.py', 'build_apple2_slot_card.py'),
                               ('atari_st_slot.py', 'build_atari_st_slot_card.py'),
                               ('c64_slots.py', 'build_c64_slot_card.py')):
            with self.subTest(helper=name):
                result = plan.plan([scripts + name], texts)
                self.assertEqual(result['decision'], 'FULL_IMAGE')
                self.assertIn(scripts + producer,
                              {row['path'] for row in plan.script_importers(scripts + name, texts)})

        # An ordinary core producer and documentation must not match the new rules.
        self.assertEqual(plan.classify_path(scripts + 'build_fes_ramtest_oss.py')['class'], 'overlay')
        self.assertEqual(plan.classify_path('docs/hil-plan.md')['class'], 'none')

    def test_inventory_exposes_all_shared_package_consumers(self):
        planner = hil_module()
        inventory = planner.read_go_inventory()
        packages = {package for entry in inventory.values() for group in entry['graphs']
                    for package in group['packages'] if package.startswith(planner.GO_ROOTS)}
        for package in sorted(packages):
            # Command directories themselves have lifecycle-specific rules; the
            # shared-package graph is the gap this check protects.
            if '/cmd/' in package:
                continue
            path = package + '/example.go'
            row = planner.classify_path(path)
            if row['class'] == 'none':
                continue
            expected = {name for name, entry in inventory.items() if any(
                package in group['packages'] for group in entry['graphs'])}
            actual, unknown = planner.go_commands([row], [inventory])
            self.assertFalse(unknown, package)
            self.assertEqual({item['command'] for item in actual}, expected, package)
            data = planner.plan([path], texts={}, go_inventories=[inventory], go_graph_changes=[])
            mandatory = {item['command'] for item in actual if item['role'] in ('image', 'host-tool', 'builder')}
            if mandatory:
                self.assertEqual(data['decision'], 'COMMAND_EVIDENCE_REQUIRED', package)
                self.assertEqual({item['command'] for item in data['command_evidence']}, mandatory, package)

    def test_shared_core_rtl_expands_to_every_consuming_producer(self):
        plan = hil_module()
        texts = plan.script_texts()
        pll = 'sources/misteross/cores/fes-pong/rtl/pixel_pll.v'
        consumers = {row['component'] for row in plan.core_consumers(pll, texts)}
        for component in ('core:pong', 'core:ramtest', 'core:riscv'):
            self.assertIn(component, consumers)
        # build_fes_splash imports build_fes_pong, whose names reach this file, so
        # the boot /idle.rbf may change: the splash rule forces a full image.
        result = run('classify', '--paths-file', self.path_file(pll), '--json')
        self.assertEqual(json.loads(result.stdout)['decision'], 'FULL_IMAGE')
        # The splash pins fes_application.vh through fes_de10nano_evidence.HPS_DDR_HEADER.
        result = run('classify', '--paths-file',
                     self.path_file('sources/misteross/cores/fes-common/generated/fes_application.vh'), '--json')
        self.assertEqual(json.loads(result.stdout)['decision'], 'FULL_IMAGE')

        self.git_repo([])
        scripts = self.repo / 'sources/misteross/scripts'
        scripts.mkdir(parents=True)
        (scripts / 'build_fes_alpha.py').write_text('INPUTS = ("cores/fes-beta/rtl/shared.v",)\n')
        (scripts / 'helper.py').write_text('ROOTS = ["cores/fes-gamma/rtl"]\n')
        (scripts / 'sim_beta.py').write_text('X = "cores/fes-delta/rtl/only.v"\n')
        for name in ('fes-beta/rtl/shared.v', 'fes-gamma/rtl/x.v', 'fes-delta/rtl/only.v'):
            path = self.repo / 'sources/misteross/cores' / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('base\n')
        self.git('add', '.')
        self.git('commit', '-m', 'scripts')
        base = self.git('rev-parse', 'HEAD')
        expected = {'fes-beta/rtl/shared.v': ['core:alpha', 'core:beta'],
                    'fes-gamma/rtl/x.v': ['core:ALL', 'core:gamma'],
                    'fes-delta/rtl/only.v': ['core:delta']}
        for name, components in expected.items():
            with self.subTest(name=name):
                self.git('checkout', '-q', base)
                (self.repo / 'sources/misteross/cores' / name).write_text('change\n')
                self.git('commit', '-qam', 'change ' + name)
                head = self.git('rev-parse', 'HEAD')
                result = run('classify', '--repo', self.repo, '--base-image-commit', base,
                             '--head', head, '--json')
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(json.loads(result.stdout)['components'], components)

    def test_producer_imports_at_head_pass_their_inputs_on(self):
        plan = hil_module()
        texts = plan.script_texts()
        modules = plan.script_modules(texts)
        self.assertEqual([item for module in modules.values() for item in module.unresolved], [])
        imports = [('build_fes_menu_package', 'build_fes_menu'),
                   ('build_fes_coleco_socket_dev', 'build_fes_coleco_oss'),
                   ('build_fes_coleco_socket_v2', 'build_fes_coleco_oss'),
                   ('build_fes_coleco_socket_v2', 'video_parts'),
                   ('build_fes_coleco_socket_v2', 'native_video_parts'),
                   ('build_fes_coleco_megacart', 'build_fes_coleco_oss'),
                   ('build_fes_coleco_megacart', 'build_fes_coleco_socket_v2'),
                   ('build_fes_splash', 'build_fes_pong'),
                   ('build_fes_splash', 'fes_de10nano_evidence')]
        for importer, imported in imports:
            with self.subTest(importer=importer, imported=imported):
                own = set().union(*(literals for literals, _ in modules[importer].defs.values()))
                inherited = sorted(literal for literal in plan.script_reads(modules, imported)
                                   if literal.startswith('cores/') and literal not in own)
                self.assertTrue(inherited, 'expected inputs named only by ' + imported)
                path = 'sources/misteross/' + inherited[0]
                rows = plan.core_consumers(path, texts)
                self.assertIn('sources/misteross/scripts/' + importer + '.py', {row['path'] for row in rows})
                if importer == 'build_fes_splash':
                    self.assertEqual(plan.plan([path], texts)['decision'], 'FULL_IMAGE')

    def test_core_consumers_follow_script_imports(self):
        plan = hil_module()
        target = 'sources/misteross/cores/fes-beta/rtl/shared.v'
        base = {'scripts/inputs.py': 'SHARED = "cores/fes-beta/rtl/shared.v"\nOTHER = "cores/fes-x/y.v"\n'
                                     'def unrelated():\n    return OTHER\n'}
        cases = {
            'module attribute': ('from scripts import inputs as board\nPINNED = (*board.SHARED,)\n', True),
            'from import constant': ('from scripts.inputs import SHARED\n', True),
            'relative import': ('from .inputs import SHARED\n', True),
            'import scripts.module': ('import scripts.inputs\nX = scripts.inputs.OTHER\n', True),
            'function default': ('from scripts import inputs\ndef f(x=inputs.SHARED):\n    return x\n', True),
            'bare module alias': ('from scripts import inputs as board\nX = getattr(board, "S" + "HARED")\n', True),
            'unrelated name only': ('from scripts import inputs\nX = inputs.unrelated()\n', False),
            'no import': ('X = "cores/fes-x/y.v"\n', False),
        }
        for name, (text, consumes) in cases.items():
            with self.subTest(case=name):
                texts = dict(base, **{'scripts/build_fes_alpha.py': text})
                components = {row['component'] for row in plan.core_consumers(target, texts)}
                self.assertEqual('core:alpha' in components, consumes)
                self.assertEqual('core:alpha' in plan.plan([target], texts)['components'], consumes)
        # Transitive: alpha -> middle -> inputs, and a re-exported module alias.
        chains = {
            'scripts/middle.py': 'from scripts import inputs\nPINNED = (inputs.SHARED,)\n',
            'scripts/build_fes_alpha.py': 'from scripts import middle\nX = middle.PINNED\n',
            'scripts/build_fes_gamma.py': 'from scripts import middle\nY = middle.inputs.OTHER\n',
        }
        components = {row['component'] for row in plan.core_consumers(target, dict(base, **chains))}
        self.assertIn('core:alpha', components)
        self.assertIn('core:gamma', components)
        # A full-image producer that imports the input forces FULL_IMAGE.
        texts = dict(base, **{'scripts/build_fes_splash.py': 'from scripts import inputs\nX = inputs.SHARED\n'})
        self.assertEqual(plan.plan([target], texts)['decision'], 'FULL_IMAGE')

    def test_changed_script_expands_to_importing_producers(self):
        plan = hil_module()
        texts = plan.script_texts()
        scripts = 'sources/misteross/scripts/'
        # The splash imports build_fes_pong and fes_de10nano_evidence; a video-part
        # producer imports build_fes_coleco_oss.
        for name in ('build_fes_pong.py', 'fes_de10nano_evidence.py', 'build_fes_coleco_oss.py'):
            with self.subTest(script=name):
                self.assertEqual(plan.plan([scripts + name], texts)['decision'], 'FULL_IMAGE')
        result = plan.plan([scripts + 'build_fes_menu.py'], texts)
        # The package producer imports this script. Both names canonicalize to
        # core:menu, so the overlay does not invent a second component.
        self.assertIn('core:menu', result['components'])
        self.assertNotIn('core:menu-package', result['components'])

        base = {'scripts/helper.py': 'X = 1\n',
                'scripts/build_fes_beta.py': 'Y = 2\n',
                'scripts/sim_beta.py': 'from scripts import build_fes_beta\n'}
        cases = {
            'direct import': ({'scripts/build_fes_alpha.py': 'from scripts import build_fes_beta\n'},
                              'OVERLAY core:alpha core:beta'),
            'transitive import': ({'scripts/middle.py': 'from scripts.build_fes_beta import Y\n',
                                   'scripts/build_fes_alpha.py': 'from scripts import middle\n'},
                                  'OVERLAY core:ALL core:alpha core:beta'),
            'relative import': ({'scripts/build_fes_alpha.py': 'from .build_fes_beta import Y\n'},
                                'OVERLAY core:alpha core:beta'),
            'full importer': ({'scripts/build_fes_splash.py': 'import scripts.build_fes_beta\n'}, 'FULL_IMAGE'),
            'no importer': ({'scripts/build_fes_alpha.py': 'from scripts import helper\n'}, 'OVERLAY core:beta'),
            'unresolved elsewhere': ({'scripts/build_fes_alpha.py': 'from scripts import missing\n'}, 'FULL_IMAGE'),
        }
        for name, (extra, decision) in cases.items():
            with self.subTest(case=name):
                result = plan.plan([scripts + 'build_fes_beta.py'], dict(base, **extra))
                self.assertEqual(result['decision'], decision)
        # A sim_* change stays out of the plan unless a deploying script imports it.
        texts = dict(base, **{'scripts/sim_gamma.py': 'X = 1\n'})
        self.assertEqual(plan.plan([scripts + 'sim_gamma.py'], texts)['decision'], 'NO_DEPLOY_CHANGE')
        texts['scripts/build_fes_alpha.py'] = 'from scripts import sim_gamma\n'
        self.assertEqual(plan.plan([scripts + 'sim_gamma.py'], texts)['decision'], 'OVERLAY core:alpha')

    def test_unresolved_script_imports_fail_closed(self):
        plan = hil_module()
        target = 'sources/misteross/cores/fes-beta/rtl/shared.v'
        cases = {
            'missing scripts module': 'from scripts import missing\n',
            'missing from-module': 'from scripts.missing import X\n',
            'nested scripts module': 'import scripts.sub.mod\n',
            'parent relative import': 'from ..other import X\n',
            'computed dynamic import': 'import importlib\nM = importlib.import_module("scripts." + "x")\n',
            'computed __import__': 'N = "x"\nM = __import__(N)\n',
            'syntax error': 'def broken(:\n',
        }
        for name, text in cases.items():
            with self.subTest(case=name):
                texts = {'scripts/helper.py': text, 'scripts/build_fes_beta.py': 'X = 1\n'}
                result = plan.plan([target], texts)
                self.assertEqual(result['decision'], 'FULL_IMAGE')
                self.assertEqual(result['rows'][0]['component'], 'unresolved-script-import')
        # Outside modules and literal imports of real scripts are fine.
        texts = {'scripts/helper.py': 'import json\nimport importlib\nM = importlib.import_module("json")\n',
                 'scripts/build_fes_beta.py': 'from scripts import helper\n'}
        self.assertEqual(plan.plan([target], texts)['decision'], 'OVERLAY core:beta')
        # sim_* scripts never deploy, so their imports do not matter.
        texts = {'scripts/sim_beta.py': 'from scripts import missing\n', 'scripts/build_fes_beta.py': 'X = 1\n'}
        self.assertEqual(plan.plan([target], texts)['decision'], 'OVERLAY core:beta')

    def test_video_part_inputs_force_full_image(self):
        result = run('classify', '--paths-file',
                     self.path_file('sources/misteross/cores/fes-common/rtl/fes_video_part_direct.v'), '--json')
        self.assertEqual(json.loads(result.stdout)['decision'], 'FULL_IMAGE')
        self.git_repo([])
        scripts = self.repo / 'sources/misteross/scripts'
        scripts.mkdir(parents=True)
        (scripts / 'build_x_video_part.py').write_text('RTL = "cores/fes-common/rtl/part.v"\n')
        (scripts / 'build_fes_splash.py').write_text('RTL = "cores/fes-beta/rtl/logo.v"\n')
        for name in ('fes-common/rtl/part.v', 'fes-common/rtl/other.v', 'fes-beta/rtl/logo.v'):
            path = self.repo / 'sources/misteross/cores' / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('base\n')
        self.git('add', '.')
        self.git('commit', '-m', 'scripts')
        base = self.git('rev-parse', 'HEAD')
        for name, decision in (('fes-common/rtl/part.v', 'FULL_IMAGE'), ('fes-beta/rtl/logo.v', 'FULL_IMAGE'),
                               ('fes-common/rtl/other.v', 'OVERLAY core:ALL')):
            with self.subTest(name=name):
                self.git('checkout', '-q', base)
                (self.repo / 'sources/misteross/cores' / name).write_text('change\n')
                self.git('commit', '-qam', 'change ' + name)
                head = self.git('rev-parse', 'HEAD')
                result = run('classify', '--repo', self.repo, '--base-image-commit', base,
                             '--head', head, '--json')
                self.assertEqual(json.loads(result.stdout)['decision'], decision)

    def test_full_image_unrecognised_and_docs_only(self):
        for path in ('image/buildroot/board/x/etc/init.d/S42x', 'unknown/file.bin',
                     'sources/misteross/sealed/fes-splash.rbf',
                     'sources/misteross/sealed/fes-splash.build-summary.json',
                     'sources/misteross/cores/fes-splash/rtl/top.v',
                     'sources/misteross/scripts/build_fes_splash.py',
                     'sources/misteross/scripts/build_atari_st_video_part.py',
                     'sources/misteross/scripts/build_video_part.py',
                     'sources/misteross/scripts/video_parts.py',
                     'sources/FogCast/appliance/software/manifest.go'):
            result = run('classify', '--paths-file', self.path_file(path), '--json')
            data = json.loads(result.stdout)
            self.assertEqual(deployment(data), 'FULL_IMAGE')
            self.assertEqual(result.returncode, 3 if data['decision'] == 'COMMAND_EVIDENCE_REQUIRED' else 0)
        for path in ('sources/FogCast/internal/example.go', 'sources/FogCast/catalog/example.go'):
            result = run('classify', '--paths-file', self.path_file(path), '--json')
            data = json.loads(result.stdout)
            self.assertEqual(result.returncode, 3)
            self.assertEqual(data['decision'], 'COMMAND_EVIDENCE_REQUIRED')
            self.assertEqual(deployment(data), 'OVERLAY kit-go+host')
        result = run('classify', '--paths-file',
                     self.path_file('sources/FogCast/internal/targetimage/lock.go'), '--json')
        data = json.loads(result.stdout)
        self.assertEqual(result.returncode, 3)
        self.assertEqual(deployment(data), 'OVERLAY host:target-image-lock')
        self.assertIn('target-image-lock', {item['command'] for item in data['command_evidence']})
        result = run('classify', '--paths-file', self.path_file('docs/guide.md'), '--json')
        self.assertEqual(json.loads(result.stdout)['decision'], 'NO_DEPLOY_CHANGE')

    def test_ci_tooling_changes_do_not_request_an_image_or_kit(self):
        for path in ('scripts/affected.py', 'scripts/test_changed.py',
                     'scripts/check_deadcode.py', 'scripts/host_tests.py',
                     'scripts/host_affected.py',
                     'scripts/test_policy.py', 'scripts/ci_gate.py',
                     'scripts/hil_plan.py', 'scripts/consistency.py',
                     'scripts/generate.py'):
            with self.subTest(path=path):
                result = run('classify', '--paths-file', self.path_file(path), '--json')
                self.assertEqual(json.loads(result.stdout)['decision'], 'NO_DEPLOY_CHANGE')
        # Mixed changes retain the real deployment requirement.
        result = run('classify', '--paths-file',
                     self.path_file('scripts/check_deadcode.py\nimage/buildroot/config'), '--json')
        self.assertEqual(json.loads(result.stdout)['decision'], 'FULL_IMAGE')

    def test_standalone_de25_bringup_has_no_deployed_consumer(self):
        planner = hil_module()
        paths = ['sources/mister-packages/packages/board/de25_nano.yaml',
                 'sources/mister-packages/packages/fabric/fes_fabric_de25_ddr_probe.yaml',
                 'sources/mister-packages/packages/fabric/fes_fabric_de25_scanout.yaml',
                 'sources/mister-packages/packages/fabric/fes_fabric_de25_computer.yaml',
                 'sources/mister-packages/packages/fabric/fes_fabric_de25_ramtest.yaml',
                 'sources/mister-packages/packages/fabric/fes_fabric_de25_pr_control.yaml',
                 'image/kernel/de25-dma/de25_dma_test.c',
                 *['sources/misteross/boards/de25nano/' + name
                   for name in ('blinky.sv', 'pins.qsf', 'clocks.sdc', 'Makefile',
                                'test_de25nano.py', 'sim/blinky_tb.cpp',
                                'ramtest/endpoint.v', 'ramtest/jtag.tcl',
                                'ramtest/generated/fes_de25_ramtest.vh')]]
        result = planner.plan(paths, texts={})
        self.assertEqual(result['decision'], 'NO_DEPLOY_CHANGE')
        self.assertTrue(all(row['class'] == 'none' for row in result['rows']))
        self.assertEqual(planner.plan([
            'sources/mister-packages/packages/fabric/fes_fabric_de25_pr_control.yaml',
            'sources/libmister-runtime/src/native/de25_pr_control.cpp'], texts={})['decision'],
            'OVERLAY mister-runtime')
        for path in ('sources/mister-packages/packages/board/de10_nano.yaml',
                     'sources/mister-packages/packages/board/de25_nano_other.yaml',
                     'sources/mister-packages/packages/platform/de25_nano.yaml',
                     'sources/mister-packages/packages/fabric/fes_fabric_de25_ramtest_other.yaml',
                     'sources/mister-packages/packages/abi/fes_application.yaml'):
            with self.subTest(path=path):
                self.assertEqual(planner.plan([*paths, path], texts={})['decision'], 'FULL_IMAGE')
        for path in ('sources/misteross/boards/de10nano/pins.qsf',
                     'sources/misteross/boards/de25nano-other/pins.qsf',
                     'sources/misteross/boards/newboard/top.sv'):
            with self.subTest(path=path):
                self.assertEqual(planner.plan([*paths, path], texts={})['decision'],
                                 'OVERLAY core:ALL')

    def test_de25_deployed_sources_select_board_specific_packages(self):
        planner = hil_module()
        for name, component in (
                ('zx81-hps/fes_de25_zx81_standard.v', 'core:de25-zx81'),
                ('zx81-hps/standard-clocks.sdc', 'core:de25-zx81'),
                ('zx81/build.py', 'core:de25-zx81'),
                ('scanout/fes_de25_session_display.v', 'core:de25-ALL'),
                ('scanout/generated/fes_de25_scanout.vh', 'core:de25-ALL'),
                ('display/vendor.lock.toml', 'core:de25-ALL'),
                ('hps/fes_de25_top_reset.v', 'core:de25-ALL'),
                ('combined/build.py', 'core:de25-ALL'),
                ('build.py', 'core:de25-ALL'),
                ('new-component/new.v', 'core:de25-ALL')):
            with self.subTest(name=name):
                result = planner.plan(['sources/misteross/boards/de25nano/' + name], texts={})
                self.assertEqual(result['decision'], 'OVERLAY ' + component)
                self.assertNotIn('core:ALL', result['components'])
        for name in ('zx81-hps/standard_expansion_harness.v',
                     'zx81-hps/standard_expansion_tb.cpp', 'zx81-hps/standard_expansion_sim.py',
                     'zx81-hps/sim.py', 'scanout/test_build.py'):
            with self.subTest(name=name):
                self.assertEqual(planner.plan([
                    'sources/misteross/boards/de25nano/' + name], texts={})['class'], 'none')

    def test_actual_de25_package_board_closures_cannot_skip_hardware(self):
        import importlib.util
        planner = hil_module()
        board = ROOT / 'sources/misteross/boards/de25nano'
        for relative, capture in (('zx81-hps/build.py', 'capture_standard_board'),
                                  ('scanout/build.py', 'capture')):
            spec = importlib.util.spec_from_file_location('hil_de25_closure', board / relative)
            module = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(module)
            paths = [path for path in getattr(module, capture)()
                     if path.startswith('sources/misteross/boards/de25nano/')]
            self.assertTrue(paths)
            for path in paths:
                with self.subTest(producer=relative, path=path):
                    row = planner.classify_path(path)
                    self.assertEqual(row['class'], 'overlay')
                    self.assertTrue(row['component'].startswith('core:de25-'))

    def test_de25_producer_changes_include_both_package_consumers(self):
        planner = hil_module()
        menu = 'sources/misteross/boards/de25nano/producers/build_menu_package.py'
        zx81 = 'sources/misteross/boards/de25nano/producers/build_zx81_package.py'
        result = planner.plan([menu], texts={menu: 'X = 1', zx81:
            'from boards.de25nano.producers import build_menu_package as common\nX = common.X\n'})
        self.assertEqual(result['components'], ['core:de25-menu', 'core:de25-zx81'])
        self.assertEqual(result['class'], 'overlay')
        self.assertEqual(planner.plan([zx81], texts={})['decision'], 'OVERLAY core:de25-zx81')
        self.assertEqual(planner.plan([
            'sources/misteross/boards/de25nano/producers/review_audio_clocks.py'], texts={})[
                'decision'], 'OVERLAY core:de25-zx81')

    def test_board_local_de25_inputs_select_their_package_consumers(self):
        planner = hil_module()
        for path, component in (
                ('boards/de25nano/shared-rtl/fes_zx81_core.v', 'core:de25-zx81'),
                ('boards/de25nano/shared-rtl/fes_menu_reader.v', 'core:de25-menu')):
            with self.subTest(path=path):
                row = planner.classify_path('sources/misteross/' + path)
                components = {row['component'], *row.get('consumers', ())}
                self.assertIn(component, components)
        # A producer naming the core root reads descendants, but not its
        # similarly named neighbour. No "cores"-wide literal is inferred.
        producer = 'sources/misteross/scripts/build_fes_alpha.py'
        texts = {producer: 'SOURCE_ROOTS = ["cores/fes-beta"]\n'}
        self.assertEqual(planner.plan([
            'sources/misteross/cores/fes-beta/rtl/core.v'], texts)['components'],
            ['core:alpha', 'core:beta'])
        self.assertEqual(planner.plan([
            'sources/misteross/cores/fes-beta-other/rtl/core.v'], texts)['components'],
            ['core:beta-other'])

    def test_native_de25_kernel_and_image_recipe_inputs_require_boot(self):
        from scripts import de25_kernel_platform, de25_owner_image
        planner = hil_module()
        # The real production integrator copies dep.DRIVER into the vendor
        # kernel. Do not retain its original QEMU-only HIL exemption.
        driver = ROOT / de25_kernel_platform.dep.DRIVER
        paths = {path.relative_to(ROOT).as_posix() for path in driver.rglob('*')
                 if path.is_file() and path.suffix != '.md' and path.name != 'de25_dma_test.c'}
        paths.update(de25_owner_image.RECIPE_FILES)
        for path in sorted(paths):
            with self.subTest(path=path):
                self.assertEqual(planner.classify_path(path)['class'], 'full')
        for path in ('image/kernel/de25-dma/de25_dma.c',
                     'image/kernel/de25-dma/de25_dma_owner.c',
                     'image/kernel/de25-dma/generated/fes_de25_scanout.h',
                     'scripts/de25_kernel_test.py', 'scripts/de25_boot_contract.py',
                     'scripts/de25_first_boot.py'):
            self.assertEqual(planner.classify_path(path)['component'], 'de25-nano-linux')
        self.assertEqual(planner.plan(['image/kernel/de25-dma/de25_dma_test.c'], texts={})[
            'decision'], 'NO_DEPLOY_CHANGE')

    def test_board_validation_tools_preserve_generated_consumer_requirements(self):
        planner = hil_module()
        paths = [
            'sources/mister-packages/cmd/mister-packages/main.go',
            'sources/mister-packages/internal/pack/board.go',
            'sources/mister-packages/internal/pack/load.go',
            'sources/mister-packages/internal/emitc/emitc.go',
            'sources/mister-packages/internal/pack/board_test.go',
            'sources/mister-packages/cmd/mister-packages/main_test.go',
            'sources/mister-packages/packages/board/de25_nano.yaml',
            'sources/misteross/boards/de25nano/blinky.v',
            'sources/misteross/boards/de25nano/test_build.py',
            'sources/misteross/boards/de25nano/Makefile',
        ]
        result = planner.plan(paths, texts={})
        self.assertEqual(result['decision'], 'NO_DEPLOY_CHANGE')
        self.assertTrue(all(row['class'] == 'none' for row in result['rows']))
        runtime_header = 'sources/libmister-runtime/src/native/generated/de10_nano.hpp'
        self.assertEqual(planner.plan([*paths, runtime_header], texts={})['decision'],
                         'OVERLAY mister-runtime')
        for path in ('sources/mister-packages/packages/abi/fes_application.yaml',
                     'sources/mister-packages/internal/emitcpp/emitcpp.go',
                     'sources/mister-packages/internal/emitc/other.go',
                     'sources/mister-packages/internal/pack/abi.go',
                     'sources/mister-packages/internal/pack/other.go'):
            with self.subTest(path=path):
                self.assertEqual(planner.plan([*paths, path], texts={})['decision'],
                                 'FULL_IMAGE')


    def test_two_tree_drift_forces_full_and_rename_out_of_image(self):
        self.repo = self.work / 'repo'
        self.repo.mkdir()
        self.git('init', '-q')
        self.git('config', 'user.email', 'test@example.com')
        self.git('config', 'user.name', 'Test')
        (self.repo / 'base.txt').write_text('base')
        self.git('add', '.')
        self.git('commit', '-m', 'base')
        base = self.git('rev-parse', 'HEAD')
        (self.repo / 'image').mkdir()
        (self.repo / 'image/config').write_text('drift')
        self.git('add', '.')
        self.git('commit', '-m', 'drift')
        merge_base = self.git('rev-parse', 'HEAD')
        (self.repo / 'sources/libmister-runtime').mkdir(parents=True)
        (self.repo / 'sources/libmister-runtime/change').write_text('code')
        self.git('add', '.')
        self.git('commit', '-m', 'overlay')
        head = self.git('rev-parse', 'HEAD')
        self.assertEqual(json.loads(run('classify', '--repo', self.repo,
                                        '--base-image-commit', merge_base, '--head', head,
                                        '--json').stdout)['class'], 'overlay')
        result = run('classify', '--repo', self.repo, '--base-image-commit', base,
                     '--head', head, '--json')
        self.assertEqual(json.loads(result.stdout)['decision'], 'FULL_IMAGE')

        image_file = self.repo / 'image/S50agent'
        image_file.parent.mkdir(exist_ok=True)
        image_file.write_text('x' * 20)
        self.git('add', '.')
        self.git('commit', '-m', 'image file')
        image_commit = self.git('rev-parse', 'HEAD')
        self.git('mv', 'image/S50agent', 'sources/libmister-runtime/S50agent')
        self.git('commit', '-m', 'rename from image')
        renamed = self.git('rev-parse', 'HEAD')
        result = run('classify', '--repo', self.repo, '--base-image-commit', image_commit,
                     '--head', renamed, '--json')
        data = json.loads(result.stdout)
        self.assertIn('image/S50agent', [row['path'] for row in data['rows']])
        self.assertEqual(data['decision'], 'FULL_IMAGE')

    def test_pr_diff_excludes_the_image_gap(self):
        self.repo = self.work / 'repo'
        self.repo.mkdir()
        self.git('init', '-q')
        self.git('config', 'user.email', 'test@example.com')
        self.git('config', 'user.name', 'Test')
        (self.repo / 'README').write_text('base')
        self.git('add', '.')
        self.git('commit', '-m', 'image')
        self.git('branch', '-M', 'main')
        image = self.git('rev-parse', 'HEAD')
        (self.repo / 'image').mkdir()
        (self.repo / 'image/config').write_text('drift')
        self.git('add', '.')
        self.git('commit', '-m', 'drift')
        self.git('checkout', '-q', '-b', 'pr')
        (self.repo / 'sources/libmister-runtime').mkdir(parents=True)
        (self.repo / 'sources/libmister-runtime/change').write_text('code')
        self.git('add', '.')
        self.git('commit', '-m', 'overlay')
        head = self.git('rev-parse', 'HEAD')
        result = run('classify', '--repo', self.repo, '--base-image-commit', image,
                     '--head', head, '--json')
        self.assertEqual(result.returncode, 0, result.stderr)
        data = json.loads(result.stdout)
        self.assertEqual(data['class'], 'overlay')
        self.assertEqual([row['path'] for row in data['rows']],
                         ['sources/libmister-runtime/change'])
        self.assertEqual(data['image_gap']['relation'], 'behind')
        self.assertGreaterEqual(data['image_gap']['commits'], 1)
        self.assertIn('image/config', data['image_gap']['paths'])
        text = run('classify', '--repo', self.repo, '--base-image-commit', image,
                   '--head', head)
        self.assertIn('WARNING:', text.stdout)
        self.assertIn('not part of the decision', text.stdout)
        self.assertIn('DECISION: OVERLAY', text.stdout)
        strict = run('classify', '--repo', self.repo, '--base-image-commit', image,
                     '--head', head, '--strict', '--json')
        strict_data = json.loads(strict.stdout)
        self.assertEqual(strict_data['decision'], 'FULL_IMAGE')
        self.assertNotIn('image_gap', strict_data)

    def test_disconnected_origin_main_falls_back_to_related_local_main(self):
        image = self.start_repo()
        (self.repo / 'image').mkdir()
        (self.repo / 'image/config').write_text('already installed\n')
        self.git('add', '.')
        self.git('commit', '-m', 'image config')
        local_main = self.git('rev-parse', 'HEAD')

        self.git('checkout', '-q', '-b', 'pr')
        runtime = self.repo / 'sources/libmister-runtime/change.cpp'
        runtime.parent.mkdir(parents=True)
        runtime.write_text('overlay\n')
        self.git('add', '.')
        self.git('commit', '-m', 'runtime overlay')
        head = self.git('rev-parse', 'HEAD')

        self.git('checkout', '-q', '--orphan', 'unrelated')
        self.git('commit', '--allow-empty', '-m', 'unrelated origin main')
        unrelated_main = self.git('rev-parse', 'HEAD')
        self.git('update-ref', 'refs/remotes/origin/main', unrelated_main)
        self.git('checkout', '-q', 'pr')

        result = run('classify', '--repo', self.repo, '--base-image-commit', image,
                     '--head', head, '--json')
        self.assertEqual(result.returncode, 0, result.stderr)
        data = json.loads(result.stdout)
        self.assertEqual(data['class'], 'overlay')
        self.assertEqual(data['image_gap']['pr_base'], local_main)
        self.assertEqual([row['path'] for row in data['rows']],
                         ['sources/libmister-runtime/change.cpp'])

    def test_merged_pr_head_falls_back_to_image_to_head_diff(self):
        image = self.start_repo()
        self.base = image
        self.git('checkout', '-q', '-b', 'pr')
        runtime = self.repo / 'sources/libmister-runtime/change.cpp'
        runtime.parent.mkdir(parents=True)
        runtime.write_text('overlay\n')
        self.git('add', '.')
        self.git('commit', '-m', 'runtime overlay')
        self.head = self.git('rev-parse', 'HEAD')
        self.git('checkout', '-q', 'main')
        self.git('merge', '--no-ff', '--no-edit', 'pr')
        main_tip = self.git('rev-parse', 'HEAD')
        self.git('update-ref', 'refs/remotes/origin/main', main_tip)

        result = run('classify', '--repo', self.repo, '--base-image-commit', image,
                     '--head', self.head, '--json')
        self.assertEqual(result.returncode, 0, result.stderr)
        data = json.loads(result.stdout)
        self.assertEqual(data['class'], 'overlay')
        self.assertIn('sources/libmister-runtime/change.cpp',
                      [row['path'] for row in data['rows']])

    def test_origin_main_at_head_does_not_fall_back_to_stale_local_main(self):
        image = self.start_repo()
        self.base = image
        self.git('checkout', '-q', '-b', 'image-gap')
        (self.repo / 'image').mkdir()
        (self.repo / 'image/config').write_text('already installed\n')
        self.git('add', '.')
        self.git('commit', '-m', 'image config')
        image_with_gap = self.git('rev-parse', 'HEAD')
        self.git('checkout', '-q', '-b', 'pr')
        runtime = self.repo / 'sources/libmister-runtime/change.cpp'
        runtime.parent.mkdir(parents=True)
        runtime.write_text('overlay\n')
        self.git('add', '.')
        self.git('commit', '-m', 'runtime overlay')
        self.head = self.git('rev-parse', 'HEAD')
        self.git('update-ref', 'refs/remotes/origin/main', self.head)

        result = run('classify', '--repo', self.repo, '--base-image-commit', image_with_gap,
                     '--head', self.head, '--json')
        self.assertEqual(result.returncode, 0, result.stderr)
        data = json.loads(result.stdout)
        self.assertEqual(data['class'], 'overlay')
        self.assertEqual([row['path'] for row in data['rows']],
                         ['sources/libmister-runtime/change.cpp'])

    def test_diverged_image_is_a_warning_not_a_decision(self):
        self.repo = self.work / 'repo'
        self.repo.mkdir()
        self.git('init', '-q')
        self.git('config', 'user.email', 'test@example.com')
        self.git('config', 'user.name', 'Test')
        (self.repo / 'README').write_text('base')
        self.git('add', '.')
        self.git('commit', '-m', 'root')
        self.git('branch', '-M', 'main')
        self.git('checkout', '-q', '-b', 'kit')
        (self.repo / 'image').mkdir()
        (self.repo / 'image/config').write_text('kit')
        self.git('add', '.')
        self.git('commit', '-m', 'kit image')
        image = self.git('rev-parse', 'HEAD')
        self.git('checkout', '-q', 'main')
        (self.repo / 'docs').mkdir()
        (self.repo / 'docs/drift').write_text('main')
        self.git('add', '.')
        self.git('commit', '-m', 'main drift')
        self.git('checkout', '-q', '-b', 'pr')
        (self.repo / 'sources/libmister-runtime').mkdir(parents=True)
        (self.repo / 'sources/libmister-runtime/change').write_text('code')
        self.git('add', '.')
        self.git('commit', '-m', 'overlay')
        head = self.git('rev-parse', 'HEAD')
        result = run('classify', '--repo', self.repo, '--base-image-commit', image,
                     '--head', head, '--json')
        self.assertEqual(result.returncode, 0, result.stderr)
        data = json.loads(result.stdout)
        self.assertEqual(data['class'], 'overlay')
        self.assertEqual([row['path'] for row in data['rows']],
                         ['sources/libmister-runtime/change'])
        self.assertEqual(data['image_gap']['relation'], 'diverged')
        self.assertIn('docs/drift', data['image_gap']['paths'])
        text = run('classify', '--repo', self.repo, '--base-image-commit', image,
                   '--head', head)
        self.assertIn('has diverged', text.stdout)
        self.assertIn('DECISION: OVERLAY', text.stdout)

    def test_shared_package_preserves_base_consumers_removed_at_head(self):
        planner = hil_module()
        package = 'sources/FogCast/internal/example'
        path = package + '/changed.go'
        def entry(role, packages):
            return {'role': role, 'graphs': [{'targets': ['linux-amd64'],
                    'packages': packages, 'embedded': [], 'embed_patterns': []}]}
        base = {'fes-update': entry('host-tool', [package])}
        head = {'fes-update': entry('host-tool', [])}
        result = planner.plan([path], texts={}, go_inventories=[base, head], go_graph_changes=[])
        self.assertEqual(result['decision'], 'COMMAND_EVIDENCE_REQUIRED')
        self.assertEqual(result['deployment_decision'], 'OVERLAY kit-go+host')
        self.assertEqual([item['command'] for item in result['command_evidence']], ['fes-update'])
        self.assertEqual(result['command_evidence'][0]['paths'], [path])

    def test_embedded_docs_are_classified_by_actual_consumers(self):
        planner = hil_module()
        path = 'sources/FogCast/internal/example/README.md'
        def entry(role):
            return {'role': role, 'graphs': [{'targets': ['linux-armv7'],
                    'packages': ['sources/FogCast/internal/example'],
                    'embedded': [path], 'embed_patterns': []}]}
        for role, name, deployment in (('kit', 'mister-agent', 'OVERLAY kit-go+host'),
                                       ('image', 'fes-kit-data', 'NO_DEPLOY_CHANGE'),
                                       ('host-tool', 'fes-update', 'NO_DEPLOY_CHANGE')):
            with self.subTest(role=role):
                result = planner.plan([path], texts={}, go_inventories=[{name: entry(role)}],
                                      go_graph_changes=[])
                if role == 'kit':
                    self.assertEqual(result['decision'], deployment)
                else:
                    self.assertEqual(result['decision'], 'COMMAND_EVIDENCE_REQUIRED')
                    self.assertEqual(result['deployment_decision'], deployment)
                    self.assertEqual(result['command_evidence'][0]['embedded_paths'], [path])

    def test_nested_appliance_module_affects_all_its_command_consumers(self):
        planner = hil_module()
        inventory = planner.read_go_inventory()
        path = 'sources/FogCast/appliance/go.mod'
        result = planner.plan([path], texts={}, go_inventories=[inventory], go_graph_changes=[])
        self.assertEqual(result['decision'], 'COMMAND_EVIDENCE_REQUIRED')
        required = {item['command'] for item in result['command_evidence']}
        self.assertTrue({'fes-boot', 'fes-kit-software', 'fes-kit-data'} <= required, required)
        self.assertNotIn('target-image-lock', required)
        self.assertNotIn('mister-bridge', {item['command'] for item in result['advisory_commands']})
        self.assertIn(path, result['go_graph_unverified'])

    def test_go_source_under_docs_or_testdata_is_not_assumed_documentation(self):
        planner = hil_module()
        for directory, filename in (('docs/example', 'code.go'),
                                    ('testdata/example', 'code.go'),
                                    ('docs/example', 'asm.s')):
            with self.subTest(directory=directory, filename=filename):
                package = 'sources/FogCast/' + directory
                path = package + '/' + filename
                inventory = {'mister-agent': {'role': 'kit', 'graphs': [{
                    'targets': ['linux-armv7'], 'packages': [package],
                    'embedded': [], 'embed_patterns': []}]}}
                known = planner.plan([path], texts={}, go_inventories=[inventory], go_graph_changes=[])
                self.assertEqual(known['decision'], 'OVERLAY kit-go+host')
                unknown = planner.plan([path], texts={}, go_inventories=[{}], go_graph_changes=[])
                self.assertEqual(unknown['decision'], 'COMMAND_EVIDENCE_REQUIRED')
                self.assertIn(path, unknown['go_graph_unverified'])

    def test_direct_host_and_optional_developer_commands(self):
        planner = hil_module()
        for name in ('fes-update', 'fes-software'):
            with self.subTest(command=name):
                path = f'sources/FogCast/cmd/{name}/main.go'
                result = planner.plan([path], texts={}, go_graph_changes=[])
                self.assertEqual(result['decision'], 'COMMAND_EVIDENCE_REQUIRED')
                self.assertIn(name, {item['command'] for item in result['command_evidence']})
                self.assertNotEqual(result['deployment_decision'], 'FULL_IMAGE')
        for name in ('mister-bridge', 'fes-parts', 'fes-spectrum-disk', 'fes-spectrum-restore'):
            with self.subTest(command=name):
                path = f'sources/FogCast/cmd/{name}/main.go'
                result = planner.plan([path], texts={}, go_graph_changes=[])
                self.assertEqual(result['decision'], 'NO_DEPLOY_CHANGE')
                self.assertIn(name, {item['command'] for item in result['advisory_commands']})

    def test_changed_imports_and_missing_historical_inventory_block(self):
        planner = hil_module()
        self.repo = self.work / 'repo'
        self.repo.mkdir()
        self.git('init', '-q')
        self.git('config', 'user.email', 'test@example.com')
        self.git('config', 'user.name', 'Test')
        source = self.repo / 'sources/FogCast/internal/example/example.go'
        source.parent.mkdir(parents=True)
        source.write_text('package example\nimport "fmt"\nvar _ = fmt.Sprint\n')
        self.git('add', '.')
        self.git('commit', '-qm', 'old source without inventory')
        base = self.git('rev-parse', 'HEAD')
        source.write_text('package example\nimport "strings"\nvar _ = strings.TrimSpace\n')
        self.git('add', '.')
        self.git('commit', '-qm', 'changed import')
        head = self.git('rev-parse', 'HEAD')
        changed = planner.go_import_changes(self.repo, base, head,
                                             ['sources/FogCast/internal/example/example.go'])
        self.assertEqual(changed, ['sources/FogCast/internal/example/example.go'])
        result = run('classify', '--repo', self.repo, '--base-image-commit', base,
                     '--head', head, '--strict', '--json')
        self.assertEqual(result.returncode, 3, result.stderr)
        data = json.loads(result.stdout)
        self.assertEqual(data['decision'], 'COMMAND_EVIDENCE_REQUIRED')
        self.assertTrue(any('inventory' in reason for reason in data['go_graph_unverified']))

    def test_git_classification_uses_both_revisions_for_embedded_assets(self):
        planner = hil_module()
        self.repo = self.work / 'repo'
        self.repo.mkdir()
        self.git('init', '-q')
        self.git('config', 'user.email', 'test@example.com')
        self.git('config', 'user.name', 'Test')
        path = 'sources/FogCast/docs/planner-fixture.md'
        asset = self.repo / path
        asset.parent.mkdir(parents=True)
        asset.write_text('base asset\n')
        inventory_file = self.repo / planner.GO_INVENTORY
        inventory_file.parent.mkdir(parents=True)
        commands = planner.read_go_inventory()
        commands['fes-update']['graphs'][0]['embedded'].append(path)
        inventory_file.write_text(json.dumps({'version': 1, 'commands': commands}))
        self.git('add', '.')
        self.git('commit', '-qm', 'base inventory')
        base = self.git('rev-parse', 'HEAD')

        # The base-only host consumer and head-only image consumer must both
        # appear, even though the executing checkout knows neither relationship.
        commands['fes-update']['graphs'][0]['embedded'].remove(path)
        commands['fes-kit-data']['graphs'][0]['embedded'].append(path)
        inventory_file.write_text(json.dumps({'version': 1, 'commands': commands}))
        asset.write_text('head asset\n')
        self.git('add', '.')
        self.git('commit', '-qm', 'head inventory')
        head = self.git('rev-parse', 'HEAD')

        result = run('classify', '--repo', self.repo, '--base-image-commit', base,
                     '--head', head, '--strict', '--json')
        self.assertEqual(result.returncode, 3, result.stderr)
        data = json.loads(result.stdout)
        self.assertEqual(data['decision'], 'COMMAND_EVIDENCE_REQUIRED')
        commands = {item['command'] for item in data['command_evidence']}
        self.assertTrue({'fes-update', 'fes-kit-data'} <= commands, commands)
        self.assertIn(path, data['go_graph_unverified'])
        self.assertFalse(any('inventory missing' in reason for reason in data['go_graph_unverified']))

    def path_file(self, path):
        filename = self.work / 'paths.txt'
        filename.write_text(path + '\n')
        return filename


if __name__ == '__main__':
    unittest.main()
