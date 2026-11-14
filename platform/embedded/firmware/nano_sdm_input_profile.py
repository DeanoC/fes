"""Explicit full-source SDM transfer and one retained native continuation."""
import hashlib,importlib.util,json
from pathlib import Path
HERE=Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('fes_input_arena',HERE/'nano_sdm_probe_profile.py');arena=importlib.util.module_from_spec(spec);spec.loader.exec_module(arena)
fit_spec=importlib.util.spec_from_file_location('fes_input_fit',HERE.parent/'nano/fit_layout.py');fit=importlib.util.module_from_spec(fit_spec);fit_spec.loader.exec_module(fit)
reader_spec=importlib.util.spec_from_file_location('fes_sdm_reader_profile',HERE/'nano_sdm_reader_profile.py');reader=importlib.util.module_from_spec(reader_spec);reader_spec.loader.exec_module(reader)
CONTRACT=dict(format=14,scope='nano-retained-source-secure-reader-v14',mode='input-transfer-retire-retained-continue',attempts=1,
 source_base=0x94800000,wire_base=0x14800000,payload_max=0x7ff000,page_bytes=4096,metadata_page=0x94fff000,
 arena_base=0x94000000,arena_bytes=0x10000,root_offset=0x4000,command_offset=0x8000,event_offset=0x9000,
 prepared_control=12,armed_control=13,source_stream=10,source_access='read-only-XN',other_streams='abort',
 transfer_bound_us=20000000,preparation_bound_us=1000000,command_bound_us=100000,
 clock_origin='before-additional-source-SHA384',table_recheck_words=5120,callback_bytes=256,
 source_digest='full-SHA384-before-preparation-and-existing-SMC-chunk-full-checks',
 reply_selection='fixed-ATF-expected-submission-job',DATA_reply_words=0,job_ids=14,
 status='IDLE-NSTATUS-CONF_DONE-INIT_DONE',continuation_before_configuration_commit=True,
 continuation_attempts=1,continuation_replay=False,
 postconfiguration_control='explicit-live-retained-completed-native-owner',
 control_command=4,control_client=1,control_job=15,control_reply_words=6,
 control_inflight=1,control_tables='full-before-and-after-query',
 control_view='fresh-fixed-disabled-24-registers-and-owner',
 control_mailbox='stable-empty-parser-descriptors-bitmap-and-FIFOs-no-ACK',
 control_bound='existing-vendor-bounded-sync-parser',control_DATA_reopen=False,reader_route=dict(reader.CONTRACT),
 continuation_scope='live-engine-and-fresh-final-view-under-original-retirement-bound',
 buffers='retained-on-every-outcome',mapping_retirement='abort-invalidate-disable-retained',automatic_reset=False,replay=False,
 physical_qualification=False,ordinary_runtime_activation=False,build_tag='cold-chain-prefix-16-hex',boot_fit=dict(fit.CONTRACT),compiler_stack_records='selected-units',final_counter_owner_scope='fresh-after-read',pretransfer_mailbox='vendor-parser-until-preparation-attempt',retirement_bound_us=1000000,retirement_controls=[13,12,8,0],retirement_queue_pointers=[6,12],retirement_abort=0x100000,retirement_allow_bypass=False,retirement_memory_reuse=False,retirement_timing='existing-guard-samples-v1',retirement_timing_phases=7,retirement_timing_added_clock_reads=0,retirement_register_batch_rows=24,retirement_table_guard_bytes=4096,retirement_fresh_prewrite_registers=24,retirement_command_snapshot='owned-fixed-24-register-superset',retirement_command_snapshot_rows=24)
