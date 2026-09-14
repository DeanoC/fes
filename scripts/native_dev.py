"""One persistent diagnostic build using the FES image recipe."""
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import tomllib

from build import (digest, output_volume, publish_file, reusable, run, write_receipt,
                   selected_cores, bundle_arguments, package_arguments,
                   package_selection_names,
                   publish_package_state, recorded_image_fingerprint,
                   reuse_status, verify_package_outputs, native_image_mode,
                   verify_package_only_outputs, remove_format1_parent_outputs)
from build_diagnostics import BuildDiagnostics
from inputs import git

# Keep the clean builder's absolute path: Buildroot host tools are not relocatable.
WORK = '/target-image-output/work-2-native-dev'
EXPORT = '/work/build/output/target-image/fes-development'


def base_key(image, fogcast):
    names = git(image, 'ls-files', '-z').split('\0')
    files = {name: digest(image / name) for name in names if name and (
        name.startswith(('buildroot/', 'containers/target-image/'))
        or (name.startswith('scripts/') and not name.startswith('scripts/tests/'))
        or name in ('Makefile', 'build/target-image.sources.lock.toml',
                    'build/target-image-container-packages.sha256'))}
    native = tomllib.loads((fogcast / 'build/native-runtime.inputs.lock.toml').read_text())
    native['mister_runtime'].pop('commit')
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
        if evidence.get('run_1_sha256') == sha == evidence.get('run_2_sha256'):
            return sha
    except (OSError, ValueError, KeyError, TypeError):
        pass
    return None


def seed_base(container, base, cold_volume, dev_volume, sha):
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
    script = '''set -eu
src=/seed/work-2-native-dev
chown "$2:$3" /dest
if ! (test -f "$src/.config" && test -d "$src/host" &&
      test -d "$src/build" && test -d "$src/target" &&
      test "$(sha256sum "$src/images/rootfs.ext4" | cut -d ' ' -f 1)" = "$1"); then
    echo 'Development: clean volume is incomplete or changed; building a fresh base'
    exit 0
fi
cp -a "$src" /dest/seed-in-progress
mv /dest/seed-in-progress /dest/work-2-native-dev
'''
    print('Development: seeding unchanged compiler/base from the clean build', flush=True)
    run([container, 'run', '--rm', '--network', 'none', '--platform', base['platform'],
         '--mount', f'type=volume,source={cold_volume},target=/seed,readonly',
         '--mount', f'type=volume,source={dev_volume},target=/dest',
         base['image'] + '@' + base['digest'], 'sh', '-c', script, 'fes-seed',
         sha, os.getuid(), os.getgid()])


def run_stage(diagnostics, name, args, **kwargs):
    if diagnostics is None:
        return run(args, **kwargs)
    with diagnostics.measure(name):
        return run(args, **kwargs)


