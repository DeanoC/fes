"""DE25 input selection for the shared appliance release/bootstrap/card path.

This adapter requires an actual managed factory. Intermediates and construction
fixtures cannot supply the factory, package verification or boot evidence.
"""
from dataclasses import dataclass
import importlib
import json
from pathlib import Path
import shutil
import subprocess
import sys

from . import appliance
from .environment import build_environment
from .media_inputs import digest

BOARD = 'de25-nano'
PROFILE = 'de25-nano'


def modules(root):
    root = Path(root).resolve()
    sys.path.insert(0, str(root / 'image/de25-nano'))
    base = importlib.import_module('managed_base')
    factory = importlib.import_module('managed_factory')
    boot = importlib.import_module('managed_boot')
    media = importlib.import_module('managed_media')
    if any(m.ROOT.resolve() != root for m in (base, factory, boot, media)):
        raise ValueError('DE25 appliance verifier differs from selected FES source')
    return base, factory, boot, media


@dataclass(frozen=True)
class Factory:
    directory: Path
    receipt: dict
    artifacts: dict
    idle: Path


def verified_inputs(root, profile, directory):
    if profile != PROFILE or directory is None:
        raise ValueError('DE25 release requires its explicit profile and --factory')
    root, directory = Path(root).resolve(), Path(directory).resolve()
    # Refuse intermediate/headless receipts before loading tools or building
    # verification helpers. A filename/JSON board claim never selects the board.
    present = {name for name in ('factory.json', 'base.json', 'image.json')
               if (directory / name).exists() or (directory / name).is_symlink()}
    if present != {'factory.json'}:
        raise ValueError('DE25 release requires an actual managed-factory receipt')
    appliance.regular(directory / 'factory.json')
    changes = subprocess.check_output(['git', '-C', root, 'status', '--porcelain',
                                       '--untracked-files=all'], text=True)
    if changes.strip():
        raise ValueError('DE25 release requires clean committed FES source')
    base, factory, boot, _ = modules(root)
    boot.authenticated_image(directory, factory.PROFILE)
    # Re-run the real package/bundle/rootfs verifier, not a supplied pass claim.
    # Its deterministic report is itself part of the factory's closed hashes.
    subprocess.run([sys.executable, root / 'image/de25-nano/build.py', 'verify-factory',
                    '--output', directory, '--cache', root / 'out/cache/de25-factory'], check=True)
    record = factory.verify_receipt(directory)
    artifacts, packages = factory.selected(directory / 'artifacts')
    verification = json.loads(appliance.regular(directory / 'factory-verify.json').read_text())
    if (verification.get('classification') != 'managed-factory-candidate' or
            verification.get('board') != BOARD or verification.get('normal_runtime_admission') != 'closed' or
            verification.get('base_contract', {}).get('board') != BOARD):
        raise ValueError('DE25 factory structural verification differs')
    provenance = appliance.ReleaseProvenance(
        record['fes_revision'], artifacts['provenance']['fogcast'], artifacts['provenance']['runtime'],
        record['files']['rootfs.ext4'], record['files']['kernel/Image'], digest(directory / 'factory.json'),
        record['files']['factory-verify.json'], record['files']['qemu-smoke.log'], record['image_passes'])
    selected = Factory(directory, record, artifacts, packages[0]['directory'] / 'core.rbf')
    return (directory / 'rootfs.ext4', directory / 'kernel/Image', provenance,
            root / 'sources/FogCast', build_environment(), selected)


def card_inputs(root, selection, vendor_boot, scratch, release, bootstrap, config, launcher):
    if not isinstance(selection, Factory) or vendor_boot is None:
        raise ValueError('DE25 card requires its selected factory and --vendor-boot')
    base, _, _, media = modules(root)
    vendor_boot = Path(vendor_boot)
    vendor = media.boot.load_lock()
    media.boot._verify_prepared_boot(vendor_boot, vendor)
    vendor_copy = scratch / 'vendor-boot'
    shutil.copytree(vendor_boot, vendor_copy)
    media.boot._verify_prepared_boot(vendor_copy, vendor)
    kernel = scratch / 'owner-kernel'; kernel.mkdir()
    for name in base.KERNEL_FILES:
        source = appliance.regular(selection.directory / 'kernel' / name)
        shutil.copyfile(source, kernel / name)
        if digest(kernel / name) != selection.receipt['files']['kernel/' + name]:
            raise ValueError('DE25 kernel changed during card snapshot')
    base.owner.selected_kernel(kernel)
    idle = scratch / 'idle.rbf'
    shutil.copyfile(appliance.regular(selection.idle), idle)
    idle_name = selection.idle.relative_to(selection.directory).as_posix()
    if digest(idle) != selection.receipt['files'][idle_name]:
        raise ValueError('DE25 factory idle changed during card snapshot')
    inputs = media.Inputs(release.image, bootstrap.image, release.manifest, idle,
                          kernel, vendor_copy, config, launcher)
    bindings = {'board': BOARD, 'kernel_sha256': digest(kernel / 'Image'),
                'owner_dtb_sha256': digest(kernel / 'de25-owner.dtb'),
                'vendor_boot': {p.relative_to(vendor_copy).as_posix(): digest(p)
                                for p in vendor_copy.rglob('*') if p.is_file()},
                'idle_sha256': digest(idle), 'factory_receipt_sha256': digest(selection.directory / 'factory.json')}
    return inputs, bindings
