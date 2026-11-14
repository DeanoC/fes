"""Explicit first-DATA fault diagnostic; no normal DMA or programming grant."""
import hashlib,importlib.util,json
from pathlib import Path
HERE=Path(__file__).resolve().parent
def module(name,filename):
 spec=importlib.util.spec_from_file_location(name,HERE/filename);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m);return m
service=module('fes_fault64_service_transform','nano_sdm_fault_service.py')
arm=module('fes_fault64_arena_source','nano_sdm_probe_profile.py')
_fit_spec=importlib.util.spec_from_file_location('fes_fault64_fit_layout',HERE.parent/'nano/fit_layout.py')
fit_layout=importlib.util.module_from_spec(_fit_spec);_fit_spec.loader.exec_module(fit_layout)
CONTRACT=dict(format=4,scope='nano-first-SDM-DATA-64-v5-qualified-publication',mode='fault64-park',attempts=1,
 arena_base=0x94000000,arena_bytes=0x10000,root_offset=0x4000,command_offset=0x8000,event_offset=0x9000,
 command_batch=6,prepared_control=12,armed_control=13,SDM_DATA_requests=1,SDM_DATA_bytes=64,
 pre_DATA_empty_snapshot=True,event_wait_us=10000,event_wait_polls=1024,clock_hz=400000000,
 build_tag='cold-chain-prefix-16-hex',USB_PIO='fresh-owned-idle',scope_reason_codes=18,
 post_START_ARADDR=0x80000000,post_START_AWADDR=0x80000000,DMA_authority=False,
 automatic_disable=False,physical_qualification=False,private_memory_callback_bytes=256,
 unpublished_queue_callback_bytes=16,ownership_proof='fresh-before-after-IO',
 timer_contract='side-effect-free-counter-before-owner-proof',preparation_bound_us=1000000,queue_bound_us=100000,boot_fit=dict(fit_layout.CONTRACT),submit_clock_after_note=True,
 empty_snapshot='fresh-after-note-before-mailbox',result_note='after-event-observation',
 event_MMIO_proof='full-snapshot-per-read-qualified-publication-once',publication_source='fixed-NS-producer-only',publication_static_error_fields=20,publication_reads_per_poll=1,clock_callback='read-only-inside-owner-guards')
FILES=('nano_sdm_fault_profile.py','nano_sdm_fault_service.py','nano_sdm_fault_platform.inc','fault64.c','fault64.h',
 'fault64_service.h','event_capture.c','event_capture.h','event_wire.c','event_wire.h','event_wait.c','event_wait.h',
 'command_queue.c','command_queue.h','probe_setup.c','probe_setup.h')
C_FILES=('command_queue.c','probe_setup.c','fault64.c','event_capture.c','event_wire.c','event_wait.c')
H_FILES=('command_queue.h','probe_setup.h','fault64.h','fault64_service.h','event_capture.h','event_wire.h','event_wait.h','nano_sdm_fault_platform.inc')
SERVICE=Path('plat/intel/soc/common/socfpga_sip_svc.c');PLATFORM=Path('plat/intel/soc/common/nano_cold_platform.c')
LD=Path('bl31/bl31.ld.S');MK=Path('plat/intel/soc/agilex5/platform.mk');HEADER=Path('plat/intel/soc/common/include/fes_nano_cold_profile.h')
NAMES=tuple('plat/intel/soc/common/'+n for n in C_FILES)+tuple('plat/intel/soc/common/include/'+n for n in H_FILES)+tuple(map(str,(SERVICE,PLATFORM,LD,MK,HEADER)))
def sha(p):return hashlib.sha256(Path(p).read_bytes()).hexdigest()
def inputs():return [HERE/n for n in FILES]
def selected(profile,enabled):
 if type(enabled) is not bool:raise ValueError('explicit boolean SDM fault64 selection required')
 if not enabled:return None
 if not isinstance(profile,dict) or profile.get('sdm_arm_probe') is not None:raise ValueError('fault64 and arm-only modes are mutually exclusive')
 if (profile.get('firmware_mmu') is None or profile.get('firmware_ram') is None or profile.get('usb_shared') is not True or
     profile.get('sd_pio') is not True or profile.get('capability_observation',{}).get('format')!=2 or
     not profile.get('hps_menu_program') or not profile.get('hps_program_platform')):
  raise ValueError('fault64 requires the complete mapped shared-device capability/MENU profile')
 mapping=profile.get('cpu_mapping') or {};configuration=profile.get('hps_configuration') or {}
 if tuple(mapping.get(k) for k in ('ram_base','ram_bytes','dma_base','dma_bytes','reader_base','reader_bytes'))!=(0x84000000,0x10000000,0x94800000,0x800000,0x95000000,0x800000) or configuration.get('base')!=0x94800000:
  raise ValueError('fault64 requires exact pinned heap/DMA/reader exclusions')
 fit_layout.checked(profile.get('boot_fit'))
 return dict(CONTRACT)
