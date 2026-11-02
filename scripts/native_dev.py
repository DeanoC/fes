"""One persistent diagnostic build using the FES image recipe."""
import hashlib
import json
import os
import platform
import shutil
from pathlib import Path
import re
import shlex
import subprocess
import tempfile
import tomllib
from types import SimpleNamespace

from build import (digest, output_volume, publish_file, reusable, run, write_receipt,
                   package_arguments,
                   package_selection_names,
                   publish_package_state, recorded_image_fingerprint,
                   reuse_status, verify_package_outputs, native_image_mode,
                   verify_package_only_outputs, remove_stale_parent_outputs,
                   target_image_source_cache_lock)
import optional_cores
from build_diagnostics import BuildDiagnostics
from inputs import git

# Keep the clean builder's absolute path: Buildroot host tools are not relocatable.
WORK = '/target-image-output/work-{passes}-native-dev'
EXPORT = '/work/build/output/target-image/fes-development'

DE25_SOURCE_RECIPES = (
    'scripts/build.py', 'scripts/native_dev.py', 'scripts/de25_appliance.py',
    'scripts/kit_software.py', 'scripts/kit_sdk.py', 'scripts/environment.py',
    'scripts/image_artifacts.py', 'scripts/recipes.py',
    'config/core-recipes.toml', 'profiles/de25-nano.toml')
DE25_SOURCE_INPUTS = ('board', 'architecture', 'sources', 'profile', 'go', 'sdk',
                      'kernel', 'owner_sdk_sha256', 'factory_recipe_sha256', 'recipe')


def de25_source_selection(root):
    import build as parent
    profile = tomllib.loads((root / 'profiles/de25-nano.toml').read_text())
    if profile.get('board') != 'de25-nano':
        raise ValueError('DE25 source profile differs from independently selected board')
    revisions = parent.validate(root, profile)
    if parent.git(root, 'status', '--porcelain', '--untracked-files=all'):
        raise ValueError('DE25 source builds require clean committed FES source')
    packages = parent.selected_packages(profile, 'de25-nano', board='de25-nano')
    if packages != ('fes.menu',):
        raise ValueError('DE25 source builds require exactly the MENU firmware recipe')
    return profile, revisions, packages


