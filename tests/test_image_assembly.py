"""The FogCast pin is the native image recipe until assembly moves once."""
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
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
    'build/native-runtime.inputs.lock.toml',
    'buildroot',
    'containers/target-image',
    'cmd/target-image-lock',
)


class ImageAssemblyFreezeTest(unittest.TestCase):
    def test_recipe_paths_exist_in_selected_fogcast(self):
        for relative in RECIPE:
            path = FOGCAST / relative
            self.assertTrue(path.exists(), f'missing image recipe {relative}')

    def test_guide_names_parent_commands_and_one_builder(self):
        text = (ROOT / 'docs' / 'image-assembly.md').read_text()
        for needle in ('make dev', 'make build', 'make media', 'target-image-native',
                       'Do not copy'):
            self.assertIn(needle, text)
