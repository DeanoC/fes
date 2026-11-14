"""Explicit arm-and-park diagnostic; no SDM request or ordinary DMA grant."""
import hashlib,json
from pathlib import Path
HERE=Path(__file__).resolve().parent
SERVICE=Path('plat/intel/soc/common/socfpga_sip_svc.c')
PLATFORM=Path('plat/intel/soc/common/nano_cold_platform.c')
LD=Path('bl31/bl31.ld.S')
CONTRACT=dict(format=3,scope='nano-sdm-arm-and-park-v3-bounded-memory',mode='arm-park',attempts=1,
 arena_base=0x94000000,arena_bytes=0x10000,root_offset=0x4000,command_offset=0x8000,event_offset=0x9000,
 command_batch=6,prepared_control=12,armed_control=13,SDM_requests=0,DMA_authority=False,
 automatic_disable=False,physical_qualification=False,USB_PIO='fresh-owned-idle',scope_reason_codes=14,private_memory_callback_bytes=256,
 unpublished_queue_callback_bytes=16,ownership_proof='fresh-before-after-IO',
 timer_contract='side-effect-free-counter-before-owner-proof',preparation_bound_us=1000000,queue_bound_us=100000)
FILES=('nano_sdm_probe_profile.py','nano_sdm_probe.h','nano_sdm_probe_platform.inc',
       'command_queue.c','command_queue.h','probe_setup.c','probe_setup.h')
NAMES=('plat/intel/soc/common/command_queue.c','plat/intel/soc/common/probe_setup.c',
 'plat/intel/soc/common/include/command_queue.h','plat/intel/soc/common/include/probe_setup.h',
 'plat/intel/soc/common/include/nano_sdm_probe.h','plat/intel/soc/common/include/nano_sdm_probe_platform.inc',
 str(LD),str(SERVICE),str(PLATFORM),'plat/intel/soc/agilex5/platform.mk',
 'plat/intel/soc/common/include/fes_nano_cold_profile.h')
def sha(p):return hashlib.sha256(Path(p).read_bytes()).hexdigest()
def inputs():return [HERE/name for name in FILES]
def selected(profile,enabled):
 if type(enabled) is not bool:raise ValueError('explicit boolean SDM arm probe selection required')
 if not enabled:return None
 if (not isinstance(profile,dict) or profile.get('firmware_mmu') is None or profile.get('firmware_ram') is None or
     profile.get('usb_shared') is not True or profile.get('sd_pio') is not True or
     profile.get('capability_observation',{}).get('format')!=2):
  raise ValueError('SDM arm probe requires complete mapped capability/shared-device profile')
 mapping=profile.get('cpu_mapping') or {};configuration=profile.get('hps_configuration') or {}
 if (mapping.get('ram_base'),mapping.get('ram_bytes'),mapping.get('dma_base'),mapping.get('dma_bytes'),
     mapping.get('reader_base'),mapping.get('reader_bytes'))!=(0x84000000,0x10000000,0x94800000,0x800000,0x95000000,0x800000) or configuration.get('base')!=0x94800000:
  raise ValueError('SDM arm probe requires the exact fixed memory exclusions')
 return dict(CONTRACT)
def checked(profile):
 value=profile.get('sdm_arm_probe') if isinstance(profile,dict) else None
 if value is None:return None
 if json.dumps(value,sort_keys=True)!=json.dumps(CONTRACT,sort_keys=True):raise ValueError('unsupported SDM arm contract')
 return selected(profile,True)
def header(profile):
 active=checked(profile) is not None
 return '#define FES_NANO_SDM_ARM_PROBE '+str(int(active))+'\n'+(
  '#define FES_NANO_PROBE_HEAP_FIRST 0x84000000ULL\n#define FES_NANO_PROBE_HEAP_LAST 0x94000000ULL\n' if active else '')
def memory_source(text):
 if '__FES_SMMU_PROBE_' in text or text.count('MEMORY {')!=1 or text.count('SECTIONS {')!=1 or text.count('    /DISCARD/ : {')!=1:
  raise ValueError('fresh reviewed linker memory context required')
 text=text.replace('MEMORY {','MEMORY {\n    SMMU_PROBE (rw!a): ORIGIN = 0x94000000, LENGTH = 0x10000')
 text=text.replace('SECTIONS {','PHDRS { private PT_LOAD FLAGS(6); image PT_LOAD FLAGS(7); stack PT_GNU_STACK FLAGS(6); }\nSECTIONS {')
 text=text.replace('>RAM','>RAM :image').replace('>NOBITS','>NOBITS :private')
 fragment='''    .fes_smmu_probe 0x94000000 (NOLOAD) : {
        __FES_SMMU_PROBE_START__ = .;
        __FES_SMMU_STREAMS__ = .; . += 0x4000;
        __FES_SMMU_SDM_ROOT__ = .; . += 0x1000;
        . = __FES_SMMU_PROBE_START__ + 0x8000;
        __FES_SMMU_CMDQ__ = .; . += 0x80;
        . = __FES_SMMU_PROBE_START__ + 0x9000;
        __FES_SMMU_EVTQ__ = .; . += 0x100;
        . = __FES_SMMU_PROBE_START__ + 0x10000;
        __FES_SMMU_PROBE_END__ = .;
    } >SMMU_PROBE :NONE
    ASSERT(__BL31_END__ <= __FES_SMMU_PROBE_START__, "probe overlaps firmware")
    ASSERT(__FES_SMMU_PROBE_END__ <= 0x94800000, "probe overlaps configuration DMA")
'''
 return text.replace('    /DISCARD/ : {',fragment+'    /DISCARD/ : {')
