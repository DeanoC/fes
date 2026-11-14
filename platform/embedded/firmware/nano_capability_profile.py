"""Opt-in, source-bound read-only capability observation; never DMA authority."""
import hashlib,json
import importlib.util
from pathlib import Path
HERE=Path(__file__).resolve().parent
_remap_spec=importlib.util.spec_from_file_location('fes_selected_remap',HERE/'nano_remap_trace.py')
nano_remap_trace=importlib.util.module_from_spec(_remap_spec);_remap_spec.loader.exec_module(nano_remap_trace)
CONTRACT=dict(format=2,scope='nano-boot-read-only-capability-remap-v2',words=13,reads=26,
 remap_observation=dict(nano_remap_trace.CONTRACT),
 sample='two-identical-owned-samples',attempts=1,register_writes=False,
 DMA_authority=False,physical_qualification=False)
FILES=('nano_capability_profile.py','nano_capability.c','nano_capability.h','nano_capability_platform.inc','nano_remap_trace.py')
NAMES=('plat/intel/soc/common/nano_capability.c','plat/intel/soc/common/include/nano_capability.h',
 'plat/intel/soc/common/include/nano_capability_platform.inc','plat/intel/soc/agilex5/platform.mk',
 'plat/intel/soc/common/nano_cold_platform.c','plat/intel/soc/common/socfpga_sip_svc.c',
 'plat/intel/soc/common/include/fes_nano_cold_profile.h')
def inputs():return [HERE/name for name in FILES]
def selected(profile,enabled):
 if type(enabled) is not bool:raise ValueError('explicit boolean capability selection required')
 if not enabled:return None
 if (not isinstance(profile,dict) or profile.get('firmware_mmu') is None or
     profile.get('firmware_ram') is None or profile.get('sd_pio') is not True or
     any(profile.get(k) is None for k in ('hps_context','hps_control','hps_reader_memory'))):
  raise ValueError('capability observation requires the complete mapped cold SD/reader profile')
 return dict(CONTRACT)
def checked(profile):
 value=profile.get('capability_observation') if isinstance(profile,dict) else None
 if value is None:return None
 if json.dumps(value,sort_keys=True)!=json.dumps(CONTRACT,sort_keys=True):
  raise ValueError('unsupported capability observation contract')
 return selected(profile,True)
def install(source,profile):
 contract=checked(profile)
 if contract is None:return None
 source=Path(source);common=source/'plat/intel/soc/common';include=common/'include'
 paths={common/'nano_capability.c':HERE/'nano_capability.c',include/'nano_capability.h':HERE/'nano_capability.h',
        include/'nano_capability_platform.inc':HERE/'nano_capability_platform.inc'}
 for target,origin in paths.items():
  if target.exists():raise ValueError('capability install requires a fresh export')
  target.write_bytes(origin.read_bytes())
 mk=source/'plat/intel/soc/agilex5/platform.mk'
 mk.write_text(mk.read_text()+'\nBL31_SOURCES += plat/intel/soc/common/nano_capability.c\n')
def binding(source,profile):
 contract=checked(profile)
 if contract is None:return None
 source=Path(source)
 if '#define FES_NANO_CAPABILITY_DIAGNOSTIC 1\n' not in (source/NAMES[-1]).read_text():
  raise ValueError('selected capability macro missing')
 return dict(contract=contract,sources={name:hashlib.sha256((source/name).read_bytes()).hexdigest() for name in NAMES})
def verify_installed(source,binding,profile):
 contract=checked(profile)
 if contract is None:
  if binding is not None:raise ValueError('unselected capability binding')
  return
 if (not isinstance(binding,dict) or binding.get('contract')!=contract or
     set(binding.get('sources',{}))!=set(NAMES)):
  raise ValueError('missing capability installation binding')
 if any(hashlib.sha256((Path(source)/name).read_bytes()).hexdigest()!=digest for name,digest in binding['sources'].items()):
  raise ValueError('capability installation changed')
def verify_image(image,profile):
 if checked(profile) is None:return None
 import nano_firmware_ram as ram
 symbols=ram.symbols(image)
 if 'fes_nano_capability_observe' not in symbols or b'FES_NANO_CAPABILITY status=' not in image or any(
      marker not in image for marker in (b'FES_NANO_REMAP kind=',b'FES_NANO_SDM_SUBMIT phase=',b'FES_NANO_SDM_RESULT status=')):
  raise ValueError('capability observer absent from linked firmware')
 return dict(contract=dict(CONTRACT),image_sha256=hashlib.sha256(image).hexdigest(),
  observer_address=symbols['fes_nano_capability_observe'][0],hardware_observed=False)

def install_remap(source,profile,start):
 return nano_remap_trace.install(source,checked(profile) is not None,start)