CONTRACT.update(retirement_command_clock='fresh-counter-monotonic-source-and-retirement-bounds',
                retirement_command_ownership='fresh-before-and-after-fixed-register-view-and-binding',
                retirement_command_progress='owned-fixed-register-view-then-fresh-consumer',
                retirement_command_progress_rows=24,
                retirement_command_observation='existing-counter-and-successful-raw-read-samples-v2-completion',
                retirement_command_observation_added_reads=0,
                retirement_command_observation_stages=11,
                retirement_command_bound_scope='original-origin-through-owned-valid-consumption',
                retirement_final_validation_bound_us=1000000,
                retirement_final_validation_origin='unchanged-command-and-enclosing-retirement',
                retirement_final_validation_policy='retained-final-snapshots-only')
FILES=('nano_sdm_input_profile.py','sdm_input_vendor_service.c','sdm_input_vendor_mailbox.c','sdm_input_mailbox.h',
 'nano_sdm_source_platform.inc','nano_sdm_input_finish.inc','input_tables.c','input_tables.h','input_transfer.c','input_transfer.h',
 'input_transfer_service.c','input_transfer_service.h','sdm_input_probe_setup.c','sdm_input_probe_setup.h','command_queue.c','command_queue.h','source_retire.c','source_retire.h','nano_sdm_retire_platform.inc','nano_sdm_control_platform.inc','nano_sdm_control_mailbox.inc','nano_sdm_reader_profile.py','nano_sdm_reader.h','nano_sdm_reader.c','nano_sdm_reader_platform.inc','nano_sdm_release_platform.inc')
C_FILES=('input_tables.c','input_transfer.c','input_transfer_service.c','probe_setup.c','command_queue.c','source_retire.c','nano_sdm_reader.c')
H_FILES=('input_tables.h','input_transfer.h','input_transfer_service.h','probe_setup.h','command_queue.h','nano_sdm_source_platform.inc','nano_sdm_input_finish.inc','source_retire.h','nano_sdm_retire_platform.inc','nano_sdm_control_platform.inc','nano_sdm_control_mailbox.inc','nano_sdm_reader.h','nano_sdm_reader_platform.inc','nano_sdm_release_platform.inc')
SERVICE=Path('plat/intel/soc/common/socfpga_sip_svc.c');MAILBOX=Path('plat/intel/soc/common/soc/socfpga_mailbox.c');MAILBOX_HEADER=Path('plat/intel/soc/common/include/socfpga_mailbox.h')
PLATFORM=Path('plat/intel/soc/common/nano_cold_platform.c');LD=Path('bl31/bl31.ld.S');MK=Path('plat/intel/soc/agilex5/platform.mk');HEADER=Path('plat/intel/soc/common/include/fes_nano_cold_profile.h')
NAMES=tuple('plat/intel/soc/common/'+n for n in C_FILES)+tuple('plat/intel/soc/common/include/'+n for n in H_FILES)+tuple(map(str,(SERVICE,MAILBOX,MAILBOX_HEADER,PLATFORM,LD,MK,HEADER)))
INPUT_SERVICE='2ecf7481fe799c84731e2a2837226bc855f807223cd497040e5aeb651de51bb9'
INPUT_MAILBOX='bbfe0bff74a71977e204b36731557707c073e93d1a6f3b8b3dfbaef0b3c5ba1b'
INPUT_HEADER='2657b7855b4dbdc586ee7459be67c602239ea1c555143f28bd3e40e9fabb84e3'
def sha(p):return hashlib.sha256(Path(p).read_bytes()).hexdigest()
def inputs():return [HERE/n for n in FILES]
def selected(profile,enabled):
 if type(enabled) is not bool:raise ValueError('explicit boolean SDM input transfer selection required')
 if not enabled:return None
 if not isinstance(profile,dict) or profile.get('sdm_arm_probe') is not None or profile.get('sdm_fault64') is not None:
  raise ValueError('SDM input, fault64 and arm-only modes are mutually exclusive')
 if (profile.get('firmware_mmu') is None or profile.get('firmware_ram') is None or profile.get('usb_shared') is not True or
     profile.get('sd_pio') is not True or profile.get('configuration_retained') is not True or profile.get('capability_observation',{}).get('format')!=2 or
     not profile.get('hps_menu_program') or not profile.get('hps_program_platform')):
  raise ValueError('SDM input transfer requires complete retained-source mapped shared-device capability/MENU profile')
 mapping=profile.get('cpu_mapping') or {};configuration=profile.get('hps_configuration') or {}
 if tuple(mapping.get(k) for k in ('ram_base','ram_bytes','dma_base','dma_bytes','reader_base','reader_bytes'))!=(0x84000000,0x10000000,0x94800000,0x800000,0x95000000,0x800000) or configuration.get('base')!=0x94800000:
  raise ValueError('SDM input transfer requires pinned heap/source/reader exclusions')
 fit.checked(profile.get('boot_fit'))
 reader.checked(profile)
 return json.loads(json.dumps(CONTRACT))
