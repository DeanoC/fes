import dataclasses
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from scripts import de25_seal_plan as sp

ROOT = Path(__file__).resolve().parents[1]


def ids(design='d', toolchain='t'):
    return {'design': design, 'toolchain': toolchain}


def probed(**models):
    """core_id -> {model: identities}; model None means a single-package producer."""
    result = {}
    for core_id, by_model in models.items():
        result[core_id] = {'models': list(by_model), 'error': None,
                           'identities': {(m or ''): v for m, v in by_model.items()}}
    return result


class RegistryTests(unittest.TestCase):
    def test_registered_recipes_are_de25_quartus_pro_producers(self):
        recipes = sp.registered()
        self.assertIn('fes.menu', recipes)
        self.assertIn('fes.spectrum', recipes)
        self.assertIn('fes.atari-st', recipes)
        for recipe in recipes.values():
            self.assertTrue(recipe.producer_script.startswith('boards/de25nano/producers/'))
            self.assertTrue((ROOT / 'sources/misteross' / recipe.producer_script).is_file())

    def test_a_non_pro_recipe_is_refused(self):
        recipe = sp.registered()['fes.menu']
        lite = dataclasses.replace(recipe, quartus=dict(recipe.quartus, edition='lite'))
        original = sp.load_recipes
        sp.load_recipes = lambda *a, **k: {'fes.menu': lite}
        try:
            with self.assertRaisesRegex(sp.PlanError, 'not a DE25 Quartus Pro'):
                sp.registered()
        finally:
            sp.load_recipes = original

    def test_an_empty_base_registry_is_allowed_for_auto_comparison(self):
        with mock.patch.object(sp, 'load_recipes', return_value={}):
            self.assertEqual(sp.registered(Path('base/core-recipes.toml'), allow_empty=True), {})
            with self.assertRaisesRegex(sp.PlanError, 'no registered DE25 recipes'):
                sp.registered(Path('base/core-recipes.toml'))


class SelectionTests(unittest.TestCase):
    def setUp(self):
        self.recipes = sp.registered()

    def test_parse(self):
        self.assertIsNone(sp.parse_selection('auto', self.recipes))
        self.assertEqual(sp.parse_selection('fes.spectrum@128,fes.menu', self.recipes),
                         [('fes.spectrum', '128'), ('fes.menu', None)])
        self.assertEqual({core for core, _ in sp.parse_selection('all', self.recipes)}, set(self.recipes))
        for bad in ('', 'fes.pong', 'build_menu_package.py', 'fes.menu@', 'fes.menu,fes.menu', '../x'):
            with self.assertRaises(sp.PlanError, msg=bad):
                sp.parse_selection(bad, self.recipes)

    def test_auto_selects_only_moved_models(self):
        base = probed(**{'fes.menu': {None: ids()}, 'fes.zx81': {None: ids()},
                         'fes.spectrum': {'48': ids('a'), '128': ids('b')}, 'fes.atari-st': {'520st': ids()}})
        head = probed(**{'fes.menu': {None: ids()}, 'fes.zx81': {None: ids(toolchain='t2')},
                         'fes.spectrum': {'48': ids('a2'), '128': ids('b'), 'plus3': ids('c')},
                         'fes.atari-st': {'520st': ids()}})
        recipes = {k: self.recipes[k] for k in head}
        jobs = sp.plan(recipes, head, None, base)
        self.assertEqual([(j['slug'], j['reason']) for j in jobs], [
            ('fes.zx81', 'toolchain selection changed'),
            ('fes.spectrum-48', 'design identity changed'),
            ('fes.spectrum-plus3', 'new or undescribed at the merge base')])
        self.assertEqual(jobs[1]['model'], '48')
        self.assertIsNone(jobs[0]['model'])

    def test_a_recipe_the_base_cannot_describe_is_built(self):
        head = probed(**{'fes.menu': {None: ids()}, 'fes.atari-st': {'520st': ids()}})
        base = probed(**{'fes.menu': {None: ids()}})
        base['fes.atari-st'] = {'models': None, 'identities': {}, 'error': 'LookupError: missing'}
        recipes = {k: self.recipes[k] for k in head}
        self.assertEqual([j['slug'] for j in sp.plan(recipes, head, None, base)], ['fes.atari-st-520st'])

    def test_no_moved_input_falls_back_to_menu(self):
        head = probed(**{'fes.menu': {None: ids()}, 'fes.zx81': {None: ids()}})
        recipes = {k: self.recipes[k] for k in head}
        jobs = sp.plan(recipes, head, None, head)
        self.assertEqual([j['slug'] for j in jobs], ['fes.menu'])

    def test_a_broken_head_producer_fails_closed(self):
        head = probed(**{'fes.menu': {None: ids()}})
        head['fes.zx81'] = {'models': None, 'identities': {}, 'error': 'BuildError: boom'}
        recipes = {k: self.recipes[k] for k in head}
        with self.assertRaisesRegex(sp.PlanError, 'cannot describe this head'):
            sp.plan(recipes, head, None, head)

    def test_explicit_selection_builds_every_model_or_the_named_one(self):
        head = probed(**{'fes.spectrum': {'48': ids(), '128': ids()}, 'fes.menu': {None: ids()}})
        recipes = {k: self.recipes[k] for k in head}
        self.assertEqual([j['slug'] for j in sp.plan(recipes, head, [('fes.spectrum', None)])],
                         ['fes.spectrum-48', 'fes.spectrum-128'])
        self.assertEqual([j['slug'] for j in sp.plan(recipes, head, [('fes.spectrum', '128')])],
                         ['fes.spectrum-128'])
        with self.assertRaisesRegex(sp.PlanError, 'has no model pentagon'):
            sp.plan(recipes, head, [('fes.spectrum', 'pentagon')])

    def test_auto_requires_a_base(self):
        head = probed(**{'fes.menu': {None: ids()}})
        with self.assertRaisesRegex(sp.PlanError, 'merge-base'):
            sp.plan({'fes.menu': self.recipes['fes.menu']}, head, None, None)

    def test_auto_uses_the_base_registry_when_a_producer_already_exists(self):
        head_recipes = {core_id: self.recipes[core_id] for core_id in ('fes.menu', 'fes.zx81')}
        base_recipes = {'fes.menu': self.recipes['fes.menu']}
        same_identities = probed(**{'fes.menu': {None: ids()}, 'fes.zx81': {None: ids()}})

        with tempfile.TemporaryDirectory() as directory:
            base_root = Path(directory) / 'base'
            output_dir = Path(directory) / 'out'
            base_registry = base_root / 'config/core-recipes.toml'
            base_registry.parent.mkdir(parents=True)
            base_registry.write_text('base recipe registry fixture\n')
            # Model an existing producer file that was not registered at the base.
            base_producer = base_root / 'sources/misteross' / self.recipes['fes.zx81'].producer_script
            base_producer.parent.mkdir(parents=True)
            base_producer.write_text('# producer already exists at base\n')

            def registry_for(path=None, *, allow_empty=False):
                if path is None:
                    return head_recipes
                self.assertEqual(Path(path), base_registry)
                self.assertTrue(allow_empty)
                return base_recipes

            def probe_checkout(root, recipes):
                if Path(root) == ROOT:
                    return same_identities
                self.assertEqual(Path(root), base_root)
                self.assertTrue(base_producer.is_file())
                # The registered recipes constrain the base probe even when a
                # matching producer source happens to be present there.
                return {core_id: same_identities[core_id] for core_id in recipes}

            with mock.patch.object(sp, 'registered', side_effect=registry_for), \
                    mock.patch.object(sp, 'probe', side_effect=probe_checkout):
                sp.main(['--select', 'auto', '--base-root', str(base_root), '--output-dir', str(output_dir)])

            plan_record = json.loads((output_dir / 'de25-plan.json').read_text())
            self.assertEqual(
                [(job['core_id'], job['reason']) for job in plan_record['jobs']],
                [('fes.zx81', 'new or undescribed at the merge base')])
            self.assertEqual(set(plan_record['base']), {'fes.menu'})


