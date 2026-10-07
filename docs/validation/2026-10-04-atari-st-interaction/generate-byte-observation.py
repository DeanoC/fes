#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Original stackless 68000 RAM byte-lane observation ROM for an FES Atari ST.

Run from an FES checkout, or supply --repo. Only --output/--record are written;
no compiler, target, programmer or copyrighted OS ROM is used. Link the result
through the shell's ordinary named 192 KiB atari-st-firmware input.

Blue background is an observation screen, never a PASS verdict. White U/L
headings identify upper/even and lower/odd byte-store columns. Each column
prints four hex digits from one actual full-word CPU read. Rows top to bottom:
1. Full-word 5AA5 baseline at $2000/$2002 (no byte store).
2. Upper byte C3 to $2000 / lower byte 3C to $2003, immediate readback.
3. Same stores with 32 NOP gaps between operations.
4. Eight repeated/gapped stores at $2008/$200A (a different physical SDRAM bank).

Rows 2..4 should read C3A5 / 5A3C. Ignoring both masks yields C3C3 / 3C3C;
swapped masks yield 5AC3 / 3CA5; inhibited byte stores retain 5AA5 / 5AA5.
Other words are printed as actually observed. Gaps distinguish a retained
single-store failure from one sensitive to neighboring commands.

No RAM stack, subroutine, trap, interrupt or OS service is used. Only full-word
framebuffer writes draw the display. CPU captures are retained at $0404..$0412;
C0DE at $0400 means program completion, not a lane PASS. Glyphs are original
seven-segment geometry. The CPU/SDRAM simulation proof is host-only and does
not establish electrical pad timing or the correctness of a native bitstream.
"""
import argparse, hashlib, importlib.util, json, pathlib, struct, subprocess
parser=argparse.ArgumentParser(description=__doc__,formatter_class=argparse.RawDescriptionHelpFormatter)
parser.add_argument('--output',type=pathlib.Path,required=True,help='write exactly 192 KiB original firmware')
parser.add_argument('--record',type=pathlib.Path,help='optional JSON source/ROM/instruction record')
parser.add_argument('--repo',type=pathlib.Path,help='FES source checkout; otherwise discover from script/cwd ancestors')
args=parser.parse_args()
candidates=[args.repo] if args.repo else [*pathlib.Path(__file__).resolve().parents,pathlib.Path.cwd(),*pathlib.Path.cwd().parents]
ROOT=next((p.resolve() for p in candidates if p and (p/'scripts/atari_st_memory_diagnostic.py').is_file() and (p/'sources/misteross/cores/fes-atari-st/diagnostic/firmware.py').is_file()),None)
if ROOT is None:parser.error('cannot locate FES sources; pass --repo /path/to/fes')
output=args.output.resolve()
output.parent.mkdir(parents=True,exist_ok=True)
HELPER=ROOT/'scripts/atari_st_memory_diagnostic.py'
spec=importlib.util.spec_from_file_location('original_memory_emitter',HELPER)
mem=importlib.util.module_from_spec(spec); spec.loader.exec_module(mem)
P=mem.Program
SCREEN=mem.SCREEN
FONT_STRIDE=64
READBACK=0x404
ROWS=[
 {'name':'baseline full words','left':0x2000,'right':0x2002,'gap':0,'repeats':0,'expected':[0x5aa5,0x5aa5]},
 {'name':'immediate single byte writes','left':0x2000,'right':0x2002,'gap':0,'repeats':1,'expected':[0xc3a5,0x5a3c]},
 {'name':'single byte writes with 32 NOP gaps','left':0x2000,'right':0x2002,'gap':32,'repeats':1,'expected':[0xc3a5,0x5a3c]},
 {'name':'eight byte writes with 32 NOP gaps, bank 1','left':0x2008,'right':0x200a,'gap':32,'repeats':8,'expected':[0xc3a5,0x5a3c]},
]
p=P(mem.ENTRY)
p.sr(0x2700)
p.move_imm('b',4,0xff8001)
p.move_imm('b',SCREEN>>16,0xff8201)
p.move_imm('b',(SCREEN>>8)&255,0xff8203)
p.move_imm('b',2,0xff820a)
p.move_imm('b',0,0xff8260)
for i in range(16):p.move_imm('w',0,0xff8240+2*i)
p.move_imm('w',0x002,0xff8240)
p.move_imm('w',0x777,0xff8242)
p.word(0x207c); p.long(SCREEN) # MOVEA.L #SCREEN,A0
p.word(0x303c); p.word(0) # MOVE.W #0,D0
p.word(0x3e3c); p.word(15999) # MOVE.W #15999,D7
p.label('clear'); p.word(0x30c0) # MOVE.W D0,(A0)+
p.word(0x51cf); p.fixups.append((len(p.code),'clear',True)); p.word(0)
# Header U/L and separators: original 7-segment geometry, only word writes.
for x,bits in [(72,0x3e),(232,0x38)]:
 for y,v in enumerate([6 if bits&1 else 0,
  (8 if bits&32 else 0)|(1 if bits&2 else 0),
  (8 if bits&32 else 0)|(1 if bits&2 else 0),6 if bits&64 else 0,
  (8 if bits&16 else 0)|(1 if bits&4 else 0),
  (8 if bits&16 else 0)|(1 if bits&4 else 0),6 if bits&8 else 0]):
  word=sum(0xf<<(12-4*c) for c in range(4) if v&(8>>c))
  for line in range(4):p.move_imm('w',word,SCREEN+(4+4*y+line)*160+(x//16)*8)
for y in [34,76,116,156,194]:
 for col in range(20):p.move_imm('w',0xaaaa,SCREEN+y*160+col*8)

def nops(count):
 for _ in range(count):p.word(0x4e71)

def draw_readback(addr, index, left, top):
 p.read('w',addr) # MOVE.W absolute-long,D0
 p.word(0x3c00) # MOVE.W D0,D6: retain actual word across all four glyph loops
 p.word(0x33c0); p.long(READBACK+2*index) # MOVE.W D0,abs.l observation only
 for digit,shift in enumerate([12,8,4,0]):
  p.word(0x3006) # MOVE.W D6,D0
  if shift>=8:p.word(0xe048) # LSR.W #8,D0
  if shift%8:p.word(0xe848) # LSR.W #4,D0
  p.word(0x0240);p.word(0x000f) # ANDI.W #15,D0
  p.word(0xed48) # LSL.W #6,D0: index into 64-byte glyph slots
  p.word(0x227c);p.long('font') # MOVEA.L #font,A1
  p.word(0xd2c0) # ADDA.W D0,A1
  p.word(0x207c);p.long(SCREEN+top*160+((left+digit*32)//16)*8)
  p.word(0x3e3c);p.word(27) # MOVE.W #27,D7
  label=f'draw_{index}_{digit}';p.label(label)
  p.word(0x3219) # MOVE.W (A1)+,D1
  p.word(0x3081) # MOVE.W D1,(A0)
  p.word(0xd0fc);p.word(160) # ADDA.W #160,A0
  p.word(0x51cf);p.fixups.append((len(p.code),label,True));p.word(0)

for row,case in enumerate(ROWS):
 p.label(f'row_{row}')
 p.move_imm('w',0x5aa5,case['left']);p.move_imm('w',0x5aa5,case['right'])
 nops(case['gap'])
 for _ in range(case['repeats']):
  p.move_imm('b',0xc3,case['left']);nops(case['gap'])
  p.move_imm('b',0x3c,case['right']+1);nops(case['gap'])
 draw_readback(case['left'],row*2,16,40+row*40)
 draw_readback(case['right'],row*2+1,176,40+row*40)
p.move_imm('w',0,mem.STAGE)
p.move_imm('w',0xc0de,mem.RESULT) # completed, NOT a hardware pass verdict
p.label('complete');p.branch('always','complete')
p.label('font')
for digit in range(16):
 for word in mem._glyph_rows(digit):p.word(word)
 for _ in range(4):p.word(0)
code=p.finish()
assert mem.ENTRY-mem.ROM_BASE+len(code)<=mem.ROM_SIZE
image=bytearray(b'\xff'*mem.ROM_SIZE)
image[:8]=struct.pack('>II',mem.STACK,mem.ENTRY)
image[0x100:0x100+len(code)]=code
if output.exists() and output.read_bytes()!=image:raise ValueError('refusing to replace different existing firmware')
output.write_bytes(image)
record={'schema':1,'kind':'fes.atari-st.byte-observation/1',
 'source_revision':subprocess.run(['git','rev-parse','HEAD'],cwd=ROOT,text=True,capture_output=True).stdout.strip() or 'unavailable',
 'rom_sha256':hashlib.sha256(image).hexdigest(),'rom_bytes':len(image),'code_bytes':len(code),
 'rom_base':mem.ROM_BASE,'entry':mem.ENTRY,'reset_ssp':mem.STACK,
 'stack_used':False,'trap_or_call_used':False,'result_is_completion_only':True,
 'screen_base':SCREEN,'readback_address':READBACK,'rows':ROWS,
 'display':'Blue background, white hex. U/L columns. Rows: baseline; immediate; 32-NOP gap; repeated/gapped bank1. No palette pass classification.',
 'glyph_geometry':{'rows':28,'width':16,'horizontal_starts':[16,176],'digit_stride':32,'row_tops':[40,80,120,160]},
 'classifications':{'correct':['C3A5','5A3C'],'mask_ignored':['C3C3','3C3C'],'mask_swapped':['5AC3','3CA5'],'byte_write_inhibited':['5AA5','5AA5']},
 'new_instruction_encodings':{'MOVE.W D0,D6':'3c00','MOVE.W D6,D0':'3006','MOVE.W D0,abs.l':'33c0','LSR.W #8,D0':'e048','LSR.W #4,D0':'e848','ANDI.W #15,D0':'0240 000f','LSL.W #6,D0':'ed48','MOVEA.L #font,A1':'227c','ADDA.W D0,A1':'d2c0','MOVE.W (A1)+,D1':'3219','MOVE.W D1,(A0)':'3081','ADDA.W #160,A0':'d0fc 00a0','DBF D7,disp16':'51cf'},
 'labels':p.labels,'inputs':[{'path':str(f.relative_to(ROOT)) if f.is_relative_to(ROOT) else f.name,'sha256':hashlib.sha256(f.read_bytes()).hexdigest()} for f in [pathlib.Path(__file__),HELPER,mem.HELPER]],
 'native_fpga_build':False,'hardware_execution':False,'validated':False}
if args.record:
 args.record.parent.mkdir(parents=True,exist_ok=True)
 args.record.write_text(json.dumps(record,indent=2,sort_keys=True)+'\n')
print(json.dumps({'path':str(output),'sha256':record['rom_sha256'],'bytes':len(image),'code_bytes':len(code),'complete_pc':p.labels['complete']},indent=2))
