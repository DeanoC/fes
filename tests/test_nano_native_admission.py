"""Fresh input snapshots preserve full validation without repeating derivation."""
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('nano_native_admission',ROOT/'platform/embedded/nano/native_admission.py')
admission=importlib.util.module_from_spec(spec);spec.loader.exec_module(admission)
sha=lambda p:hashlib.sha256(Path(p).read_bytes()).hexdigest()


class Model:
    def __init__(self,root,scope=admission.SCOPE):
        self.scope=scope
        self.root=root;self.uboot=root/'out/uboot';self.entry=root/'out/entry';self.record=root/'out/admission.json';self.calls=0
        def write(path,raw):path.parent.mkdir(parents=True,exist_ok=True);path.write_bytes(raw)
        for name in admission.CODE_TREES:write(root/name/'checker.py',b'fixture code')
        for name in ('producer.py','runtime.go','firmware.c'):write(root/name,name.encode())
        self.projects=[root/'out/computer',root/'out/menu']
        for p in self.projects:
            write(p/'board/vendor.zip',b'fixture vendor archive')
            write(p/'board/output.sof',b'fixture compiled artifact')
            write(p/'board/hps/build-inputs.json',json.dumps({'source_roots':['sources/misteross/core','sources/mister-packages/abi.yaml']}).encode())
        write(root/'sources/misteross/core/core.sv',b'fixture RTL')
        write(root/'sources/mister-packages/abi.yaml',b'fixture ABI')
        u=dict(native_handoff=dict(producer_sources={'firmware.c':sha(root/'firmware.c')}))
        e=dict(target='de25-nano',native_program='fpgaload',hps_project=str(self.projects[0]),hps_menu_project=str(self.projects[1]),
               sources={'producer.py':sha(root/'producer.py')},native_build_sources={'runtime.go':sha(root/'runtime.go')},
               ram_start=0x84000000,ram_end=0x94000000)
        if scope==admission.USB_SCOPE:
            context={'package_id':'a'*64};pins={'handoff_sha256':'b'*64}
            e.update(native_program='usbcheck',usb_control=True,usb_shared=False,usb_input='gamepad',
                     hps_menu_project=None,hps_context=context,hps_io=pins)
            u['native_handoff']['cold_boot_profile']=dict(usb_probe=True,usb_pio=True,usb_shared=False,
                usb_input='gamepad',sd_pio=False,hps_context=context,hps_io=pins)
        write(self.uboot/'result.json',json.dumps(u).encode());write(self.entry/'result.json',json.dumps(e).encode())
        for p in (*admission._paths(root,self.uboot,self.entry,self.scope),self.uboot/'build/.config',self.entry/'payload.elf'):write(p,b'fixture '+p.name.encode())
        self.extra=root/'out/operator.py';write(self.extra,b'fixture frozen caller')
    def value(self):
        fit,spl,image,profile=admission._paths(self.root,self.uboot,self.entry,self.scope)
        return fit,spl,image,json.loads((self.uboot/'result.json').read_text()),json.loads((self.entry/'result.json').read_text()),profile
    def validate(self,u,e):
        self.calls+=1
        if (u,e)!=(self.uboot,self.entry):raise AssertionError('selection changed')
        value=self.value()
        if (value[4]['ram_start'],value[4]['ram_end'])!=(0x84000000,0x94000000):raise ValueError('full validator rejected memory geometry')
        return value
    def prepare(self,validate=None,output=None):
        self.digest=admission.prepare(self.root,self.uboot,self.entry,output or self.record,validate=validate or self.validate,extra_inputs=[self.extra],scope=self.scope)
        return self.digest
    def load(self,**kw):
        return admission.load(self.root,self.uboot,self.entry,self.record,expected_sha256=kw.pop('expected_sha256',self.digest),owns=kw.pop('owns',lambda:True),extra_inputs=kw.pop('extra_inputs',[self.extra]),scope=kw.pop('scope',self.scope),**kw)


