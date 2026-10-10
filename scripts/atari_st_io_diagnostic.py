#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-or-later
"""Original EmuTOS AUTO diagnostic for raw IKBD bytes and measured YM sound.

Uses the existing original FAT12 builder and bounded 68000 emitter. No Atari
ROM, assembler or external executable is distributed. After the READY banner,
send schedule.json events through the normal attached host session input API.
Each group advances only after its exact ACIA bytes were read by the guest.
Unexpected bytes show FAIL and the observed/expected hexadecimal byte. Missing
bytes leave that group waiting. IO PASS then repeats six 3-second YM phases.

The guest enters supervisor through GEMDOS Super(0), masks only MFP ACIA channel
6, and polls the keyboard ACIA. Timer C/VBL and the established EmuTOS screen
remain active. No test writes target RAM or CPU responses from outside the guest.
Stop the library session normally; this program intentionally never returns.
"""
from __future__ import annotations
import argparse
import hashlib
import json
from pathlib import Path
import struct
import atari_st_disk_diagnostic as disk

ACIA_STATUS, ACIA_DATA = 0xFFFC00, 0xFFFC02
YM_ADDRESS, YM_DATA = 0xFF8800, 0xFF8802
HZ200 = 0x4BA
PHASE_TICKS = 600


def key(usage, down):
    return {"Player": 0, "Device": 0, "Kind": 0, "Action": int(down), "Code": 0x1000 + usage, "Value": 0}


def pad(player, code, down):
    return {"Player": player, "Device": 1, "Kind": 1, "Action": int(down), "Code": code, "Value": 0}


def groups():
    result = [
        ("SHIFT+A", [key(0xE1,1), key(4,1), key(4,0), key(0xE1,0)], [0x2A,0x1E,0x9E,0xAA]),
        ("CTRL ALIASES", [key(0xE0,1),key(0xE4,1),key(0xE0,0),key(0xE4,0)], [0x1D,0x9D]),
        ("ALT ALIASES", [key(0xE2,1),key(0xE6,1),key(0xE2,0),key(0xE6,0)], [0x38,0xB8]),
        ("RIGHT SHIFT", [key(0xE5,1),key(5,1),key(5,0),key(0xE5,0)], [0x36,0x30,0xB0,0xB6]),
        ("ESC+BACKSPACE", [key(41,1),key(41,0),key(42,1),key(42,0)], [0x01,0x81,0x0E,0x8E]),
    ]
    for player in (0,1):
        header = 0xFF if player == 0 else 0xFE
        events, expected = [], []
        for code, mask in ((100,1),(101,2),(102,4),(103,8),(104,0x80),(105,0x80)):
            events.extend((pad(player,code,1),pad(player,code,0)))
            expected.extend((header,mask,header,0))
        result.append((f"PAD{player+1} UDLR A/B", events, expected))
    result.append(("TWO PADS", [pad(0,100,1),pad(1,103,1),pad(0,100,0),pad(1,103,0)],
                   [0xFF,1,0xFE,8,0xFF,0,0xFE,0]))
    return result


def schedule():
    return {"schema":1,"ready_banner":"FES ST IO READY", "event_interval_ms":100,
            "group_barrier":"Wait for prior group PASS / current WAIT banner before sending the next group.",
            "groups":[{"name":name,"events":events,"expected_acia_bytes":expected} for name,events,expected in groups()],
            "audio_phase_seconds":3,"audio_phases":[
                {"name":"A 440","tone_hz":2000000/(16*284),"channel":"A","period":284},
                {"name":"B 661","tone_hz":2000000/(16*189),"channel":"B","period":189},
                {"name":"C 880","tone_hz":2000000/(16*142),"channel":"C","period":142},
                {"name":"NOISE","noise_period":16},
                {"name":"ENVELOPE","tone_hz":2000000/(16*284),"envelope_period":7812,"shape":14},
                {"name":"SILENCE","all_volumes_zero":True}],
            "audio_measurement":"Repeat 18-second cycle after IO PASS; discard phase edges, compare L/R, measure tones and noise/envelope/silence. Stop must silence HDMI.",
            "physical_usb_acceptance":False}


