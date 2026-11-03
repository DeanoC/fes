#!/usr/bin/env python3
"""Stream actual tenfoot/XRGB frames through the owned endpoint's RTL model.

Host simulation only. No RBF, board access, DDR grant or runtime admission.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import time
import uuid

HERE = Path(__file__).resolve().parent
FES = HERE.parents[3]
sys.path.insert(0, str(HERE))
import producer
from scripts.export_core_package import build_identity, encode_build_record

RECIPE = Path(__file__).resolve().relative_to(FES).as_posix()
CONTRACT = 'sources/misteross/cores/fes-common/generated/fes_embedded_control.vh'
SOURCES = [RECIPE, CONTRACT,
    'sources/misteross/cores/fes-common/rtl/fes_embedded_control.sv',
    'sources/misteross/prototypes/embedded_control/fes_embedded_window_reader.sv',
    'sources/misteross/prototypes/embedded_control/embedded_window_top.sv',
    'sources/misteross/prototypes/embedded_control/window_probe.cpp',
    'sources/misteross/prototypes/embedded_control/producer.py',
    'sources/misteross/scripts/export_core_package.py',
    'sources/misteross/scripts/core_package.py',
    'platform/embedded/firmware/generated/fes_embedded_control.h',
    'platform/embedded/tamago/scanout/testdata/frame/main.go',
    'platform/embedded/tamago/scanout/surface.go',
    'platform/embedded/tamago/go.mod', 'platform/embedded/tamago/go.sum',
    'sources/FogCast/go.mod', 'sources/FogCast/go.sum']


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def run(output, label, argv, env=None, cap=180, expect=0):
    if shutil.disk_usage(FES).free < 30 * (1 << 30):
        raise ValueError('less than 30 GiB free before job')
    start = time.monotonic()
    with (output / (label + '.log')).open('xb') as log:
        child = subprocess.Popen([str(a) for a in argv], cwd=FES, env=env,
                                 stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        beat = start + 12
        try:
            while child.poll() is None:
                now = time.monotonic()
                if now - start > cap:
                    raise TimeoutError(label + ' deadline')
                if now >= beat:
                    print(label + ': running', flush=True)
                    beat = now + 12
                time.sleep(.1)
        except BaseException:
            if child.poll() is None:
                os.killpg(child.pid, signal.SIGTERM)
                try:
                    child.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(child.pid, signal.SIGKILL)
                    child.wait()
            raise
    receipt = dict(argv=[str(a) for a in argv], returncode=child.returncode,
                   elapsed_seconds=time.monotonic()-start, log_sha256=sha(output/(label+'.log')))
    (output/(label+'-result.json')).write_text(json.dumps(receipt, indent=2)+'\n')
    if child.returncode != expect:
        raise ValueError(label + ': unexpected exit; see ' + str(output/(label+'.log')))
    return (output/(label+'.log')).read_text()


def first_party_go_sources(raw):
    decoder = json.JSONDecoder()
    result = set()
    while raw.strip():
        value, used = decoder.raw_decode(raw.lstrip())
        raw = raw.lstrip()[used:]
        directory = Path(value['Dir'])
        if not directory.is_relative_to(FES):
            continue
        for key in ('GoFiles', 'CgoFiles', 'SFiles', 'CFiles', 'HFiles', 'EmbedFiles'):
            for name in value.get(key, []):
                path = directory/name
                if path.is_symlink() or not path.is_file():
                    raise ValueError('non-regular renderer source: '+str(path))
                result.add(path.relative_to(FES).as_posix())
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, default=FES/'out'/('embedded-window-'+uuid.uuid4().hex))
    parser.add_argument('--allow-dirty', action='store_true')
    args = parser.parse_args()
    output = args.output.absolute()
    if (output.exists() or not output.is_relative_to(FES/'out') or '..' in output.parts or
            any(p.is_symlink() for p in (output, *output.parents))):
        raise ValueError('fresh private FES out/ directory required')
    output.mkdir(parents=True)
    environment = os.environ.copy()
    root = subprocess.check_output(['go', '-C', str(FES/'platform/embedded/tamago'), 'env', 'GOROOT'],
                                   cwd=FES, env=environment, text=True, timeout=30).strip()
    go = Path(root)/'bin/go'
    environment['GOTOOLCHAIN'] = 'local'
    imports = run(output, 'renderer-sources', [go, '-C', 'platform/embedded/tamago', 'list',
        '-deps', '-json', '-tags', 'noglfw', './scanout/testdata/frame'], environment)
    selected = sorted(set(SOURCES) | first_party_go_sources(imports))
    source = {name: sha(FES/name) for name in selected}
    # Keep the existing 64KiB functional-record bound. The full renderer source
    # closure is a checked sidecar whose complete digest enters that record.
    closure = output/'renderer-source-inputs.json'
    closure.write_bytes(producer.canonical(source))
    primary = sorted(set(SOURCES))
    tracked = subprocess.run(['git', 'ls-files', '--error-unmatch', '--', *selected],
                             cwd=FES, capture_output=True).returncode == 0
    changed = subprocess.run(['git', 'diff', '--quiet', 'HEAD', '--', *selected], cwd=FES).returncode
    if changed not in (0, 1):
        raise ValueError('cannot check selected source state')
    dirty = not tracked or changed != 0
    if dirty and not args.allow_dirty:
        raise ValueError('commit selected inputs or use labelled --allow-dirty simulation')
    run(output, 'renderer-build', [go, '-C', 'platform/embedded/tamago', 'build', '-buildvcs=false',
        '-tags', 'noglfw', '-o', output/'frame-renderer', './scanout/testdata/frame'], environment)
    tools, paths = producer.tools_snapshot()
    tools['go'] = str(go)+' | '+subprocess.check_output([str(go), 'version'], text=True, env=environment).strip()+' | sha256:'+sha(go)
    model_env = producer.build_environment(paths)
    rows = []
    for label, base, stride in (('packed', 0x94000000, 5120), ('padded-page-edge', 0x94000ff0, 5200)):
        case = output/label
        case.mkdir()
        run(case, 'render', [output/'frame-renderer', '--output', case/'frames', '--stride', str(stride)], environment)
        frames = json.loads((case/'frames/frames.json').read_text())
        assert frames['renderer']=='tenfoot.NewApp/NewPresenter' and frames['copy']=='scanout.Surface.Present'
        assert len(frames['sha256'])==2 and frames['sha256'][0]!=frames['sha256'][1]
        for index, digest in enumerate(frames['sha256']):
            assert sha(case/f'frames/frame-{index}.bin')==digest
        fields = dict(format=2, repository=producer.canonical_repository(producer.git('remote', 'get-url', 'origin').stdout.strip()),
            revision=producer.git('rev-parse', 'HEAD').stdout.strip(), source_path='.',
            recipe=RECIPE, recipe_sha256=source[RECIPE], abi_definition=CONTRACT,
            abi_definition_sha256=source[CONTRACT], dependencies={}, tools=tools,
            source_roots=primary, source_inputs={name: source[name] for name in primary},
            parameters=dict(top='embedded_window_top', control_abi=0x10203042,
                window_base=base, window_bytes=8<<20, row_bytes=5120, rows=720, stride_bytes=stride,
                source_state='working-tree' if dirty else 'committed', simulation_only=True,
                frame0_sha256=frames['sha256'][0], frame1_sha256=frames['sha256'][1],
                renderer_executable_sha256=sha(output/'frame-renderer'),
                renderer_source_inputs_sha256=sha(closure),
                source_scope='selected first-party sources; external modules pinned by go.sum and renderer executable',
                verilator_flags='--cc --exe --build -j2 -Wall -Wno-UNUSEDSIGNAL', cpp_standard='c++17'))
        record = encode_build_record(fields)
        build_id = build_identity(record)
        (case/'build-inputs.json').write_bytes(record)
        obj = case/'obj'
        command = [paths['verilator'], '--cc', '--exe', '--build', '-j', '2', '--top-module', 'embedded_window_top',
            '-Wall', '-Wno-UNUSEDSIGNAL', '--Mdir', obj, '-I'+str(FES/Path(CONTRACT).parent),
            "-GBUILD_ID=128'h"+build_id, '-GWINDOW_BASE='+str(base), '-GSTRIDE_BYTES='+str(stride),
            '-CFLAGS', '-std=c++17 -O2 -I'+str(FES/'platform/embedded/firmware/generated'),
            FES/'sources/misteross/cores/fes-common/rtl/fes_embedded_control.sv',
            HERE/'fes_embedded_window_reader.sv', HERE/'embedded_window_top.sv', HERE/'window_probe.cpp']
        run(case, 'build', command, model_env)
        executable = obj/'Vembedded_window_top'
        argv = [executable, case/'frames/frame-0.bin', case/'frames/frame-1.bin', hex(base), str(stride), build_id]
        observed = json.loads(run(case, 'model', argv, model_env, cap=60))
        assert observed==dict(status='passed', cases=15, width=1280, height=720, frame_bytes=3686400,
                             stride_bytes=stride, window_base=base, physical_ddr=False, runtime_admission=False)
        bad = ('0' if build_id[0]!='0' else '1')+build_id[1:]
        negative = run(case, 'wrong-id', [*argv[:-1], bad], model_env, expect=1, cap=30)
        assert 'window live build identity differs' in negative
        negative = run(case, 'corrupt-data', [*argv, 'corrupt-data'], model_env, expect=1, cap=30)
        assert 'pixel bytes differ from native XRGB framebuffer' in negative
        assert all(sha(case/f'frames/frame-{i}.bin')==digest for i,digest in enumerate(frames['sha256']))
        rows.append(dict(geometry=label, build_id=build_id, build_inputs_sha256=sha(case/'build-inputs.json'),
                         executable_sha256=sha(executable), frames=frames, observed=observed,
                         invalid_geometry_windows=8, wrong_identity_rejected=True, corrupt_pixel_rejected=True))
    # No source may change while the renderer and RTL artifacts are produced.
    assert all(sha(FES/name)==digest for name,digest in source.items())
    report = dict(status='passed', classification='tenfoot/native-XRGB/owned-AXI-window host simulation',
        source_commit=producer.git('rev-parse', 'HEAD').stdout.strip(), source_state='working-tree' if dirty else 'committed',
        source_sha256=source, renderer_executable_sha256=sha(output/'frame-renderer'), variants=rows,
        physical_ddr=False, pixel_clock_and_hdmi=False, bridge_release=False, runtime_admission=False)
    (output/'result.json').write_text(json.dumps(report, indent=2)+'\n')
    print('PASS actual tenfoot frames, two window geometries,30 lifecycle/fault cases and16 invalid windows:', output, flush=True)


if __name__ == '__main__':
    def interrupted(*_):
        # The existing producer integration caller has a 180s subprocess cap.
        # Retire our own process group before that outer caller must stop us.
        raise TimeoutError('150-second window model deadline/interruption')
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGALRM, interrupted)
    signal.alarm(150)
    try:
        main()
    except (OSError, ValueError, KeyError, AssertionError, subprocess.SubprocessError) as error:
        print('window reader:', error, file=sys.stderr)
        sys.exit(1)
    finally:
        signal.alarm(0)
