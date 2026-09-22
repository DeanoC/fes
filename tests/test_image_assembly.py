"""FES image/ is the native image recipe; FogCast supplies agent and kit."""
from pathlib import Path
import hashlib
import tomllib
import unittest

ROOT = Path(__file__).resolve().parents[1]
IMAGE = ROOT / 'image'
FOGCAST = ROOT / 'sources' / 'FogCast'
RECIPE = (
    'Makefile',
    'scripts/target-image-container.sh',
    'scripts/fetch-target-image-sources.sh',
    'scripts/fetch-native-runtime-inputs.sh',
    'scripts/build-target-image.sh',
    'scripts/verify-target-image.sh',
    'scripts/verify-target-image-source-cache.sh',
    'scripts/qemu-smoke-target-image.sh',
    'scripts/native-extra-cores.sh',
    'build/target-image.sources.lock.toml',
    'build/native-inputs.toml',
    'buildroot',
    'containers/target-image',
)
FOGCAST_INPUTS = (
    'cmd/target-image-lock',

)


class ImageAssemblyTest(unittest.TestCase):
    def test_recipe_paths_exist_in_fes_image(self):
        for relative in RECIPE:
            path = IMAGE / relative
            self.assertTrue(path.exists(), f'missing image recipe {relative}')

    def test_fogcast_keeps_selector(self):
        for relative in FOGCAST_INPUTS:
            path = FOGCAST / relative
            self.assertTrue(path.exists(), f'missing FogCast image input {relative}')

    def test_splash_and_idle_pin_sealed_misteross_splash(self):
        policy = tomllib.loads((IMAGE / 'build/native-inputs.toml').read_text())
        expected = {
            'repository': 'sources/misteross',
            'commit': '2609827b8de1397b6d6a2fc877be56e657e1ba11',
            'path': 'sealed/fes-splash.rbf',
            'sha256': 'feb0a66a3384d56a234fcdbd4ee2665fe366310946b206f5aefbfa6ad6e6e83f',
            'size': 1962648,
        }
        for section in ('splash_rbf', 'idle_rbf'):
            self.assertEqual({key: policy[section][key] for key in expected},
                             expected, section)
        self.assertEqual(policy['splash_rbf']['fat_destination'], '/menu.rbf')
        self.assertEqual(policy['idle_rbf']['install_path'],
                         '/usr/share/mister-runtime/idle.rbf')
        sealed = ROOT / 'sources/misteross/sealed/fes-splash.rbf'
        self.assertTrue(sealed.is_file(), 'in-tree splash seal is missing')
        self.assertEqual(sealed.stat().st_size, expected['size'])
        self.assertEqual(hashlib.sha256(sealed.read_bytes()).hexdigest(),
                         expected['sha256'])
        fetch = (IMAGE / 'scripts/fetch-native-runtime-inputs.sh').read_text()
        self.assertIn('sources/*)', fetch)
        self.assertNotIn("repository = 'https://github.com/DeanoC/misteross'",
                         (IMAGE / 'build/native-inputs.toml').read_text())
        for relative in ('scripts/fetch-native-runtime-inputs.sh',
                         'scripts/verify-native-runtime-inputs.sh'):
            text = (IMAGE / relative).read_text()
            self.assertNotIn('require_transitional_menu_identity', text)
            self.assertNotIn('Distribution_MiSTer', text)
        from scripts.media_inside import Provenance
        Provenance('f' * 40, 'native-integration-dev', 'a' * 64, 'b' * 64, 'c' * 64,
                   expected['repository'], expected['commit'], expected['path'],
                   expected['size'], expected['sha256'])

    def test_parent_invokes_fes_image_not_fogcast_scripts(self):
        for name in ('scripts/build.py', 'scripts/native_dev.py', 'scripts/media.py'):
            text = (ROOT / name).read_text()
            self.assertNotIn('fogcast / "scripts/target-image-container.sh"', text, name)
            self.assertNotIn("fogcast / 'scripts/target-image-container.sh'", text, name)
            self.assertNotIn('fogcast / "scripts/build-target-image.sh"', text, name)
            self.assertNotIn("fogcast / 'scripts/build-target-image.sh'", text, name)
            self.assertNotIn('fogcast / "scripts/verify-target-image.sh"', text, name)
            self.assertNotIn("fogcast / 'scripts/verify-target-image.sh'", text, name)
        build = (ROOT / 'scripts/build.py').read_text()
        self.assertIn('make", "-C", IMAGE', build)
        self.assertIn('FOGCAST_DIR', build)
        native = (ROOT / 'scripts/native_dev.py').read_text()
        self.assertIn("image / 'scripts/target-image-container.sh'", native)

    def test_guide_names_parent_commands_and_fes_recipe(self):
        text = (ROOT / 'docs' / 'image-assembly.md').read_text()
        for needle in ('make dev', 'make build', 'make media', 'image/', 'FOGCAST_DIR'):
            self.assertIn(needle, text)
        self.assertNotIn('Do not copy', text)

    def test_default_package_contract_is_documented_as_the_complete_fes_set(self):
        profile = tomllib.loads((ROOT / 'profiles/native-integration-dev.toml').read_text())
        self.assertEqual(profile['native_image_mode'], 'package-only')
        self.assertEqual(
            [entry['core_id'] for entry in profile['fpga_packages']],
            ['fes.pong', 'fes.zx81', 'fes.coleco'])

        readme = (ROOT / 'README.md').read_text()
        packages = (ROOT / 'docs/core-packages.md').read_text()
        getting_started = (ROOT / 'docs/getting-started.md').read_text()
        development = (ROOT / 'docs/development.md').read_text()
        for text in (readme, packages, getting_started, development):
            for needle in ('fes.pong', 'fes.zx81', 'fes.coleco', 'HIP/nextpnr'):
                self.assertIn(needle, text)
        self.assertIn('closed package set', readme)
        self.assertIn('closed package set', packages)
        self.assertIn('same closed package set', getting_started)
        self.assertIn('same closed package set', development)
        self.assertIn('Quartus', readme)
        self.assertIn('Quartus', development)
        self.assertIn('explicit', development)
        self.assertIn('fes-zx81.package-selection.toml', packages)
        self.assertIn('fes-coleco.package-selection.toml', packages)
        for text in (readme, packages, getting_started, development):
            self.assertNotIn('single-package Pong-only', text)
            self.assertNotIn('currently installs only the described FES Pong package', text)

        current_docs = (
            'README.md',
            'docs/README.md',
            'docs/core-packages.md',
            'docs/getting-started.md',
            'docs/development.md',
            'docs/component-boundaries.md',
            'docs/project-map.md',
            'docs/fes-zx81.md',
        )
        current_text = '\n'.join((ROOT / relative).read_text()
                                  for relative in current_docs)
        for stale in (
                'installs the described FES Pong package',
                'publishes the selected `fes-pong.package-selection.toml`',
                'The current profile selects the described FES Pong package',
                'It is not in the selected image',
                'does not install this package in the native image catalog',
                'described format-2 FES Pong package'):
            self.assertNotIn(stale.replace('`', chr(96)), current_text)
