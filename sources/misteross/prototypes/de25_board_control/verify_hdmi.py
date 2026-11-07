#!/usr/bin/env python3
"""Model actual board IRQ qualification and the captured I2C busy handshake."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess

import hdmi_control

HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[3]
WRAPPER='''module hdmi_control_model(input wire clk, reset, raw_irq, ack_low,
    output wire scl, sda, ready, irq_seen, master_sda_release, controller_clk,
    output wire [5:0] phase);
    tri1 bus;
    assign bus = ack_low ? 1'b0 : 1'bz;
    assign sda = bus;
    fes_de25_zx81_board dut(.CLOCK0_50(clk),.ninit_done(reset),.pixel_clk(),
        .video_rgb(24'd0),.video_de(1'b0),.video_hs(1'b0),.video_vs(1'b0),
        .HDMI_TX_INT(raw_irq),.HDMI_TX_CLK(),.HDMI_TX_DE(),.HDMI_TX_HS(),
        .HDMI_TX_VS(),.HDMI_TX_D(),.HDMI_I2C_SCL(scl),.HDMI_I2C_SDA(bus),
        .HDMI_MCLK(),.HDMI_SCLK(),.HDMI_LRCLK(),.HDMI_I2S());
    assign ready = dut.hdmi_config.READY;
    assign irq_seen = dut.hdmi_config.HDMI_TX_INT;
    assign master_sda_release = dut.hdmi_config.u0.SDO;
    assign controller_clk = dut.hdmi_config.mI2C_CTRL_CLK;
    assign phase = dut.hdmi_config.u0.SD_COUNTER;
endmodule
module sys_pll(input wire refclk,rst,output wire outclk_0,locked);
    assign outclk_0=refclk;
    assign locked=!rst;
endmodule
'''


def sha(p):return hashlib.sha256(Path(p).read_bytes()).hexdigest()


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--project',type=Path,required=True)
    parser.add_argument('--output',type=Path,required=True)
    parser.add_argument('--yosys',type=Path,default=os.environ.get('FES_YOSYS') or shutil.which('yosys'))
    args=parser.parse_args();out=args.output.absolute();project=args.project.resolve()
    if out.exists() or not out.is_relative_to(ROOT/'out') or any(p.is_symlink() for p in (out,*out.parents)):
        parser.error('fresh private output beneath out required')
    if shutil.disk_usage(ROOT).free<30*(1<<30):parser.error('30 GiB free required')
    if args.yosys is None:parser.error('Yosys required for vendor process synthesis')
    original=subprocess.check_output(['git','show','79d616e85e19d65f0d645b67a85ffe7e142dcdfc:'+hdmi_control.BOARD_SOURCE],cwd=ROOT)
    local=project/'fes_mailbox';expected=hdmi_control.board(original)
    if (local/'board.v').read_bytes()!=expected:parser.error('project IRQ board does not match reviewed derivation')
    original_config=local/'I2C_HDMI_Config.v.original'
    if (local/'I2C_HDMI_Config.v').read_bytes()!=hdmi_control.configuration(original_config.read_bytes()):
        parser.error('project initializer does not match reviewed busy handshake')
    files=[HERE/'hdmi_control.py',Path(__file__),HERE/'hdmi_probe.cpp',local/'board.v',local/'I2C_HDMI_Config.v',original_config,local/'I2C_Controller.v',local/'fes_de25_top_reset.v']
    source_hashes={str(p):sha(p) for p in files}
    out.mkdir(parents=True);commands=[]
    # Vendor clocked blocks use blocking assignments across processes. Direct
    # event simulation races on SD_COUNTER. Synthesize those actual processes
    # into explicit register next-state logic, without changing the vendor HDL.
    # This is a synthesized digital circuit model, not a Quartus/board proof.
    synthesized={}
    for label,config in [('qualified',local/'I2C_HDMI_Config.v'),('stale-completion',original_config)]:
        netlist=out/(label+'-netlist.v');script=out/(label+'-processes.ys')
        script.write_text('read_verilog '+ ' '.join(json.dumps(str(p)) for p in (config,local/'I2C_Controller.v'))+'\nhierarchy -check -top I2C_HDMI_Config\nproc\nmemory_map\nopt_clean\nwrite_verilog -noattr '+json.dumps(str(netlist))+'\n')
        synthesis=subprocess.run([str(args.yosys),'-Q','-T','-s',str(script)],cwd=ROOT,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=30)
        log=out/(label+'-synthesis.log');log.write_bytes(synthesis.stdout)
        if synthesis.returncode:raise ValueError('HDMI process synthesis failed: '+str(log))
        synthesized[label]=dict(netlist=str(netlist),sha256=sha(netlist),log_sha256=sha(log))
    for label in ('positive','raw-irq','stale-completion'):
        negative=label!='positive';case=out/label;case.mkdir()
        netlist=Path(synthesized['stale-completion' if label=='stale-completion' else 'qualified']['netlist'])
        (case/'model.v').write_text(WRAPPER)
        (case/'board.v').write_bytes(expected.replace(b'.HDMI_TX_INT(hdmi_irq_sync[1])',b'.HDMI_TX_INT(HDMI_TX_INT)') if label=='raw-irq' else expected)
        argv=['verilator','--cc','--exe','--build','-j','2','-Wall',
              '-Wno-DECLFILENAME','-Wno-PINCONNECTEMPTY','-Wno-PINMISSING','-Wno-UNUSEDSIGNAL',
              '-Wno-WIDTH','-Wno-CASEINCOMPLETE','-Wno-BLKSEQ','-Wno-UNDRIVEN',
              '-Wno-CASEOVERLAP',
              '--top-module','hdmi_control_model','--Mdir',str(case/'model'),
              str(case/'model.v'),str(case/'board.v'),str(local/'fes_de25_top_reset.v'),
              str(netlist),str(HERE/'hdmi_probe.cpp')]
        build=subprocess.run(argv,cwd=ROOT,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=90)
        (case/'build.log').write_bytes(build.stdout)
        if build.returncode:raise ValueError('HDMI model build failed: '+str(case/'build.log'))
        execute=[str(case/'model/Vhdmi_control_model')]
        result=subprocess.run(execute,cwd=ROOT,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=60)
        (case/'execute.log').write_bytes(result.stdout)
        commands.append(dict(label=label,negative=negative,build=argv,execute=execute,returncode=result.returncode,build_log_sha256=sha(case/'build.log'),execute_log_sha256=sha(case/'execute.log')))
        if negative:
            marker=b'raw IRQ reached the consumer without a clock' if label=='raw-irq' else b'STOP before three complete bytes/ACKs'
            if result.returncode==0 or marker not in result.stdout:
                raise ValueError('HDMI negative control did not expose '+label)
        elif result.returncode or b'accepted=99 nacks=1' not in result.stdout:
            raise ValueError('HDMI model failed: '+str(case/'execute.log'))
    if any(sha(p)!=digest for p,digest in source_hashes.items()):raise ValueError('HDMI model sources changed')
    record=dict(status='passed',scope='Actual board/reset RTL and Yosys process-synthesized captured I2C circuit; resolved open-drain slave model, digital PLL stub; no Quartus circuit equivalence, physical electrical/timing or metastability proof',source_sha256=source_hashes,commands=commands,yosys_sha256=sha(args.yosys),synthesized=synthesized,initializer_busy_handshake_modified=True,vendor_byte_controller_modified=False,register_table_unchanged=True,IRQ_consumer_bypass_negative_failed=True,stale_completion_negative_failed=True,hardware_actions=False)
    (out/'result.json').write_text(json.dumps(record,indent=2)+'\n')
    print('PASS HDMI startup, NACK retry, IRQ reinit and stopped-clock reset; raw IRQ and stale completion negatives rejected:',out)


if __name__=='__main__':main()