def verify_de25_development(root, output, *, expected_fingerprint=None):
    """Read a source workspace without resolving packages or producing software.

    The receipt is insufficient by itself: bind current source policy, the real
    factory verifier and its package selection, then compare every flat copy.
    """
    import build as parent
    import image_artifacts
    from scripts import de25_appliance

    root, output = Path(root).resolve(), Path(output).resolve()
    regular = de25_appliance.appliance.regular
    profile, revisions, _ = de25_source_selection(root)
    present = {name for name in ('image.json', 'development.json', 'factory.json', 'base.json')
               if (output / name).exists() or (output / name).is_symlink()}
    if present not in ({'image.json'}, {'development.json'}):
        raise ValueError('DE25 source workspace requires one image or development receipt')
    kind = 'image' if 'image.json' in present else 'development'
    info = json.loads(regular(output / 'inputs.json').read_text())
    base, factory, _, _ = de25_appliance.modules(root)
    if (not isinstance(info, dict) or
            set(info) != set(DE25_SOURCE_INPUTS) | {'fpga_packages', 'image_base_fingerprint',
                'image_fingerprint', 'build_mode', 'factory', 'structural', 'two_pass_reproducibility'} or
            info['board'] != 'de25-nano' or info['architecture'] != 'aarch64' or
            info['sources'] != revisions or info['profile'] != profile or
            info['owner_sdk_sha256'] != base.sdk_lock()['sdk_sha256'] or
            info['factory_recipe_sha256'] != factory.recipe_key(root) or
            info['recipe'] != {name: parent.digest(regular(root / name)) for name in DE25_SOURCE_RECIPES} or
            info['structural'] != 'pass' or info['two_pass_reproducibility'] != 'not-run-single-pass' or
            info['build_mode'] != ('committed-source' if kind == 'image' else 'incremental-development')):
        raise ValueError('DE25 source workspace differs from selected source or recipe')
    source_key = hashlib.sha256(json.dumps(
        {name: info[name] for name in DE25_SOURCE_INPUTS}, sort_keys=True).encode()).hexdigest()
    if source_key != info['image_base_fingerprint']:
        raise ValueError('DE25 source fingerprint differs from its recorded inputs')
    fingerprint = parent.recorded_image_fingerprint(info)
    if expected_fingerprint is not None and expected_fingerprint != fingerprint:
        raise ValueError('DE25 source workspace differs from selected build inputs')
    if type(info['factory']) is not str:
        raise ValueError('DE25 cached factory path differs from its source workspace')
    relative = Path(info['factory'])
    if (relative.is_absolute() or '..' in relative.parts or len(relative.parts) != 3 or
            relative.parts[0] != 'generations' or relative.parts[-1] != 'factory' or
            not re.fullmatch(re.escape(fingerprint) + r'(?:-[a-z0-9_]+)?', relative.parts[1]) or
            any((output / Path(*relative.parts[:i])).is_symlink() for i in range(1, 4))):
        raise ValueError('DE25 cached factory path differs from its source workspace')
    receipt = json.loads(regular(output / (kind + '.json')).read_text())
    if not isinstance(receipt, dict) or set(receipt) != (
            {'inputs', 'files', 'fes_revision'} if kind == 'image' else {'inputs', 'files'}):
        raise ValueError('DE25 source receipt metadata differs')
    hit, reason = parent.reuse_status(output, kind, fingerprint)
    if not hit:
        raise ValueError('DE25 source outputs are missing, changed, or stale: ' + reason)
    _, _, _, _, _, result = de25_appliance.verified_inputs(root, 'de25-nano', output / relative)
    if (result.receipt['fes_revision'] != parent.git(root, 'rev-parse', 'HEAD') or
            (kind == 'image' and receipt['fes_revision'] != result.receipt['fes_revision']) or
            result.artifacts['provenance'] != {
                'fogcast': revisions['FogCast'], 'runtime': revisions['libmister-runtime']} or
            len(result.artifacts['packages']) != 1 or len(info['fpga_packages']) != 1 or any(
                info['fpga_packages'][0].get(key) != value
                for key, value in result.artifacts['packages'][0].items())):
        raise ValueError('DE25 source factory differs from selected component revisions or MENU')
    if info['kernel'] != {name: result.receipt['files']['kernel/' + name] for name in base.KERNEL_FILES}:
        raise ValueError('DE25 source kernel differs from its verified factory')
    packages = image_artifacts.packages_at(output / relative / 'artifacts',
                                           result.artifacts['packages'], board='de25-nano')
    names = parent.verify_package_outputs(output, packages, board='de25-nano')
    for name, target in (('linux.img', 'rootfs.ext4'), ('Image', 'kernel/Image'),
                         ('de25-owner.dtb', 'kernel/de25-owner.dtb')):
        if parent.digest(regular(output / name)) != result.receipt['files'][target]:
            raise ValueError('DE25 cached development output differs from its verified factory')
        names.append(name)
    software = regular(output / relative.parent / 'software-build.json')
    if parent.digest(regular(output / 'software-build.json')) != parent.digest(software):
        raise ValueError('DE25 source software record differs from its generation')
    software = json.loads(software.read_text())
    go, sdk = info['go'], info['sdk']
    if (not isinstance(go, dict) or set(go) != {'version', 'go_sha256', 'compile_sha256'} or
            not isinstance(go['version'], str) or not go['version'] or any(
                not isinstance(go[name], str) or not re.fullmatch('[0-9a-f]{64}', go[name])
                for name in ('go_sha256', 'compile_sha256')) or
            not isinstance(sdk, dict) or sdk.get('profile') not in ('managed-base', 'managed-factory') or
            sdk.get('kernel_image_sha256') != info['kernel']['Image'] or
            sdk.get('builder_image') != base.sdk_lock()['builder_image'] or
            not isinstance(software, dict) or set(software) != {'go', 'runtime'} or
            not isinstance(software['go'], dict) or not isinstance(software['runtime'], dict) or
            any(software['go'].get(name) != value for name, value in go.items()) or
            software['go'].get('target') != 'linux/arm64 GOARM64=v8.0 CGO_ENABLED=0' or
            software['runtime'].get('de25_image_sdk') != sdk or
            software['runtime'].get('key') != sdk.get('key') or
            software['runtime'].get('archive_sha256') != sdk.get('archive_sha256') or
            software['runtime'].get('container_id') != sdk.get('builder_image')):
        raise ValueError('DE25 source software differs from its recorded Go compiler or authenticated SDK')
    names.extend(('software-build.json', 'inputs.json', str(relative / 'factory.json')))
    if set(receipt['files']) != set(names):
        raise ValueError('DE25 source receipt does not cover the complete output selection')
    if kind == 'image':
        host_fp, _ = parent.host_fingerprint(revisions, profile, info['go']['version'])
        if not parent.reuse_status(output, 'host', host_fp)[0]:
            raise ValueError('DE25 cold source workspace requires its matching verified host outputs')
    return result


