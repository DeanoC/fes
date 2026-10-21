# SPDX-License-Identifier: MIT
"""D64 to standard 1541 GCR tracks. Development simulation, not the production media path."""
from functools import reduce
from operator import xor
import struct

CODE = (10,11,18,19,14,15,22,23,9,25,26,27,13,29,30,21)
LENGTHS = (6250,6666,7142,7692)
GAPS = (9,12,17,8)

def zone(track):
    if not 1 <= track <= 35: raise ValueError('track must be 1..35')
    return 3 if track <= 17 else 2 if track <= 24 else 1 if track <= 30 else 0

def sector_count(track):
    return (17,18,19,21)[zone(track)]

def offset(track,sector):
    if not 0 <= sector < sector_count(track): raise ValueError('invalid sector')
    return (sum(sector_count(t) for t in range(1,track))+sector)*256

def encode(data):
    if len(data)%4: raise ValueError('GCR blocks require multiples of four bytes')
    result=bytearray()
    for start in range(0,len(data),4):
        bits=0
        for b in data[start:start+4]:
            bits=(bits<<10)|(CODE[b>>4]<<5)|CODE[b&15]
        result.extend(bits.to_bytes(5,'big'))
    return bytes(result)

def decode(data):
    if len(data)%5: raise ValueError('invalid GCR block length')
    inverse={v:i for i,v in enumerate(CODE)}
    result=bytearray()
    for start in range(0,len(data),5):
        bits=int.from_bytes(data[start:start+5],'big')
        for shift in (30,20,10,0):
            try: result.append((inverse[(bits>>(shift+5))&31]<<4)|inverse[(bits>>shift)&31])
            except KeyError: raise ValueError('invalid GCR symbol') from None
    return bytes(result)

def packet(payload,track,sector,id1,id2):
    if len(payload)!=256: raise ValueError('sector must be 256 bytes')
    header=bytes((8,track^sector^id1^id2,sector,track,id2,id1,15,15))
    body=bytes((7,))+payload+bytes((reduce(xor,payload,0),0,0))
    return b'\xff'*5+encode(header)+b'\x55'*9+b'\xff'*5+encode(body)

def tracks(d64):
    if len(d64)!=174848: raise ValueError('expected exact 35-track D64')
    bam=offset(18,0)
    id1,id2=d64[bam+162:bam+164]
    result=[]
    for track in range(1,36):
        z=zone(track)
        data=bytearray()
        for sector in range(sector_count(track)):
            pos=offset(track,sector)
            data.extend(packet(d64[pos:pos+256],track,sector,id1,id2))
            data.extend(b'\x55'*GAPS[z])
        assert len(data)<=LENGTHS[z]
        data.extend(b'\x55'*(LENGTHS[z]-len(data)))
        result.append(bytes(data))
    return result

def g64(d64):
    ts=tracks(d64)
    count=84
    header=bytearray(b'GCR-1541'+bytes((0,count))+struct.pack('<H',max(LENGTHS)))
    offsets=[0]*count;speeds=[0]*count
    data=bytearray()
    for track,body in enumerate(ts,1):
        index=(track-1)*2
        offsets[index]=12+count*8+len(data)
        speeds[index]=zone(track)
        data.extend(struct.pack('<H',len(body))+body)
    return bytes(header)+struct.pack('<84I',*offsets)+struct.pack('<84I',*speeds)+bytes(data)
