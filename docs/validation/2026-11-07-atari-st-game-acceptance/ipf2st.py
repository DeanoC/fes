#!/usr/bin/env python3
"""ipf2st.py LIB OUTDIR IPF...: decode IPF disks with the SPS decoder library (capsimg, private build, non-commercial
licence) and write a raw .st image only when the disk is a plain ST layout: every sector the boot-sector BPB
describes is present once per track with good ID and data CRCs, no extra/odd sectors. Prints one JSON line per disk."""
import ctypes as C, json, sys, hashlib, os
from pathlib import Path
lib = C.CDLL(sys.argv[1]); out = Path(sys.argv[2]); out.mkdir(exist_ok=True)
class II(C.Structure):
    _fields_ = [(n, C.c_uint32) for n in ('type','release','revision','mincyl','maxcyl','minhead','maxhead')] + [('dt', C.c_uint32*7), ('platform', C.c_uint32*4)]
class T2(C.Structure):
    _pack_ = 1
    _fields_ = [('type',C.c_uint32),('cyl',C.c_uint32),('head',C.c_uint32),('sectorcnt',C.c_uint32),('sectorsize',C.c_uint32),
                ('trackbuf',C.c_void_p),('tracklen',C.c_uint32),('timelen',C.c_uint32),('timebuf',C.c_void_p),('overlap',C.c_int32),
                ('startbit',C.c_uint32),('wseed',C.c_uint32),('weakcnt',C.c_uint32)]
lib.CAPSInit()
FLAGS = (1<<0)|(1<<2)|(1<<8)|(1<<9)  # INDEX DENVAR UPDATEFD TYPE

def crc16(data, crc=0xFFFF):
    for b in data:
        crc ^= b << 8
        for _ in range(8):
            crc = ((crc << 1) ^ 0x1021) & 0xFFFF if crc & 0x8000 else (crc << 1) & 0xFFFF
    return crc

def decode(buf):
    """Return list of (id(c,h,r,n), data or None, idcrc_ok, datacrc_ok) from raw MFM bytes (one revolution, wrapped)."""
    bits = ''.join(f'{b:08b}' for b in buf + buf[:2000])
    n = len(buf) * 8
    sync = '0100010010001001'
    def byte_at(p):
        return int(bits[p+1:p+16:2], 2)
    marks, p = [], bits.find(sync)
    while p != -1 and p < n:
        if bits[p+16:p+32] == sync and bits[p+32:p+48] == sync:
            marks.append(p + 48); p = bits.find(sync, p + 48)
        else:
            p = bits.find(sync, p + 1)
    res = []
    for i, m in enumerate(marks):
        if byte_at(m) != 0xFE:
            continue
        hdr = bytes(byte_at(m + 16*k) for k in range(7))
        idok = crc16(b'\xa1\xa1\xa1' + hdr) == 0
        data, dok = None, False
        for m2 in marks[i+1:i+3]:
            mk = byte_at(m2)
            if mk in (0xFB, 0xF8) and m2 - m < 16 * 60:
                size = 128 << (hdr[4] & 3)
                if m2 + 16 * (size + 3) <= len(bits):
                    raw = bytes(byte_at(m2 + 16*k) for k in range(size + 3))
                    data, dok = raw[1:size+1], crc16(b'\xa1\xa1\xa1' + raw) == 0 and mk == 0xFB
                break
        res.append((tuple(hdr[1:5]), data, idok, dok))
    return res

for ipf in sys.argv[3:]:
    rec = {'ipf': os.path.basename(ipf), 'ipf_sha256': hashlib.sha256(open(ipf,'rb').read()).hexdigest()}
    iid = lib.CAPSAddImage()
    try:
        if lib.CAPSLockImage(iid, ipf.encode()) != 0:
            rec['error'] = 'lock image failed'; print(json.dumps(rec), flush=True); continue
        lib.CAPSLoadImage(iid, 0)
        ii = II(); lib.CAPSGetImageInfo(C.byref(ii), iid)
        rec['cyl'] = [ii.mincyl, ii.maxcyl]; rec['heads'] = [ii.minhead, ii.maxhead]
        tracks, problems = {}, []
        for cyl in range(ii.mincyl, ii.maxcyl + 1):
            for head in range(ii.minhead, ii.maxhead + 1):
                t = T2(); t.type = 2
                if lib.CAPSLockTrack(C.byref(t), iid, cyl, head, FLAGS) != 0 or not t.trackbuf:
                    tracks[(cyl, head)] = []; continue
                buf = C.string_at(t.trackbuf, t.tracklen)
                tracks[(cyl, head)] = decode(buf) if t.tracklen else []
                if t.weakcnt: problems.append(f'weak bits c{cyl}h{head}')
        lib.CAPSUnlockAllTracks(iid)
        boot = next((d for idt, d, a, b in tracks.get((0, 0), []) if idt[2] == 1 and idt[0] == 0 and b), None)
        if boot is None:
            rec['error'] = 'no good boot sector'; print(json.dumps(rec), flush=True); continue
        bps = boot[11] | boot[12] << 8; spt = boot[24] | boot[25] << 8; sides = boot[26] | boot[27] << 8; tot = boot[19] | boot[20] << 8
        rec['bpb'] = {'bps': bps, 'spt': spt, 'sides': sides, 'sectors': tot}
        if bps != 512 or spt not in (9, 10) or sides not in (1, 2) or tot % (spt * sides):
            rec['error'] = 'BPB not a supported shape'; print(json.dumps(rec), flush=True); continue
        ncyl = tot // (spt * sides)
        if not 80 <= ncyl <= 82:
            rec['error'] = f'{ncyl} tracks'; print(json.dumps(rec), flush=True); continue
        img = bytearray()
        for cyl in range(ncyl):
            for head in range(sides):
                secs = tracks.get((cyl, head), [])
                got = {}
                for idt, d, a, b in secs:
                    if not (a and b) or idt[0] != cyl or idt[1] != head or idt[3] != 2 or not 1 <= idt[2] <= spt:
                        problems.append(f'odd sector c{cyl}h{head} id{idt} idcrc={a} datacrc={b}'); continue
                    if idt[2] in got and got[idt[2]] != d: problems.append(f'conflicting dup c{cyl}h{head}r{idt[2]}')
                    got[idt[2]] = d
                for r in range(1, spt + 1):
                    if r not in got: problems.append(f'missing c{cyl}h{head}r{r}'); img += bytes(512)
                    else: img += got[r]
        extra = [k for k, v in tracks.items() if (k[0] >= ncyl or k[1] >= sides) and v]
        if extra: problems.append(f'{len(extra)} tracks outside the BPB shape carry sectors')
        rec['problems'] = problems[:12]; rec['problem_count'] = len(problems)
        name = Path(ipf).stem + '.st'
        if not problems:
            (out / name).write_bytes(img); rec['st'] = name; rec['st_sha256'] = hashlib.sha256(img).hexdigest(); rec['st_size'] = len(img)
    finally:
        lib.CAPSUnlockImage(iid); lib.CAPSRemImage(iid)
    print(json.dumps(rec), flush=True)
