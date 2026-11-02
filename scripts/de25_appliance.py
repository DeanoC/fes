"""DE25 image and input selection for shared FES assembly commands.

This adapter requires an actual managed factory. Intermediates and construction
fixtures cannot supply the factory, package verification or boot evidence.
"""
from dataclasses import dataclass
import importlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parent))

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


def image_action(root, args):
    """Route ordinary image commands through the existing factory producer."""
    import image_artifacts
    import media
    root = Path(root).resolve()
    output = args.output or root / 'out/de25-nano/factory'
    if args.action == 'image':
        if any(value is None for value in (args.kernel, args.sdk, args.artifacts)):
            raise ValueError('DE25 image assembly requires --kernel, --sdk and --artifacts')
        # Independent board/ELF/interface checks precede tools and output creation.
        image_artifacts.load(args.artifacts, board=BOARD)
        with media.operation(root, PROFILE, board=BOARD):
            subprocess.run([sys.executable, root / 'image/de25-nano/build.py', 'managed-factory',
                            '--output', output, '--cache', root / 'out/cache/de25-factory',
                            '--kernel', args.kernel, '--sdk', args.sdk, '--artifacts', args.artifacts,
                            '--container-runtime', os.environ.get('CONTAINER_RUNTIME', 'docker')], check=True)
        return
    if args.action != 'verify':
        raise ValueError('unsupported DE25 image action')
    provided = image_artifacts.load(args.artifacts, board=BOARD)[0] if args.artifacts else None
    with media.operation(root, PROFILE, board=BOARD):
        _, _, _, _, _, selected = verified_inputs(root, PROFILE, output)
        if provided is not None:
            fields = ('format', 'board', 'architecture', 'provenance', 'binaries', 'packages')
            if any(provided.get(key) != selected.artifacts.get(key) for key in fields):
                raise ValueError('DE25 factory differs from supplied artifact selection')
        if args.kernel is not None or args.sdk is not None:
            base, _, _, _ = modules(root)
            if args.kernel is not None:
                base.owner.selected_kernel(args.kernel)
                for name in base.KERNEL_FILES:
                    if digest(appliance.regular(args.kernel / name)) != selected.receipt['files']['kernel/' + name]:
                        raise ValueError('DE25 factory differs from supplied kernel')
            if args.sdk is not None and digest(appliance.regular(args.sdk)) != base.sdk_lock()['sdk_sha256']:
                raise ValueError('DE25 factory differs from supplied owner compiler')
    print('DE25 factory structure and bound virtual boot verified; native acceptance remains open.')


def export_artifacts(root, directory, output):
    """Read actual factory executables; input copies alone cannot prove export."""
    import image_artifacts
    import media
    root, directory, output = Path(root).resolve(), Path(directory).resolve(), Path(output).resolve()
    if output.exists() or output.is_symlink():
        raise ValueError('artifact output already exists; choose a new directory')
    if output == directory or output.is_relative_to(directory) or directory.is_relative_to(output):
        raise ValueError('artifact output overlaps the selected DE25 factory')
    with media.operation(root, PROFILE, board=BOARD):
        rootfs, _, provenance, _, _, selected = verified_inputs(root, PROFILE, directory)
        _, factory, _, _ = modules(root)
        path = appliance.regular(directory / factory.INPUTS)
        if digest(path) != selected.receipt['files'][factory.INPUTS]:
            raise ValueError('DE25 factory inputs changed during artifact export')
        inputs = json.loads(path.read_text())
        expected = dict(selected.artifacts['binaries'])
        # Base helpers are rebuilt by the factory, separately from its four
        # selected software-bundle executables; bind their actual producer record.
        for name in ('fes-kit-data', 'fes-kit-software'):
            expected[name] = inputs['executables'][name]
        if digest(appliance.regular(rootfs)) != provenance.rootfs_sha256:
            raise ValueError('DE25 factory image changed during artifact export')
        packages = image_artifacts.packages_at(directory / 'artifacts', selected.artifacts['packages'], board=BOARD)
        output.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.TemporaryDirectory(prefix='.de25-artifacts-', dir=output.parent) as temporary:
            scratch = Path(temporary)
            binaries = scratch / 'bin'; binaries.mkdir()
            for name in image_artifacts.BINARIES:
                source = ('/usr/sbin/' if name.startswith('fes-') else '/usr/share/fes/software/bin/') + name
                subprocess.run(['debugfs', '-R', 'dump ' + source + ' bin/' + name, str(rootfs)],
                               cwd=scratch, check=True, stdout=subprocess.DEVNULL)
                if digest(appliance.regular(binaries / name)) != expected[name]:
                    raise ValueError('DE25 factory exported executable differs: ' + name)
            packed = scratch / 'artifacts'
            image_artifacts.pack(binaries, packages[0]['directory'], packages[0]['selection_path'],
                                 selected.artifacts['provenance']['fogcast'],
                                 selected.artifacts['provenance']['runtime'], packed, board=BOARD)
            manifest = json.loads((packed / 'artifacts.json').read_text())
            manifest.update(source_image_sha256=provenance.rootfs_sha256,
                            source_factory_receipt_sha256=provenance.image_receipt_sha256,
                            source_fes_revision=provenance.fes_revision)
            (packed / 'artifacts.json').write_text(json.dumps(manifest, indent=2, sort_keys=True) + '\n')
            image_artifacts.load(packed, board=BOARD)
            if (digest(appliance.regular(directory / 'factory.json')) != provenance.image_receipt_sha256 or
                    factory.verify_receipt(directory) != selected.receipt):
                raise ValueError('DE25 factory changed during artifact export')
            packed.rename(output)
        return manifest


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
