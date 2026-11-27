"""Cross-check both UART decoders against the native KSZ9031 FLP transcript."""
import importlib.util
from pathlib import Path
import unittest

from test_nano_mac_bidirectional_replay import BIDI, IDENTITY, trial, wire

path = Path(__file__).resolve().parents[1] / 'platform/embedded/nano/mac_spl/verify_positive.py'
spec = importlib.util.spec_from_file_location('fes_flp_tx_test', path)
TX = importlib.util.module_from_spec(spec)
spec.loader.exec_module(TX)

journal = (f"FES_CBL_TX_FLP id={IDENTITY} portal=1 pointers=1 snapshot=1 attempted=1 applied=1 restart=1 restored=1 "
           "selector=401c pointer=0023 dev0_pointer=0003 bmcr=1140 old_hi=0003 old_lo=4000 "
           "set_hi=0006 set_lo=1a80 final_hi=0003 final_lo=4000 final_selector=401c final_bmcr=1140")
untouched = (f"FES_CBL_TX_FLP id={IDENTITY} portal=0 pointers=0 snapshot=0 attempted=0 applied=0 restart=0 restored=0 "
             "selector=0000 pointer=0000 dev0_pointer=0000 bmcr=0000 old_hi=0000 old_lo=0000 "
             "set_hi=0000 set_lo=0000 final_hi=0000 final_lo=0000 final_selector=0000 final_bmcr=0000")


def flp_block(rows, us):
    out = []
    for line in rows:
        if line.startswith('FES_CBL_TX_PHY') and ' sample=0 ' in line:
            for index, stamp in ((2, us-10000), (3, us)):
                out.append(line.replace('TX_PHY', 'TX_TRAIN').replace('sample=0 ', f'sample={index} ')
                           .replace(f'us={us} ', f'us={stamp} '))
        if line.startswith('FES_CBL_MAC_TX END'):
            out.append(journal)
        out.append(line)
    return out


class PhySetupReplay(unittest.TestCase):
    def cases(self):
        head, (first, second), tail, receipt = trial()
        one = flp_block(first, 1000000)
        two = flp_block(second, 2000000)
        receipt = dict(receipt, phy_flp_16ms=True)
        yield BIDI, wire(head + one + two + tail), receipt
        only_tx = [x for x in one if not x.startswith('FES_CBL_BIDI')]
        yield TX, wire(head + only_tx + tail[:-1] + [TX.PARK]), dict(receipt, scope='de25-cbl-positive-mac-spl-v1')

    def test_timing_changes_and_restoration(self):
        for decoder, raw, receipt in self.cases():
            with self.subTest(scope=receipt['scope']):
                out = decoder.decode_positive(raw, receipt)['phy_flp']
                p = out[0] if isinstance(out, list) else out
                self.assertEqual((p['attempted'], p['restored'], len(p['training'])), (1, 1, 2))
                unchanged = (raw.replace(b'attempted=1', b'attempted=0')
                             .replace(b'hi=0003', b'hi=0006').replace(b'lo=4000', b'lo=1a80'))
                decoder.decode_positive(unchanged, receipt)
                for old, new in (
                    (b'portal=1', b'portal=0'), (b'pointers=1', b'pointers=0'), (b'snapshot=1', b'snapshot=0'),
                    (b'attempted=1', b'attempted=0'), (b'applied=1', b'applied=0'), (b'restart=1', b'restart=0'),
                    (b'restored=1', b'restored=0'), (b'old_hi=0003', b'old_hi=0006'), (b'set_lo=1a80', b'set_lo=4000'),
                    (b'final_selector=401c', b'final_selector=4000'), (b'final_bmcr=1140', b'final_bmcr=1940'),
                    (b'final_lo=4000', b'final_lo=1a80'), (b'sample=2 ', b'sample=3 '),
                    (b'us=990000 ', b'us=1000000 '), (b'TX_TRAIN', b'TX_OTHER'),
                    (b'ter=0023', b'ter=bad!'), (b'sample=3 us=1000000 id1=0022', b'sample=3 us=1000000 id1=0023'),
                    (b'adv=01e1', b'adv=0021'),
                ):
                    with self.subTest(old=old), self.assertRaises(ValueError):
                        decoder.decode_closed(raw.replace(old, new, 1), receipt)
                with self.assertRaises(ValueError):
                    decoder.decode_closed(raw, {k:v for k,v in receipt.items() if k != 'phy_flp_16ms'})
                without = b'\r\n'.join(row for row in raw.split(b'\r\n') if not row.startswith(b'FES_CBL_TX_FLP'))
                with self.assertRaises(ValueError):
                    decoder.decode_closed(without, receipt)

    def test_refusal_before_mdio(self):
        for decoder, raw, receipt in self.cases():
            rows = raw.decode().splitlines()
            # Keep a released MAC but reject before PHY configuration or DMA.
            first = next(i for i,r in enumerate(rows) if r.startswith('FES_CBL_TX_TRAIN'))
            end = next(i for i,r in enumerate(rows) if r.startswith('FES_CBL_MAC_TX END'))
            terminal = rows[end]
            for old, new in (('rc=0 stage=8','rc=-1 stage=4'), ('possible=1 dispatch=1 tps=1','possible=0 dispatch=0 tps=0'),
                             ('disposition=closed','disposition=revoked-undispatched'), ('desc=3000003c dma=00000001','desc=00000000 dma=00000000'),
                             ('phy=0f49','phy=0000')):
                terminal = terminal.replace(old, new)
            rows[first:end+1] = [untouched, terminal]
            if decoder is BIDI:
                i = next(i for i,r in enumerate(rows) if r.startswith('FES_CBL_BIDI RX'))
                power = next(i for i,r in enumerate(rows) if ' stage=30 ' in r)
                rows[i:power] = [f'FES_CBL_BIDI RX id={IDENTITY} round=1 possible=0 armed=0 matched=0 stopped=0 retained=0 failure=0 desc=00000000 channel=00000000 mac=00000000 mtl=00000000 dma=00000000 ch=00000000 dwell_us=0 reply=-',
                                 f'FES_CBL_BIDI ROUND id={IDENTITY} round=1 vmid=1 cmd=12 success=0']
            self.assertFalse(decoder.decode_closed(wire(rows), receipt)['target_succeeded'])
            with self.assertRaises(ValueError):
                decoder.decode_closed(wire(rows).replace(b'portal=0', b'portal=1'), receipt)
