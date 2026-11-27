#!/usr/bin/env python3
"""Reverify a prepared native launcher SD image against its matched build inputs."""
from pathlib import Path
import subprocess
import tempfile

import launcher_image as image


def verify(uboot, entry, sdk, recovery, files, output, *, recovery_profile='standard', recovery_source_root=None):
    receipt_path = output/'result.json'
    raw = image.regular(receipt_path, 1 << 20)
    receipt = image.record(raw)
    disk = output/'native-launcher-sd.img'
    if disk.resolve() != disk or not disk.is_file() or not 0 < disk.stat().st_size <= 256 << 20:
        raise ValueError('bounded regular launcher SD image required')
    if not isinstance(receipt, dict) or type(receipt.get('format')) is not int or receipt['format'] != 1:
        raise ValueError('invalid launcher receipt format')
    if receipt.get('image_bytes') != disk.stat().st_size or receipt.get('image_sha256') != image.sha(disk):
        raise ValueError('launcher SD image differs from its receipt')

    def admit():
        return image.admit_inputs(uboot, entry, sdk, recovery, files, recovery_profile=recovery_profile,
                                  recovery_source_root=recovery_source_root)

    _, inputs, base, native, fit_data = admit()
    expected = {
        'status': 'prepared',
        'native_boot_id': native['build_id'],
        'startup': native['startup'],
        'preferences': 'sd-checkpoint',
        'recovery_profile': recovery_profile,
        'recovery_source_root': str(recovery_source_root) if recovery_source_root is not None else None,
        'protected_prefix_sha256': base['prefix_sha256'],
        'inputs': inputs,
    }
    if (any(name not in receipt or receipt[name] != value for name, value in expected.items()) or
            any(receipt.get(name) is not False for name in ('hardware_actions', 'power_on_qualified', 'reboot_persistence_qualified'))):
        raise ValueError('launcher receipt differs from the admitted build and recovery inputs')
    selected = {Path(name): digest for name, digest in inputs.items()}
    with tempfile.TemporaryDirectory(prefix='fes-tamago-launcher-verify-') as temporary:
        scratch = Path(temporary)

        def run(label, argv):
            transcript = scratch/(label+'.log')
            with transcript.open('xb') as log:
                subprocess.run(argv, stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT,
                               check=True, timeout=120)
            return transcript

        image.verify_prepared_image(disk, receipt, selected, fit_data, scratch, run)
    # The boot and recovery producers also bind nested files (SPL, entry ELF,
    # boot configuration) which are absent from the launcher's flat inventory.
    if (admit()[1] != inputs or image.regular(receipt_path, 1 << 20) != raw or
            disk.resolve() != disk or not disk.is_file() or disk.stat().st_size != receipt['image_bytes'] or
            image.sha(disk) != receipt['image_sha256']):
        raise ValueError('launcher image, receipt or inputs changed during verification')
    return receipt['image_sha256']


if __name__ == '__main__':
    try:
        digest = verify(**image.parse_args(__doc__))
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        raise SystemExit('Launcher verification failed: '+str(error))
    print('Verified native launcher SD image:', digest)
