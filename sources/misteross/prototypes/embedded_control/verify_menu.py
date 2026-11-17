#!/usr/bin/env python3
"""Check owned 720p scanout with actual Tenfoot frames; never programs hardware."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import uuid

from verify_window import FES, HERE, first_party_go_sources, producer, run, sha

MENU = FES/'sources/misteross/cores/fes-menu/rtl'
INPUTS = [Path(__file__), HERE/'verify_window.py', HERE/'producer.py',
          HERE/'fes_embedded_menu_scanout.sv', HERE/'fes_embedded_menu_control.sv',
          HERE/'embedded_menu_top.sv', HERE/'menu_probe.cpp', HERE/'menu_bench.hpp', HERE/'menu_rpc.cpp', HERE/'menu_firmware.hpp',
          FES/'platform/embedded/firmware/nano_fabric_stop.c', FES/'platform/embedded/firmware/nano_fabric_stop.h',
          FES/'platform/embedded/firmware/nano_probe_stop.c', FES/'platform/embedded/firmware/nano_probe_stop.h',
          FES/'platform/embedded/firmware/generated/fes_de25_ddr_probe.h',
          FES/'platform/embedded/tamago/scanout/menu.go',
          FES/'platform/embedded/tamago/scanout/menu_integration_test.go',
          HERE.parent/'de25_board_control/fes_de25_read_width.sv',
          MENU/'fes_menu_video.v', MENU/'fes_menu_reader.v',
          FES/'sources/misteross/cores/fes-common/rtl/fes_embedded_control.sv',
          FES/'sources/misteross/cores/fes-common/generated/fes_embedded_control.vh',
          FES/'platform/embedded/firmware/generated/fes_embedded_control.h',
          FES/'platform/embedded/tamago/go.mod', FES/'platform/embedded/tamago/go.sum',
          FES/'sources/FogCast/go.mod', FES/'sources/FogCast/go.sum',
          FES/'sources/misteross/scripts/export_core_package.py',
          FES/'sources/misteross/scripts/core_package.py',
          FES/'sources/misteross/scripts/source_repository.py',
          FES/'sources/misteross/scripts/rfc3986_validator.py']


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, default=FES/'out'/('embedded-menu-'+uuid.uuid4().hex))
    parser.add_argument('--allow-dirty', action='store_true')
    args = parser.parse_args()
    output = args.output.absolute()
    if (output.exists() or not output.is_relative_to(FES/'out') or '..' in output.parts or
            any(p.is_symlink() for p in (output, *output.parents))):
        raise ValueError('fresh private FES out/ directory required')
    output.mkdir(parents=True)
    env = dict(os.environ)
    root = subprocess.check_output(['go', '-C', str(FES/'platform/embedded/tamago'), 'env', 'GOROOT'],
                                   text=True, timeout=30).strip()
    go = Path(root)/'bin/go'
    env['GOTOOLCHAIN'] = 'local'
    imports = run(output, 'renderer-sources', [go, '-C', 'platform/embedded/tamago', 'list', '-deps',
                  '-json', '-tags', 'noglfw', './scanout/testdata/frame'], env)
    selected = sorted({p.relative_to(FES).as_posix() for p in INPUTS} | first_party_go_sources(imports))
    sources = {name: sha(FES/name) for name in selected}
    tracked = subprocess.run(['git', 'ls-files', '--error-unmatch', '--', *selected],
                             cwd=FES, capture_output=True).returncode == 0
    changed = subprocess.run(['git', 'diff', '--quiet', 'HEAD', '--', *selected], cwd=FES).returncode
    if changed not in (0, 1):
        raise ValueError('cannot check selected source state')
    dirty = not tracked or changed == 1
    if dirty and not args.allow_dirty:
        raise ValueError('commit selected inputs or use labelled --allow-dirty simulation')
    (output/'source-inputs.json').write_text(json.dumps(sources, indent=2, sort_keys=True)+'\n')
    run(output, 'renderer-build', [go, '-C', 'platform/embedded/tamago', 'build', '-buildvcs=false',
        '-tags', 'noglfw', '-o', output/'frame-renderer', './scanout/testdata/frame'], env)
    run(output, 'render', [output/'frame-renderer', '--output', output/'frames'], env)
    frames = json.loads((output/'frames/frames.json').read_text())
    assert frames['renderer'] == 'tenfoot.NewApp/NewPresenter' and frames['copy'] == 'scanout.Surface.Present'
    assert len(frames['sha256']) == 2 and frames['sha256'][0] != frames['sha256'][1]
    for i, digest in enumerate(frames['sha256']):
        assert sha(output/f'frames/frame-{i}.bin') == digest
    # Same guarded, RAM-hint-only Agilex derivation as the existing display lane.
    original = (MENU/'fes_menu_reader.v').read_bytes()
    before = b'(* ramstyle = "M10K" *)'
    assert original.count(before) == 1
    derived = output/'agilex-rtl'
    derived.mkdir()
    reader = derived/'fes_menu_reader.v'
    reader.write_bytes(original.replace(before, b'(* ramstyle = "M20K" *)'))
    tools, paths = producer.tools_snapshot()
    tools['go'] = dict(path=str(go), sha256=sha(go), version=subprocess.check_output([go, 'version'], env=env, text=True).strip())
    model_env = producer.build_environment(paths)
    def build(label, scanout, parameters=(), probe=HERE/'menu_probe.cpp', objects=()):
        obj = output/(label+'-obj')
        run(output, label+'-build', [paths['verilator'], '--cc', '--exe', '--build', '-j', '2',
            '--top-module', 'embedded_menu_top', *parameters, '-Wall', '-Wno-UNUSEDSIGNAL', '-Wno-PINCONNECTEMPTY', '--Mdir', obj,
            '-I'+str(FES/'sources/misteross/cores/fes-common/generated'), '-CFLAGS',
            '-std=c++17 -O2 -I'+str(FES/'platform/embedded/firmware/generated')+' -I'+str(FES/'platform/embedded/firmware'),
            *(['-LDFLAGS', ' '.join(str(p) for p in objects)] if objects else []),
            FES/'sources/misteross/cores/fes-common/rtl/fes_embedded_control.sv',
            HERE.parent/'de25_board_control/fes_de25_read_width.sv',
            MENU/'fes_menu_video.v', reader, scanout, HERE/'fes_embedded_menu_control.sv', HERE/'embedded_menu_top.sv', probe], model_env)
        return obj/'Vembedded_menu_top'
    executable = build('menu', HERE/'fes_embedded_menu_scanout.sv')
    arguments = [output/'frames/frame-0.bin', output/'frames/frame-1.bin']
    observed = run(output, 'menu-model', [executable, *arguments], model_env, cap=90)
    assert observed.startswith('PASS owned menu scanout: ')
    # The oracle must reject an acknowledgement at prefetch rather than display.
    negative_dir = output/'early-ack'
    negative_dir.mkdir()
    mutant = negative_dir/'fes_embedded_menu_scanout.sv'
    raw = (HERE/'fes_embedded_menu_scanout.sv').read_bytes()
    anchor = b".COMPLETE_FRAME_ACK(1'b1)"
    assert raw.count(anchor) == 1
    mutant.write_bytes(raw.replace(anchor, b".COMPLETE_FRAME_ACK(1'b0)"))
    negative_executable = build('early-ack', mutant)
    rejected = run(output, 'early-ack-model', [negative_executable, *arguments], model_env, expect=1, cap=30)
    assert 'frame acknowledged before final pixel' in rejected
    invalid_executable = build('invalid-arena', HERE/'fes_embedded_menu_scanout.sv', ['-GWINDOW_BYTES=4194304'])
    invalid = run(output, 'invalid-arena-model', [invalid_executable, *arguments, 'invalid'], model_env, cap=30)
    assert 'PASS invalid single-slot arena rejected before DDR access' in invalid
    native = output/'native-csr'
    native.mkdir()
    firmware_objects = []
    for name in ('nano_fabric_stop', 'nano_probe_stop'):
        obj = native/(name+'.o'); firmware_objects.append(obj)
        run(output, name+'-build', ['cc', '-std=c11', '-Wall', '-Wextra', '-Werror',
            '-I'+str(FES/'platform/embedded/firmware'), '-c', FES/('platform/embedded/firmware/'+name+'.c'), '-o', obj], env)
    rpc = build('menu-csr', HERE/'fes_embedded_menu_scanout.sv', ["-GCSR_MODE=1'b1"], HERE/'menu_rpc.cpp', firmware_objects)
    (native/'descriptor.json').write_text(json.dumps(dict(Executable=str(rpc), Frames=[str(p) for p in arguments]), indent=2)+'\n')
    native_env = dict(env, FES_MENU_CSR_DIR=str(native))
    raw_native = run(output, 'native-menu', [go, '-C', 'platform/embedded/tamago', 'test', '-race', '-count=1',
        '-timeout', '90s', '-json', '-run', '^TestMenuActualRTL$', './scanout'], native_env, cap=120)
    events = [json.loads(line) for line in raw_native.splitlines() if line.startswith('{')]
    native_cases = [e['Test'] for e in events if e.get('Action') == 'pass' and e.get('Test', '').startswith('TestMenuActualRTL/')]
    assert len(native_cases) >= 25 and not any(e.get('Action') in ('fail', 'skip') for e in events)
    assert all(sha(FES/name) == digest for name, digest in sources.items())
    assert all(sha(output/f'frames/frame-{i}.bin') == digest for i, digest in enumerate(frames['sha256']))
    result = dict(status='passed', source_state='working-tree' if dirty else 'committed',
        source_revision=producer.git('rev-parse', 'HEAD').stdout.strip(),
        source_inputs_sha256=sha(output/'source-inputs.json'), tools=tools, frames=frames,
        reader_derivation=dict(original_sha256=sha(MENU/'fes_menu_reader.v'), derived_sha256=sha(reader),
                               change='RAM style M10K to M20K only'),
        native_presenter=dict(status='passed', executable_sha256=sha(rpc), cases=native_cases, log_sha256=sha(output/'native-menu.log')),
        executable_sha256=sha(executable), observed=observed.strip(), early_ack_negative_rejected=True, invalid_arena_rejected=True,
        pixel_clock_domain='single; board CDC not supplied', hardware_actions=False,
        memory_bus_bits=256, reader_bus_bits=128,
        physical_ddr=False, physical_hdmi=False, runtime_admission=False)
    (output/'result.json').write_text(json.dumps(result, indent=2, sort_keys=True)+'\n')
    print('PASS actual Tenfoot frames through owned DDR endpoint and 720p scanout; host-only:', output)


if __name__ == '__main__':
    main()
