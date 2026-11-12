import pathlib,os,sys,stat,fcntl,struct,time,json,hashlib,datetime,subprocess
root=pathlib.Path('/home/deano/fes/out/dev/de25-linux-platform');sys.path.insert(0,str(root))
from scripts import de25_owner_image as owner
image_dir=pathlib.Path('/home/deano/fes/out/artifacts/de25-owner-image/829cf0353/candidate')
output=root/'out/owner-card-install-20261031';backup_dir=pathlib.Path('/home/deano/kit-status/rollback/de25-TRWU8J8G-before-owner-20261031');backup_dir.mkdir(exist_ok=False)
status=pathlib.Path('/home/deano/kit-status/de25-TRWU8J8G-STATUS.md');lock=open(str(status)+'.lock','a');fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
record={'board':'TRWU8J8G','device':'/dev/sda','expected_capacity':62534975488,'image_sha256':'de2a9a4ca35214ab75230d5807a0a8900d99f7869e1346c83f4e41bce0853138','image_bytes':3984588800,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'time_cap_seconds':1800,'source_revision':subprocess.check_output(['git','-C',str(root),'rev-parse','HEAD'],text=True).strip(),'backup':str(backup_dir/'baseline.img'),'flash_changed':False}
start=time.monotonic();last=-30

def emit(state,force=False,**extra):
 global last
 if time.monotonic()-start>1800:raise TimeoutError('card operation time cap')
 record.update(state=state,elapsed_seconds=round(time.monotonic()-start,1),**extra)
 if force or time.monotonic()-last>=30:
  temp=output/'receipt.tmp';temp.write_text(json.dumps(record,indent=2)+'\n');temp.replace(output/'receipt.json');print(json.dumps(record),flush=True);last=time.monotonic()

def note(message):
 with status.open('a') as f:f.write('\n'+message+'\n')

def digest_file(path):
 h=hashlib.sha256()
 with path.open('rb') as f:
  while data:=f.read(4<<20):h.update(data)
 return h.hexdigest()

def check_card(fd):
 info=os.fstat(fd)
 assert stat.S_ISBLK(info.st_mode) and os.major(info.st_rdev)==8 and os.minor(info.st_rdev)==0
 assert struct.unpack('Q',fcntl.ioctl(fd,0x80081272,b'\0'*8))[0]==record['expected_capacity']
 assert pathlib.Path('/sys/block/sda/removable').read_text().strip()=='1'
 prop=subprocess.check_output(['udevadm','info','--query=property','--name=/dev/sda'],text=True)
 assert 'ID_BUS=usb\n' in prop and 'ID_SERIAL=Generic_STORAGE_DEVICE-0:0\n' in prop
 assert 'ID_PATH=pci-0000:80:14.0-usb-0:11.4.1:1.0-scsi-0:0:0:0\n' in prop
 for line in pathlib.Path('/proc/self/mountinfo').read_text().splitlines():assert line.split()[2] not in ('8:0','8:1','8:2')

fd=None
try:
 note('## Active claim '+record['started_utc']+'\n- Owner: Codex #1312 owner-kernel SD/boot trial. Deano moved the bottom kit card to the reader after the prepared-image request. Scope: verify exact USB card/layout, preserve checked rollback bytes, write/read back the verified candidate, then board boot and userspace diagnostics. No QSPI or FPGA programming authorized by this stage.')
 emit('verifying-candidate',True)
 assert owner.verify_candidate(image_dir)['image_sha256']==record['image_sha256']
 fd=os.open('/dev/sda',os.O_RDWR|os.O_EXCL|os.O_CLOEXEC);check_card(fd)
 mbr=os.pread(fd,512,0);assert hashlib.sha256(mbr).hexdigest()==owner.boot.load_lock()['mbr_sha256']
 emit('backing-up-current-card',True)
 h=hashlib.sha256();remaining=record['image_bytes'];done=0
 with (backup_dir/'baseline.img').open('xb') as target:
  while remaining:
   data=os.read(fd,min(4<<20,remaining));assert data
   h.update(data)
   if any(data):target.write(data)
   else:target.seek(len(data),1)
   remaining-=len(data);done+=len(data);emit('backing-up-current-card',bytes_done=done)
  target.truncate(record['image_bytes']);target.flush();os.fsync(target.fileno())
 baseline=h.hexdigest();assert digest_file(backup_dir/'baseline.img')==baseline
 (backup_dir/'baseline.img').chmod(0o444)
 fat=backup_dir/'boot-readback';fat.mkdir();owner.boot.extract_boot_files(backup_dir/'baseline.img',fat,owner.boot.load_lock())
 record['baseline_sha256']=baseline;record['baseline_backup_verified']=True
 (backup_dir/'backup.json').write_text(json.dumps(record,indent=2)+'\n')
 # Keep the complete overwritten extent. The card's remaining unused tail is untouched.
 check_card(fd);assert os.pread(fd,512,0)==mbr
 emit('writing-candidate',True,bytes_done=0);os.lseek(fd,0,os.SEEK_SET);done=0
 with (image_dir/owner.IMAGE).open('rb') as source:
  while data:=source.read(4<<20):
   view=memoryview(data)
   while view:
    written=os.write(fd,view);assert written>0;view=view[written:]
   done+=len(data);emit('writing-candidate',bytes_done=done)
 assert done==record['image_bytes'];os.fsync(fd);fcntl.ioctl(fd,0x1261);os.close(fd);fd=None
 fd=os.open('/dev/sda',os.O_RDONLY|os.O_EXCL|os.O_CLOEXEC);check_card(fd);h=hashlib.sha256();remaining=record['image_bytes'];done=0
 emit('verifying-device-readback',True,bytes_done=0)
 while remaining:
  data=os.read(fd,min(4<<20,remaining));assert data;h.update(data);remaining-=len(data);done+=len(data);emit('verifying-device-readback',bytes_done=done)
 assert h.hexdigest()==record['image_sha256'];record['readback_sha256']=h.hexdigest();os.close(fd);fd=None
 subprocess.run(['blockdev','--rereadpt','/dev/sda'],check=True)
 emit('passed',True,bytes_done=done)
 note('## SD preparation complete; board claim retained '+datetime.datetime.now(datetime.timezone.utc).isoformat()+'\n- Candidate SD image written and complete 3,984,588,800-byte device readback matches '+record['image_sha256']+'. Exact overwritten baseline extent backed up/verified at '+record['backup']+' SHA256 '+baseline+'. Remaining card tail unchanged. No QSPI/FPGA or board boot action. Awaiting card insertion and candidate boot; do not treat as baseline promotion. Kit remains CLAIMED by Codex #1312.')
except BaseException as error:
 emit('failed',True,error=str(error));note('## SD trial requires attention\n- Card preparation failed; inspect '+str(output)+'/receipt.json before using card. Board remains claimed. Rollback backup if complete: '+str(backup_dir)+'.')
 raise
finally:
 if fd is not None:os.close(fd)