def build_de25_development(root, args):
    """Build committed DE25 sources through the shared software/factory builders.

    Each complete generation is immutable. The existing factory cache retains
    its compiler/base packages; changed software is rebuilt and refreshed by
    that producer. The final receipt changes only after full verification.
    """
    import build as parent
    import kit_software as software
    import module_sources
    from scripts import de25_appliance
    import image_artifacts

    root = Path(root).resolve()
    profile, revisions, packages = de25_source_selection(root)
    if platform.system() != 'Linux' or platform.machine() not in ('x86_64', 'amd64'):
        raise ValueError('DE25 source builds require Linux amd64')
    workspace = Path(args.output).resolve() if args.output is not None else root / 'out/de25-nano'
    for path in (args.kernel, args.sdk, args.de25_image):
        path = Path(path).resolve()
        if workspace == path or workspace in path.parents or path in workspace.parents:
            raise ValueError('DE25 source workspace overlaps an input')
    if shutil.disk_usage(workspace if workspace.exists() else root).free < 30 * 1024**3:
        raise ValueError('DE25 source build requires at least 30 GiB free')
    env = parent.build_environment()
    output = workspace / 'development' if args.action == 'dev' else workspace
    (root / 'out').mkdir(exist_ok=True)
    with parent.locked_diagnostics(root, workspace, args.action) as (_, diagnostics):
        # No target source checkout, compiler/SDK or OS build precedes this.
        selected, unavailable = parent.resolve_packages_for_action(
            revisions, output, env, args.action, packages, board='de25-nano')
        if unavailable or len(selected) != 1:
            raise ValueError('DE25 MENU firmware is required')
        env = software.software_environment()
        base, factory, _, _ = de25_appliance.modules(root)
        sdk = software.de25_sdk(root, Path(args.de25_image))
        if sdk['profile'] not in ('managed-base', 'managed-factory'):
            raise ValueError('DE25 source builds require a verified managed SDK/base ABI')
        base.owner.selected_kernel(Path(args.kernel).resolve())
        kernel = {name: parent.digest(Path(args.kernel) / name) for name in base.KERNEL_FILES}
        if kernel['Image'] != sdk['kernel_image_sha256']:
            raise ValueError('DE25 source SDK and selected kernel differ')
        if Path(args.sdk).is_symlink() or parent.digest(Path(args.sdk)) != base.sdk_lock()['sdk_sha256']:
            raise ValueError('DE25 source owner compiler differs from its source lock')
        container = env.get('CONTAINER_RUNTIME', 'docker')
        for name in ('git', 'make', 'go', container):
            if shutil.which(name) is None:
                raise ValueError('required executable is missing: ' + name)
        parent.run([container, 'info', '--format', '{{.ServerVersion}}'])
        temporary = root / 'out/tmp'
        temporary.mkdir(parents=True, exist_ok=True)
        env['GOTMPDIR'] = str(temporary)
        revision = parent.git(root, 'rev-parse', 'HEAD')
        # Each selected component remains a real FES commit, including profile
        # overrides. The image recipe comes from this FES commit.
        fogcast = parent.source_checkout('FogCast', revisions['FogCast'], '-de25-host')
        go = subprocess.check_output(['go', 'version'], cwd=fogcast, env=env, text=True).strip()
        goroot = Path(subprocess.check_output(['go', 'env', 'GOROOT'], cwd=fogcast, env=env, text=True).strip())
        info = {'board': 'de25-nano', 'architecture': 'aarch64', 'sources': revisions,
                'profile': profile, 'go': {'version': go, 'go_sha256': parent.digest(goroot / 'bin/go'),
                    'compile_sha256': parent.digest(goroot / 'pkg/tool/linux_amd64/compile')},
                'sdk': sdk, 'kernel': kernel,
                'owner_sdk_sha256': base.sdk_lock()['sdk_sha256'],
                'factory_recipe_sha256': factory.recipe_key(root),
                'recipe': {name: parent.digest(root / name) for name in DE25_SOURCE_RECIPES}}
        fingerprint = hashlib.sha256(json.dumps(info, sort_keys=True).encode()).hexdigest()
        fingerprint, info = parent.image_fingerprint(fingerprint, info, selected)
        output.mkdir(parents=True, exist_ok=True)
        generation = output / 'generations' / fingerprint
        kind = 'development' if args.action == 'dev' else 'image'
        hit, reason = parent.reuse_status(output, kind, fingerprint)
        force = args.action == 'rebuild'
        diagnostics.cache(kind, 'forced' if force else 'hit' if hit else 'miss',
                          'forced rebuild' if force else reason)

        def verify_generation(directory):
            _, _, _, _, _, result = de25_appliance.verified_inputs(root, 'de25-nano', directory / 'factory')
            if result.artifacts['provenance'] != {
                    'fogcast': revisions['FogCast'], 'runtime': revisions['libmister-runtime']}:
                raise ValueError('DE25 source factory differs from selected component revisions')
            if len(result.artifacts['packages']) != 1 or any(
                    selected[0]['inputs'].get(key) != value
                    for key, value in result.artifacts['packages'][0].items()):
                raise ValueError('DE25 source factory differs from selected MENU')
            return result

        if args.action in ('build', 'rebuild'):
            host_fp, host_info = parent.host_fingerprint(revisions, profile, go)
            parent.build_host_outputs(workspace, profile, revisions, host_fp, host_info,
                ['make', '-C', fogcast, 'CONTAINER_RUNTIME=' + container, 'VERSION=' + profile['version']],
                env, force=force, diagnostics=diagnostics)
        if hit and not force:
            verify_de25_development(root, output, expected_fingerprint=fingerprint)
            print('DE25 development: reusing verified source generation', flush=True)
            return
        generation.parent.mkdir(parents=True, exist_ok=True)
        if generation.exists() and not force:
            verify_generation(generation)
        else:
            # Retain a failed attempt for diagnosis. A complete generation is
            # never replaced, including an explicit forced source/core rebuild.
            attempt = Path(tempfile.mkdtemp(prefix='.attempt-', dir=generation.parent))
            work = root / 'out/work'
            work.mkdir(parents=True, exist_ok=True)
            with tempfile.TemporaryDirectory(prefix='de25-sources-', dir=work) as scratch:
                scratch = Path(scratch)
                module = module_sources.materialize(root, 'mister-packages', revision, scratch / 'source')
                snapshot = module.parents[1]
                staged = {}
                for name in module_sources.COMPONENTS:
                    staged[name] = (snapshot / ('sources/' + name) if revisions[name] == revision else
                        module_sources.materialize(root, name, revisions[name], scratch / name))
                if profile.get('check_packages'):
                    from consistency import check
                    check(root, staged)
                native = snapshot / 'image/build/software'
                native.mkdir(parents=True)
                shutil.copyfile(Path(args.de25_image) / 'rootfs.ext4', native / 'base.img')
                if parent.digest(native / 'base.img') != sdk['base_rootfs_sha256']:
                    raise ValueError('DE25 source base changed while copying')
                with diagnostics.measure('AArch64 source software'):
                    go_record = software.build_go(snapshot, profile['version'], revisions['FogCast'], env,
                        'de25-nano', fogcast_source=staged['FogCast'])
                    runtime_record = software.build_runtime(snapshot, Path(args.de25_image),
                        revisions['libmister-runtime'], env, None, 'de25-nano',
                        runtime_source=staged['libmister-runtime'], base_factory=False, provision=True)
                binary = attempt / 'bin'
                binary.mkdir()
                for name in ('mister-agent', 'fogcast-kit', 'fogcast-tenfoot'):
                    shutil.copyfile(staged['FogCast'] / ('bin/' + name + '-linux-aarch64'), binary / name)
                shutil.copyfile(native / 'runtime/build/mister-runtime', binary / 'mister-runtime')
                for program in binary.iterdir():
                    program.chmod(0o555)
                image_artifacts.pack(binary, selected[0]['directory'], selected[0]['selection_path'],
                    revisions['FogCast'], revisions['libmister-runtime'], attempt / 'artifacts', board='de25-nano')
                (attempt / 'software-build.json').write_text(json.dumps(
                    {'go': go_record, 'runtime': runtime_record}, indent=2, sort_keys=True) + '\n')
            with diagnostics.measure('DE25 managed factory'):
                de25_appliance.image_action(root, SimpleNamespace(action='image', output=attempt / 'factory',
                    kernel=Path(args.kernel), sdk=Path(args.sdk), artifacts=attempt / 'artifacts'))
            verify_generation(attempt)
            if generation.exists():
                # Rebuilds preserve the previous same-input generation as well.
                generation = generation.with_name(generation.name + '-' + attempt.name.removeprefix('.attempt-'))
            attempt.rename(generation)
            verify_generation(generation)
        info = dict(info, build_mode='incremental-development' if kind == 'development' else 'committed-source',
                    factory=str((generation / 'factory').relative_to(output)),
                    structural='pass', two_pass_reproducibility='not-run-single-pass')
        _, published_packages = factory.selected(generation / 'factory/artifacts')
        selections = {name: generation / 'factory/artifacts' / name
                      for name in parent.package_selection_names(published_packages, board='de25-nano')}
        names = parent.publish_package_state(published_packages, selections, output, board='de25-nano')
        for source, destination in (('rootfs.ext4', 'linux.img'), ('kernel/Image', 'Image'),
                                    ('kernel/de25-owner.dtb', 'de25-owner.dtb')):
            parent.publish_file(generation / 'factory' / source, output / destination)
            names.append(destination)
        for name in ('software-build.json',):
            parent.publish_file(generation / name, output / name)
            names.append(name)
        (output / 'inputs.json').write_text(json.dumps(info, indent=2, sort_keys=True) + '\n')
        names.extend(('inputs.json', str((generation / 'factory/factory.json').relative_to(output))))
        parent.write_receipt(output, kind, fingerprint, names)
        print('DE25 source factory: ' + str(generation / 'factory'), flush=True)
        print('DE25 development image: ' + str(output / 'linux.img'), flush=True)


