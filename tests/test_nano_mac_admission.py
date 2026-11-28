import gzip
import hashlib
import importlib.util
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('mac_admission', ROOT / 'platform/embedded/nano/mac_spl/verify_admission.py')
replay = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(replay)
ID = 'ad310001ad310001'
RECEIPT = dict(scope='de25-cbl-post-ddr-admission-v1', status='prepared',
               parking_revision='87b2d7f4721c819b554212f2b80c193a07a2531e',
               historical_trace='87b2d7f4721c819b554212f2b80c193a07a2531e', diagnostic_id=ID)
ROWS = (
    'CPU pass={} el=00000003 mpidr=0000000081000000 sctlr=0000000030c50830 daif=00000000000003c0 frequency=17d78400 scope=00000001 bd=00000001 bank_base=0000000080000000 bank_size=0000000040000000 ram_size=0000000040000000',
    'BOARD pass={} cause=00000004 per0=ff7fbfff bridge=0000004f timeouts=00000000 tsn=00000003 route=00000030 sid=00000000',
    'CLOCK pass={} stat=02030300 main_en=00000f5e main_bypass=00000000 noc_clk=00000000 noc_div=28240098 per_en=0fffffff per_bypass=00000000 emac_sel=00000000 counter_rst=00000000',
    'SMMU pass={} read=00000001 idr0=080f7e3f idr1=0e739d18 idr5=00400075 cr0=00000000 ack=00000000 secure_cr0=00000000 secure_ack=00000000 cr1=00000000 cr2=00000004 gbpa=00100000 irq=00000000 gerror=00000000 gerrorn=00000000 cmdprod=00000000 cmdcons=00000000 evtprod=00000000 evtcons=00000000',
    'SUPPLY pass={} read=00000001 sdm=00000000 l4sp=05f5e100 emac=07735940 mailbox=00000000 words=00000006 w0=00000000 w1=001a0300 w2=c0000089 w3=00000003 w4=00000000 w5=0000000f',
)


def fixture():
    # Retain the actual matched CPU/DDR framing. All new snapshot fields here are a host fixture.
    wire = gzip.decompress((ROOT / 'docs/validation/2026-11-27-de25-native-ethernet/tx04-uart.gz').read_bytes())
    records = ['FES_CBL_MAC_ADMIT BEGIN id=' + ID]
    for index in range(2):
        records += ['FES_CBL_MAC_ADMIT ' + row.format(index) for row in ROWS]
    records += ['FES_CBL_MAC_ADMIT END stable=1 held=1 eligible=1']
    lines = wire.decode('ascii').replace('93604db663af4e1a', ID).splitlines()
    start = next(i for i, row in enumerate(lines) if row.startswith('FES_CBL_MAC_TX BEGIN'))
    lines[start:start + 4] = records
    lines[-1] = 'FES_CBL_MAC_ADMIT PARK no-storage no-bl31'
    return ('\r\n'.join(lines) + '\r\n').encode('ascii')


