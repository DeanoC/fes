"""Real boot/client/cycle/replay selection for the two native role workers.

This is the UART observer half of the adapter, with injectable exchange. It
opens no UART and replaces no file/source or live transport admission checks.
"""
import copy,hashlib,importlib.util,json,re,sys,types
from pathlib import Path
ROOT=Path('/home/deano/fes/out/dev/de25-embedded-os')
def require(v,message):
 if not v:raise ValueError(message)
def sha(p):
 with Path(p).open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()
def load(name,path):
 spec=importlib.util.spec_from_file_location(name,path);module=importlib.util.module_from_spec(spec)
 sys.modules[name]=module;spec.loader.exec_module(module);return module

class Capture:
 def __init__(self,role,bundle,closure):
  require(role in ('native-diagnostic','native-baseline'),'native role required');bundle.verify()
  require(all(sha(p)==h for p,h in closure['files'].items()),'capture source/artifact bytes changed')
  plan_path=str(bundle.path)
  require(closure['files'].get(plan_path)==bundle.sha256,'capture plan differs')
  self.role,self.bundle=role,bundle;self.closure=copy.deepcopy(closure)
  selection=closure['selections'][role];u=json.loads((Path(selection['uboot'])/'result.json').read_text())
  e=json.loads((Path(selection['entry'])/'result.json').read_text());c=json.loads((Path(selection['composition'])/'result.json').read_text())
  self.manifest=c;self.uboot=u;self.entry=e;self.selected=role=='native-diagnostic'
  cold=u['native_handoff']['cold_boot_profile'];context=cold['hps_context']
  expected=bundle.plan['forward' if self.selected else 'rollback']['target_boot']['sha256']
  require(c['fit_sha256']==u['fit_sha256']==expected,'capture FIT/firmware role differs')
  require(c['native_boot_id']==u['native_handoff']['build_id'],'capture build differs')
  image=next(r['image'] for r in bundle.plan['roles'] if r['role']==role)
  if self.selected:
   require(image['kind']=='matched-native-sof' and c['sof_sha256']==image['sha256'],'capture complete native SOF differs')
   from image_selection import check as selected_image_check
   selected_image_check(bundle)
  else:require(image['kind']=='hps-phase1-rbf' and c['phase1_sha256']==image['sha256'],'capture baseline phase-one image differs')
  self.sdk=json.loads((Path(selection['sdk'])/'result.json').read_text())
  require(self.sdk['usb_shared'] is e['usb_shared'] is cold['usb_shared'] is True,'shared USB required')
  require(self.sdk['usb_input']==e['usb_input']==cold['usb_input']=='boot-keyboard','boot keyboard selection differs')
  require(self.sdk['hps_io']==e['hps_io']==cold['hps_io'],'HPS I/O differs')
  self.policy=dict(input='boot-keyboard',descriptor_sha256=hashlib.sha256(b'').hexdigest(),descriptor_bytes=0,
   hps_context=dict(sha256=e['hps_context_sha256'],package_id=e['hps_context']['package_id'],build_id=e['hps_context']['build_id']),hps_io=e['hps_io'])
  package=dict(package_id=context['package_id'],payload_sha256=context['core_sha256'],payload_bytes=context['core_size'],build_id=context['build_id'])
  self.boot_options=dict(build_id=u['native_handoff']['build_id'],owner_seed=cold['owner_seed'],atf=u['native_handoff']['atf_handoff'],
   firmware_ram_trace=True,firmware_mmu=u['firmware_mmu_proof'],boot_startup=u['native_handoff']['startup'],capability_observe=self.selected,
   package=package,hps_context=self.policy['hps_context'],hps_io=self.policy['hps_io'],usb_control=True,usb_shared=True)
  if self.selected:
   require(json.dumps(cold.get('sdm_input_transfer'),sort_keys=True)==json.dumps(closure['input_contract'],sort_keys=True) and cold['sdm_input_transfer']['format']==21 and cold['sdm_input_transfer']['scope']=='nano-source-rearm-terminal-cold-recovery-v21' and 'sdm_arm_probe' not in cold and 'sdm_fault64' not in cold,'explicit exact retained native continuation contract required')
   require(json.dumps(cold.get('boot_fit'),sort_keys=True)==json.dumps(closure['boot_fit'],sort_keys=True),'exact FIT16 boot memory contract required')
  if self.selected:require(self.sdk['configuration_retained'] is e['configuration_retained'] is cold['configuration_retained'] is True,'explicit retained arena contract required')
  self.client_options={k:copy.deepcopy(self.boot_options[k]) for k in ('build_id','owner_seed','atf','package','firmware_ram_trace','firmware_mmu','boot_startup','capability_observe')}
  self.client_options.update(build_record=self.sdk,shared_usb=self.policy,programs=bundle.plan['programs'],initial_titles=bundle.plan['initial_titles'])
  if self.selected:
   bound=copy.deepcopy(self.sdk);bound['sdm_reader_route']=copy.deepcopy(cold['sdm_reader_route'])
   require(bound['sdm_reader_route']==closure['reader_route'],'reader binding differs from the admitted closure')
   self.client_options.update(build_record=bound,retained_reader_route=copy.deepcopy(cold['sdm_reader_route']),source_rearm_profile=copy.deepcopy(cold),remap_observe=True)
  # Save/restore the names together. Reloading only native_trace leaves a cached
  # InstallerClient subclass and installer_cycle.replay bound to the old gate.
  names=('remap_trace','configuration_trace','capability_trace','window_trace','menu_trace','native_trace','service_trace','paired_firmware_trace','paired_tenfoot_trace','installer_client','installer_cycle')
  previous={n:sys.modules.get(n) for n in names};oldpath=sys.path[:]
  try:
   # Source-bound decoder dependencies precede unchanged recovery sources.
   sys.path[:0]=[str(ROOT/'platform/embedded/nano'),str(ROOT/'platform/embedded/entry'),str(ROOT/'platform/embedded/firmware')]
   droot=Path(closure['decoder_root'])/'platform/embedded/nano'
   sys.path[:0]=[str(droot),str(droot.parent/'entry'),str(droot.parent/'firmware')]
   for key in list(sys.modules):
    if key=='sdm_trace' or key.startswith('sdm_trace.'):
     previous[key]=sys.modules.pop(key)
   # The exact restored baseline predates the candidate's Updates section.
   # Select its own source-bound installer navigation explicitly by role.
   def module_path(n):
    return (Path(closure['baseline_auditor_root']) if not self.selected and n in ('installer_client','installer_cycle') else droot)/(n+'.py')
   modules={n:load(n,module_path(n)) for n in names}
   self.native=modules['native_trace'];self.paired=modules['paired_tenfoot_trace']
   self.client=modules['installer_client'].InstallerClient;self.cycle=modules['installer_cycle']
   if self.selected:require(self.paired.reader_route.checked(self.client_options['build_record'])==cold['sdm_reader_route'],'compiled role reader binding differs')
   require(issubclass(self.client,self.paired.PairedTenfootClient) and
    self.cycle.InstallerClient is self.client and self.paired.decode_native_boot is self.native.decode_native_boot and
    self.paired.menu_opening is modules['paired_firmware_trace'].menu_opening and
    self.paired.reader_route is modules['window_trace'].reader_route and
    modules['service_trace'].decode_native_boot is self.native.decode_native_boot and
    modules['service_trace'].extract_configuration is modules['configuration_trace'].extract,
    'capture consumers use different boot gates')
  finally:
   sys.path[:]=oldpath
   for key in list(sys.modules):
    if (key=='sdm_trace' or key.startswith('sdm_trace.')) and key not in previous:sys.modules.pop(key,None)
   for n,m in previous.items():
    if m is None:sys.modules.pop(n,None)
    else:sys.modules[n]=m
  self.seal=json.dumps(dict(boot=self.boot_options,client=self.client_options),sort_keys=True)
 def verify(self,*,full=True):
  self.bundle.verify()
  require(json.dumps(dict(boot=self.boot_options,client=self.client_options),sort_keys=True)==self.seal,'capture options changed')
  if full:require(all(sha(p)==h for p,h in self.closure['files'].items()),'capture source/artifact closure changed')
 def decode(self,raw):
  self.verify(full=False);require(type(raw) is bytes and len(raw)<=1<<20,'bounded UART bytes required')
  end=re.search(rb'(?m)^FES_ENTRY EL1 HANDOFF\r?\n',raw)
  prefix=raw[:end.end()] if end else raw
  return self.native.decode_native_boot(prefix,**self.boot_options)
 def connect(self,initial,*,exchange,owns,now):
  self.verify()
  return self.client(initial,installed=[True,True],exchange=exchange,owns=owns,now=now,**copy.deepcopy(self.client_options))
 def exercise(self,client,*,settle):
  self.verify();require(isinstance(client,self.client),'client uses a different capture gate')
  return self.cycle.exercise(client,discovered=True,settle=settle)
 def replay(self,raw,receipt):
  self.verify();return self.cycle.replay(raw,receipt,discovered=True,**copy.deepcopy(self.client_options))
