"""Raw observer framing never grants route or DMA authority."""
import unittest

from test_nano_paired_tenfoot_trace import Model
import capability_trace
import remap_trace
import test_nano_mmu_boot as mapped_boot
from test_nano_native_trace import OWNER, EPOCH


def fixture():
    regs = ' por1=4 bypass=0 ctrl=3 ids=a000a ar=0 aw=0 access=unqualified\n'
    return ('FES_NANO_START phase=12 status=0\n'+
        'FES_NANO_REMAP kind=1 phase=0 operand=0'+regs+
        'FES_NANO_REMAP kind=1 phase=1 operand=0'+regs+
        'FES_NANO_START phase=13 status=0\n'+
        'FES_NANO_CORE_TRANSFER phase=write bytes=65536\n'+
        'FES_NANO_REMAP kind=2 phase=0 operand=94000000'+regs+
        'FES_NANO_REMAP kind=2 phase=1 operand=14000000'+regs+
        'FES_NANO_SDM_SUBMIT phase=before address=14000000 bytes=10000'+regs+
        'FES_NANO_SDM_RESULT status=0\n'+
        'FES_NANO_CORE_TRANSFER phase=complete bytes=65536\n').encode()


class RemapTraceTests(unittest.TestCase):
    def test_complete_cold_capability_observer_requires_explicit_selection_and_order(self):
        raw = mapped_boot.base_trace().replace('U-Boot 2025.01-fes-nano-',
            mapped_boot.transition()+'U-Boot 2025.01-fes-nano-', 1)
        smmu = ('NOTICE:  FES_NANO_SMMU status=0 reads=26 idr0=80f7e3f idr1=e739d18 '
            'aidr=1 s_idr1=80000018 cr0=0 ack=0 gbpa=0 agbpa=0 s_cr0=0 s_ack=0 '
            's_init=0 s_gbpa=0 s_agbpa=0 identified=1 settled=1 bypass_controls_clear=1 access=unqualified\r\n')
        values = dict.fromkeys(capability_trace.FIELDS, 0)
        values.update(idr0=0x80f7e3f, idr1=0xe739d18)
        capability = (f'NOTICE:  FES_NANO_CAPABILITY status=0 reads=26 owner={OWNER:x} epoch={EPOCH:x} '+
            ' '.join(key+'='+format(values[key], 'x') for key in capability_trace.FIELDS)+
            ' access=unqualified\r\n')
        anchor = 'NOTICE:  FES_NANO_OWNED_BOOT '
        raw = raw.replace(anchor, smmu+capability+anchor, 1)
        def decode(data, selected=True):
            return mapped_boot.trace.decode_native_boot(data, mapped_boot.BUILD, mapped_boot.SEED,
                atf=mapped_boot.atf_descriptor('1'*64, 90173), firmware_ram_trace=True,
                firmware_mmu=mapped_boot.proof(), capability_observe=selected)
        result = decode(raw)
        self.assertTrue(result['complete'], result['errors'])
        self.assertFalse(result['capability_boot_smmu']['access_qualified'])
        self.assertFalse(result['capability_observation']['DMA_access_granted'])
        for changed in (raw.replace(capability, ''), raw.replace(smmu, ''),
                raw.replace(capability, capability*2), raw.replace(smmu, smmu*2),
                raw.replace(smmu+capability, capability+smmu),
                raw.replace(capability, capability.replace('reads=26', 'reads=25')),
                raw.replace(capability, capability.replace(f'owner={OWNER:x}', 'owner=1')),
                raw.replace(capability, capability.replace('idr0=80f7e3f', 'idr0=1'))):
            with self.subTest(tail=changed[-100:]):
                self.assertFalse(decode(changed)['complete'])
        self.assertFalse(decode(raw, False)['complete'])

    def test_exact_context_preserves_line_positions_and_grants_no_authority(self):
        raw = fixture()
        cleaned, result = remap_trace.extract(raw, required=True, configuration_scope=True)
        self.assertEqual(len(cleaned.splitlines()), len(raw.splitlines()))
        self.assertTrue(result['complete'])
        self.assertTrue(result['vendor_operand_subtraction_matches'])
        self.assertTrue(result['submission_starts_at_transformed_pointer'])
        for key in ('observations_atomic', 'source_attested', 'SDM_route_qualified',
                    'DMA_access_granted', 'independent_drain'):
            self.assertFalse(result[key])
        self.assertIsNone(result['SDM_tcu_input'])
        self.assertEqual(remap_trace.extract(cleaned), (cleaned, None))

    def test_missing_duplicate_partial_overflow_and_out_of_scope_lines_reject(self):
        raw = fixture()
        for changed in (raw.replace(b'FES_NANO_SDM_RESULT status=0\n', b''),
                raw+b'FES_NANO_SDM_RESULT status=0\n',
                raw.replace(b'phase=1 operand=0', b'phase=0 operand=0'),
                raw.replace(b'status=0\nFES_NANO_CORE_TRANSFER phase=complete',
                            b'status=2147483648\nFES_NANO_CORE_TRANSFER phase=complete'),
                raw.replace(b'FES_NANO_START phase=12 status=0\n', b''),
                raw.replace(b'access=unqualified\n', b'access=unqualified', 1)):
            with self.subTest(raw=changed), self.assertRaises(ValueError):
                remap_trace.extract(changed, required=True, configuration_scope=True)
        with self.assertRaises(ValueError):
            remap_trace.extract(b'', required=True)

    def test_unselected_or_repeated_remap_diagnostics_reject_in_actual_client(self):
        model = Model()
        with self.assertRaises(ValueError):
            model.client(initial=fixture()+model.initial)
        client = model.client()
        with self.assertRaises(ValueError):
            client._observations(fixture())

    def test_capability_owner_epoch_count_and_identity_remain_independent(self):
        smmu = dict(idr0=1, idr1=2)
        values = dict(status=0, reads=26, owner=3, epoch=4, **smmu)
        self.assertEqual(capability_trace.errors(values, smmu, 3, 4), [])
        for key in ('status', 'reads', 'owner', 'epoch', 'idr0', 'idr1'):
            changed = dict(values)
            changed[key] += 1
            with self.subTest(key=key):
                self.assertTrue(capability_trace.errors(changed, smmu, 3, 4))
        self.assertTrue(capability_trace.errors(values, None, 3, 4))
