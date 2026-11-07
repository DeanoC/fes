"""Run the normal two-pass reconstructor on explicitly marked construction inputs."""
import json
from pathlib import Path
import subprocess
import sys

sys.path.insert(0, '/work')
from scripts import appliance_media, appliance_media_inside

media = appliance_media_inside.de25_media()
inputs_root, output = Path('/inputs'), Path('/export')
inputs = media.Inputs(inputs_root / 'factory-binding-fixture.img', inputs_root / 'bootstrap-first.img',
                      inputs_root / 'construction-binding.json', inputs_root / 'fixture-idle.rbf',
                      Path('/kernel'), Path('/vendor'))

class LocalRunner:
    def path(self, path):
        return str(path)

    def disk(self, command):
        return subprocess.check_output(command, text=True)

bindings = {'format': 1, 'kind': 'fes-de25-card-construction-fixture', 'test_fixture': True,
            'board': 'de25-nano', 'hardware': 'not-run',
            'source_revision': json.loads((inputs_root / 'source.json').read_text())['source_revision'],
            'recipe': {name: media.boot.digest(Path('/work') / name) for name in appliance_media.RECIPE_FILES}}
image, evidence = appliance_media.reconstruct(output, inputs, bindings, LocalRunner(), board='de25-nano')
record = json.loads(evidence.read_text())
assert record['board'] == 'de25-nano' and record['test_fixture'] is True
assert record['boot_contract']['root_device'] == '/dev/mmcblk0p2'
assert record['assembly_sha256'] == [media.boot.digest(image)] * 2
expected = json.loads((inputs_root / 'receipt.json').read_text())['card_assembly_sha256'][0]
assert record['assembly_sha256'] == [expected] * 2
print(json.dumps({'classification': 'shared reconstructor using real vendor/kernel/boot-selector with factory binding fixture',
                  'hardware_access': 'not-performed', 'board': record['board'],
                  'assembly_sha256': record['assembly_sha256'], 'layout_id': record['layout_id'],
                  'geometry': record['geometry'], 'full_release': 'not-produced',
                  'factory_admission': 'not-tested', 'native_boot': 'not-run'}, indent=2))
