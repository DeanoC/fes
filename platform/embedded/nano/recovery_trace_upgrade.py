"""One explicit trace-recovery upgrade from qualified PHY-prepare recovery.

This private prototype preserves rollback files and independently verifies the new
FAT boot FIT, raw mirror, manifest and protected native/package files. It never
activates the new loader itself. Any failure consumes the attempt.
"""
import hashlib,json,re,secrets
from pathlib import Path
from upload_memory import ScratchMemory
from recovery_return_package import BASE_FIT,BASE_SPL,BASE_BL31,MODE
import recovery
import recovery_return_trace

OLD_VERSION='U-Boot 2025.01'+recovery.LOCALVERSION
BOOT='u-boot.itb';META='FES/RECOVERY.JSON';PRIME='FES/UPLOAD.BIN';PACKAGE='FES/PKGTEST.BIN'
BACKUPS={BOOT:'FES/R1107A.ITB',META:'FES/R1107A.JSN'}
def sha(raw):return hashlib.sha256(raw).hexdigest()

class Upgrade:
    def __init__(self,console,old,new,prime,package,transfer,owns,save):
        self.console=console
        self.old={k:bytes(old[k]) if k in ('fit','manifest') else str(old[k]) for k in ('fit','manifest','prefix_sha256')}
        self.new={k:bytes(new[k]) if k in ('fit','manifest') else str(new[k]) for k in ('fit','manifest','prefix_sha256')}
        self.prime,self.package=bytes(prime),bytes(package)
        self.transfer,self.owns,self.save=transfer,owns,save
        self.active=False
        self.memory=ScratchMemory();self.attempted=False;self.token=secrets.token_hex(16)
        self.r={'scope':'one explicit UART spare-card recovery upgrade; no Ethernet or native activation',
                'pass':False,'sd_write_attempted':False,'writes':[],'commands':[],'memory':[],'transfers':[]}
    def persist(self):self.save(self.r)
    def require(self):
        if not self.active:raise ValueError('upgrade session is retired or not started')
        if not self.owns():raise ValueError('upgrade lost kit claim')
    def command(self,value,timeout=15):
        self.require()
        output=self.console.command(value,timeout)
        if not self.owns():raise ValueError('upgrade lost kit claim during command')
        self.r['commands'].append({'command':value,'output':output});self.persist();return output
    def marker(self,name):
        key='FES_UPGRADE_'+name.upper()
        out=self.command('echo '+key+'=${'+name+'}')
        values=re.findall(r'(?m)^'+key+r'=([^\r\n]*)\r?$',out)
        if len(values)!=1:raise ValueError('upgrade marker observation missing')
        return values[0]
    def scope(self,stage):
        if self.marker('fes_upgrade_session')!=self.token:raise ValueError('upgrade boot/session changed')
        observed=self.memory.observe(self.command('bdinfo'))
        self.r['memory'].append({'stage':stage,'map':observed});self.persist()
    def digest(self,address,data):
        text=self.command(f'hash sha256 {address:x} {len(data):x}')
        if not re.search(r'(?i)(?<![0-9a-f])'+sha(data)+r'(?![0-9a-f])',text):raise ValueError('upgrade RAM/SD digest differs')
    def read(self,name,address,data):
        out=self.command(f'fatload mmc 0:1 {address:x} '+name,60)
        if not re.search(r'(?m)^\s*'+str(len(data))+r' bytes read(?:\s|$)',out):raise ValueError('upgrade FAT read length differs')
        self.memory.loaded(address,len(data));self.digest(address,data)
    def prefix(self,expected):
        out=self.command('mmc read 83000000 0 8000',45)
        if '32768 blocks read: OK' not in out:raise ValueError('upgrade prefix read incomplete')
        text=self.command('hash sha256 83000000 1000000')
        if not re.search(r'(?i)(?<![0-9a-f])'+expected+r'(?![0-9a-f])',text):raise ValueError('upgrade protected prefix differs')
    def write_fat(self,name,address,data):
        if name not in (BOOT,META,*BACKUPS.values()):raise ValueError('upgrade target is outside fixed files')
        self.scope('before-write-'+name);self.digest(address,data)
        row={'target':name,'sha256':sha(data),'bytes':len(data),'attempted':True,'acknowledged':False}
        self.r['sd_write_attempted']=True;self.r['writes'].append(row);self.persist()
        out=self.command(f'fatwrite mmc 0:1 {address:x} '+name+f' {len(data):x}',60)
        if not re.search(r'(?m)^\s*'+str(len(data))+r' bytes written(?:\s|$)',out):raise ValueError('upgrade FAT write length differs')
        row['acknowledged']=True;self.persist()
        self.read(name,0x83000000,data);row['readback_verified']=True;self.persist()
    def receive(self,purpose,data):
        self.require()
        self.scope('before-uart-'+purpose)
        row={'purpose':purpose,'bytes':len(data),'sha256':sha(data),'attempted':True,'complete':False}
        self.r['transfers'].append(row);self.persist()
        observation=self.transfer(purpose,data,len(self.prime))
        self.require()
        if (observation['pass'] is not True or observation['source_bytes']!=len(data) or
            observation['source_sha256']!=sha(data) or observation['address']!=0x82000000 or
            observation['reserved_bytes']!=len(self.prime) or not observation['prompt_parked']):
            raise ValueError('UART transfer receipt differs')
        self.scope('after-uart-'+purpose);self.digest(0x82000000,data)
        row.update(complete=True,observation=observation);self.persist()
    def run(self):
        if self.attempted:raise ValueError('upgrade attempt is consumed')
        self.attempted=True;self.active=True
        try:
            self.persist()
            if not (0<len(self.old['fit'])<2<<20 and 0<len(self.new['fit'])<2<<20 and
                    0<len(self.old['manifest'])<16384 and 0<len(self.new['manifest'])<16384 and
                    4<<20<len(self.prime)<=8<<20 and 0<len(self.package)<=8<<20):
                raise ValueError('upgrade source lengths differ from fixed bootstrap contract')
            if any(re.fullmatch(r'[a-f0-9]{64}',p['prefix_sha256']) is None for p in (self.old,self.new)):
                raise ValueError('upgrade prefix identity differs')
            before,after=json.loads(self.old['manifest']),json.loads(self.new['manifest'])
            if (not isinstance(before,dict) or not isinstance(after,dict) or
                    sha(self.old['fit'])!=BASE_FIT or before.get('fit_sha256')!=BASE_FIT or
                    before.get('native_handoff') is not False or before.get('automatic_writes') is not False or
                    before.get('return_observations') is not None or sha(self.new['fit'])==BASE_FIT or
                    after.get('fit_sha256')!=sha(self.new['fit']) or
                    after.get('spl_sha256')!=BASE_SPL or after.get('bl31_sha256')!=BASE_BL31 or
                    after.get('return_observations')!=dict(scope=MODE,phases=len(recovery_return_trace.PHASES),
                        profile_sha256=sha(Path(recovery_return_trace.__file__).read_bytes())) or
                    after.get('native_handoff') is not False or after.get('automatic_writes') is not False):
                raise ValueError('upgrade is not the exact baseline-to-trace transition')
            versions=re.findall(r'(?m)^U-Boot [^\r\n]*\r?$',self.command('version'))
            if (len(versions)!=1 or re.fullmatch(re.escape(OLD_VERSION)+
                    r' \([^()\r\n]{1,100}\)socfpga_agilex5\r?',versions[0]) is None):
                raise ValueError('upgrade requires the explicitly qualified old recovery loader')
            if self.marker('fes_upload_session') or self.marker('fes_upgrade_session'):
                raise ValueError('upgrade cannot adopt a previous boot/session')
            self.memory.observe(self.command('bdinfo'))
            self.command('setenv fes_upgrade_session '+self.token)
            self.scope('admit')
            self.command('mmc dev 0')
            info=self.command('mmcinfo')
            if 'SD version' not in info or '3.7 GiB' not in info:raise ValueError('upgrade spare card type/capacity differs')
            if not re.search(r'(?m)^\s*1\s+32768\s+262144\s',self.command('mmc part')):raise ValueError('upgrade partition differs')
            files=self.command('fatls mmc 0:1 FES').lower()
            if any(name.rsplit('/',1)[1].lower() in files for name in BACKUPS.values()):raise ValueError('upgrade rollback destination already exists')
            self.prefix(self.old['prefix_sha256'])
            self.read(PACKAGE,0x82000000,self.package)
            self.read(BOOT,0x83000000,self.old['fit']);self.read(META,0x82800000,self.old['manifest'])
            # A real FAT load reserves the entire UART window in live LMB.
            self.read(PRIME,0x82000000,self.prime);self.scope('reserved-uart-window')
            self.receive('probe',bytes(range(256))*4)
            self.write_fat(BACKUPS[BOOT],0x83000000,self.old['fit'])
            self.write_fat(BACKUPS[META],0x82800000,self.old['manifest'])
            padded=(max(len(self.old['fit']),len(self.new['fit']))+511)&~511
            image=self.new['fit']+bytes(padded-len(self.new['fit']))
            self.receive('fit',image)
            self.write_fat(BOOT,0x82000000,self.new['fit'])
            self.scope('before-raw-fit');self.digest(0x82000000,image)
            row={'target':'raw-fit-sector-512','bytes':padded,'sha256':sha(image),'attempted':True,'acknowledged':False}
            self.r['writes'].append(row);self.persist()
            blocks=padded//512;out=self.command(f'mmc write 82000000 200 {blocks:x}',60)
            if not re.search(r'\b'+str(blocks)+r' blocks written: OK',out):raise ValueError('upgrade raw FIT write incomplete')
            row['acknowledged']=True;self.persist();self.prefix(self.new['prefix_sha256'])
            row['readback_verified']=True;self.persist()
            self.receive('manifest',self.new['manifest'])
            self.write_fat(META,0x82000000,self.new['manifest'])
            self.read(BOOT,0x83000000,self.new['fit']);self.read(META,0x82800000,self.new['manifest'])
            self.read(PRIME,0x83000000,self.prime);self.read(PACKAGE,0x83000000,self.package)
            self.read(BACKUPS[BOOT],0x83000000,self.old['fit']);self.read(BACKUPS[META],0x83000000,self.old['manifest'])
            self.prefix(self.new['prefix_sha256']);self.scope('complete')
            if not self.console.parked:raise ValueError('upgrade did not end at the same loader prompt')
            self.r.update(pass_=True,new_card_verified=True,native_activation=False,network_used=False,
                          rollback_files_verified=True,protected_native_package_verified=True,prompt_parked=True)
            self.r['pass']=self.r.pop('pass_');self.persist();return self.r
        except BaseException as error:
            self.r['error']=str(error);self.persist();raise
        finally:
            self.active=False;self.r['session_retired']=True;self.persist()
