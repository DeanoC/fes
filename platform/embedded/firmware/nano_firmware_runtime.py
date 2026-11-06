"""Source-pinned, insertion-only diagnostics in the selected cold BL31 path."""
import hashlib
import importlib.util
import re
from pathlib import Path
from verify import function

HERE = Path(__file__).resolve().parent
_spec = importlib.util.spec_from_file_location('fes_runtime_startup', HERE.parent/'nano/runtime_startup.py')
startup = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(startup)
ENTRY = 'bl31/aarch64/bl31_entrypoint.S'
MAIN = 'bl31/bl31_main.c'
MAIN_SHA = 'ef7585cdfc725ea3f668fe162d1434f0afa92f2260db3f123c37cc2f56c2d30b'
PLATFORM = 'plat/intel/soc/agilex5/bl31_plat_setup.c'
# Exact existing FES owned/cold transform of the pinned Terasic platform.
PLATFORM_SHA = 'aa66cc9cd230b4be5a7b28444ed83a802db03a282399b5a5131069ba03dd86e6'
INCLUDE = 'plat/intel/soc/common/include'


def symbol(phase):
    return 'FES_RAM_RT_'+phase.replace('-','_').upper()


def header():
    return ('/* Private selected cold diagnostics; no nonsecure API. */\n' +
            ''.join('#define '+symbol(phase)+' '+str(i)+'\n'
                    for i,phase in enumerate(startup.RUNTIME_PHASES)) +
            '#define FES_RAM_RT_COUNT '+str(len(startup.RUNTIME_PHASES))+'\n'
            '#ifndef __ASSEMBLER__\nvoid fes_nano_ram_runtime_trace(unsigned int phase);\n#endif\n')


def assembly_data():
    return ('.pushsection .rodata.fes_ram_runtime,"a"\n.balign 8\n'
            'fes_nano_ram_runtime_table:\n'+
            ''.join('.quad fes_nano_ram_runtime_'+p.replace('-','_')+'\n' for p in startup.RUNTIME_PHASES)+
            ''.join('fes_nano_ram_runtime_'+p.replace('-','_')+
                    ': .asciz "FES_NANO_RAM RUNTIME phase='+p+'\\r\\n"\n'
                    for p in startup.RUNTIME_PHASES)+'.popsection\n')


def c_mark(phase):
    return '\tfes_nano_ram_runtime_trace('+symbol(phase)+');\n'


def c_source(text, *, platform):
    expected = PLATFORM_SHA if platform else MAIN_SHA
    if hashlib.sha256(text.encode()).hexdigest() != expected:
        raise ValueError('unreviewed cold runtime '+('platform' if platform else 'main')+' source')
    name = 'bl31_platform_setup' if platform else 'bl31_main'
    original = function(text,name)
    body = original
    calls = (('fes_nano_bridge_platform_setup();','owned'),
             ('gicv3_driver_init(&plat_gicv3_gic_data);','gic-driver'),
             ('gicv3_distif_init();','gic-distributor'),
             ('gicv3_rdistif_init(plat_my_core_pos());','gic-redistributor'),
             ('gicv3_cpuif_enable(plat_my_core_pos());','gic-cpu')) if platform else (
             ('bl31_platform_setup();','platform'),('bl31_lib_init();','libraries'),
             ('runtime_svc_init();','services'),('bl31_prepare_next_image_entry();','next-image'),
             ('bl31_plat_runtime_setup();','runtime'))
    for call,phase in calls:
        anchor='\t'+call+'\n'
        if body.count(anchor)!=1:raise ValueError('cold runtime call context changed: '+phase)
        body=body.replace(anchor,c_mark(phase+'-before')+anchor+c_mark(phase+'-after'))
    if platform:
        # Bracket both the optional V3 init and the unconditional notification.
        before='#if SIP_SVC_V3\n'
        after='\tmailbox_hps_stage_notify(HPS_EXECUTION_STATE_SSBL);\n'
    else:
        before='\tconsole_flush();\n\tconsole_switch_state(CONSOLE_FLAG_RUNTIME);\n'
        after=before
    if body.count(before)!=1 or body.count(after)!=1:
        raise ValueError('cold runtime final-call context changed')
    phase='mailbox' if platform else 'console'
    if platform:
        body=body.replace(before,c_mark(phase+'-before')+before)
        body=body.replace(after,after+c_mark(phase+'-after'))
    else:
        body=body.replace(before,c_mark(phase+'-before')+before+c_mark(phase+'-after'))
    result='#include "nano_firmware_runtime.h"\n'+text.replace(original,body)
    # Only inserted complete diagnostic statements/header may disappear.
    inverse=result.removeprefix('#include "nano_firmware_runtime.h"\n')
    inverse=re.sub(r'^\tfes_nano_ram_runtime_trace\(FES_RAM_RT_[A-Z_]+\);\n','',inverse,flags=re.M)
    if inverse!=text:raise ValueError('cold runtime transformation changed original source')
    return result


def entry_source(text):
    before,end,warm=text.partition('endfunc bl31_entrypoint\n')
    if not end:raise ValueError('cold runtime entry boundary changed')
    original=before
    def mark(phase):return '\tfes_ram_uart_text fes_nano_ram_runtime_'+phase.replace('-','_')+'\n'
    call='\tbl\tbl31_main\n'
    if before.count(call)!=1:raise ValueError('cold runtime main entry changed')
    before=before.replace(call,mark('main-before')+call+mark('main-after'))
    call='\tbl\tclean_dcache_range\n'
    parts=before.split(call)
    if len(parts)!=3:raise ValueError('cold runtime exit cache context changed')
    before=parts[0]+mark('data-clean-before')+call+mark('data-clean-after')+parts[1]+mark('bss-clean-before')+call+mark('bss-clean-after')+parts[2]
    exit='\tb\tel3_exit\n'
    if before.count(exit)!=1:raise ValueError('cold runtime exit branch changed')
    before=before.replace(exit,mark('exit-before')+exit)
    if re.sub(r'^\tfes_ram_uart_text fes_nano_ram_runtime_[a-z_]+\n','',before,flags=re.M)!=original:
        raise ValueError('cold runtime entry transformation changed original operations')
    return ('#include "nano_firmware_runtime.h"\n'+before+end+warm+
            '\n#include "nano_firmware_runtime.inc"\n'+assembly_data())


def install(source):
    names=(MAIN,PLATFORM,ENTRY)
    # Validate/generate every transform before replacing any existing input.
    texts={MAIN:c_source((source/MAIN).read_text(),platform=False),
           PLATFORM:c_source((source/PLATFORM).read_text(),platform=True),
           ENTRY:entry_source((source/ENTRY).read_text())}
    for name,text in texts.items():(source/name).write_text(text)
    (source/INCLUDE/'nano_firmware_runtime.h').write_text(header())
    (source/INCLUDE/'nano_firmware_runtime.inc').write_bytes((HERE/'nano_firmware_runtime.inc').read_bytes())
    return list(names)+[INCLUDE+'/nano_firmware_runtime.h',INCLUDE+'/nano_firmware_runtime.inc']
