"""One independent, read-only recovery check of the native scratch write.

The caller supplies its fresh qualified recovery boot, exclusive console/claim
and reviewed plan. This neither opens a device nor grants programming authority.
"""
import hashlib
import re
import secrets
import threading

import recovery
from native_trace import checked_scratch_plan, SCRATCH_PATTERN_SHA256
from upload_memory import ScratchMemory

TAIL_RAM = 0x82800000
PREFIX_RAM = 0x83000000
MANIFEST_RAM = 0x82000000
PREFIX_BYTES = 32768*512


class ScratchReadback:
    def __init__(self, console, kit, plan, *, owns):
        self.plan=checked_scratch_plan(plan)
        self.kit=dict(kit,fit=bytes(kit['fit']),manifest=bytes(kit['manifest']))
        if (not 0<len(self.kit['fit'])<=8<<20 or not 0<len(self.kit['manifest'])<=16384 or
                not re.fullmatch('[0-9a-f]{64}',self.kit['prefix_sha256']) or
                hashlib.sha256(self.kit['fit']).hexdigest()!=self.kit['fit_sha256']):
            raise ValueError('qualified recovery files required')
        cid=bytes.fromhex(self.plan['cid'])
        self.card=(format(cid[0],'x'),format(int.from_bytes(cid[1:3],'big'),'x'),cid[3:8].decode('ascii'))
        if not all(32<=b<127 for b in cid[3:8]):raise ValueError('bounded ASCII spare-card product name required')
        self.console,self.owns=console,owns
        self.memory=ScratchMemory();self.token=secrets.token_hex(16)
        self.retired=False;self.guard=threading.Lock()

    def run(self, receipt):
        if not self.guard.acquire(blocking=False):raise ValueError('scratch readback active')
        try:
            if self.retired:raise ValueError('scratch readback retired')
            self.retired=True
            if (set(receipt)-{'commands','sd_write_attempted'} or
                    receipt.get('commands')!=[] or receipt.get('sd_write_attempted',False) is not False):
                raise ValueError('fresh read-only receipt required')
            receipt.update(status='running',read_only=True,sd_write_attempted=False,
                memory_observations=[],session=self.token,lba=self.plan['sectors']-8,sectors=8,
                pattern_sha256=SCRATCH_PATTERN_SHA256,plan=dict(self.plan),
                independent_tail_readback=False,boot_files_unchanged=False,
                recovery_manifest_unchanged=False,mbr_unchanged=False,protected_prefix_unchanged=False)
            try:
                self._run(receipt)
            except BaseException as error:
                receipt.update(status='failed',error=str(error),retired=True)
                raise
        finally:
            self.guard.release()

    def _run(self, receipt):
        def command(value, timeout=15):
            if not self.owns():raise ValueError('scratch readback claim changed')
            record={'command':value,'status':'started'}
            receipt['commands'].append(record)
            output=self.console.command(value,timeout)
            record.update(output=output,status='returned')
            if not self.owns():raise ValueError('scratch readback claim changed')
            return output

        def observe(stage):
            raw=command('echo FES_SCRATCH_SESSION=${fes_sd_scratch_session}')
            markers=re.findall(r'(?m)^FES_SCRATCH_SESSION=([^\r\n]*)\r?$',raw)
            first=self.memory.baseline is None
            if markers!=['' if first else self.token]:raise ValueError('readback boot marker differs')
            actual=self.memory.observe(command('bdinfo'))
            if first:
                command('setenv fes_sd_scratch_session '+self.token)
                if re.findall(r'(?m)^FES_SCRATCH_SESSION=([^\r\n]*)\r?$',command('echo FES_SCRATCH_SESSION=${fes_sd_scratch_session}'))!=[self.token]:
                    raise ValueError('readback boot marker not retained')
            receipt['memory_observations'].append({'stage':stage,'map':actual})

        def digest(address,size,expected):
            raw=command(f'hash sha256 {address:x} {size:x}')
            rows=re.findall(r'(?m)^sha256 for ([0-9a-fA-F]+) \.\.\. ([0-9a-fA-F]+) ==> ([0-9a-fA-F]{64})\r?$',raw)
            if (len(re.findall(r'(?m)^sha256 for ',raw))!=1 or len(rows)!=1 or
                    (int(rows[0][0],16),int(rows[0][1],16),rows[0][2].lower())!=
                    (address,address+size-1,expected)):
                raise ValueError('recovery readback hash differs')

        def raw_read(address,lba,count):
            raw=command(f'mmc read {address:x} {lba:x} {count:x}',45)
            rows=re.findall(r'(?m)^MMC read: dev # (\d+), block # (\d+), count (\d+) \.\.\. (\d+) blocks read: OK\r?$',raw)
            if (len(re.findall(r'(?m)^MMC read:',raw))!=1 or len(rows)!=1 or
                    tuple(map(int,rows[0]))!=(0,lba,count,count)):
                raise ValueError('raw SD read incomplete or wrong device/extent')

        def files():
            for name,address,data,expected in (
                ('u-boot.itb',PREFIX_RAM,self.kit['fit'],self.kit['fit_sha256']),
                ('FES/RECOVERY.JSON',MANIFEST_RAM,self.kit['manifest'],hashlib.sha256(self.kit['manifest']).hexdigest())):
                command('fatsize mmc 0:1 '+name)
                sizes=re.findall(r'(?m)^FES_SCRATCH_FILESIZE=([0-9a-fA-F]+)\r?$',command('echo FES_SCRATCH_FILESIZE=${filesize}'))
                if len(sizes)!=1 or int(sizes[0],16)!=len(data):raise ValueError('protected file size differs')
                out=command(f'fatload mmc 0:1 {address:x} {name} {len(data):x}',30)
                if not re.search(r'(?m)^\s*'+str(len(data))+r' bytes read(?:\s|$)',out):raise ValueError('protected file read incomplete')
                digest(address,len(data),expected)
                self.memory.loaded(address,len(data))

        def media():
            raw_read(PREFIX_RAM,0,32768);digest(PREFIX_RAM,PREFIX_BYTES,self.kit['prefix_sha256'])
            raw_read(TAIL_RAM,0,1);digest(TAIL_RAM,512,self.plan['mbr_sha256'])
            files()

        recovery.check_version(command('version'))
        observe('admit')
        command('mmc dev 0')
        info=command('mmcinfo')
        if ('SD version' not in info or 'Capacity: 3.7 GiB' not in info or
                re.findall(r'(?m)^Manufacturer ID:\s*([0-9a-fA-F]+)\s*$',info)!=[self.card[0]] or
                re.findall(r'(?m)^OEM:\s*([0-9a-fA-F]+)\s*$',info)!=[self.card[1]] or
                re.findall(r'(?m)^Name:\s*([^\r\n]+?)\s*\r?$',info)!=[self.card[2]]):
            raise ValueError('independent spare-card manufacturer/product/capacity differs')
        rows=re.findall(r'(?m)^\s*(\d+)\s+(\d+)\s+(\d+)\s',command('mmc part'))
        if rows!=[('1','32768','262144')]:raise ValueError('qualified spare partition layout differs')
        media()
        observe('before-tail')
        if any(low<TAIL_RAM+4096 and TAIL_RAM<=high for low,high,_ in self.memory.owned):
            raise ValueError('tail read would overwrite an owned file load')
        raw_read(TAIL_RAM,self.plan['sectors']-8,8)
        digest(TAIL_RAM,4096,SCRATCH_PATTERN_SHA256)
        # Raw MMC reads do not allocate LMB regions. Do not invent a file-load
        # reservation; require the actual map to remain identical instead.
        observe('after-tail')
        media()
        observe('complete')
        receipt.update(status='verified',independent_tail_readback=True,boot_files_unchanged=True,
            recovery_manifest_unchanged=True,mbr_unchanged=True,protected_prefix_unchanged=True,
            card_summary_verified=True,full_cid_reobserved=False)