def base_key(image, fogcast):
    names = git(image, 'ls-files', '-z').split('\0')
    files = {name: digest(image / name) for name in names if name and (
        name.startswith(('buildroot/', 'containers/target-image/', 'licenses/'))
        or (name.startswith('scripts/') and not name.startswith('scripts/tests/'))
        or name in ('Makefile', 'build/target-image.sources.lock.toml',
                    'build/target-image-container-packages.sha256'))}
    native = tomllib.loads((image / 'build/native-inputs.toml').read_text())
    native['mister_runtime'].pop('commit', None)
    # Source changes are handled by dirclean; all other locked policy stays in key.
    data = {'files': files, 'native_policy': native, 'runner': digest(Path(__file__)),
            'uid': os.getuid(), 'gid': os.getgid()}
    return hashlib.sha256(json.dumps(data, sort_keys=True).encode()).hexdigest()


def seed_digest(output, info):
    """Old parent Python changes need not invalidate the child's compiled base."""
    try:
        previous = json.loads((output / 'inputs.json').read_text())
        if any(previous.get(key) != info.get(key) for key in ('sources', 'profile', 'go')):
            return None
        receipt = json.loads((output / 'image.json').read_text())
        recorded = recorded_image_fingerprint(previous)
        if receipt['inputs'] != recorded:
            return None
        if not {'linux.img', 'reproducibility.txt'} <= receipt['files'].keys():
            return None
        if not reusable(output, 'image', receipt['inputs']):
            return None
        sha = digest(output / 'linux.img')
        evidence = dict(line.split('=', 1) for line in
                        (output / 'reproducibility.txt').read_text().splitlines())
        passes = evidence.get('image_passes', '2')
        if (evidence.get('run_1_sha256') == sha and
                ((passes == '1' and 'run_2_sha256' not in evidence) or
                 (passes == '2' and evidence.get('run_2_sha256') == sha))):
            return sha
    except (OSError, ValueError, KeyError, TypeError):
        pass
    return None


