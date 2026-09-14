"""FES image/ is the native image recipe; FogCast supplies lock, agent and kit."""
from pathlib import Path
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
    'buildroot',
    'containers/target-image',
)
FOGCAST_INPUTS = (
    'cmd/target-image-lock',
    'build/native-runtime.inputs.lock.toml',
)


class ImageAssemblyTest(unittest.TestCase):
    def test_recipe_paths_exist_in_fes_image(self):
        for relative in RECIPE:
            path = IMAGE / relative
            self.assertTrue(path.exists(), f'missing image recipe {relative}')

    def test_fogcast_keeps_selector_and_runtime_lock(self):
        for relative in FOGCAST_INPUTS:
            path = FOGCAST / relative
            self.assertTrue(path.exists(), f'missing FogCast image input {relative}')

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
            for needle in ('fes.pong', 'fes.zx81', 'fes.coleco', 'HIP/nextpnr', 'format-1'):
                self.assertIn(needle, text)
        self.assertIn('closed package set', readme)
        self.assertIn('closed package set', packages)
        self.assertIn('same closed package set', getting_started)
        self.assertIn('same closed package set', development)
        self.assertIn('Quartus', readme)
        self.assertIn('historical format-1', development)
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
