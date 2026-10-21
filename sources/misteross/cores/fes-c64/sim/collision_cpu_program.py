#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Open RAM test of real6510 collision IRQ/read/clear; no demo LOAD claim."""
from pathlib import Path
import argparse
import sys
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'diagnostic'))
from asm6502 import assemble
PROGRAM=r'''

.org $1000
SEI
LDA #7
STA $00
LDA #5
STA $01
LDA #0
STA $d015
STA $d010
STA $d017
STA $d01b
STA $d01c
STA $d01d
LDA #$1b
STA $d011
LDA #8
STA $d016
LDA #$18
STA $d018
LDX #14
positions:
LDA #40
STA $d000,X
LDA #64
STA $d001,X
DEX
DEX
BPL positions
LDA #1
STA $d82a
LDA $12ff
STA $d015
LDA #15
STA $d019
LDA #6
STA $d01a
CLI
waitirq:
LDA $1203
CMP #$a5
BNE waitirq
waitline:
LDA $d012
CMP #100
BNE waitline
LDA $d01e
STA $1204
LDA $d01f
STA $1205
NOP
NOP
LDA $d01e
STA $1206
LDA $d01f
STA $1207
LDA #15
STA $d019
LDA #6
STA $d01a
LDA $d019
STA $1209
LDA #$a5
STA $1208
halt:
JMP halt
.org $1100
PHA
LDA #0
STA $d01a
LDA $d019
STA $1200
LDA $d01e
STA $1201
LDA $d01f
STA $1202
LDA #15
STA $d019
LDA #$a5
STA $1203
PLA
RTI
'''
if __name__=='__main__':
    parser=argparse.ArgumentParser()
    parser.add_argument('--output',type=Path,required=True)
    args=parser.parse_args()
    args.output.write_bytes(assemble(PROGRAM,base=0x1000,size=512,fill=0xea))
