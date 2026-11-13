"""Reviewed sealed secure-stream reader route; no live permission or I/O.

Quartus Pro26.3 parameters_data.tcl maps AxUSER[8] to AxMMUSECSID,
AxUSER[24:9] to AxMMUSID. The complete fixed generated wrapper (not a
matching substring) ties those bits to17'h3: secure stream1. Arm SMMUv3
3.11 selects Secure GBPA for this stream when both SMMUEN controls are0.
AxPROT=3 still describes a Non-secure memory transaction. Neither a cleared
Non-secure abort nor a new translation-table/memory-reuse grant is needed.
"""
import hashlib
import json
import re
from pathlib import Path
import xml.etree.ElementTree as ET

ROOT=Path(__file__).resolve().parents[3]
IP='hps_subsys/ip/hps_subsys/agilex_hps.ip'
WRAPPER='hps_subsys/ip/hps_subsys/agilex_hps/intel_sundancemesa_mpfe_100/synth/agilex_hps_intel_sundancemesa_mpfe_100_d7hziti.sv'
CONTRACT=dict(format=1,scope='nano-sealed-secure-SID1-retained-source-reader-v1',
 ip_sha256='5bc2e311b06fd55bb311e231968fe30d3d17d465fc4155237072672cda337ecb',
 wrapper_sha256='3f3d357986f918355ec9fb5c816727ed808f6dd2fd692fbd49aa7116f9208949',
 parameter_definition_sha256='7b7b65f95ab1a70f1bbd1a985893c6425d0366896070ff6160d1ed9eded5591c',
 stream=1,secure_stream=1,axuser=0x3e0,arprot=3,arcache=2,
 bank='secure',secure_controls=0,nonsecure_control=0,nonsecure_abort=0x100000,
 source_retained=True,memory_reuse=False,register_writes=False,physical_qualification=False)
PARAMETERS={'f2sdram_data_width':'256','f2sdram_address_width':'32','f2sdram_SMMU':'false','f2s_data_width':'0','EMIF_AXI_Enable':'true'}

def digest(v):return hashlib.sha256((json.dumps(v,sort_keys=True,separators=(',',':'))+'\n').encode()).hexdigest()
def _expected(profile):
 try:
  boot=profile['hps_context'];menu=profile['hps_menu_program'];context=menu['context']
  a=profile['hps_program_platform']['platform'];b=menu['platform']['platform']
  if (boot['programming_profile']!='de25-native-computer-v1' or context['programming_profile']!='de25-embedded-hps-first-menu-prototype-v1' or
      context['control_abi']!=0x10203043 or menu['memory']['bus_base']!=0x95000000 or menu['memory']['bytes']!=0x800000 or
      tuple(menu['access'][k] for k in ('aruser','arprot','arcache'))!=(224,3,2)):
   raise ValueError('retained reader requires the complete sealed MENU/reader selection')
  for key in ('platform','device','hps_io_hash','pfg_identity','hps_parameters_sha256','hps_configuration_sha256'):
   if not isinstance(a[key],str) or not a[key] or a[key]!=b[key]:raise ValueError('reader HPS fingerprints differ')
  return dict(contract=dict(CONTRACT),boot_context_sha256=digest(boot),menu_context_sha256=digest(context),
              boot_platform_sha256=digest(a),menu_platform_sha256=digest(b),menu_access_sha256=digest(menu['access']))
 except (KeyError,TypeError) as error:raise ValueError('complete sealed reader source binding required') from error

def checked_binding(binding):
 fields={'contract','boot_context_sha256','menu_context_sha256','boot_platform_sha256','menu_platform_sha256','menu_access_sha256'}
 if not isinstance(binding,dict) or set(binding)!=fields or json.dumps(binding.get('contract'),sort_keys=True)!=json.dumps(CONTRACT,sort_keys=True):
  raise ValueError('complete current sealed reader binding required')
 if any(not isinstance(binding[k],str) or not re.fullmatch('[0-9a-f]{64}',binding[k]) or not int(binding[k],16) for k in fields-{'contract'}):
  raise ValueError('complete sealed reader identity digests required')
 return binding

def checked(profile):
 if not isinstance(profile,dict) or 'sdm_reader_route' not in profile:raise ValueError('sealed retained reader route required')
 expected=_expected(profile)
 checked_binding(profile['sdm_reader_route'])
 if json.dumps(profile['sdm_reader_route'],sort_keys=True)!=json.dumps(expected,sort_keys=True):
  raise ValueError('sealed retained reader route differs from its complete compiled roles')
 return expected

def derive(boot_project,menu_project,profile):
 # Called inside cold.make's existing source/file/tree guard, after the normal
 # sealed package, full vendor adapter and platform inventory validators.
 for name in (boot_project,menu_project):
  if not isinstance(name,(str,Path)) or not str(name):raise ValueError('both private retained reader projects are required')
  project=Path(name).absolute()
  if not project.is_relative_to(ROOT/'out') or '..' in project.parts:raise ValueError('private retained reader project required')
  for rel,key in ((IP,'ip_sha256'),(WRAPPER,'wrapper_sha256')):
   p=project/'board/hps/project'/rel
   if any(v.is_symlink() for v in (p,*p.parents)) or not p.is_file() or not 0<p.stat().st_size<=2<<20:
    raise ValueError('private bounded retained reader source required')
   if hashlib.sha256(p.read_bytes()).hexdigest()!=CONTRACT[key]:raise ValueError('unreviewed complete retained reader source: '+rel)
  ns={'i':'http://www.accellera.org/XMLSchema/IPXACT/1685-2014'}
  rows=ET.parse(project/'board/hps/project'/IP).findall('.//i:parameter',ns)
  values={}
  for row in rows:
   key=row.get('parameterId')
   if key in PARAMETERS:
    if key in values:raise ValueError('duplicate retained reader parameter')
    values[key]=row.findtext('i:value',namespaces=ns)
  if values!=PARAMETERS:raise ValueError('retained reader parameters differ')
 return _expected(profile)