class PowerboatScriptTests(unittest.TestCase):
    def setUp(self):
        self.dir = ROOT / 'scripts/powerboat'

    def test_sealer_takes_its_jobs_from_the_exact_head_plan(self):
        sealer = (self.dir / 'de25-seal-auto.sh').read_text()
        self.assertIn('"$W/scripts/de25_seal_plan.py"', sealer)
        self.assertNotIn('build_menu_package.py', sealer)
        self.assertIn('build-summary.json', sealer)
        self.assertIn('git -C "$R" merge-base', sealer)

    def test_runner_checks_and_smokes_every_package(self):
        runner = (self.dir / 'hil-de25-core.sh').read_text()
        self.assertIn('mapfile -t PACKAGE_LINES < "$B/core-build.result"', runner)
        self.assertIn('C_BOOT_SCREEN_TOOL="$FES_SOURCE/scripts/de25_boot_screen.py"', runner)

    def test_hilq_selection_has_no_producer_names(self):
        routing = (self.dir / 'de25_hilq.py').read_text()
        self.assertNotIn('build_menu_package', routing)
        spec = importlib.util.spec_from_file_location('de25_hilq', self.dir / 'de25_hilq.py')
        hilq = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(hilq)
        self.assertEqual(hilq.de25_producer(['sources/misteross/boards/de25nano/spectrum/build.py']), 'de25:auto')
        self.assertEqual(hilq.de25_producer([], ['core:de25-spectrum', 'core:de25-atari-st']),
                         'de25:fes.atari-st,fes.spectrum')
        self.assertEqual(hilq.de25_producer([], ['core:de25-ALL']), 'de25:all')
        self.assertIsNone(hilq.de25_producer(['sources/FogCast/internal/zx81machine/rom.go']))
        promoter = (self.dir / 'de25-baseline-auto.sh').read_text()
        self.assertIn('menu:fes.menu zx81:fes.zx81', promoter)


if __name__ == '__main__':
    unittest.main()
