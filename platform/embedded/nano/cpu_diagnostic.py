#!/usr/bin/env python3
"""Build a volatile Nano CPU-state SPL that cannot access storage or enter BL31."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import tarfile
import time

HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[2]
sys.path.insert(0,str(HERE.parent/'firmware'))
sys.path.insert(0,str(HERE))
from verify import function
import prepare_uboot

PIN='d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413'
REFERENCE_SOF='e71a72180cb77ddd05a9c2ae49afa739d74786f9366f00233fd27bbebff6ea09'
PARK='FES_NANO_CPU_DIAG PARK no-ddr no-storage no-bl31'
DISABLE=('SPL_MMC','SPL_FS_FAT','SPL_FS_EXT4',
         'SPL_RAM_DEVICE','SPL_WDT','SPL_ALTERA_SDRAM')
STARTUP=('arch/arm/cpu/armv8/start.S','arch/arm/mach-socfpga/lowlevel_init_agilex5.S',
         'arch/arm/lib/crt0_64.S','arch/arm/include/asm/macro.h','arch/arm/cpu/armv8/u-boot-spl.lds')
STARTUP_OBJECTS=('arch/arm/cpu/armv8/start.o','arch/arm/mach-socfpga/lowlevel_init_agilex5.o',
                 'arch/arm/lib/crt0_64.o')
TRACE_STAGES={format(i,'02x'):name for i,name in enumerate((
    'c-entry','sample-done','dm-begin','dm-done','managers-done','pinmux-done',
    'timer-begin','timer-done','mailbox-begin','mailbox-done','notify-begin','notify-done',
    'clock-begin','clock-done','console-begin','console-done','report-begin','park'),1)}
TRACE_STAGES.update(e0='dm-error',e1='clock-error')

def sha(path):
    with Path(path).open('rb') as stream:return hashlib.file_digest(stream,'sha256').hexdigest()

def decode_trace(data,identity):
    """Retain partial checkpoints without treating them as CPU/boot admission."""
    text=data.decode(errors='replace') if isinstance(data,bytes) else data
    records=re.findall(r'(?m)^FES_NANO_CPU_TRACE id=([0-9a-f]{16}) stage=([0-9a-f]{2}) value=([0-9a-f]{8}) mpidr=([0-9a-f]{16}) el=([0-3])\r?$',text)
    rows=[{'stage':stage,'name':TRACE_STAGES.get(stage,'unknown'),'value':int(value,16),
           'mpidr':mpidr,'el':int(el)} for seen,stage,value,mpidr,el in records if seen==identity]
    stages=[int(row['stage'],16) for row in rows]
    expected=list(range(1,19))
    prefix=stages==expected[:len(stages)] and len(stages)<=18
    error_prefix=(len(stages)>1 and stages[-1] in (0xe0,0xe1) and
                  stages[:-1]==expected[:len(stages)-1] and stages[-2]==(3 if stages[-1]==0xe0 else 13))
    mismatch=any(seen!=identity for seen in re.findall(r'(?m)^FES_NANO_CPU_TRACE id=([0-9a-f]{16})',text))
    same_scope=len({(row['mpidr'],row['el']) for row in rows})<=1
    return {'records':rows,'last_checkpoint':rows[-1] if rows else None,'identity_mismatch':mismatch,
            'sequence_valid':bool(rows) and (prefix or error_prefix) and not mismatch and same_scope,
            'complete':stages==expected and not mismatch and same_scope,'native_admission':False}

def contain_source(source):
    """Keep observations unchanged, park for either admission result, remove later init."""
    report=source/'arch/arm/mach-socfpga/fes_nano_spl.c'
    text=report.read_text();anchor='    if (source.failed) hang();'
    if text.count(anchor)!=1:raise ValueError('CPU report stop context changed')
    park='''__attribute__((__noreturn__,__noinline__)) static void fes_nano_cpu_diag_park(void)
{
    fes_nano_cpu_trace(FES_CPU_PARK,0);
    puts("'''+PARK+'''\\n");
    for (;;) asm volatile("yield" ::: "memory");
}
'''
    text=text.replace('void fes_nano_spl_report(void)',park+'void fes_nano_spl_report(void)')
    report.write_text('#include "cpu_trace.h"\n'+text.replace(anchor,'    fes_nano_cpu_diag_park();'))
    board=source/'arch/arm/mach-socfpga/spl_agilex5.c';text=board.read_text()
    old=function(text,'board_init_f');anchor='\tfes_nano_spl_report();'
    if old.count(anchor)!=1 or old.index(anchor)>old.index('uclass_get_device(UCLASS_RAM'):
        raise ValueError('CPU report is not before pinned DDR initialization')
    new=old[:old.index(anchor)+len(anchor)]+'\n\thang(); /* Defensive stop if a diagnostic ABI ever returns. */\n}\n'
    checkpoints=(
        ('\tfes_nano_spl_early();','\tfes_nano_cpu_trace(FES_CPU_C_ENTRY,0);\n\tfes_nano_spl_early();\n\tfes_nano_cpu_trace(FES_CPU_SAMPLE_DONE,0);'),
        ('\tret = spl_early_init();','\tfes_nano_cpu_trace(FES_CPU_DM_BEGIN,0);\n\tret = spl_early_init();\n\tfes_nano_cpu_trace(ret ? FES_CPU_DM_ERROR : FES_CPU_DM_DONE,(unsigned)ret);'),
        ('\tsocfpga_get_managers_addr();','\tsocfpga_get_managers_addr();\n\tfes_nano_cpu_trace(FES_CPU_MANAGERS_DONE,0);'),
        ('\tsysmgr_pinmux_init();','\tsysmgr_pinmux_init();\n\tfes_nano_cpu_trace(FES_CPU_PINMUX_DONE,0);'),
        ('\ttimer_init();','\tfes_nano_cpu_trace(FES_CPU_TIMER_BEGIN,0);\n\ttimer_init();\n\tfes_nano_cpu_trace(FES_CPU_TIMER_DONE,0);'),
        ('\tmbox_init();','\tfes_nano_cpu_trace(FES_CPU_MAILBOX_BEGIN,0);\n\tmbox_init();\n\tfes_nano_cpu_trace(FES_CPU_MAILBOX_DONE,0);'),
        ('\tmbox_hps_stage_notify(HPS_EXECUTION_STATE_FSBL);','\tfes_nano_cpu_trace(FES_CPU_NOTIFY_BEGIN,0);\n\tmbox_hps_stage_notify(HPS_EXECUTION_STATE_FSBL);\n\tfes_nano_cpu_trace(FES_CPU_NOTIFY_DONE,0);'),
        ('\tret = uclass_get_device(UCLASS_CLK, 0, &dev);','\tfes_nano_cpu_trace(FES_CPU_CLOCK_BEGIN,0);\n\tret = uclass_get_device(UCLASS_CLK, 0, &dev);\n\tfes_nano_cpu_trace(ret ? FES_CPU_CLOCK_ERROR : FES_CPU_CLOCK_DONE,(unsigned)ret);\n\tif (!ret) fes_nano_cpu_trace_serial();'),
        ('\tpreloader_console_init();','\tfes_nano_cpu_trace(FES_CPU_CONSOLE_BEGIN,0);\n\tpreloader_console_init();\n\tfes_nano_cpu_trace(FES_CPU_CONSOLE_DONE,0);\n\tfes_nano_cpu_trace_serial();'),
        (anchor,'\tfes_nano_cpu_trace(FES_CPU_REPORT_BEGIN,0);\n'+anchor))
    for before,after in checkpoints:
        if new.count(before)!=1:raise ValueError('CPU checkpoint context changed: '+before.strip())
        new=new.replace(before,after)
    board.write_text('#include "cpu_trace.h"\n'+text.replace(old,new))
    # Keep SPL_ATF for the seed's configuration/startup selection, but remove
    # its later invocation independently. Native entry must remain impossible.
    late=source/'common/spl/spl.c';text=late.read_text();anchor='\t\tjump_to_image = &spl_invoke_atf;'
    if text.count(anchor)!=1:raise ValueError('pinned late ATF invocation changed')
    late.write_text(text.replace(anchor,'\t\thang(); /* CPU diagnostic never invokes firmware. */'))
    soc=report.parent
    trace=[soc/name for name in ('cpu_trace.c','cpu_trace.h')]
    for path in trace:shutil.copyfile(HERE/path.name,path)
    mk=soc/'Makefile';mk.write_text(mk.read_text()+'\nifdef CONFIG_SPL_BUILD\nobj-y += cpu_trace.o\nendif\n')
    # The pinned DT rule otherwise links U-Boot proper even for an SPL-only
    # target. CPU observation needs the static board DT, never BL33 or a FIT.
    makefile=source/'Makefile';text=makefile.read_text()
    anchor='dts/dt.dtb: dtbs_prepare u-boot\n'
    if text.count(anchor)!=1:raise ValueError('pinned SPL-only DT dependency changed')
    makefile.write_text(text.replace(anchor,'dts/dt.dtb: prepare scripts\n'))
    return {str(p.relative_to(source)):sha(p) for p in (report,board,late,mk,makefile,*trace)}

def main():
    p=argparse.ArgumentParser(description=__doc__)
    for name in ('reference-repo','seed-build','entry-build','vendor-sof','quartus-pfg','output'):
        p.add_argument('--'+name,required=True,type=Path)
    p.add_argument('--cross-prefix',required=True);args=p.parse_args()
    output=args.output.absolute()
    if (output.exists() or '..' in output.parts or not output.is_relative_to(ROOT/'out') or
            any(v.is_symlink() for v in (output,*output.parents))):p.error('output must be fresh/private under out/')
    repo=args.reference_repo.resolve(strict=True);seed=args.seed_build.resolve(strict=True)
    vendor=args.vendor_sof.resolve(strict=True);pfg=args.quartus_pfg.resolve(strict=True)
    if shutil.disk_usage(ROOT).free<30*1024**3:raise ValueError('less than 30 GiB free')
    git=lambda *a:subprocess.check_output(['git','-C',repo,*a])
    if git('rev-parse','HEAD').decode().strip()!=PIN or git('status','--porcelain','--untracked-files=all'):
        raise ValueError('requires clean pinned Terasic U-Boot')
    r=json.loads((seed/'result.json').read_text());native=r.get('native_handoff',{})
    if (r.get('status')!='built' or r.get('uboot_revision')!=PIN or r.get('bl31_bridge_mode')!='owned-cold-serial' or
            sha(seed/'build/.config')!=r.get('config_sha256') or sha(vendor)!=REFERENCE_SOF):
        raise ValueError('seed configuration/reference identity differs')
    for name,digest in native['producer_sources'].items():
        if sha(ROOT/name)!=digest:raise ValueError('seed producer source changed: '+name)
    env=os.environ.copy();env.update(r['environment'])
    for name in ('gcc','ld','as','ar','objcopy','objdump','readelf','nm'):
        if str(Path(args.cross_prefix+name).resolve())!=r['cross_tools'][name]['path']:
            raise ValueError('cross tool differs from seed: '+name)
    for name,tool in r['cross_tools'].items():
        if sha(tool['path'])!=tool['sha256']:raise ValueError('tool bytes changed: '+name)
    inputs={str(ROOT/name):digest for name,digest in native['producer_sources'].items()}
    inputs.update({str(v):sha(v) for v in (Path(__file__),HERE/'cpu_trace.c',HERE/'cpu_trace.h',seed/'result.json',seed/'build/.config',vendor,pfg)})
    startup={name:sha(repo/name) for name in STARTUP}
    for name,digest in startup.items():
        if sha(seed/'source'/name)!=digest:raise ValueError('seed startup source differs: '+name)
    inputs.update({str(seed/'build/spl'/name):sha(seed/'build/spl'/name) for name in STARTUP_OBJECTS})
    inputs.update({tool['path']:tool['sha256'] for tool in r['cross_tools'].values()})
    output.mkdir(parents=True);source=output/'source';source.mkdir();build=output/'build';build.mkdir()
    with tarfile.open(fileobj=io.BytesIO(git('archive',PIN))) as archive:archive.extractall(source,filter='data')
    prepared=prepare_uboot.prepare(source,args.entry_build.resolve(strict=True),r['bl31_sha256'])
    if prepared['cold_boot_profile']!=native['cold_boot_profile']:raise ValueError('source chain differs from seed')
    transformed=contain_source(source)
    if any(sha(source/name)!=digest for name,digest in startup.items()):raise ValueError('vendor startup modified')
    identity=hashlib.sha256(json.dumps({'inputs':inputs,'transformed':transformed,'mode':'cpu-only-stop'},sort_keys=True).encode()).hexdigest()[:16]
    header=source/'arch/arm/mach-socfpga/fes_cpu_diag_config.h'
    header.write_text('#define FES_CPU_DIAG_ID "'+identity+'"\n')
    transformed[str(header.relative_to(source))]=sha(header)
    shutil.copyfile(seed/'build/.config',build/'.config');commands=[]
    def run(label,argv):
        argv=[str(v) for v in argv];commands.append(argv)
        with (output/(label+'.log')).open('wb') as log:
            child=subprocess.Popen(argv,env=env,cwd=ROOT,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
            deadline=time.monotonic()+120;beat=time.monotonic()+12
            while child.poll() is None:
                if time.monotonic()>deadline:
                    os.killpg(child.pid,signal.SIGTERM)
                    try:child.wait(timeout=5)
                    except subprocess.TimeoutExpired:os.killpg(child.pid,signal.SIGKILL);child.wait()
                    raise TimeoutError(label+' deadline')
                if time.monotonic()>beat:print('CPU diagnostic: '+label,flush=True);beat=time.monotonic()+12
                time.sleep(.1)
            if child.returncode:raise RuntimeError(label+' failed; inspect '+str(output/(label+'.log')))
    config=[source/'scripts/config','--file',build/'.config']
    for option in DISABLE:run('disable-'+option,[*config,'-d',option])
    run('localversion',[*config,'--set-str','LOCALVERSION','-fes-nano-cpu-'+identity])
    # Select only the physical board's DT while retaining the seed's exact
    # target selection; no unrelated board trees are needed for observation.
    make=['make','-C',source,'O='+str(build),'CROSS_COMPILE='+args.cross_prefix,
          'dtb-y=socfpga_agilex5_de25_nano.dtb']
    run('olddefconfig',[*make,'olddefconfig']);text=(build/'.config').read_text()
    if any('CONFIG_'+v+'=y\n' in text for v in DISABLE):raise ValueError('CPU-only exclusion reenabled')
    if 'CONFIG_SPL_ATF=y\n' not in text or 'CONFIG_SPL_ATF=y\n' not in (seed/'build/.config').read_text():
        raise ValueError('vendor SPL_ATF selection was not retained')
    if ('CONFIG_TARGET_SOCFPGA_AGILEX5_DE25_NANO=y\n' not in text or
            'CONFIG_DEFAULT_DEVICE_TREE="socfpga_agilex5_de25_nano"\n' not in text):
        raise ValueError('CPU diagnostic target is not the physical Nano')
    run('spl-build',[*make,'-j','4','spl/u-boot-spl-dtb.hex'])
    startup_objects={}
    for index,name in enumerate(STARTUP_OBJECTS):
        paths=[]
        for label,tree in (('seed',seed),('diagnostic',output)):
            path=output/(label+'-startup-'+str(index)+'.bin');paths.append(path)
            run(label+'-startup-'+str(index),[args.cross_prefix+'objcopy','-O','binary','-j','.text*',tree/'build/spl'/name,path])
        if not paths[0].stat().st_size or paths[0].read_bytes()!=paths[1].read_bytes():raise ValueError('emitted startup changed: '+name)
        startup_objects[name]={'text_sha256':sha(paths[0]),'seed_match':True}
    spl=build/'spl/u-boot-spl-dtb.hex';elf=build/'spl/u-boot-spl'
    run('spl-disassembly',[args.cross_prefix+'objdump','-d',elf])
    assembly=(output/'spl-disassembly.log').read_text()
    block=re.search(r'<fes_nano_cpu_diag_park>:(.*?)(?=\n[0-9a-f]+ <|\Z)',assembly,re.S)
    if not block or 'yield' not in block[1] or re.search(r'\b(?:ret|wfi|wfe)\b',block[1]):raise ValueError('target park is not a non-sleeping stop')
    run('spl-symbols',[args.cross_prefix+'nm','-S',elf]);symbols=(output/'spl-symbols.log').read_text()
    forbidden=('mmc_init','spl_load_image_fat','spl_invoke_atf','fes_nano_spl_handoff','sdram_mmr_init','initr_watchdog')
    if any(re.search(r'(?m)\b'+v+r'$',symbols) for v in forbidden):raise ValueError('excluded code linked into CPU SPL')
    trace=re.findall(r'(?m)^([0-9a-f]+) ([0-9a-f]+) [dD] fes_nano_cpu_trace_record$',symbols)
    if len(trace)!=1 or not 0x1000<=int(trace[0][0],16)<0x40000-int(trace[0][1],16):raise ValueError('trace not in loaded OCRAM data')
    for name in ('source','fes_nano_spl_record'):
        data=re.findall(r'(?m)^([0-9a-f]+) ([0-9a-f]+) [dD] '+name+'$',symbols)
        if len(data)!=1 or not 0x1000<=int(data[0][0],16)<0x40000-int(data[0][1],16):raise ValueError('cold state not in loaded OCRAM data')
    candidate=output/'cpu-state.sof'
    run('pfg-combine',[pfg,'-c',vendor,candidate,'-o','hps_path='+str(spl)])
    run('pfg-info',[pfg,'-i',candidate]);info=(output/'pfg-info.log').read_text(errors='replace')
    if 'Device name: A5EB013BB23BE4SCS' not in info or 'HPS/FPGA configuration order: HPS_AFTER_INIT_DONE' not in info:
        raise ValueError('unexpected device/configuration order')
    if (any(sha(Path(v))!=h for v,h in inputs.items()) or any(sha(source/v)!=h for v,h in transformed.items()) or
            any(sha(source/name)!=digest for name,digest in startup.items())):raise ValueError('diagnostic inputs changed')
    report={'status':'prepared','classification':'CPU-only volatile SPL observation; unconditionally parks before DDR/storage/BL31; no native admission',
            'diagnostic_id':identity,'source_revision':PIN,'source_base':subprocess.check_output(['git','-C',ROOT,'rev-parse','HEAD'],text=True).strip(),
            'reference_chain_only':native['cold_boot_profile']['chain_sha256'],'sof_sha256':sha(candidate),'spl_sha256':sha(spl),'spl_elf_sha256':sha(elf),
            'reference_sof_sha256':REFERENCE_SOF,'config_sha256':sha(build/'.config'),'inputs':inputs,'transformed_sources':transformed,
            'vendor_startup_sources':startup,'vendor_startup_objects':startup_objects,'spl_atf_selection_retained':True,
            'trace':{'ocram_address':int(trace[0][0],16),'bytes':int(trace[0][1],16),'capacity':32,'line_poll_limit':262144,
                     'serial_before_clocks':False,'uart_configuration_modified':False,'early_readback_over_jtag_qualified':False,
                     'stages':TRACE_STAGES},
            'excluded_symbols':list(forbidden),'disabled_options':list(DISABLE),'commands':commands,
            'limitations':['not a native boot chain or signed boot','no DDR/storage/EL3/Go/drain qualification','embedded SPL not independently decoded from SOF',
                          'pre-clock checkpoints are OCRAM only; JTAG trace readback is unqualified','bounded UART polls cannot preempt a stalled MMIO read']}
    (output/'result.json').write_text(json.dumps(report,indent=2,sort_keys=True)+'\n');print('Prepared CPU-only diagnostic:',output,flush=True)

if __name__=='__main__':
    try:main()
    except (OSError,ValueError,RuntimeError,subprocess.SubprocessError) as error:raise SystemExit('CPU diagnostic: '+str(error))
