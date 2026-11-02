#!/usr/bin/env python3
"""Check the actual DDR reader RTL against an injected external AXI bus."""
import argparse,json,os,subprocess,sys,uuid
from pathlib import Path
from verify import HERE,ROOT,PRODUCER,inputs_for,run,sha
import reader_memory


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--project',type=Path)
    parser.add_argument('--output',type=Path,default=ROOT/'out'/('de25-ddr-reader-'+uuid.uuid4().hex))
    args=parser.parse_args()
    try:
        output=args.output.absolute()
        for p in (output,*([args.project.absolute()] if args.project else [])):
            if '..' in p.parts or any(q.is_symlink() for q in (p,*p.parents)) or not p.is_relative_to(ROOT/'out'):
                raise ValueError('DDR model paths must be private worktree out/ paths')
        if output.exists():raise ValueError('DDR model output must be fresh')
        if args.project:
            inputs,_,local=inputs_for(args.project.absolute())
            memory=reader_memory.checked(inputs['reader_memory'])
            if memory['backend']!='hps-ddr':raise ValueError('project does not select the DDR reader')
            build_id=inputs['embedded_control']['build_id']
        else:
            local=None;build_id='00112233445566778899aabbccddeeff'
            memory=reader_memory.selection('hps-ddr',0x1000,0x2000)
        output.mkdir(parents=True)
        captured=output/'source';(captured/'generated').mkdir(parents=True)
        selected={'fes_de25_embedded.sv':local/'fes_de25_embedded.sv' if local else HERE/'fes_de25_embedded.sv',
                  'fes_embedded_control.sv':local/'fes_embedded_control.sv' if local else ROOT/'sources/misteross/cores/fes-common/rtl/fes_embedded_control.sv',
                  'generated/fes_embedded_control.vh':local/'generated/fes_embedded_control.vh' if local else ROOT/'sources/misteross/cores/fes-common/generated/fes_embedded_control.vh',
                  'ddr_probe.cpp':HERE/'ddr_probe.cpp'}
        hashes={}
        for name,p in selected.items():
            (captured/name).write_bytes(p.read_bytes());hashes[name]=sha(p)
        tools,paths=PRODUCER.tools_snapshot();env=PRODUCER.build_environment(paths)
        obj=output/'obj'
        command=[paths['verilator'],'--cc','--exe','--build','--top-module','fes_de25_embedded',
          '-Wall','-Wno-UNUSEDSIGNAL','--Mdir',obj,'-I'+str(captured/'generated'),
          "-DFES_DE25_COMPUTER_BUILD_ID=128'h"+build_id,'-GMEMORY_BACKEND=1',
          '-GDDR_BASE='+str(memory['bus_base']),'-GDDR_BYTES='+str(memory['bytes']),
          '-CFLAGS','-std=c++17 -O2',captured/'fes_embedded_control.sv',captured/'fes_de25_embedded.sv',captured/'ddr_probe.cpp']
        run('build',command,ROOT,env,output)
        executable=obj/'Vfes_de25_embedded';abi=reader_memory.control_abi(memory)
        argv=[executable,build_id,str(abi),str(memory['bus_base']),str(memory['bytes'])]
        observed=json.loads(run('model',argv,ROOT,env,output))
        if (observed.get('status')!='passed' or observed.get('backend')!='hps-ddr' or
            observed.get('build_id')!=build_id or observed.get('control_abi')!=abi or
            observed.get('bus_base')!=memory['bus_base'] or observed.get('bytes')!=memory['bytes'] or
            len(observed.get('cases',[]))!=23 or observed.get('physical_ddr') is not False or
            observed.get('runtime_admission') is not False):
            raise ValueError('DDR reader model observations differ')
        wrong=('0' if build_id[0]!='0' else '1')+build_id[1:]
        run('wrong-id',[executable,wrong,*argv[2:]],ROOT,env,output,expect=1,marker='DDR live build identity mismatch')
        run('wrong-abi',[executable,build_id,str(abi^1),*argv[3:]],ROOT,env,output,expect=1,marker='DDR control ABI mismatch')
        run('wrong-window',[*argv[:3],str(memory['bus_base']^0x1000),argv[4]],ROOT,env,output,expect=1,marker='compiled DDR window differs')
        record={'status':'passed','classification':'external AXI model; no physical DDR or bridge qualification',
          'source_sha256':hashes,'tools':tools,'executable_sha256':sha(executable),
          'reader_memory':memory,'observed':observed,'negative_identity_cases':3,
          'hardware_access':False,'bridge_release':False,'runtime_admission':False}
        (output/'result.json').write_text(json.dumps(record,indent=2)+'\n')
        print('PASS23 DDR reader cases and3 identity/window rejection cases:',output)
        return 0
    except (OSError,ValueError,KeyError,subprocess.SubprocessError) as error:
        print('DDR reader check:',error,file=sys.stderr);return 1


if __name__=='__main__':sys.exit(main())