def build_prg_with_symbols(*, phase_ticks=PHASE_TICKS):
    if not 20 <= phase_ticks <= PHASE_TICKS:
        raise ValueError("phase_ticks must be 20..600")
    a = disk._M68k()
    # Same identifiable startup and real Mshrink path as the disk guest.
    a.word(0x7CFF); a.word(0x286F); a.word(4)
    a.lea("stack_end",7)
    a.push_long(0); size_offset=len(a.code)-4
    a.word(0x2F0C); a.push_word(0); a.call(0x4A,10)
    a.expect(0)
    a.push_long(0); a.call(0x20,4) # Super(0)
    # ANDI.B #$BF,IERB/IMRB: disable only interrupt6 (ACIA/GPIP4).
    for address in (0xFFFA09,0xFFFA15):
        a.word(0x0239); a.word(0xBF); a.long(address)
    # Clear stale pending/in-service channel6; preserve other channels.
    for address in (0xFFFA0D,0xFFFA11):
        a.word(0x13FC); a.word(0xBF); a.long(address)
    # Keyboard 8N1 /64, no ACIA RX IRQ. Keep timer/VBL interrupts enabled.
    a.word(0x13FC); a.word(0x16); a.long(ACIA_STATUS)
    a.word(0x46FC); a.word(0x2300)
    a.label("drain")
    a.word(0x0839); a.word(0); a.long(ACIA_STATUS) # BTST #0,status
    a.branch(7,"drained")
    a.word(0x1039); a.long(ACIA_DATA); a.branch(0,"drain")
    a.label("drained")
    # IKBD joystick event mode gives both physical ports to joysticks.
    a.label("tx_ready")
    a.word(0x0839); a.word(1); a.long(ACIA_STATUS); a.branch(7,"tx_ready")
    a.word(0x13FC); a.word(0x14); a.long(ACIA_DATA)

    def show(label):
        a.push_pointer(label); a.call(9,4)
    def set_marker(value):
        a.lea("marker",4); a.word(0x28BC); a.long(value) # MOVE.L #imm,(A4)
    show("ready"); set_marker(0x494F0000)
    for index,(name,events,expected) in enumerate(groups()):
        show(f"wait_{index}"); set_marker(0x494F0100+index)
        a.lea(f"expected_{index}",2)
        a.word(0x3E3C); a.word(len(expected)-1) # D7 loop count
        a.label(f"receive_{index}")
        a.word(0x0839); a.word(0); a.long(ACIA_STATUS); a.branch(7,f"receive_{index}")
        a.word(0x7000); a.word(0x1039); a.long(ACIA_DATA)
        a.word(0xB01A) # CMP.B (A2)+,D0
        a.branch(6,"byte_failure")
        a.relative(0x51CF,f"receive_{index}")
        show(f"pass_{index}")
    show("io_pass"); set_marker(0x494F5041)
    # Save only port direction bits from mixer7; never change YM port latches.
    a.word(0x13FC); a.word(7); a.long(YM_ADDRESS)
    a.word(0x7C00); a.word(0x1C39); a.long(YM_ADDRESS) # MOVE.B addr,D6
    a.word(0x0206); a.word(0xC0) # ANDI.B #c0,D6
    def ym(reg,value):
        a.word(0x13FC); a.word(reg); a.long(YM_ADDRESS)
        a.word(0x13FC); a.word(value); a.long(YM_DATA)
    def mixer(value):
        a.word(0x1006); a.word(0x0000); a.word(value) # MOVE.B D6,D0; ORI.B #value,D0
        a.word(0x13FC); a.word(7); a.long(YM_ADDRESS)
        a.word(0x13C0); a.long(YM_DATA)
    a.label("audio_loop")
    phases=schedule()["audio_phases"]
    for index,phase in enumerate(phases):
        for reg in (8,9,10): ym(reg,0)
        mixer(0x3F)
        if index < 3:
            period=phase["period"]; ym(2*index,period&255); ym(2*index+1,period>>8)
            mixer(0x3F^(1<<index)); ym(8+index,15)
        elif index == 3:
            ym(6,16); mixer(0x37); ym(8,15)
        elif index == 4:
            ym(0,284&255); ym(1,284>>8); ym(11,7812&255); ym(12,7812>>8); ym(13,14)
            mixer(0x3E); ym(8,16)
        show(f"audio_{index}"); set_marker(0x41550000+index)
        a.word(0x2A39); a.long(HZ200) # MOVE.L hz200,D5
        a.label(f"audio_wait_{index}")
        a.word(0x2039); a.long(HZ200); a.word(0x9085) # SUB.L D5,D0
        a.word(0x0C80); a.long(phase_ticks); a.branch(5,f"audio_wait_{index}") # BCS
    a.branch(0,"audio_loop")
    a.label("byte_failure")
    # Preserve observed D0, get preceding expected byte, print both hex pairs.
    a.word(0x1800); a.word(0x162A); a.word(0xFFFF) # D4 observed,D3 expected -1(A2)
    show("fail_byte")
    def hex_byte(register):
        for shift in (True,False):
            a.word(0x7000); a.word(0x1000|register)
            if shift: a.word(0xE808) # LSR.B #4,D0
            a.word(0x0200); a.word(15)
            a.lea("hex",0); a.word(0x1030); a.word(0) # MOVE.B (A0,D0.W),D0
            a.word(0x3F00); a.call(2,2)
    hex_byte(4); show("expected_text"); hex_byte(3); show("newline")
    a.label("failure"); set_marker(0x494F4641); show("fail")
    a.label("halt"); a.branch(0,"halt")
    strings={"ready":"\x1bE FES ST IO READY\r\n", "io_pass":"IO PASS - exact keyboard / two pads\r\n",
             "fail_byte":"FAIL observed=", "expected_text":" expected=", "newline":"\r\n",
             "fail":"FAIL - Stop this library session\r\n", "hex":"0123456789ABCDEF"}
    for index,(name,_,_) in enumerate(groups()):
        strings[f"wait_{index}"]=f"WAIT {index+1} {name}\r\n"
        strings[f"pass_{index}"]=f"PASS {index+1} {name}\r\n"
    for index,phase in enumerate(phases): strings[f"audio_{index}"]=f"YM {index} {phase['name']} 3s\r\n"
    for label,value in strings.items():
        a.label(label); a.code.extend(value.encode("ascii")+b"\0")
    for index,(_,_,expected) in enumerate(groups()):
        a.label(f"expected_{index}"); a.code.extend(bytes(expected))
    if len(a.code)&1: a.code.append(0)
    a.label("marker"); a.long(0)
    a.code.extend(bytes(2048)); a.label("stack_end")
    struct.pack_into(">I",a.code,size_offset,256+len(a.code))
    text=a.finish()
    return struct.pack(">H6IH",0x601A,len(text),0,0,0,0,0,1)+text,a.labels


