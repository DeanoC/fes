"""Bind this new attempt to the actual recovery after its predecessor stopped."""
from pathlib import Path
import hashlib,json,tarfile
from transport import require,sha
N=Path('/home/deano/fes/out/dev/de25-source-release')
D=N/'out/recovery-chain-reset-proof';P=N/'out/recovery-chain-reset-probe'
F=N/'out/four-second-recovery-stop-proof';U=N/'out/recovery-chain-usb-reset'
def read(p):return json.loads(Path(p).read_text())
def classify(r,proof):
 require(r['status']=='passed-independent-recovery-only-observation' and r['procedure_sha256']=='cd52d5e8790fe6d31cb31e85e8c4fa27c522ef364042f8e03008cfb06fd311ca','different recovery observation')
 v=r['observation'];require(v['fresh_recovery_verified'] and v['read_only_registers_verified'] and v['NS_reset_reads']==19 and v['USB0_reset_pair_held'] and v['SD_or_QSPI_writes']==0,'incomplete or writing recovery')
 require(not v['ordinary_runtime_admission'] and not v['Secure_SMMU_reset_qualified'] and not v['source_reuse_authorized'],'recovery grants unsupported native/Secure/reuse authority')
 require(len(r['root_process_scan'])==3 and all(q['original_process_stopped'] for q in r['root_process_scan']) and r['root_adapter_and_UART_FD_scan']['open_handles']==[] and r['USB_identity_inode_permissions_restored'] and r['JTAG_auto_1_15M_restored'],'recovery handles or transport not restored')
 require(proof['recovery_observation_passed'] and proof['all_hardware_handles_closed'] and proof['SD_or_QSPI_writes']==0 and not proof['native_dispatch'] and not proof['hardware_acceptance'],'wrong recovery receipt')
 return True
def check():
 r=read(D/'result.json');ret=read(D/'retention-proof.json');classify(r,ret)
 require(ret['archive_sha256']=='859b73e1e634c63c22624010d576b7c10fa689cf7a1acfa747070864292e30de' and sha(ret['archive'])==ret['archive_sha256'],'actual recovery archive changed')
 selected=[(D/'result.json','analysis/result.json'),(P/'procedure.json','procedure/procedure.json'),(P/'attempt/result.json','attempt/result.json'),(P/'attempt/recovery-rollback/completed-program.json','attempt/recovery-rollback/completed-program.json')]
 with tarfile.open(ret['archive'],'r:gz') as t:
  for p,name in selected:
   m=t.getmember(name);require(m.isfile() and hashlib.file_digest(t.extractfile(m),'sha256').hexdigest()==sha(p),'archived actual recovery differs')
 procedure=read(P/'procedure.json')
 require(procedure['failed_procedure_sha256']=='f39c1a04a59680865e2b686295af04c66e3c71b1befe00aa83ccf53855c60772' and procedure['failed_trace_archive_sha256']=='0e028e09b082f02756c50cd9f22e3261d506fe4631cf20c0324bc4d69e1753e1' and procedure['host_reset_archive_sha256']=='d194360fafb9b8a97ce63c9db6432851fa4a9bd88bff9529c7bf58404a12ddd9','recovery belongs to another stop/reset')
 for d,key in [(F,'failed_trace_archive_sha256'),(U,'host_reset_archive_sha256')]:
  v=read(d/'retention-proof.json');require(v['archive_sha256']==procedure[key] and sha(v['archive'])==v['archive_sha256'],'starting failure/reset archive differs')
 files={str(p):sha(p) for p,_ in selected};files.update({str(D/'retention-proof.json'):sha(D/'retention-proof.json'),ret['archive']:ret['archive_sha256']})
 for d in [F,U]:
  q=d/'retention-proof.json';v=read(q);files[str(q)]=sha(q);files[v['archive']]=v['archive_sha256']
 return dict(scope='actual-host-adapter-reset-and-read-only-recovery-after-f39',files=files,fresh_recovery_observed=True,Secure_SMMU_reset_qualified=False,DMA_authority=False,native_dispatch=False,fresh_epoch_required=True)
