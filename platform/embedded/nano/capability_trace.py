"""Raw, read-only boot observation. No address, isolation or DMA authority."""
FIELDS=('idr0','idr1','idr3','idr5','cr1','cr2','sdm_ctrl','sdm_ids','sdm_ar','sdm_aw','por1','mac_ctrl','mac_ids')
PATTERN=(r'(?:NOTICE:[ \t]+)?FES_NANO_CAPABILITY status=(?P<status>-?[0-9]{1,10}) reads=(?P<reads>[0-9]{1,3}) '
 r'owner=(?P<owner>[0-9a-f]{1,16}) epoch=(?P<epoch>[0-9a-f]{1,16}) '+
 ' '.join(name+r'=(?P<'+name+r'>[0-9a-f]{1,8})' for name in FIELDS)+r' access=unqualified')
def errors(values,smmu,owner,epoch):
 result=[]
 if values['status']!=0 or values['reads']!=26:result.append('capability observation failed or has wrong read count')
 if owner is None or epoch is None or values['owner']!=owner or values['epoch']!=epoch:
  result.append('capability observation changed cold boot owner/epoch')
 if smmu is None or any(values[name]!=smmu[name] for name in ('idr0','idr1')):
  result.append('capability identity differs from initial SMMU observation')
 return result
