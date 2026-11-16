"""Exact selected sources gain bounded call receipts; original operations remain."""
import hashlib
import json
from pathlib import Path

HERE=Path(__file__).resolve().parent
PSCI='lib/psci/psci_main.c'
PSCI_SHA='6fb15f8a4f45a92401845437121e7798502c16f21d74cf64213b57884c2af56e'
FWCALL='arch/arm/cpu/armv8/fwcall.c'
FWCALL_SHA='cff4269b09f794b4ea4d486c32c4d5f23da2d87c1a7e114ea51177b5fe029408'
INCLUDE='plat/intel/soc/common/include'
C_PHASES=('affinity-enter','cache-before','cache-after','affinity-return')


def uart_assembly(label,message):
    """Only x8..x14/NZCV; no stack/call/data writes, fixed UART stores only."""
    prefix='.Lfes_call_'+label
    return f'''    adr x8, {prefix}_text
    mov w13, #128
{prefix}_next:
    cbz w13, {prefix}_park
    ldrb w9, [x8], #1
    sub w13, w13, #1
    cbz w9, {prefix}_drain
    mov x10, #0x2114
    movk x10, #0x10c0, lsl #16
    mov w11, #0x86a0
    movk w11, #1, lsl #16
{prefix}_wait:
    ldr w12, [x10]
    cmn w12, #1
    b.eq {prefix}_park
    tbnz w12, #5, {prefix}_write
    subs w11, w11, #1
    b.ne {prefix}_wait
    b {prefix}_park
{prefix}_write:
    sub x10, x10, #20
    str w9, [x10]
    dsb sy
    b {prefix}_next
{prefix}_drain:
    mov x10, #0x2114
    movk x10, #0x10c0, lsl #16
    mov w11, #0x86a0
    movk w11, #1, lsl #16
{prefix}_drain_wait:
    ldr w12, [x10]
    cmn w12, #1
    b.eq {prefix}_park
    tbnz w12, #6, {prefix}_done
    subs w11, w11, #1
    b.ne {prefix}_drain_wait
{prefix}_park:
    wfe
    b {prefix}_park
{prefix}_done:
    b {prefix}_end
    .balign 4
{prefix}_text:
    .asciz "{message}\\r\\n"
    .balign 4
{prefix}_end:
'''


def fwcall_source(text):
    if hashlib.sha256(text.encode()).hexdigest()!=FWCALL_SHA:
        raise ValueError('unreviewed selected U-Boot SMC implementation')
    start=text.index('void smc_call(struct pt_regs *args)');end=text.index('\n}\n',start)+3
    original=text[start:end]
    needle='\t\t"smc\t#0\\n"\n'
    if original.count(needle)!=1:raise ValueError('selected SMC instruction changed')
    before='''    mov x19, xzr
    mov w16, #4
    movk w16, #0xc400, lsl #16
    cmp x0, x16
    b.ne .Lfes_call_skip_before_%=
    cbnz x2, .Lfes_call_skip_before_%=
    mov x16, #0x300
    bic x16, x1, x16
    cbnz x16, .Lfes_call_skip_before_%=
    mov x19, #1
'''+uart_assembly('issue_%=','FES_NANO_CALL phase=smc-before')+'''.Lfes_call_skip_before_%=:
'''
    after='''    cbz x19, .Lfes_call_skip_after_%=
'''+uart_assembly('return_%=','FES_NANO_CALL phase=smc-after')+'''.Lfes_call_skip_after_%=:
'''
    quoted=lambda asm:''.join('\t\t'+json.dumps(line+'\n')+'\n' for line in asm.splitlines())
    inserted=quoted(before)+needle+quoted(after)
    body=original.replace(needle,inserted)
    clobber='\t\t  "x16", "x17");'
    if body.count(clobber)!=1:raise ValueError('SMC clobber context changed')
    body=body.replace(clobber,'\t\t  "x16", "x17", "x19", "cc", "memory");')
    inverse=body.replace(inserted,needle).replace('"x16", "x17", "x19", "cc", "memory");','"x16", "x17");')
    if inverse!=original:raise ValueError('SMC instrumentation changed original operations')
    return text[:start]+body+text[end:]


def psci_source(text):
    if hashlib.sha256(text.encode()).hexdigest()!=PSCI_SHA:
        raise ValueError('unreviewed selected PSCI affinity source')
    start=text.index('int psci_affinity_info(');end=text.index('\n}\n',start)+3
    original=text[start:end];body=original
    declaration='\tunsigned int target_idx;\n'
    added=declaration+'\tint fes_result;\n\tint fes_trace = lowest_affinity_level == 0U && (target_affinity & ~0x300UL) == 0U;\n\tif (fes_trace) fes_nano_ram_call_trace(0);\n'
    flush='\tflush_cpu_data_by_index(target_idx,\n\t\t\t\tpsci_svc_cpu_data.aff_info_state);\n'
    returned='\treturn psci_get_aff_info_state_by_idx(target_idx);'
    final='\tfes_result = psci_get_aff_info_state_by_idx(target_idx);\n\tif (fes_trace) fes_nano_ram_call_trace(3);\n\treturn fes_result;'
    if any(body.count(a)!=1 for a in (declaration,flush,returned)):
        raise ValueError('PSCI affinity call context changed')
    body=body.replace(declaration,added).replace(flush,'\tif (fes_trace) fes_nano_ram_call_trace(1);\n'+flush+'\tif (fes_trace) fes_nano_ram_call_trace(2);\n').replace(returned,final)
    inverse=body.replace(added,declaration).replace('\tif (fes_trace) fes_nano_ram_call_trace(1);\n','').replace('\tif (fes_trace) fes_nano_ram_call_trace(2);\n','').replace(final,returned)
    if inverse!=original:raise ValueError('PSCI instrumentation changed original operations')
    return '#include "nano_firmware_call.h"\n'+text[:start]+body+text[end:]


def header():
    return '/* Private diagnostic leaf: x8..x14/NZCV caller-saved only. */\nvoid fes_nano_ram_call_trace(unsigned int phase);\n'


def assembly():
    lines=['.pushsection .text.fes_ram_call,"ax"','.balign 4',
           '.global fes_nano_ram_call_trace','.type fes_nano_ram_call_trace, %function',
           'fes_nano_ram_call_trace:']
    for index in range(4):lines.extend((f'    cmp w0, #{index}',f'    b.eq .Lfes_call_phase_{index}'))
    lines.extend(('.Lfes_call_bad:', '    wfe','    b .Lfes_call_bad'))
    for index,phase in enumerate(C_PHASES):
        lines.extend((f'.Lfes_call_phase_{index}:',uart_assembly(str(index),'FES_NANO_CALL phase='+phase),'    ret'))
    lines.extend(('.size fes_nano_ram_call_trace, .-fes_nano_ram_call_trace','.popsection'))
    return '\n'.join(lines)+'\n'


def install(source):
    psci=source/PSCI;entry=source/'bl31/aarch64/bl31_entrypoint.S'
    text=psci_source(psci.read_text());body=entry.read_text()+'\n'+assembly()
    psci.write_text(text);entry.write_text(body)
    (source/INCLUDE/'nano_firmware_call.h').write_text(header())
    return [PSCI,'bl31/aarch64/bl31_entrypoint.S',INCLUDE+'/nano_firmware_call.h']
