"""Sealed DDR state and explicit loader reservation fail closed on drift."""
import copy
import hashlib
from pathlib import Path
import struct
import sys
import unittest
from unittest.mock import patch

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/firmware'))
import nano_firmware_percpu as placement
import nano_firmware_ram as ram
from test_nano_firmware_selection import elf,profile


class PerCPUPlacementTests(unittest.TestCase):
    def test_exact_source_transform_and_original_declaration_inverse(self):
        original='''#include <platform_def.h>
#include <lib/cassert.h>
#include <lib/el3_runtime/cpu_data.h>
cpu_data_t percpu_data[PLATFORM_CORE_COUNT];
'''
        with self.assertRaises(ValueError):placement.source(original)
        with patch.object(placement,'SOURCE_SHA',hashlib.sha256(original.encode()).hexdigest()):
            output=placement.source(original)
        self.assertIn('section(".data.fes_nano_percpu")',output)
        self.assertIn(' = {0};',output)
        self.assertIn('offsetof(cpu_data_t, psci_svc_cpu_data.aff_info_state)',output)
        self.assertEqual(output.count('CASSERT('),6)
        missing=original.replace('cpu_data_t percpu_data','cpu_data_t changed')
        with patch.object(placement,'SOURCE_SHA',hashlib.sha256(missing.encode()).hexdigest()):
            with self.assertRaises(ValueError):placement.source(missing)

    def test_loaded_array_must_be_aligned_initialized_writable_and_disjoint(self):
        image,locations=elf();table=ram.symbols(image)
        placement.validate(image,table)
        changes=[('percpu_data',0x58000,256),('percpu_data',0x80000088,256),
                 ('percpu_data',0x80000080,255),('percpu_data',0x80000080,512),
                 ('percpu_data',0x80000040,256),('__DATA_START__',0x800000c0,0),
                 ('__DATA_END__',0x80000100,0)]
        for name,address,size in changes:
            changed=copy.deepcopy(table);changed[name]=(address,size)
            with self.subTest(name=name,address=address,size=size),self.assertRaises(ValueError):placement.validate(image,changed)
        for offset,value,fmt in ((0x280,1,'B'),(0x37f,1,'B'),(124,5,'I'),(152,0x80,'Q')):
            changed=bytearray(image);struct.pack_into('<'+fmt,changed,offset,value)
            with self.subTest(offset=offset),self.assertRaises(ValueError):placement.validate(changed,table)
        for name in ('percpu_data','__DATA_START__','__DATA_END__'):
            changed=copy.deepcopy(table);del changed[name]
            with self.assertRaises(ValueError):placement.validate(image,changed)

    def test_profile_requires_percpu_placement_and_bounded_bss(self):
        good=profile();good['firmware_ram']=dict(ram.CONTRACT)
        self.assertEqual(ram.checked(good)['format'],5)
        self.assertEqual(ram.CONTRACT['bss_limit'],0x60000)
        for delta in (dict(format=3,scope='matched-spl-el3-ocram-nobits-v3'),dict(bss_end=0x5dec0),
                      dict(percpu_policy=None),dict(percpu_bytes=255),dict(percpu_alignment=8)):
            changed=copy.deepcopy(good);changed['firmware_ram'].update(delta)
            with self.assertRaises(ValueError):ram.checked(changed)


if __name__=='__main__':unittest.main()
