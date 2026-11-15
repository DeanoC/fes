"""Check exact startup object bytes and every changed linked relocation site."""
import hashlib,struct
from pathlib import Path


class ELF:
    def __init__(self,path):
        self.path=Path(path);self.data=self.path.read_bytes();d=self.data
        assert d[:6]==b'\x7fELF\x02\x01'
        h=struct.unpack_from('<16sHHIQQQIHHHHHH',d)
        self.sections=[struct.unpack_from('<IIQQQQIIQQ',d,h[6]+i*h[11]) for i in range(h[12])]
        r=self.sections[h[13]];names=d[r[4]:r[4]+r[5]]
        self.names=[self.string(names,r[0]) for r in self.sections]
        self.symbols=[];self.tables={}
        for i,s in enumerate(self.sections):
            if s[1]!=2:continue
            st=self.sections[s[6]];strings=d[st[4]:st[4]+st[5]];symbols=[]
            for off in range(s[4],s[4]+s[5],s[9]):
                name,info,other,index,value,size=struct.unpack_from('<IBBHQQ',d,off)
                symbols.append({'name':self.string(strings,name),'info':info,'index':index,'value':value,'size':size})
            self.tables[i]=symbols;self.symbols+=symbols

    @staticmethod
    def string(data,off):return data[off:data.index(0,off)].decode()

    def function(self,name):
        found=[s for s in self.symbols if s['name']==name and s['info']&15==2 and s['size'] and s['index']<len(self.sections)]
        assert len(found)==1,(self.path,name,len(found))
        return found[0]

    def content(self,s):
        section=self.sections[s['index']];start=section[4]+s['value']-section[3]
        return self.data[start:start+s['size']]

    def relocations(self,f):
        found=[]
        for r in self.sections:
            if r[1]!=4 or r[7]!=f['index']:continue
            for off in range(r[4],r[4]+r[5],r[9]):
                offset,info,addend=struct.unpack_from('<QQq',self.data,off)
                if not f['value']<=offset<f['value']+f['size']:continue
                sym=self.tables[r[6]][info>>32]
                name=sym['name'] or self.names[sym['index']]
                found.append((offset-f['value'],info&0xffffffff,name,addend))
        return sorted(found)

    def address_bytes(self,address,size):
        for section in self.sections:
            if section[1]!=8 and section[3]<=address and address+size<=section[3]+section[5]:
                off=section[4]+address-section[3];return self.data[off:off+size]
        raise AssertionError(('address is not file-backed',hex(address),size))


def immediate21(word):
    imm=((word>>5)&0x7ffff)<<2 | (word>>29)&3
    return imm-(1<<21) if imm&(1<<20) else imm


