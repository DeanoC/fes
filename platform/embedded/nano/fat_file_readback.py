"""Read one native-created file after a separately qualified recovery boot.

This shares the raw prefix/MBR, protected-file, fresh-marker and live LMB
observations with the scratch verifier. It issues no SD writes or network I/O.
"""
import hashlib

from fat_file_trace import checked_plan, payload
from sd_scratch_readback import ScratchReadback, TAIL_RAM


class FATFileReadback(ScratchReadback):
    target_label='file'

    def _checked_plan(self,plan):
        return checked_plan(plan)

    def __init__(self,console,kit,plan,*,owns):
        super().__init__(console,kit,plan,owns=owns)
        for prefix,key in (('recovery','manifest'),('fit','fit')):
            data=self.kit[key]
            if (len(data)!=self.plan[prefix+'_bytes'] or
                    hashlib.sha256(data).hexdigest()!=self.plan[prefix+'_sha256']):
                raise ValueError('FAT readback protected file differs from expected plan')
        self.updated=payload(self.plan['path'],updated=True)
        self.updated_sha256=hashlib.sha256(self.updated).hexdigest()

    def _target_metadata(self):
        return dict(path=self.plan['path'],bytes=len(self.updated),sha256=self.updated_sha256,
                    independent_file_readback=False,power_loss_qualification=False,ordinary_admission=False)

    def _read_target(self,raw_read,load_file,digest):
        if any(low<TAIL_RAM+len(self.updated) and TAIL_RAM<=high for low,high,_ in self.memory.owned):
            raise ValueError('file read would overwrite another owned file load')
        load_file(self.plan['path'],TAIL_RAM,len(self.updated),self.updated_sha256)
        # The target is hashed before the later MBR read reuses this session's
        # private buffer. Keep its real file-load reservation in the LMB ledger.

    def _target_pass(self):
        return dict(independent_file_readback=True)