def build_prg(*, phase_ticks=PHASE_TICKS): return build_prg_with_symbols(phase_ticks=phase_ticks)[0]


def build_disk(*, phase_ticks=PHASE_TICKS):
    image=bytearray(disk.DISK_BYTES)
    image[:11]=b"\x60\x1cFESIO \x12\x34\x56"
    struct.pack_into("<HBHBHHBHHHH",image,11,512,2,1,2,112,1440,0xF9,3,9,2,0)
    fat=bytearray(3*disk.SECTOR); fat[:3]=b"\xf9\xff\xff"
    next_cluster=2
    def allocate(data):
        nonlocal next_cluster
        first=next_cluster
        count=(len(data)+disk.CLUSTER_BYTES-1)//disk.CLUSTER_BYTES
        for index in range(count):
            cluster=next_cluster; next_cluster+=1
            disk._fat_set(fat,cluster,0xFFF if index==count-1 else next_cluster)
            offset=disk.DATA_SECTOR*disk.SECTOR+(cluster-2)*disk.CLUSTER_BYTES
            block=data[index*disk.CLUSTER_BYTES:(index+1)*disk.CLUSTER_BYTES]
            image[offset:offset+len(block)]=block
        return first
    directory_cluster=allocate(bytes(disk.CLUSTER_BYTES))
    program=build_prg(phase_ticks=phase_ticks); program_cluster=allocate(program)
    directory=(disk._entry(b".          ",directory_cluster,0,0x10)+
               disk._entry(b"..         ",0,0,0x10)+
               disk._entry(b"IOTEST  PRG",program_cluster,len(program)))
    offset=disk.DATA_SECTOR*disk.SECTOR+(directory_cluster-2)*disk.CLUSTER_BYTES
    image[offset:offset+len(directory)]=directory
    root=bytearray(7*disk.SECTOR)
    root[:32]=disk._entry(b"AUTO       ",directory_cluster,0,0x10)
    readme=("FES original GPL-2.0-or-later IKBD/YM guest diagnostic.\r\n"
            "IOTEST.PRG starts automatically; use the generated schedule.json.\r\n"
            "Stop normally after measuring the repeating six audio phases.\r\n").encode("ascii")
    root[32:64]=disk._entry(b"README  TXT",allocate(readme),len(readme))
    image[disk.ROOT_SECTOR*disk.SECTOR:disk.DATA_SECTOR*disk.SECTOR]=root
    image[disk.SECTOR:4*disk.SECTOR]=fat; image[4*disk.SECTOR:7*disk.SECTOR]=fat
    return bytes(image)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output",type=Path,required=True)
    args=parser.parse_args(); args.output.mkdir(parents=True,exist_ok=True)
    prg,symbols=build_prg_with_symbols(); image=build_disk()
    (args.output/"IOTEST.PRG").write_bytes(prg); (args.output/"io-auto.st").write_bytes(image)
    record=schedule()|{"prg_sha256":hashlib.sha256(prg).hexdigest(),"disk_sha256":hashlib.sha256(image).hexdigest(),"symbols":symbols}
    (args.output/"schedule.json").write_text(json.dumps(record,indent=2)+"\n")
    print(json.dumps({k:record[k] for k in ("prg_sha256","disk_sha256")},sort_keys=True))
if __name__=="__main__": main()
