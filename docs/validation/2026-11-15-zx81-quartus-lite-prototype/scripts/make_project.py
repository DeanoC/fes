"""Prototype only: Quartus Lite fit of the current DE10 ZX81 shell RTL."""
import re, sys, random, os
from pathlib import Path
root = Path(sys.argv[1]).resolve()          # sources/misteross
out = Path(sys.argv[2]).resolve(); seed = int(sys.argv[3]); model = sys.argv[4]; pin = sys.argv[5] == 'pin'
out.mkdir(parents=True, exist_ok=True)
src = (root/'scripts/build_fes_zx81_oss.py').read_text()
rtl = eval(re.search(r'RTL_SOURCES = (\(.*?\))\n', src).group(1))
# Scratch copy of zx81_rom_link with a LANE parameter per lane (shim selects MIF).
link = (root/'cores/fes-zx81/rtl/zx81_rom_link.v').read_text()
link = re.sub(r'MISTRAL_M10K #\([^;]*?\) lane(\d)', lambda m: f'MISTRAL_M10K #(.LANE({m.group(1)})) lane{m.group(1)}', link)
(out/'zx81_rom_link_q.v').write_text(link)
(out/'shims.v').write_text('''
module MISTRAL_FF(input CLK, input DATAIN, output reg Q, input ACLR, input ENA, input SCLR, input SLOAD, input SDATA);
  always @(posedge CLK) Q <= DATAIN;
endmodule
module MISTRAL_M10K #(parameter LANE = 0) (input CLK1, input [9:0] A1ADDR, input [9:0] A1DATA, input A1EN,
  input B1EN, input [9:0] B1ADDR, output [9:0] B1DATA, input ACLR0, input ACLR1);
  altsyncram #(.operation_mode("ROM"), .width_a(10), .widthad_a(10), .numwords_a(1024),
    .outdata_reg_a("UNREGISTERED"), .address_aclr_a("NONE"), .ram_block_type("M10K"),
    .init_file(LANE==0?"lane0.mif":LANE==1?"lane1.mif":LANE==2?"lane2.mif":LANE==3?"lane3.mif":
               LANE==4?"lane4.mif":LANE==5?"lane5.mif":LANE==6?"lane6.mif":"lane7.mif"),
    .intended_device_family("Cyclone V"), .lpm_type("altsyncram")) rom (
    .clock0(CLK1), .address_a(B1ADDR), .q_a(B1DATA));
endmodule
''')
rnd = random.Random(1234)
patterns = {}
for lane in range(8):
    words = [rnd.randrange(1024) for _ in range(1024)]
    patterns[lane] = words
    rows = ''.join(f'{a:03X} : {w:03X};\n' for a, w in enumerate(words))
    (out/f'lane{lane}.mif').write_text(f'DEPTH = 1024;\nWIDTH = 10;\nADDRESS_RADIX = HEX;\nDATA_RADIX = HEX;\nCONTENT BEGIN\n{rows}END;\n')
import json; (out/'patterns.json').write_text(json.dumps(patterns))
rel = lambda p: str(root/p)
define = {'zx81': None, 'zx80': 'ZX81_MODEL_ZX80', 'ts1000': 'ZX81_MODEL_TS1000', 'ts1500': 'ZX81_MODEL_TS1500'}[model]
L = ['set_global_assignment -name FAMILY "Cyclone V"', 'set_global_assignment -name DEVICE 5CSEBA6U23I7',
     'set_global_assignment -name TOP_LEVEL_ENTITY top', 'set_global_assignment -name PROJECT_OUTPUT_DIRECTORY output_files',
     'set_global_assignment -name GENERATE_RBF_FILE ON', 'set_global_assignment -name NUM_PARALLEL_PROCESSORS 4',
     f'set_global_assignment -name SEED {seed}', 'set_global_assignment -name VERILOG_INPUT_VERSION SYSTEMVERILOG_2005',
     'set_global_assignment -name ON_CHIP_BITSTREAM_DECOMPRESSION OFF',
     f'set_global_assignment -name SEARCH_PATH "{rel("cores/fes-zx81/generated")}"',
     f'set_global_assignment -name SEARCH_PATH "{rel("cores/fes-zx81/rtl")}"',
     f'set_global_assignment -name SEARCH_PATH "{rel("cores/fes-common/generated")}"',
     'set_global_assignment -name VERILOG_MACRO "QUARTUS=1"', 'set_global_assignment -name VERILOG_MACRO "FES_ZX81_ROM_LINK=1"',
     "set_global_assignment -name VERILOG_MACRO \"FES_ZX81_BUILD_ID=128'h0123456789abcdef0123456789abcdef\"",
     'set_parameter -name EXPANSION_SOCKET 1',
     f'set_global_assignment -name SDC_FILE "{rel("cores/fes-zx81/clocks.sdc")}"',
     f'set_global_assignment -name VERILOG_FILE "{out/"shims.v"}"',
     f'set_global_assignment -name VERILOG_FILE "{out/"zx81_rom_link_q.v"}"']
if define: L.append(f'set_global_assignment -name VERILOG_MACRO "{define}=1"')
for s in rtl:
    if s.endswith('.vh') or s.endswith('zx81_rom_link.v'): continue
    kind = 'SYSTEMVERILOG_FILE' if s.endswith('.sv') else 'VERILOG_FILE'
    L.append(f'set_global_assignment -name {kind} "{rel(s)}"')
if pin:
    for lane in range(8 if define != 'ZX81_MODEL_ZX80' else 4):
        L.append(f'set_location_assignment M10K_X5_Y{73+lane}_N0 -to "*|MISTRAL_M10K:lane{lane}|*"')
L.append((root/'cores/fes-zx81/constraints.qsf').read_text())
(out/'top.qsf').write_text('\n'.join(L) + '\n')
(out/'top.qpf').write_text('QUARTUS_VERSION = "17.0";\nPROJECT_REVISION = "top";\n')
