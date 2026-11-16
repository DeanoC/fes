"""Boot-FIT storage and recovery scratch, separate from FPGA configuration RAM."""
import json
FIT_MAX_BYTES=16<<20
FIT_VERIFY_BASE=0x99000000
UPLOAD_BASE=0x82000000
UPLOAD_READBACK=0x83000000
UPLOAD_END=0x84000000
PACKAGE_MAX_BYTES=8<<20
CONTRACT=dict(format=1,scope='sealed-native-FIT-16MiB-v1',max_bytes=FIT_MAX_BYTES,verify_base=FIT_VERIFY_BASE,
 verify_end=FIT_VERIFY_BASE+FIT_MAX_BYTES,upload_base=UPLOAD_BASE,readback_base=UPLOAD_READBACK,upload_end=UPLOAD_END)
def checked(value):
 if json.dumps(value,sort_keys=True)!=json.dumps(CONTRACT,sort_keys=True):raise ValueError('unsupported boot FIT memory contract')
 return dict(CONTRACT)
def for_entry(entry):
 base=entry.get('entry_image_address');size=entry.get('entry_image_size')
 if type(base) is not int or type(size) is not int or base!=0x96080000 or not 0<size<=32<<20:
  raise ValueError('fixed native entry window required for FIT copy')
 # The fixed maximum entry range, actual staging/exclusions and SPL's pinned
 # high DDR heap/BSS are all independent of the verified FIT source copy.
 ranges=[[0,0x80000],[0x80000000,0x82000000],[0x82000000,0x84000000],
  [0x84000000,0x94000000],[0x94000000,0x94010000],[0x94800000,0x95000000],
  [0x95000000,0x95800000],[0x95800000,0x95a00000],[base,base+(32<<20)],
  [0xb0000000,0xc0000000],*entry.get('staging_exclusions',[])]
 if any(first<FIT_VERIFY_BASE+FIT_MAX_BYTES and FIT_VERIFY_BASE<end for first,end in ranges):raise ValueError('FIT source copy overlaps an exclusion')
 if not 0x80000000<=FIT_VERIFY_BASE<FIT_VERIFY_BASE+FIT_MAX_BYTES<=0xc0000000:raise ValueError('FIT copy outside DDR')
 if UPLOAD_BASE+FIT_MAX_BYTES!=UPLOAD_READBACK or UPLOAD_READBACK+FIT_MAX_BYTES!=UPLOAD_END:raise ValueError('FIT upload buffers overlap or escape scratch')
 return dict(CONTRACT)
def header(value):
 checked(value)
 return '#define FES_NANO_VERIFIED_FIT_BASE 0x99000000UL\n#define FES_NANO_VERIFIED_FIT_MAX 16777216UL\n'