class Admission(unittest.TestCase):
    def test_stable_results_and_all_six_response_words(self):
        raw = fixture()
        result = replay.decode_admission(raw, RECEIPT)
        self.assertTrue(result['stable'] and result['held'] and result['eligible'] and result['parked'])
        self.assertEqual(result['uart_sha256'], hashlib.sha256(raw).hexdigest())
        self.assertEqual(result['snapshots'][0]['SUPPLY']['w5'], 15)
        self.assertEqual(result['failures'][0], {'held': [], 'eligible': []})

    def test_denied_snapshots_distinguish_custody_from_initialization(self):
        raw = fixture().replace(b'eligible=1', b'eligible=0')
        init_incomplete = raw.replace(b' w3=00000003', b' w3=00000001').replace(b' sdm=00000000', b' sdm=10000000')
        result = replay.decode_admission(init_incomplete, RECEIPT)
        self.assertTrue(result['held'])
        self.assertFalse(result['eligible'])
        self.assertEqual(result['failures'][1]['eligible'], ['SDM INIT_DONE', 'vendor SDM status'])
        for old, new, reason in ((b'daif=00000000000003c0', b'daif=00000000000002c0', 'CPU exception masks'),
                                 (b'bank_base=0000000080000000', b'bank_base=0000000000000000', 'runtime DDR bank'),
                                 (b'emac=07735940', b'emac=00000000', 'EMAC clock rate'),
                                 (b'counter_rst=00000000', b'counter_rst=00000001', 'EMAC clock source')):
            data = replay.decode_admission(raw.replace(old, new), RECEIPT)
            self.assertTrue(data['held'])
            self.assertEqual(data['failures'][0]['eligible'], [reason])
        dirty = raw.replace(b' gerror=00000000', b' gerror=00000002').replace(b'held=1', b'held=0')
        result = replay.decode_admission(dirty, RECEIPT)
        self.assertFalse(result['held'])
        self.assertEqual(result['failures'][1]['held'], ['SMMU disabled with empty queues'])

    def test_each_word_of_both_snapshots_participates_in_stability(self):
        raw = fixture().replace(b'w5=0000000f', b'w5=00000000', 1)
        with self.assertRaises(ValueError):
            replay.decode_admission(raw, RECEIPT)
        raw = raw.replace(b'stable=1 held=1 eligible=1', b'stable=0 held=0 eligible=0')
        result = replay.decode_admission(raw, RECEIPT)
        self.assertFalse(result['stable'] or result['held'] or result['eligible'])

    def test_unread_smmu_and_suppliers_must_match_clock_gate(self):
        import re
        raw = fixture().replace(b'stat=02030300', b'stat=02010100')
        raw = raw.replace(b'held=1 eligible=1', b'held=0 eligible=0')
        with self.assertRaises(ValueError):
            replay.decode_admission(raw, RECEIPT)
        zero = []
        for row in raw.splitlines(keepends=True):
            if row.startswith((b'FES_CBL_MAC_ADMIT SMMU', b'FES_CBL_MAC_ADMIT SUPPLY')):
                row = re.sub(rb'=[0-9a-f]{8}\b', b'=00000000', row)
            zero.append(row)
        data = replay.decode_admission(b''.join(zero), RECEIPT)
        self.assertTrue(data['stable'])
        self.assertFalse(data['held'])

    def test_incomplete_or_foreign_evidence_rejected(self):
        raw = fixture()
        for old, new in ((b'stage=24 value=00000023', b'stage=24 value=00000000'),
                         (b'stage=31 value=00000000', b'stage=31 value=90000021'),
                         (b'mpidr=0000000081000000 el=3', b'mpidr=0000000081000100 el=3'),
                         (b'FES_CBL_MAC_ADMIT CPU pass=0', b'FES_CBL_MAC_ADMIT CPU pass=1'),
                         (b'scope=00000001', b'scope=00000000'),
                         (b'SMMU pass=1 read=00000001', b'SMMU pass=1 read=00000000'),
                         (b'w5=0000000f', b'w5=f'), (b'words=00000006', b'words=00000005'),
                         (b' w3=00000003', b' w3=0000000b'),
                         (b'FES_CBL_MAC_ADMIT PARK ', b'FES_CBL_MAC_TX PARK '),
                         (b'DDR: init success', b'DDR: initialization pending'),
                         (b'DDR: init success', b'DDR: init success\r\nFES_CBL_MAC_TX BEGIN'),
                         (b'DDR: init success', b'Trying to boot from MMC1\r\nDDR: init success')):
            with self.subTest(old=old), self.assertRaises(ValueError):
                replay.decode_admission(raw.replace(old, new), RECEIPT)
        for name in ('scope', 'status', 'parking_revision', 'historical_trace', 'diagnostic_id'):
            with self.subTest(name=name), self.assertRaises(ValueError):
                replay.decode_admission(raw, {**RECEIPT, name: 'ffff'})
