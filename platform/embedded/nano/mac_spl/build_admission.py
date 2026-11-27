#!/usr/bin/env python3
"""Build the CBL after-DDR inventory SPL from the qualified initial power trace."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import subprocess
import sys

from build_positive import HERE, TRACE, digest, parking_inputs, replace_once


def instrument(source, trace, base, parking):
    soc = source / 'arch/arm/mach-socfpga'
    board = soc / 'spl_agilex5.c'
    original = trace.function(board.read_text(), 'board_init_f')
    start = original.index('\tret = uclass_get_device_by_name(UCLASS_NOP, "socfpga-secreg"')
    stop = original.index('#if CONFIG_IS_ENABLED(PHY_CADENCE_COMBOPHY)', start)
    ddr = original[start:stop]
    if ddr.count('uclass_get_device(UCLASS_RAM') != 1:
        raise ValueError('missing matched pre-storage DDR probe')
    hashes = base(source, False)
    for name, contents in parking.items():
        if (soc / name).read_bytes() != contents:
            raise ValueError('matched v2 CPU policy differs: '+name)
    cold = soc / 'nano_cold.c'
    with cold.open('a') as f:
        f.write('''
int fes_positive_cpu_scope(struct fes_nano_cold *s, struct fes_nano_cold_sample *now)
{
    return s && s->attempted && !s->failed && s->record.core == 0 &&
           !sample(s, now) && stable(&s->record.first, now, 0);
}
''')
    report = soc / 'fes_nano_spl.c'
    text = report.read_text()
    old = trace.function(text, 'fes_nano_spl_report')
    new = replace_once(old,'    fes_nano_cpu_diag_park();','    if (source.failed) fes_nano_cpu_diag_park();')
    observer = soc / 'fes_admission.inc'
    observer.write_bytes((HERE / 'admission.inc').read_bytes())
    report.write_text(replace_once(text,old,new)+'\n#include "fes_admission.inc"\n')
    edge = '\tfes_nano_spl_report();\n\thang(); /* Defensive stop if a diagnostic ABI ever returns. */'
    board.write_text('extern void fes_nano_post_ddr_admission(void);\n'+replace_once(board.read_text(),edge,
        '\tfes_nano_spl_report();\n'+ddr+'\tfes_nano_post_ddr_admission();\n\thang();'))
    clock = soc/'clock_manager_agilex5.c'
    with clock.open('a') as f:
        f.write('''
void fes_nano_admission_rates(unsigned *l4sp, unsigned *emac)
{
    *l4sp = cm_get_rate_dm(AGILEX5_L4_SP_CLK);
    *emac = cm_get_rate_dm(AGILEX5_EMAC0_CLK);
}
''')
    # Keep the stock CONFIG_STATUS decoding and retain all six words returned
    # by that same query. A short reply cannot supply completion evidence.
    mailbox = soc/'mailbox_s10.c'
    text = mailbox.read_text()
    extraction = replace_once(text,'static __always_inline int mbox_get_fpga_config_status_common(',
                                'static int mbox_get_fpga_config_status_common(')
    query = trace.function(extraction,'mbox_get_fpga_config_status_common')
    query = replace_once(query,'static int mbox_get_fpga_config_status_common(u32 cmd)',
                         'int fes_nano_admission_sdm_status(u32 raw[6], u32 *words, u32 *mailbox)')
    query = replace_once(query,'\tint ret;', '\tu32 cmd = MBOX_CONFIG_STATUS;\n\tint ret;')
    query = replace_once(query,'\tif (ret)\n\t\treturn ret;', '''	*mailbox = ret;
	*words = ret ? 0 : reconfig_status_resp_len;
	if (ret) return ret;
	if (*words > 6) return -22;
	for (u32 i = 0; i < *words; ++i) raw[i] = reconfig_status_resp[i];
	if (*words != 6) return -22;''')
    mailbox.write_text(text+'\n'+query)
    for path in (cold, report, observer, board, clock, mailbox):
        hashes[str(path.relative_to(source))] = digest(path)
    return hashes


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--trace-checkout',type=Path,required=True)
    args,rest=p.parse_known_args()
    root=args.trace_checkout.resolve(strict=True)
    if (subprocess.check_output(['git','-C',root,'rev-parse','HEAD'],text=True).strip()!=TRACE or
            subprocess.check_output(['git','-C',root,'status','--porcelain','--untracked-files=no']) or
            '--console-control' in rest):
        raise ValueError('requires the full clean, matched v2 first-C trace')
    producer=root/'platform/embedded/nano/cpu_diagnostic.py'
    sys.path.insert(0,str(producer.parent))
    spec=importlib.util.spec_from_file_location('cbl_v2_ddr_trace',producer)
    trace=importlib.util.module_from_spec(spec)
    spec.loader.exec_module(trace)
    trace.PARK='FES_CBL_MAC_ADMIT PARK no-storage no-bl31'
    base=trace.contain_source
    parking=parking_inputs(root)
    trace.contain_source=lambda source,_:instrument(source,trace,base,parking)
    output=Path(rest[rest.index('--output')+1]).resolve()
    sys.argv=[str(producer),*rest]
    trace.main()
    asm=(output/'spl-disassembly.log').read_text()
    symbols=(output/'spl-symbols.log').read_text()
    for symbol in ('fes_nano_post_ddr_admission','fes_nano_cpu_diag_park','fes_nano_admission_sdm_status'):
        if not re.search(r'^\w+ <'+symbol+r'>:',asm,re.M):
            raise ValueError('missing admission symbol '+symbol)
    forbidden=('fes_nano_positive_tx','fes_tx_once','fes_tx_terminal','spl_invoke_atf','fes_nano_spl_handoff')
    if any(re.search(r'\b'+symbol+r'$',symbols,re.M) for symbol in forbidden):
        raise ValueError('DMA or handoff path linked into admission SPL')
    original=output/'result.json'
    receipt=json.loads(original.read_bytes())
    receipt.update(classification='Standalone CBL after-DDR read-only MAC eligibility inventory',diagnostic_mode='mac-admission')
    original.write_text(json.dumps(receipt,indent=2,sort_keys=True)+'\n')
    sys.path.insert(0,str(HERE.parent))
    from sof_hps import inspect
    sof=output/'cpu-state.sof'
    covered=inspect(sof.read_bytes(),(output/'build/spl/u-boot-spl-dtb.hex').read_bytes(),
                    Path(rest[rest.index('--vendor-sof')+1]).read_bytes())
    result=dict(scope='de25-cbl-post-ddr-admission-v1',status='prepared',historical_trace=TRACE,parking_revision=TRACE,
                diagnostic_id=receipt['diagnostic_id'],sof_sha256=digest(sof),spl_sha256=receipt['spl_sha256'],
                embedded_spl=covered,build_receipt_sha256=digest(original),excluded_symbols=list(forbidden),
                parking_sha256={n:hashlib.sha256(v).hexdigest() for n,v in parking.items()},
                source_sha256={f.name:digest(f) for f in (Path(__file__),HERE/'build_positive.py',HERE/'admission.inc',HERE.parent/'sof_hps.py')})
    (output/'mac-admission.json').write_text(json.dumps(result,indent=2,sort_keys=True)+'\n')


if __name__=='__main__':
    main()