def seed_base(container, base, cold_volume, dev_volume, sha, selected_work):
    if selected_work not in (WORK.format(passes=1), WORK.format(passes=2)):
        raise ValueError("invalid development work directory")
    if subprocess.run([container, 'volume', 'inspect', dev_volume],
                      stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
        return  # Includes interrupted builds: normal Buildroot make resumes them.
    if not sha or subprocess.run([container, 'volume', 'inspect', cold_volume],
                                stdout=subprocess.DEVNULL,
                                stderr=subprocess.DEVNULL).returncode:
        return
    # Copy only into a new development volume. The source is mounted read-only.
    # Validate before copying; use a temporary directory so interrupted copies
    # cannot be mistaken for a usable Buildroot output on the next invocation.
    # Seed only the clean tree already at the selected path; host tools embed it.
    script = '''set -eu
src=/seed/$4
chown "$2:$3" /dest
if ! (test -f "$src/.config" && test -d "$src/host" &&
      test -d "$src/build" && test -d "$src/target" &&
      test "$(sha256sum "$src/images/rootfs.ext4" | cut -d ' ' -f 1)" = "$1"); then
    echo 'Development: clean volume is incomplete or changed; building a fresh base'
    exit 0
fi
cp -a "$src" /dest/seed-in-progress
mv /dest/seed-in-progress "/dest/$4"
'''
    print('Development: seeding unchanged compiler/base from the clean build', flush=True)
    run([container, 'run', '--rm', '--network', 'none', '--platform', base['platform'],
         '--mount', f'type=volume,source={cold_volume},target=/seed,readonly',
         '--mount', f'type=volume,source={dev_volume},target=/dest',
         base['image'] + '@' + base['digest'], 'sh', '-c', script, 'fes-seed',
         sha, os.getuid(), os.getgid(), Path(selected_work).name])


def run_stage(diagnostics, name, args, **kwargs):
    if diagnostics is None:
        return run(args, **kwargs)
    with diagnostics.measure(name):
        return run(args, **kwargs)


def build_development(root, image, fogcast, runtime, profile_name, profile, info,
                      fingerprint, env, fogcast_make, image_make, packages=None,
                      unavailable=(), diagnostics=None):
    mode = native_image_mode(profile)
    selected_work = env.get('FES_IMAGE_WORK', WORK.format(passes=os.environ.get('IMAGE_PASSES', '1')))
    if mode != 'package-only':
        raise ValueError('FES native development requires package-only mode')
    output = root / 'out' / profile_name / 'development'
    owned_diagnostics = diagnostics is None
    diagnostics = diagnostics or BuildDiagnostics(output, 'dev')
    hit, reason = reuse_status(output, 'development', fingerprint)
    diagnostics.cache('development', 'hit' if hit else 'miss', reason)
    if hit:
        verify_package_only_outputs(output, packages, unavailable)
        if unavailable:
            optional_cores.warn(unavailable)
        if owned_diagnostics:
            diagnostics.finish('success')
        print('Development: reusing checked output; nothing to rebuild', flush=True)
        return
    if not packages:
        raise ValueError('package-only native development requires a selected format-2 package')
    output.mkdir(parents=True, exist_ok=True)
    manifest = optional_cores.publish_manifest(output, unavailable)
    selection_args = ['NATIVE_RUNTIME_MODE=package-only'] + package_arguments(packages, manifest)
    selection_env = dict(env)
    selection_env.update(dict(argument.split('=', 1) for argument in selection_args))
    key = base_key(image, fogcast)
    volume = output_volume(root, profile_name + '-development-' + key)
    # A failed invocation must not leave an older success receipt for this run.
    (output / 'development.json').unlink(missing_ok=True)
    env = dict(env, TARGET_IMAGE_OUTPUT_VOLUME=volume, FOGCAST_DIR=str(fogcast))
    source_lock = tomllib.loads((image / 'build/target-image.sources.lock.toml').read_text())
    base = source_lock['container']
    container = env['TARGET_IMAGE_CONTAINER_RUNTIME']
    ref = base['image'] + '@' + base['digest']
    if subprocess.run([container, 'image', 'inspect', ref], stdout=subprocess.DEVNULL,
                      stderr=subprocess.DEVNULL).returncode:
        run_stage(diagnostics, 'target subprocess',
                  [container, 'pull', '--platform', base['platform'], ref])
    with diagnostics.measure('native base setup'):
        seed_base(container, base, output_volume(root, profile_name), volume,
                  seed_digest(output.parent, info), selected_work)
    run_stage(diagnostics, 'target subprocess', fogcast_make + ['build-agent', 'build-fogcast-kit'], env=env)
    platform.build_kit_software(root, fogcast, env)
    run_stage(diagnostics, 'target subprocess', image_make + ['build-target-image-lock-container'], env=env)
    with target_image_source_cache_lock(image):
        run_stage(diagnostics, 'target subprocess', [image / 'scripts/target-image-container.sh', 'fetch',
             '/work/scripts/fetch-target-image-sources.sh'], env=selection_env)
    run_stage(diagnostics, 'target subprocess', image_make + ['target-image-native-fetch', 'LIBMISTER_RUNTIME_DIR=' + str(runtime),
                     *selection_args], env=env)
    env.update(dict(argument.split('=', 1) for argument in selection_args))
    env['LIBMISTER_RUNTIME_DIR'] = str(runtime)
    # The shared toolchain options live only in fogcast_toolchain.fragment, so the
    # incremental build uses the same generated external config and cached SDK as
    # the clean build (and as the seeded base it continues from).
    run_stage(diagnostics, 'target toolchain subprocess',
              [image / 'scripts/build-target-image.sh', '--ensure-toolchain', 'native-dev'], env=env)
    # Read the authoritative epoch; do not invent another image configuration.
    recipe = (image / 'scripts/build-target-image.sh').read_text()
    epoch_match = re.search(r'^epoch=([0-9]+)$', recipe, re.MULTILINE)
    if not epoch_match:
        raise ValueError('FES image recipe has no supported fixed image epoch')
    epoch = epoch_match.group(1)
    make = shlex.join(['make', '-C', '/work/build/cache/target-image/buildroot',
                      'O=' + selected_work, 'BR2_EXTERNAL=/work/buildroot',
                      'BR2_DL_DIR=/work/build/cache/target-image/dl'])
    runtime_revision = git(runtime, 'rev-parse', 'HEAD')
    if not re.fullmatch('[0-9a-f]{40}', runtime_revision):
        raise ValueError('runtime revision must be a full commit ID')
    selection_copy = ''
    package_selection_files = package_selection_names(packages)
    for package_selection_name in package_selection_files:
        selection_copy += f'''\nrm -f {EXPORT}/{package_selection_name}.new
cp /work/build/cache/target-image/native/{package_selection_name} {EXPORT}/{package_selection_name}.new
chmod 0444 {EXPORT}/{package_selection_name}.new
mv {EXPORT}/{package_selection_name}.new {EXPORT}/{package_selection_name}'''
    script = f'''set -eu
test "$(id -u)" -ne 0
WORK={selected_work}
rm -rf /target-image-output/seed-in-progress
export SOURCE_DATE_EPOCH={epoch} E2FSPROGS_FAKE_TIME={epoch}
/work/scripts/verify-target-image-source-cache.sh /work/build/target-image.sources.lock.toml /work/build/cache/target-image
/work/bin/target-image-lock-linux-amd64 verify-inputs --lock /work/build/target-image.sources.lock.toml --cache /work/build/cache/target-image
mkdir -p "$WORK"
/work/scripts/toolchain_cache.py config external "$WORK/fogcast.generated.defconfig"
TOOLCHAIN_HOST=$(/work/scripts/toolchain_cache.py path)
/work/scripts/toolchain_cache.py extract "$(dirname "$TOOLCHAIN_HOST")"
"$TOOLCHAIN_HOST/relocate-sdk.sh"
{make} BR2_DEFCONFIG="$WORK/fogcast.generated.defconfig" defconfig
/work/scripts/toolchain_cache.py validate-config external "$WORK/.config"
if [ "$(cat "$WORK"/.fes-runtime-commit 2>/dev/null || true)" != {runtime_revision} ]; then
    rm -f "$WORK"/.fes-runtime-commit
    {make} mister-runtime-dirclean
fi
{make}
printf '%s\\n' {runtime_revision} > "$WORK"/.fes-runtime-commit.new
mv "$WORK"/.fes-runtime-commit.new "$WORK"/.fes-runtime-commit
mkdir -p {EXPORT}
cp "$WORK"/images/rootfs.ext4 {EXPORT}/linux.img.new
mv {EXPORT}/linux.img.new {EXPORT}/linux.img
{selection_copy}
'''
    print(f'Development: persistent base {key[:16]} in {volume}', flush=True)
    run_stage(diagnostics, 'target Buildroot subprocess',
              [image / 'scripts/target-image-container.sh', 'run', 'sh', '-c', script],
              env=dict(env, NATIVE_RUNTIME_MODE=mode))
    built = image / 'build/output/target-image/fes-development'
    exported = Path(EXPORT)
    run_stage(diagnostics, 'target verification subprocess',
              [image / 'scripts/verify-target-image.sh', 'native-dev', exported / 'linux.img',
               exported / 'manifest.tsv', exported / 'library-report.tsv'],
              env=dict(env, NATIVE_RUNTIME_MODE=mode))
    names = ['linux.img', 'manifest.tsv', 'library-report.tsv']
    for name in names:
        publish_file(built / name, output / name)
    built_selections = {name: built / name for name in package_selection_files}
    names.extend(publish_package_state(
        packages, built_selections if packages else None, output))
    remove_stale_parent_outputs(output)
    verify_package_only_outputs(output, packages, unavailable)
    record = dict(info, build_mode='incremental-development', base_key=key,
                  output_volume=volume, structural='pass', two_pass_reproducibility='not-run')
    (output / 'inputs.json').write_text(json.dumps(record, indent=2, sort_keys=True) + '\n')
    receipt_names = names + ['inputs.json']
    if manifest is not None:
        receipt_names.append(optional_cores.MANIFEST_NAME)
    write_receipt(output, 'development', fingerprint, receipt_names)
    if owned_diagnostics:
        diagnostics.finish('success')
    if unavailable:
        optional_cores.warn(unavailable)
    print(f'Development image: {output / "linux.img"} (structural checks passed; diagnostic only)',
          flush=True)