def targets(linked,obj,func,code,relocations):
    masks={274:0x60ffffe0,275:0x60ffffe0,277:0x003ffc00,282:0x03ffffff,283:0x03ffffff,285:0x003ffc00}
    original=obj.content(obj.function(func['name']))
    for offset,kind,target,addend in relocations:
        assert kind in masks,('unreviewed relocation type',kind)
        word=struct.unpack_from('<I',code,offset)[0]
        object_word=struct.unpack_from('<I',original,offset)[0]
        assert word&~masks[kind]==object_word&~masks[kind],(func['name'],target,'non-relocation bits changed')
        name=target.removeprefix('.bss.')
        candidates=[s for s in linked.symbols if s['name']==name and s['index']]
        values={s['value']+addend for s in candidates}
        pc=func['value']+offset
        if kind in (282,283):
            imm=word&0x3ffffff
            if imm&(1<<25):imm-=1<<26
            assert pc+imm*4 in values,(func['name'],target,'wrong branch target')
        elif kind==274:assert pc+immediate21(word) in values,(func['name'],target,'wrong ADR target')
        elif values:
            if kind==275:assert ((pc&~4095)+(immediate21(word)<<12)) in {v&~4095 for v in values},(func['name'],target,'wrong ADRP page')
            elif kind==277:assert (word>>10)&4095 in {v&4095 for v in values},(func['name'],target,'wrong ADD low bits')
            elif kind==285:assert ((word>>10)&4095)*4 in {v&4095 for v in values},(func['name'],target,'wrong load low bits')
        else:
            # The early setup's merged string sections have no final symbol.
            # Resolve their paired ADRP+ADD and verify the exact source string.
            assert target.startswith('.rodata.') and kind in (275,277),(func['name'],target)
            high=[r for r in relocations if r[1:]==(275,target,addend)]
            low=[r for r in relocations if r[1:]==(277,target,addend)]
            assert len(high)==len(low)==1
            hi,lo=high[0][0],low[0][0]
            wh=struct.unpack_from('<I',code,hi)[0];wl=struct.unpack_from('<I',code,lo)[0]
            assert wh&31 == wl>>5&31
            address=((func['value']+hi)&~4095)+(immediate21(wh)<<12)+((wl>>10)&4095)
            section=obj.sections[obj.names.index(target)]
            expected=obj.data[section[4]+addend:section[4]+section[5]].split(b'\0',1)[0]+b'\0'
            assert expected and len(expected)<=512 and linked.address_bytes(address,len(expected))==expected,(func['name'],target,'linked literal differs')


def check(old_dir,new_dir,old_linked,new_linked,names):
    linked_old,linked_new=ELF(old_linked),ELF(new_linked)
    def objects(directory):
        found={}
        for path in Path(directory).glob('*.o'):
            e=ELF(path)
            for s in e.symbols:
                if s['name'] in names and s['size'] and s['info']&15==2:
                    assert s['name'] not in found
                    found[s['name']]=e
        return found
    a,b=objects(old_dir),objects(new_dir);out={}
    for name in names:
        old,new=a[name],b[name];f,g=old.function(name),new.function(name)
        before,after=old.content(f),new.content(g)
        assert before==after,(name,'pre-relocation instruction bytes changed')
        ra,rb=old.relocations(f),new.relocations(g)
        assert ra==rb,(name,'relocation target/type/addend changed',ra,rb)
        lf,lg=linked_old.function(name),linked_new.function(name)
        la,lb=linked_old.content(lf),linked_new.content(lg)
        assert len(la)==len(lb)==len(before)
        sites={offset for offset,_,_,_ in rb}
        changed=[offset for offset in range(0,len(la),4) if la[offset:offset+4]!=lb[offset:offset+4]]
        assert set(changed)<=sites,(name,'linked change outside relocation',changed,sites)
        targets(linked_old,old,lf,la,ra)
        targets(linked_new,new,lg,lb,rb)
        for offset,kind,target,addend in rb:
            # CALL26/JUMP26: prove the linked destination is the same named
            # function, at its legitimate new address, plus the same addend.
            if kind in (282,283):
                for e,func,data in ((linked_old,lf,la),(linked_new,lg,lb)):
                    word=struct.unpack_from('<I',data,offset)[0];imm=word&0x3ffffff
                    if imm&(1<<25):imm-=1<<26
                    target_symbols=[s for s in e.symbols if s['name']==target and s['index']]
                    assert len(target_symbols)==1,(name,target)
                    assert func['value']+offset+imm*4==target_symbols[0]['value']+addend,(name,target,'wrong branch target')
        out[name]={'object_old':str(old.path),'object_new':str(new.path),'object_code_sha256':hashlib.sha256(before).hexdigest(),
                   'exact_object_code_and_relocations':True,'relocations':rb,'linked_changed_offsets':changed,
                   'changed_linked_words_only_at_relocations':True,'linked_call_targets_checked':True,
                   'all_relocation_bitfields_targets_and_literals_checked':True,
                   'old_address':lf['value'],'address':lg['value'],'size':len(before)}
    return out
