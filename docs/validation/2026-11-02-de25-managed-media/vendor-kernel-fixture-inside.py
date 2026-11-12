"""Construction fixture with real vendor/kernel/boot-selector bytes; never deploy."""
import hashlib
import json
from pathlib import Path
import shutil
import sys

sys.path.insert(0, '/work')
from scripts import appliance, appliance_inside, appliance_media_inside

media = appliance_media_inside.de25_media()
output = Path('/export')
vendor = media.boot.load_lock()
kernel = media.owner.selected_kernel(Path('/kernel'))
media.boot._verify_prepared_boot(Path('/vendor'), vendor)
binary = output / 'fes-boot'
appliance.validate_static_arm(binary, 'aarch64')
tree = output / 'fixture-tree'
for directory in ('.fes-bootstrap', 'etc/fes', 'usr/share/mister-runtime'):
    (tree / directory).mkdir(parents=True, exist_ok=True)
(tree / 'CONSTRUCTION-FIXTURE-NOT-A-FACTORY').write_text('No runtime, admitted MENU, factory acceptance or boot proof. Do not deploy.\n')
(tree / 'etc/fes-board').write_text('de25-nano\n')
(tree / 'etc/fes/software-base.json').write_text('{"board":"de25-nano","architecture":"aarch64"}\n')
idle = output / 'fixture-idle.rbf'
idle.write_bytes(b'construction fixture only; not FPGA data\n')
shutil.copyfile(idle, tree / 'usr/share/mister-runtime/idle.rbf')
factory = output / 'factory-binding-fixture.img'
with factory.open('xb') as stream:
    stream.truncate(256 << 20)
media.media.run('mke2fs', '-q', '-F', '-t', 'ext4', '-d', tree, factory)
# This is only a schema/construction binding. It has no release evidence,
# factory receipt, packaging boot result, qualified package or deployment claim.
manifest = {'format': 1, 'board': 'de25-nano', 'boot_abi': 'fes-bootstrap-v1',
            'version': 'CONSTRUCTION-FIXTURE-NOT-A-FACTORY',
            'kernel_sha256': kernel['files']['Image']['sha256'],
            'image_sha256': media.boot.digest(factory), 'image_size': factory.stat().st_size,
            'fes_revision': json.loads((output / 'source.json').read_text())['source_revision'],
            'fogcast_revision': json.loads((output / 'platform-build.json').read_text())['fogcast_revision'],
            'runtime_revision': json.loads((output / 'source.json').read_text())['source_revision']}
binding = output / 'construction-binding.json'
binding.write_bytes(appliance.canonical(manifest))
bootstrap_hashes = []
for name in ('bootstrap-first.img', 'bootstrap-second.img'):
    appliance_inside.assemble(output / name, binary, binding, board='de25-nano')
    bootstrap_hashes.append(media.boot.digest(output / name))
assert bootstrap_hashes[0] == bootstrap_hashes[1]
inputs = media.Inputs(factory, output / 'bootstrap-first.img', binding, idle, Path('/kernel'), Path('/vendor'))
reports = []
for name in ('card-first.fixture.img', 'card-second.fixture.img'):
    reports.append(media.assemble(output / name, inputs, vendor))
assert reports[0] == reports[1]
assert reports[0]['geometry']['partition_2']['type'] == 0x83
assert reports[0]['boot_contract']['root_device'] == '/dev/mmcblk0p2'
receipt = {'classification': 'real vendor/kernel/boot-selector bytes with construction-only factory/idle fixtures',
           'source_revision': manifest['fes_revision'], 'factory_admission': 'not-tested; no factory produced',
           'release_boot_smoke': 'not-run', 'CPU_execution': 'not-run', 'native_boot': 'not-run',
           'hardware_access': 'not-performed', 'vendor_prefix_sha256': vendor['prefix_sha256'],
           'vendor_uboot_sha256': media.boot.digest(Path('/vendor/u-boot.itb')),
           'vendor_boot_script_sha256': media.boot.digest(Path('/vendor/boot.scr.uimg')),
           'actual_kernel_Image_sha256': kernel['files']['Image']['sha256'],
           'actual_kernel_DTB_sha256': kernel['files']['de25-owner.dtb']['sha256'],
           'bootstrap_binary_sha256': media.boot.digest(binary), 'bootstrap_assembly_sha256': bootstrap_hashes,
           'card_assembly_sha256': [r['image_sha256'] for r in reports], 'card_report': reports[0]}
(output / 'receipt.json').write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')
print(json.dumps({k: v for k, v in receipt.items() if k != 'card_report'}, indent=2), flush=True)