def checked(profile):
 v=profile.get('sdm_input_transfer') if isinstance(profile,dict) else None
 if v is None:return None
 if json.dumps(v,sort_keys=True)!=json.dumps(CONTRACT,sort_keys=True):raise ValueError('unsupported SDM input transfer contract')
 return selected(profile,True)
def header(profile):
 if checked(profile) is None:return '#define FES_NANO_SDM_SOURCE_RELEASE 0\n#define FES_NANO_RETAINED_READER_ROUTE 0\n#define FES_NANO_SDM_INPUT_TRANSFER 0\n#define FES_NANO_SDM_SOURCE_LEASE 0\n#define FES_NANO_SDM_RETIRE_SOURCE 0\n#define FES_NANO_RETIRE_TIMING 0\n#define FES_NANO_RETIRE_QUEUE_OBSERVE 0\n#define FES_NANO_RETIRE_BATCHED_OBSERVE 0\n'
 chain=profile.get('chain_sha256','')
 if len(chain)!=64 or any(c not in '0123456789abcdef' for c in chain):raise ValueError('complete input transfer chain required')
 return ('#define FES_NANO_SDM_SOURCE_RELEASE 0\n#define FES_NANO_SDM_INPUT_TRANSFER 1\n#define FES_NANO_SDM_SOURCE_LEASE 1\n#define FES_NANO_SDM_RETIRE_SOURCE 1\n#define FES_NANO_RETIRE_TIMING 1\n#define FES_NANO_RETIRE_QUEUE_OBSERVE 1\n#define FES_NANO_RETIRE_BATCHED_OBSERVE 1\n#define FES_NANO_INPUT_BUILD "'+chain[:16]+'"\n'
  '#define FES_NANO_RETAINED_READER_ROUTE 1\n#define FES_NANO_READER_ROUTE_SHA '+chr(34)+reader.digest(reader.checked(profile))+chr(34)+'\n'
  '#define FES_NANO_PROBE_HEAP_FIRST 0x84000000ULL\n#define FES_NANO_PROBE_HEAP_LAST 0x94000000ULL\n')
