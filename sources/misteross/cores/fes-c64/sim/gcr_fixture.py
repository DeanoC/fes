#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Generate original disk sectors for the four nominal 1541 density zones."""
import argparse
from pathlib import Path
import sys
# The final location is cores/fes-c64/sim within the misteross module.
sys.path.insert(0,str(Path(__file__).resolve().parents[3]))
from scripts import c64_gcr

def main():
 parser=argparse.ArgumentParser(description=__doc__)
 parser.add_argument('--output',type=Path,required=True)
 args=parser.parse_args();args.output.mkdir(parents=True,exist_ok=True)
 disk=bytearray((i*37+(i//256)*11)&255 for i in range(174848))
 bam=c64_gcr.offset(18,0);disk[bam+162:bam+164]=b'DG'
 tracks=c64_gcr.tracks(disk)
 for track in (1,18,25,31):
  expected=bytearray()
  for sector in range(c64_gcr.sector_count(track)):
   start=c64_gcr.offset(track,sector)
   packet=c64_gcr.packet(disk[start:start+256],track,sector,ord('D'),ord('G'))
   expected.extend(packet[5:15]);expected.extend(packet[29:354])
  for name,data in [('track',tracks[track-1]),('blocks',expected)]:
   (args.output/f'{name}-{track}.hex').write_text(''.join(f'{b:02x}\n' for b in data))
 (args.output/'synthetic.g64').write_bytes(c64_gcr.g64(disk))
if __name__=='__main__':main()
