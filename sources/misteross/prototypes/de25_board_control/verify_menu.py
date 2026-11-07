#!/usr/bin/env python3
"""Verify the actual two-clock board menu; never build or program a bitstream."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import uuid

HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[3]
PIPELINE=HERE.parent/'embedded_control'
sys.path.insert(0,str(PIPELINE))
from verify_window import first_party_go_sources, run, sha
sys.path.insert(0,str(HERE))
import menu_transport

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--output',type=Path,default=ROOT/'out'/('de25-menu-'+uuid.uuid4().hex))
    p.add_argument('--project',type=Path)
    p.add_argument('--allow-dirty',action='store_true')
    args=p.parse_args();out=args.output.absolute()
    if out.exists() or not out.is_relative_to(ROOT/'out') or '..' in out.parts or any(p.is_symlink() for p in (out,*out.parents)):
        p.error('fresh private output below out/ required')
    build='0123456789abcdef0123456789abcdef'
    if args.project:
        # inputs_for rechecks the full source, sealed package and generated
        # clock/reset/video routes. This model does not replace those receipts.
        module_spec=importlib.util.spec_from_file_location('menu_source_verifier',HERE/'verify.py')
        verifier=importlib.util.module_from_spec(module_spec);module_spec.loader.exec_module(verifier)
        inputs,_,_=verifier.inputs_for(args.project.absolute())
        if not menu_transport.enabled(inputs):p.error('compiled menu personality required')
        for name in menu_transport.INPUTS+(menu_transport.PREFIX+'fes_de25_read_width.sv',
                'sources/misteross/cores/fes-common/rtl/fes_embedded_control.sv',
                'sources/misteross/cores/fes-common/generated/fes_embedded_control.vh'):
            if sha(ROOT/name)!=inputs['functional_snapshot_sha256'].get(name):
                raise ValueError('model sources differ from selected sealed project')
        build=inputs['embedded_control']['build_id']
    out.mkdir(parents=True);env=dict(os.environ)
    go_root=subprocess.check_output(['go','-C',str(ROOT/'platform/embedded/tamago'),'env','GOROOT'],text=True,timeout=30).strip()
    go=Path(go_root)/'bin/go';env['GOTOOLCHAIN']='local'
    dependencies=run(out,'renderer-sources',[go,'-C','platform/embedded/tamago','list','-deps','-json','-tags','noglfw','./scanout/testdata/frame'],env)
    files=[Path(__file__),HERE/'menu_cdc_probe.cpp',HERE/'menu_board_probe.cpp',HERE/'menu_board_model.sv',
        *(ROOT/n for n in menu_transport.INPUTS),HERE/'fes_de25_read_width.sv',
        ROOT/'sources/misteross/cores/fes-common/rtl/fes_embedded_control.sv',
        ROOT/'sources/misteross/cores/fes-common/generated/fes_embedded_control.vh',
        ROOT/'platform/embedded/firmware/generated/fes_embedded_control.h',
        ROOT/'platform/embedded/tamago/go.mod',ROOT/'platform/embedded/tamago/go.sum',
        ROOT/'sources/FogCast/go.mod',ROOT/'sources/FogCast/go.sum',PIPELINE/'verify_window.py']
    names=sorted({p.relative_to(ROOT).as_posix() for p in files}|first_party_go_sources(dependencies))
    sources={name:sha(ROOT/name) for name in names}
    tracked=subprocess.run(['git','ls-files','--error-unmatch','--',*names],cwd=ROOT,capture_output=True).returncode==0
    changed=subprocess.run(['git','diff','--quiet','HEAD','--',*names],cwd=ROOT).returncode
    if changed not in (0,1):raise ValueError('cannot inspect selected source')
    dirty=not tracked or changed==1
    if dirty and not args.allow_dirty:raise ValueError('commit selected sources or label with --allow-dirty')
    (out/'source-inputs.json').write_text(json.dumps(sources,sort_keys=True,indent=2)+'\n')
    run(out,'renderer-build',[go,'-C','platform/embedded/tamago','build','-buildvcs=false','-tags','noglfw','-o',out/'frames-tool','./scanout/testdata/frame'],env)
    run(out,'render',[out/'frames-tool','--output',out/'frames'],env)
    frames=json.loads((out/'frames/frames.json').read_text())
    assert frames['renderer']=='tenfoot.NewApp/NewPresenter' and frames['copy']=='scanout.Surface.Present'
    for i,h in enumerate(frames['sha256']):assert sha(out/f'frames/frame-{i}.bin')==h
    reader=out/'fes_menu_reader.v';reader.write_bytes(menu_transport.reader((ROOT/(menu_transport.VIDEO+'fes_menu_reader.v')).read_bytes()))
    cdc=HERE/'fes_de25_menu_cdc.sv'
    def build_cdc(label,source):
        model=out/(label+'-obj')
        run(out,label+'-build',['verilator','--cc','--exe','--build','-j','2','-Wall','--top-module','fes_de25_menu_cdc',
            '-GTIMEOUT_CYCLES=64','--Mdir',model,'-CFLAGS','-std=c++17 -O2',source,HERE/'menu_cdc_probe.cpp'],env,cap=90)
        return model/'Vfes_de25_menu_cdc'
    exe=build_cdc('cdc',cdc)
    observed=run(out,'cdc-model',[exe],env,cap=30)
    assert observed.startswith('PASS 12 bounded two-clock CDC cases;')
    mutants=[('old-request',b'arm_count==3 && !request_sync[1]',b'!request_sync[1]','pixel reset adopted an old high request'),
        ('no-timeout',b"timer==TIMER_BITS'(TIMEOUT_CYCLES-1)",b"1'b0",'CPU reply missing or duplicated')]
    for label,anchor,replacement,marker in mutants:
        raw=cdc.read_bytes();assert raw.count(anchor)==1
        case=out/label;case.mkdir();source=case/cdc.name;source.write_bytes(raw.replace(anchor,replacement))
        mutant=build_cdc(label,source)
        text=run(out,label+'-model',[mutant],env,expect=1,cap=30)
        assert marker in text
    obj=out/'board-obj'
    run(out,'board-build',['verilator','--cc','--exe','--build','-j','2','-Wall','-Wno-UNUSEDSIGNAL','-Wno-PINCONNECTEMPTY',
        '--top-module','menu_board_model',"-GBUILD_ID=128'h"+build,'--Mdir',obj,
        '-I'+str(ROOT/'sources/misteross/cores/fes-common/generated'),'-CFLAGS',
        '-std=c++17 -O2 -I'+str(ROOT/'platform/embedded/firmware/generated'),
        HERE/'menu_board_model.sv',HERE/'fes_de25_menu.sv',cdc,PIPELINE/'embedded_menu_top.sv',
        PIPELINE/'fes_embedded_menu_control.sv',PIPELINE/'fes_embedded_menu_scanout.sv',HERE/'fes_de25_read_width.sv',
        ROOT/'sources/misteross/cores/fes-common/rtl/fes_embedded_control.sv',ROOT/(menu_transport.VIDEO+'fes_menu_video.v'),
        reader,HERE/'menu_board_probe.cpp'],env,cap=90)
    board=run(out,'board-model',[obj/'Vmenu_board_model',out/'frames/frame-0.bin',out/'frames/frame-1.bin',build],env,cap=90)
    assert board.startswith('PASS board menu: 100/74.25 MHz, actual Tenfoot pixels, three frames, owned drain')
    assert all(sha(ROOT/n)==h for n,h in sources.items())
    if args.project:verifier.inputs_for(args.project.absolute())
    report=dict(status='passed',source_revision=subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip(),
        source_state='working-tree' if dirty else 'committed',source_inputs_sha256=sha(out/'source-inputs.json'),
        build_id=build,system_hz=100000000,pixel_hz=74250000,cdc_cases=12,exact_CSR_packets=640,
        frames=frames,frame_sequence=[1,2,3],slot_sequence=[0,1,0],independent_pixel_oracle=True,
        actual_pipeline_and_width_adapter=True,negative_controls_rejected=[x[0] for x in mutants],
        stopped_pixel_clock_bounded=True,in_flight_reset_no_replay=True,
        board_csr_timeout_cycles=1048576,model_timeout_cycles=256,
        source_derived_M20K_reader_sha256=sha(reader),hardware_actions=False,physical_DDR=False,physical_HDMI=False,
        generated_HPS_AXI_crossing_simulated=False,timing_signoff=False,runtime_admission=False,
        logs_sha256={p.name:sha(p) for p in out.glob('*.log')})
    (out/'result.json').write_text(json.dumps(report,sort_keys=True,indent=2)+'\n')
    print('PASS two-clock Nano menu:',out)

if __name__=='__main__':main()