def install(source,profile,start,mmu,ram,mailbox):
 if checked(profile) is None:return None
 if not all(isinstance(v,dict) for v in (start,mmu,ram,mailbox)) or start.get('generated_sha256')!=INPUT_SERVICE:
  raise ValueError('complete exact START/MMU/RAM/mailbox prerequisites required')
 if (not isinstance(ram.get('sources'),dict) or str(PLATFORM) not in ram['sources'] or
     not isinstance(mmu.get('sources'),dict) or mailbox.get('generated_sha256')!=INPUT_MAILBOX or mailbox.get('header_sha256')!=INPUT_HEADER or
     start.get('mailbox_generated_sha256')!=INPUT_MAILBOX):raise ValueError('bound source/MMU/RAM/mailbox state required')
 source=Path(source)
 for p,h in ((SERVICE,INPUT_SERVICE),(MAILBOX,INPUT_MAILBOX),(MAILBOX_HEADER,INPUT_HEADER)):
  if sha(source/p)!=h:raise ValueError('exact reviewed source bytes required for '+str(p))
 common=source/'plat/intel/soc/common';include=common/'include'
 if any((directory/n).exists() for directory,names in ((common,C_FILES),(include,H_FILES)) for n in names):raise ValueError('fresh SDM input export required')
 if header(profile) not in (source/HEADER).read_text() or '#define FES_NANO_SDM_FAULT64 0\n' not in (source/HEADER).read_text() or '#define FES_NANO_SDM_ARM_PROBE 0\n' not in (source/HEADER).read_text():
  raise ValueError('exclusive SDM input selection required')
 for directory,names in ((common,C_FILES),(include,H_FILES)):
  for n in names:(directory/n).write_bytes((HERE/({'probe_setup.c':'sdm_input_probe_setup.c','probe_setup.h':'sdm_input_probe_setup.h'}.get(n,n))).read_bytes())
 for p,n in ((SERVICE,'sdm_input_vendor_service.c'),(MAILBOX,'sdm_input_vendor_mailbox.c'),(MAILBOX_HEADER,'sdm_input_mailbox.h')):(source/p).write_bytes((HERE/({'probe_setup.c':'sdm_input_probe_setup.c','probe_setup.h':'sdm_input_probe_setup.h'}.get(n,n))).read_bytes())
 (source/PLATFORM).write_text((source/PLATFORM).read_text()+'\n#if defined(FES_NANO_SDM_SOURCE_LEASE) && FES_NANO_SDM_SOURCE_LEASE\n#include "nano_sdm_source_platform.inc"\n#endif\n')
 (source/LD).write_text(arena.memory_source((source/LD).read_text()))
 (source/MK).write_text((source/MK).read_text()+'\nBL31_SOURCES += '+' '.join('plat/intel/soc/common/'+n for n in C_FILES)+'\n')
 (source/MK).write_text((source/MK).read_text()+''.join('$(BUILD_PLAT)/bl31/'+n+'.o: override CFLAGS += -fstack-usage\n' for n in ('nano_cold_platform','socfpga_sip_svc','socfpga_mailbox',*[Path(n).stem for n in C_FILES])))
 mmu['sources'][str(LD)]=sha(source/LD);mmu['sources'][str(MK)]=sha(source/MK)
 if str(PLATFORM) not in ram.get('sources',{}):raise ValueError('bound cold provider required')
 ram['sources'][str(PLATFORM)]=sha(source/PLATFORM)
 start['pre_sdm_input_generated_sha256']=start['generated_sha256'];start['generated_sha256']=sha(source/SERVICE)
 start['pre_sdm_input_mailbox_sha256']=start['mailbox_generated_sha256'];start['mailbox_generated_sha256']=sha(source/MAILBOX)
 mailbox['pre_sdm_input_generated_sha256']=mailbox['generated_sha256'];mailbox['generated_sha256']=sha(source/MAILBOX)
 mailbox['pre_sdm_input_header_sha256']=mailbox['header_sha256'];mailbox['header_sha256']=sha(source/MAILBOX_HEADER)
 return dict(contract=dict(CONTRACT),sources={n:sha(source/n) for n in NAMES})
def verify_installed(source,binding,profile):
 if checked(profile) is None:
  if binding is not None:raise ValueError('unselected SDM input binding')
  return
 if not isinstance(binding,dict) or json.dumps(binding.get('contract'),sort_keys=True)!=json.dumps(CONTRACT,sort_keys=True) or set(binding.get('sources',{}))!=set(NAMES):raise ValueError('complete input binding required')
 if any(sha(Path(source)/n)!=h for n,h in binding['sources'].items()):raise ValueError('input source changed')
 if header(profile) not in (Path(source)/HEADER).read_text():raise ValueError('input header changed')