def build_development(root, image, fogcast, runtime, profile_name, profile, info,
                      fingerprint, env, fogcast_make, image_make, bundles, packages=None,
                      diagnostics=None):
    mode = native_image_mode(profile)
    if mode == 'format1' and profile.get('bundle_interface') != 'selection':
        raise ValueError('make dev requires native-integration-dev; historical profiles stay cold')
    output = root / 'out' / profile_name / 'development'
    owned_diagnostics = diagnostics is None
    diagnostics = diagnostics or BuildDiagnostics(output, 'dev')
    hit, reason = reuse_status(output, 'development', fingerprint)
    diagnostics.cache('development', 'hit' if hit else 'miss', reason)
    if hit:
        if mode == 'package-only':
            verify_package_only_outputs(output, packages)
        else:
            verify_package_outputs(output, packages)
        if owned_diagnostics:
            diagnostics.finish('success')
        print('Development: reusing checked output; nothing to rebuild', flush=True)
        return
    cores = selected_cores(profile) if mode == 'format1' else ()
    if mode == 'package-only':
        if not packages:
            raise ValueError('package-only native development requires a selected format-2 package')
        cores = ()
        selection_args = ['NATIVE_RUNTIME_MODE=package-only'] + package_arguments(packages)
    else:
        selection_args = (['NATIVE_RUNTIME_MODE=format1'] +
                          bundle_arguments(cores, bundles) + package_arguments(packages))
    key = base_key(image, fogcast)
    volume = output_volume(root, profile_name + '-development-' + key)
    output.mkdir(parents=True, exist_ok=True)
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
                  seed_digest(output.parent, info))
    run_stage(diagnostics, 'target subprocess', fogcast_make + ['build-agent', 'build-fogcast-kit'], env=env)
    run_stage(diagnostics, 'target subprocess', image_make + ['build-target-image-lock-container'], env=env)
    run_stage(diagnostics, 'target subprocess', [image / 'scripts/target-image-container.sh', 'fetch',
         '/work/scripts/fetch-target-image-sources.sh'], env=env)
    run_stage(diagnostics, 'target subprocess', image_make + ['target-image-native-fetch', 'LIBMISTER_RUNTIME_DIR=' + str(runtime),
                     *selection_args], env=env)
    env.update(dict(argument.split('=', 1) for argument in selection_args))
    env['LIBMISTER_RUNTIME_DIR'] = str(runtime)
    # Read the authoritative epoch; do not invent another image configuration.
    recipe = (image / 'scripts/build-target-image.sh').read_text()
    epoch_match = re.search(r'^epoch=([0-9]+)$', recipe, re.MULTILINE)
    if not epoch_match:
        raise ValueError('FES image recipe has no supported fixed image epoch')
    epoch = epoch_match.group(1)
    make = shlex.join(['make', '-C', '/work/build/cache/target-image/buildroot',
                      'O=' + WORK, 'BR2_EXTERNAL=/work/buildroot',
                      'BR2_DL_DIR=/work/build/cache/target-image/dl'])
    runtime_revision = git(runtime, 'rev-parse', 'HEAD')
    if not re.fullmatch('[0-9a-f]{40}', runtime_revision):
        raise ValueError('runtime revision must be a full commit ID')
    selection_copy = ''
    if mode == 'format1':
        selection_copy = '\n'.join(
            f'rm -f {EXPORT}/{core}.selection.toml.new\n'
            f'cp /work/build/cache/target-image/native/{core}.selection.toml {EXPORT}/{core}.selection.toml.new\n'
            f'chmod 0444 {EXPORT}/{core}.selection.toml.new\n'
            f'mv {EXPORT}/{core}.selection.toml.new {EXPORT}/{core}.selection.toml'
            for core in cores)
    package_selection_files = package_selection_names(packages)
    for package_selection_name in package_selection_files:
        selection_copy += f'''\nrm -f {EXPORT}/{package_selection_name}.new
cp /work/build/cache/target-image/native/{package_selection_name} {EXPORT}/{package_selection_name}.new
chmod 0444 {EXPORT}/{package_selection_name}.new
mv {EXPORT}/{package_selection_name}.new {EXPORT}/{package_selection_name}'''
    script = f'''set -eu
test "$(id -u)" -ne 0
rm -rf /target-image-output/seed-in-progress
export SOURCE_DATE_EPOCH={epoch} E2FSPROGS_FAKE_TIME={epoch}
/work/scripts/verify-target-image-source-cache.sh /work/build/target-image.sources.lock.toml /work/build/cache/target-image
/work/bin/target-image-lock-linux-amd64 verify-inputs --lock /work/build/target-image.sources.lock.toml --cache /work/build/cache/target-image
{make} fogcast_target_native_dev_defconfig
if [ "$(cat {WORK}/.fes-runtime-commit 2>/dev/null || true)" != {runtime_revision} ]; then
    rm -f {WORK}/.fes-runtime-commit
    {make} mister-runtime-dirclean
fi
{make}
printf '%s\\n' {runtime_revision} > {WORK}/.fes-runtime-commit.new
mv {WORK}/.fes-runtime-commit.new {WORK}/.fes-runtime-commit
mkdir -p {EXPORT}
cp {WORK}/images/rootfs.ext4 {EXPORT}/linux.img.new
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
               exported / 'manifest.tsv', exported / 'library-report.tsv',
               *([exported / 'megadrive.selection.toml'] if mode == 'format1' else [])],
              env=dict(env, NATIVE_RUNTIME_MODE=mode))
    names = ['linux.img', 'manifest.tsv', 'library-report.tsv'] + [
        core + '.selection.toml' for core in cores]
    for name in names:
        publish_file(built / name, output / name)
    if mode == 'format1':
        for core in cores:
            for name in (core + '.rbf', core + '-rbf.toml'):
                publish_file(bundles[core] / name, output / name)
                names.append(name)
    built_selections = {name: built / name for name in package_selection_files}
    names.extend(publish_package_state(
        packages, built_selections if packages else None, output))
    if mode == 'package-only':
        remove_format1_parent_outputs(output)
        verify_package_only_outputs(output, packages)
    record = dict(info, build_mode='incremental-development', base_key=key,
                  output_volume=volume, structural='pass', two_pass_reproducibility='not-run')
    (output / 'inputs.json').write_text(json.dumps(record, indent=2, sort_keys=True) + '\n')
    write_receipt(output, 'development', fingerprint, names + ['inputs.json'])
    if owned_diagnostics:
        diagnostics.finish('success')
    print(f'Development image: {output / "linux.img"} (structural checks passed; diagnostic only)',
          flush=True)