class NativeAdmissionTests(unittest.TestCase):
    def test_full_validation_once_then_same_tuple_with_fresh_hashes(self):
        with tempfile.TemporaryDirectory() as temp:
            m=Model(Path(temp));m.prepare();self.assertEqual(m.calls,1)
            self.assertEqual(m.load(),m.value());self.assertEqual(m.calls,1)
            returned=m.load();returned[3]['native_handoff'].clear()
            self.assertEqual(m.load(),m.value());self.assertEqual(m.calls,1)
            with self.assertRaises(FileExistsError):m.prepare()
            self.assertEqual(m.calls,1)

    def test_changed_artifact_source_checker_and_extra_input_are_rejected(self):
        names=['out/uboot/build/u-boot.itb','out/uboot/build/spl/u-boot-spl-dtb.hex','out/entry/entry.img','out/entry/payload.elf','out/uboot/build/.config',
               'platform/embedded/nano/sd-profile.json','runtime.go','producer.py','firmware.c','platform/embedded/firmware/checker.py',
               'sources/misteross/core/core.sv','sources/mister-packages/abi.yaml','out/computer/board/vendor.zip','out/menu/board/output.sof','out/operator.py']
        for name in names:
            with self.subTest(name=name),tempfile.TemporaryDirectory() as temp:
                m=Model(Path(temp));m.prepare();(m.root/name).write_bytes(b'changed')
                with self.assertRaises(ValueError):m.load()
                self.assertEqual(m.calls,1)

    def test_tree_membership_changes_and_symlinks_fail_closed(self):
        for mode in ('add-project','add-source','delete','symlink'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as temp:
                m=Model(Path(temp));m.prepare()
                path=m.projects[0]/'board/new.sv' if mode=='add-project' else m.root/'sources/misteross/core/new.sv'
                if mode.startswith('add'):path.write_bytes(b'new input')
                else:
                    path=m.projects[0]/'board/output.sof';path.unlink()
                    if mode=='symlink':path.symlink_to(m.extra)
                with self.assertRaises((ValueError,FileNotFoundError)):m.load()

    def test_receipt_and_memory_changes_cannot_reuse_validation(self):
        for field,value in [('ram_start',0x95000000),('hps_menu_project','/tmp/foreign'),('native_build_sources',{})]:
            with self.subTest(field=field),tempfile.TemporaryDirectory() as temp:
                m=Model(Path(temp));m.prepare();p=m.entry/'result.json';e=json.loads(p.read_text());e[field]=value;p.write_text(json.dumps(e))
                with self.assertRaises((ValueError,FileNotFoundError)):m.load()

    def test_full_validator_rejection_and_during_validation_mutation_create_no_record(self):
        for mode in ('reject','mutate','wrong-result'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as temp:
                m=Model(Path(temp))
                def validate(u,e):
                    value=m.validate(u,e)
                    if mode=='reject':raise ValueError('injected full-check failure')
                    if mode=='mutate':(m.projects[0]/'board/output.sof').write_bytes(b'changed during validation')
                    if mode=='wrong-result':value=list(value);value[4]=dict(value[4],ram_start=0)
                    return value
                with self.assertRaises(ValueError):m.prepare(validate)
                self.assertFalse(m.record.exists())

    def test_invalid_memory_geometry_still_runs_the_full_validator(self):
        with tempfile.TemporaryDirectory() as temp:
            m=Model(Path(temp));p=m.entry/'result.json';e=json.loads(p.read_text());e['ram_end']=0x96000000;p.write_text(json.dumps(e))
            with self.assertRaisesRegex(ValueError,'memory geometry'):m.prepare()
            self.assertEqual(m.calls,1);self.assertFalse(m.record.exists())

    def test_frozen_digest_and_record_authority_are_checked(self):
        for mode in ('digest','extra-key','live-authority','wrong-path','omitted-input','omitted-tree'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as temp:
                m=Model(Path(temp));m.prepare()
                if mode=='digest':
                    with self.assertRaises(ValueError):m.load(expected_sha256='0'*64)
                    continue
                r=json.loads(m.record.read_text())
                if mode=='extra-key':r['grant']='hardware'
                if mode=='live-authority':r['live_authority']=True
                if mode=='wrong-path':r['checked']['paths'][0]=str(m.extra)
                if mode=='omitted-input':r['inputs']['files'].pop(str(m.root/'runtime.go'))
                if mode=='omitted-tree':r['inputs']['trees'].pop(str(m.projects[0]))
                m.record.write_text(json.dumps(r));m.digest=sha(m.record)
                with self.assertRaises(ValueError):m.load()

    def test_claim_loss_before_or_after_input_checks_and_selection_mismatch(self):
        with tempfile.TemporaryDirectory() as temp:
            m=Model(Path(temp));m.prepare()
            with self.assertRaises(ValueError):m.load(owns=lambda:False)
            calls=[]
            def owns():calls.append(True);return len(calls)==1
            with self.assertRaisesRegex(ValueError,'after input checks'):m.load(owns=owns)
            self.assertEqual(len(calls),2)
            with self.assertRaises(ValueError):admission.load(m.root,m.entry,m.uboot,m.record,expected_sha256=m.digest,owns=lambda:True)

    def test_record_cannot_enter_its_own_source_closure_or_omit_operator_inputs(self):
        with tempfile.TemporaryDirectory() as temp:
            m=Model(Path(temp))
            with self.assertRaises(ValueError):m.prepare(output=m.projects[0]/'record.json')
            self.assertEqual(m.calls,0)
            m.prepare()
            with self.assertRaises(ValueError):m.load(extra_inputs=[])

    def test_explicit_external_tool_is_also_rehashed(self):
        with tempfile.TemporaryDirectory() as temp, tempfile.TemporaryDirectory() as tools:
            m=Model(Path(temp));tool=Path(tools)/'pinned-tool';tool.write_bytes(b'qualified tool')
            extra=[m.extra,tool]
            digest=admission.prepare(m.root,m.uboot,m.entry,m.record,validate=m.validate,extra_inputs=extra)
            self.assertEqual(admission.load(m.root,m.uboot,m.entry,m.record,expected_sha256=digest,owns=lambda:True,extra_inputs=extra),m.value())
            tool.write_bytes(b'changed tool')
            with self.assertRaises(ValueError):admission.load(m.root,m.uboot,m.entry,m.record,expected_sha256=digest,owns=lambda:True,extra_inputs=extra)


class USBProbeAdmissionTests(unittest.TestCase):
    def test_usb_scope_is_explicit_and_preserves_the_checked_tuple_and_single_project(self):
        with tempfile.TemporaryDirectory() as temp:
            m=Model(Path(temp),admission.USB_SCOPE)
            with self.assertRaises(ValueError):
                admission.prepare(m.root,m.uboot,m.entry,m.record,validate=m.validate,extra_inputs=[m.extra])
            self.assertEqual(m.calls,0)
            m.prepare();self.assertEqual(m.load(),m.value());self.assertEqual(m.calls,1)
            r=json.loads(m.record.read_text());self.assertEqual(r['scope'],admission.USB_SCOPE)
            self.assertFalse(r['live_authority'])
            self.assertIn(str(m.projects[0]),r['inputs']['trees'])
            self.assertNotIn(str(m.projects[1]),r['inputs']['trees'])
            self.assertEqual(Path(r['checked']['paths'][-1]).name,'probe-profile.json')
            with self.assertRaises(ValueError):m.load(scope=admission.SCOPE)

    def test_scope_tag_cannot_convert_usb_and_core_records_even_with_recomputed_digest(self):
        for before,after in ((admission.USB_SCOPE,admission.SCOPE),(admission.SCOPE,admission.USB_SCOPE)):
            with self.subTest(before=before),tempfile.TemporaryDirectory() as temp:
                m=Model(Path(temp),before);m.prepare();r=json.loads(m.record.read_text());r['scope']=after
                m.record.write_text(json.dumps(r));m.digest=sha(m.record)
                with self.assertRaises(ValueError):m.load(scope=after)

    def test_mixed_entry_and_cold_capabilities_reject_before_full_validation(self):
        cases=[('entry','usb_shared',True),('entry','usb_control',False),('entry','usb_input',None),
               ('entry','native_program','fpgaload'),('entry','selected_programs',True),
               ('entry','hps_rom',{}),('entry','hps_rom_sha256','a'*64),
               ('entry','hps_menu_project','out/menu'),('entry','hps_context',{}),
               ('cold','usb_probe',False),('cold','usb_probe',1),('cold','usb_pio',False),
               ('cold','sd_pio',True),('cold','usb_shared',True),('cold','hps_computer',{}),
               ('cold','hps_io',{'different':'pins'})]
        for side,key,value in cases:
            with self.subTest(side=side,key=key),tempfile.TemporaryDirectory() as temp:
                m=Model(Path(temp),admission.USB_SCOPE)
                p=(m.entry if side=='entry' else m.uboot)/'result.json';r=json.loads(p.read_text())
                target=r if side=='entry' else r['native_handoff']['cold_boot_profile'];target[key]=value
                p.write_text(json.dumps(r))
                with self.assertRaises(ValueError):m.prepare()
                self.assertEqual(m.calls,0);self.assertFalse(m.record.exists())

    def test_usb_source_artifact_profile_membership_and_operator_changes_invalidate(self):
        for name in ('out/entry/payload.elf','platform/embedded/nano/probe-profile.json',
                     'out/computer/board/vendor.zip','sources/misteross/core/core.sv','out/operator.py','new-source'):
            with self.subTest(name=name),tempfile.TemporaryDirectory() as temp:
                m=Model(Path(temp),admission.USB_SCOPE);m.prepare()
                p=m.root/name if name!='new-source' else m.projects[0]/'board/new.sv';p.write_bytes(b'changed')
                with self.assertRaises(ValueError):m.load()

    def test_full_usb_validation_failure_wrong_profile_and_during_check_mutation_leave_no_record(self):
        for mode in ('reject','profile','mutate'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as temp:
                m=Model(Path(temp),admission.USB_SCOPE)
                def validate(u,e):
                    value=list(m.validate(u,e))
                    if mode=='reject':raise ValueError('full USB validator rejected')
                    if mode=='profile':value[-1]=m.root/'platform/embedded/nano/sd-profile.json'
                    if mode=='mutate':(m.projects[0]/'board/output.sof').write_bytes(b'changed')
                    return value
                with self.assertRaises(ValueError):m.prepare(validate)
                self.assertFalse(m.record.exists())

    def test_usb_claim_must_survive_fresh_hash_checks(self):
        with tempfile.TemporaryDirectory() as temp:
            m=Model(Path(temp),admission.USB_SCOPE);m.prepare()
            with self.assertRaises(ValueError):m.load(owns=lambda:False)
            calls=[]
            def owns():calls.append(True);return len(calls)==1
            with self.assertRaisesRegex(ValueError,'after input checks'):m.load(owns=owns)