def private_function(image,name):
 import struct
 if image[:6]!=b'\x7fELF\x02\x01':raise ValueError('ELF64 little-endian image required')
 offset=struct.unpack_from('<Q',image,40)[0];stride,count=struct.unpack_from('<HH',image,58)
 if stride!=64 or not 0<count<=4096 or offset+stride*count>len(image):raise ValueError('complete static section table required')
 sections=[struct.unpack_from('<IIQQQQIIQQ',image,offset+i*stride) for i in range(count)];found=[]
 for s in sections:
  if s[1]!=2:continue
  if s[9]!=24 or s[5]%24 or s[4]+s[5]>len(image) or s[6]>=count:raise ValueError('bounded static symbols required')
  strings=sections[s[6]]
  if strings[1]!=3 or strings[4]+strings[5]>len(image):raise ValueError('bounded static names required')
  names=image[strings[4]:strings[4]+strings[5]]
  for at in range(s[4],s[4]+s[5],24):
   n,info,other,index,address,size=struct.unpack_from('<IBBHQQ',image,at)
   if n>=len(names):raise ValueError('bounded symbol name required')
   end=names.find(b'\0',n)
   if end<0:raise ValueError('terminated symbol name required')
   if names[n:end]!=name.encode():continue
   if info!=2 or not 0<index<count or not address or not size or size%4:raise ValueError('local function identity required')
   owner=sections[index]
   if owner[1]!=1 or not owner[2]&4 or not owner[3]<=address or address+size>owner[3]+owner[5]:raise ValueError('local helper must be bounded executable code')
   found.append(dict(address=address,bytes=size))
 if len(found)!=1:raise ValueError('one private final source-scope helper required')
 return found[0]
def verify_image(image,profile):
 if checked(profile) is None:return None
 import nano_firmware_ram as ram
 symbols=ram.symbols(image);retire_scope=private_function(image,'retire_owned');retire_tables=private_function(image,'retire_valid');private_scope=private_function(image,'sdm_input_final_scope')
 fixed={'__FES_SMMU_PROBE_START__':0x94000000,'__FES_SMMU_STREAMS__':0x94000000,'__FES_SMMU_SDM_ROOT__':0x94004000,'__FES_SMMU_CMDQ__':0x94008000,'__FES_SMMU_EVTQ__':0x94009000,'__FES_SMMU_PROBE_END__':0x94010000}
 roots=('fes_nano_sdm_source_prepare','fes_nano_sdm_source_start_ready','fes_nano_sdm_source_validate','fes_nano_sdm_source_finish','fes_nano_sdm_transfer_result','nano_probe_arm','nano_sdm_transfer_before','nano_sdm_transfer_response','nano_sdm_transfer_status_result','mailbox_read_response_async_expected','nano_source_retire','nano_source_continue','nano_cmdq_submit','fes_nano_sdm_transfer_control_begin','fes_nano_sdm_transfer_control_finish','fes_nano_sdm_source_control_ready','fes_nano_sdm_control_mailbox_idle','fes_nano_sdm_source_reader_ready','fes_nano_sdm_transfer_reader_ready','nano_sdm_reader_observed')
 if any(symbols.get(n,(0,))[0]!=v for n,v in fixed.items()) or any(n not in symbols for n in roots) or any(n in symbols for n in ('fes_nano_sdm_fault64_arm','fes_nano_sdm_probe_arm_park','fes_nano_sdm_input_arm_park')):
  raise ValueError('input arena/hooks absent or conflicting diagnostic installed')
 if any(marker not in image for marker in (profile['chain_sha256'][:16].encode(),b'FES_NANO_INPUT_RESULT build=',b'FES_NANO_RETIRE_RESULT build=',b'FES_NANO_RETIRE_TIMING build=',b'FES_NANO_RETIRE_QUEUE build=',b'FES_NANO_INPUT_CONTINUE build=',b'FES_NANO_INPUT_PARK retained=1 resume=0 activated=0')):raise ValueError('input build/terminal markers absent')
 return dict(contract=dict(CONTRACT),symbols=fixed,private_final_scope=private_scope,private_retire_scope=retire_scope,private_retire_tables=retire_tables,build_tag=profile['chain_sha256'][:16],image_sha256=hashlib.sha256(image).hexdigest(),hardware_accepted=False,physical_programming_admission=False)
