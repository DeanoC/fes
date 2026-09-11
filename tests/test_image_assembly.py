"""FES image/ is the native image recipe; FogCast supplies lock, agent and kit."""
from pathlib import Path
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