def checked(profile):
 value=profile.get('sdm_fault64') if isinstance(profile,dict) else None
 if value is None:return None
 if json.dumps(value,sort_keys=True)!=json.dumps(CONTRACT,sort_keys=True):raise ValueError('unsupported fault64 contract')
 return selected(profile,True)
def header(profile):
 if checked(profile) is None:return '#define FES_NANO_SDM_FAULT64 0\n'
 chain=profile.get('chain_sha256','')
 if len(chain)!=64 or any(c not in '0123456789abcdef' for c in chain):raise ValueError('complete cold chain required for fault64 build tag')
 return ('#define FES_NANO_SDM_FAULT64 1\n#define FES_NANO_FAULT64_BUILD "'+chain[:16]+'"\n'
  '#define FES_NANO_PROBE_HEAP_FIRST 0x84000000ULL\n#define FES_NANO_PROBE_HEAP_LAST 0x94000000ULL\n')
def install(source,profile,start,mmu,ram):
 if checked(profile) is None:return None
 if (not isinstance(mmu,dict) or not isinstance(ram,dict) or not isinstance(start,dict) or
     start.get('generated_sha256')!=service.INPUT_SHA256):raise ValueError('complete MMU/RAM/remap bindings required')
 source=Path(source);common=source/'plat/intel/soc/common';include=common/'include'
 for directory,names in ((common,C_FILES),(include,H_FILES)):
  for name in names:
   target=directory/name
   if target.exists():raise ValueError('fresh fault64 export required')
   target.write_bytes((HERE/name).read_bytes())
 if '#define FES_NANO_SDM_ARM_PROBE 0\n' not in (source/HEADER).read_text() or header(profile) not in (source/HEADER).read_text():
  raise ValueError('complete exclusive fault64 header selection required')
 (source/SERVICE).write_bytes(service.changed((source/SERVICE).read_bytes()))
 (source/PLATFORM).write_text((source/PLATFORM).read_text()+'\n#if defined(FES_NANO_SDM_FAULT64) && FES_NANO_SDM_FAULT64\n#include "nano_sdm_fault_platform.inc"\n#endif\n')
 (source/LD).write_text(arm.memory_source((source/LD).read_text()))
 (source/MK).write_text((source/MK).read_text()+'\nBL31_SOURCES += '+' '.join('plat/intel/soc/common/'+n for n in C_FILES)+'\n')
 mmu['sources'][str(LD)]=sha(source/LD);mmu['sources'][str(MK)]=sha(source/MK)
 if str(PLATFORM) not in ram.get('sources',{}):raise ValueError('cold RAM platform source binding required')
 ram['sources'][str(PLATFORM)]=sha(source/PLATFORM)
 start['pre_sdm_fault64_generated_sha256']=start['generated_sha256'];start['generated_sha256']=sha(source/SERVICE)
 return dict(contract=dict(CONTRACT),sources={name:sha(source/name) for name in NAMES})
def verify_installed(source,binding,profile):
 if checked(profile) is None:
  if binding is not None:raise ValueError('unselected fault64 binding')
  return
 if (not isinstance(binding,dict) or json.dumps(binding.get('contract'),sort_keys=True)!=json.dumps(CONTRACT,sort_keys=True) or
     set(binding.get('sources',{}))!=set(NAMES)):raise ValueError('complete fault64 binding required')
 if any(sha(Path(source)/name)!=digest for name,digest in binding['sources'].items()):raise ValueError('fault64 source changed')
 if header(profile) not in (Path(source)/HEADER).read_text():raise ValueError('fault64 build selection changed')
def verify_image(image,profile):
 if checked(profile) is None:return None
 import nano_firmware_ram as ram
 symbols=ram.symbols(image)
 expected={'__FES_SMMU_PROBE_START__':0x94000000,'__FES_SMMU_STREAMS__':0x94000000,'__FES_SMMU_SDM_ROOT__':0x94004000,
  '__FES_SMMU_CMDQ__':0x94008000,'__FES_SMMU_EVTQ__':0x94009000,'__FES_SMMU_PROBE_END__':0x94010000}
 roots=('fes_nano_sdm_fault64_arm','fes_nano_sdm_fault64_start_result','fes_nano_sdm_fault64_before','fes_nano_sdm_fault64_after',
  'nano_probe_prepare','nano_probe_arm','nano_evt_wait_finish','nano_fault64_after',
  'fes_nano_sdm_fault64_submit','nano_fault64_submit')
 if any(symbols.get(n,(0,))[0]!=v for n,v in expected.items()) or any(n not in symbols for n in roots) or 'fes_nano_sdm_probe_arm_park' in symbols:
  raise ValueError('fault64 reservation/functions absent or arm-only hook selected')
 if any(marker not in image for marker in (profile['chain_sha256'][:16].encode(),b'FES_NANO_FAULT64 phase=result ',b'FES_NANO_FAULT64 phase=park retained=1 resume=0')):
  raise ValueError('fault64 build tag/terminal markers absent')
 return dict(contract=dict(CONTRACT),symbols=expected,build_tag=profile['chain_sha256'][:16],
  image_sha256=hashlib.sha256(image).hexdigest(),hardware_accepted=False,physical_programming_admission=False)