def service_source(data):
 if hashlib.sha256(data).hexdigest()!='2ecf7481fe799c84731e2a2837226bc855f807223cd497040e5aeb651de51bb9':
  raise ValueError('SDM arm probe requires the exact reviewed remap observer service')
 text=data.decode();edits=[]
 for before,after in [('#include <assert.h>','#include <assert.h>\n#include "nano_sdm_probe.h"'),
  ('        status = intel_fpga_config_start(0);','        fes_nano_sdm_probe_arm_park(g_remapper_bypass);\n        status = intel_fpga_config_start(0);')]:
  if text.count(before)!=1:raise ValueError('SDM owned START context changed')
  text=text.replace(before,after);edits.append((before,after))
 restored=text
 for before,after in reversed(edits):restored=restored.replace(after,before)
 if restored.encode()!=data:raise ValueError('SDM hook changes original operations')
 return text.encode()
def install(source,profile,start,mmu,ram):
 if checked(profile) is None:return None
 if not isinstance(mmu,dict) or not isinstance(start,dict) or start.get('generated_sha256')!='2ecf7481fe799c84731e2a2837226bc855f807223cd497040e5aeb651de51bb9':
  raise ValueError('complete MMU/remap source bindings required')
 source=Path(source);common=source/'plat/intel/soc/common';include=common/'include'
 for name in ('command_queue.c','probe_setup.c'):
  path=common/name
  if path.exists():raise ValueError('fresh probe export required')
  path.write_bytes((HERE/name).read_bytes())
 for name in ('command_queue.h','probe_setup.h','nano_sdm_probe.h','nano_sdm_probe_platform.inc'):
  path=include/name
  if path.exists():raise ValueError('fresh probe export required')
  path.write_bytes((HERE/name).read_bytes())
 (source/SERVICE).write_bytes(service_source((source/SERVICE).read_bytes()))
 (source/PLATFORM).write_text((source/PLATFORM).read_text()+'\n#if defined(FES_NANO_SDM_ARM_PROBE) && FES_NANO_SDM_ARM_PROBE\n#include "nano_sdm_probe_platform.inc"\n#endif\n')
 (source/LD).write_text(memory_source((source/LD).read_text()))
 mk=source/'plat/intel/soc/agilex5/platform.mk'
 mk.write_text(mk.read_text()+'\nBL31_SOURCES += plat/intel/soc/common/command_queue.c plat/intel/soc/common/probe_setup.c\n')
 mmu['sources'][str(LD)]=sha(source/LD)
 mmu['sources']['plat/intel/soc/agilex5/platform.mk']=sha(mk)
 if not isinstance(ram,dict) or str(PLATFORM) not in ram.get('sources',{}):raise ValueError('cold RAM platform binding required')
 ram['sources'][str(PLATFORM)]=sha(source/PLATFORM)
 start['pre_sdm_probe_generated_sha256']=start['generated_sha256'];start['generated_sha256']=sha(source/SERVICE)
 return dict(contract=dict(CONTRACT),sources={name:sha(source/name) for name in NAMES})
def verify_installed(source,binding,profile):
 if checked(profile) is None:
  if binding is not None:raise ValueError('unselected SDM arm binding')
  return
 if not isinstance(binding,dict) or binding.get('contract')!=CONTRACT or set(binding.get('sources',{}))!=set(NAMES):
  raise ValueError('complete SDM arm binding required')
 if any(sha(Path(source)/name)!=value for name,value in binding['sources'].items()):raise ValueError('SDM arm sources changed')
def verify_image(image,profile):
 if checked(profile) is None:return None
 import nano_firmware_ram as ram
 symbols=ram.symbols(image)
 expected={'__FES_SMMU_PROBE_START__':0x94000000,'__FES_SMMU_STREAMS__':0x94000000,
  '__FES_SMMU_SDM_ROOT__':0x94004000,'__FES_SMMU_CMDQ__':0x94008000,
  '__FES_SMMU_EVTQ__':0x94009000,'__FES_SMMU_PROBE_END__':0x94010000}
 if any(symbols.get(name,(0,))[0]!=value for name,value in expected.items()) or any(name not in symbols for name in ('fes_nano_sdm_probe_arm_park','nano_probe_prepare','nano_probe_arm')):
  raise ValueError('SDM arm reservation/functions absent')
 if b'FES_NANO_SDM_ARM phase=park submitted=0 retained=1' not in image:raise ValueError('terminal arm marker absent')
 # The ordinary producer separately verifies the complete load/MMU/RAM profile.
 return dict(contract=dict(CONTRACT),symbols=expected,image_sha256=hashlib.sha256(image).hexdigest(),
  hardware_accepted=False,SDM_request_submitted=False)
