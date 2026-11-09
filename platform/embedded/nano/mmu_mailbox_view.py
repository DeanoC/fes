"""Cold-only mailbox register views: fixed reads, no commands or authority."""
import hashlib
import json
import re
import struct

ALIAS=0x90000
PHYSICAL=0x10a30000
REGISTERS=(('cin',0),('cout',0x20),('rin',0x24),('rout',4),('bell',0x480),('status',0x2c))
SCOPE=dict(sha256='2df96d2e53a7589c3ac5a9b800769a2e33e9332adab0e86e137f3c7df9b2820f',bytes=356,
           external_branch_indices=[2,7,12,18,24,29,34,38,48,57,61,65,68,74,78,81,83,85,87],root_indices=[36])
CONTRACT=dict(format=1,scope='cold-el3-mailbox-readonly-views-v1',alias=ALIAS,physical=PHYSICAL,bytes=4096,
              secure=True,readonly=True,execute_never=True,registers=[list(v) for v in REGISTERS],
              phases=['mmu-off','mmu-on-ns','mmu-on-secure-alias'],guard=SCOPE,
              maximum_reads=18,record='six-u32-hex16-comma-v1',hex_formatter='fixed-local-uart-hex-v1',offset_encoding='six-nine-bit-word-offsets-v1',uart_polls=100000,uart_bytes=384,hardware_authority=False)
SHA=hashlib.sha256(json.dumps(CONTRACT,sort_keys=True,separators=(',',':')).encode()).hexdigest()


# Six 9-bit word offsets fit in one private scratch register. No pointer table
# or response FIFO is read; the six-iteration back edge is checked in the ELF.
OFFSETS=sum((offset//4)<<(9*i) for i,(_,offset) in enumerate(REGISTERS))


def assembly():
    rows=['/* Fixed cold-only reads; original CPU/MMU/all-master guards frame each view. */',
          '.macro mmu_mailbox_snapshot phase,base']
    def label(kind):
        name='fes_nano_mmu_mailbox_'+kind+'_\\phase'
        rows.extend(['.global '+name,'.type '+name+',%function',name+':'])
        return name
    def end(name):rows.append('.size '+name+',.-'+name)
    name=label('scope_before');rows.append('mmu_log_scope');end(name)
    rows+=['mov w17,#384','mmu_text .Lmailbox_begin_\\phase']
    name=label('setup')
    rows.append('movz x6,#'+hex(OFFSETS&65535))
    rows.extend('movk x6,#'+hex((OFFSETS>>shift)&65535)+',lsl #'+str(shift) for shift in (16,32,48))
    rows+=['mmu_constant x5,\\base','mov w4,#0'];end(name)
    rows+=['mov w17,#384','mmu_text .Lmailbox_row_\\phase']
    name=label('reads')
    rows+=['mov x0,#9','mul x0,x4,x0','lsrv x0,x6,x0','and x0,x0,#0x1ff',
           'add x0,x5,x0,lsl #2','ldr w1,[x0]'];end(name)
    rows+=['mmu_hex x1','mov w9,#44','mmu_char']
    name=label('loop');rows+=['add w4,w4,#1','cmp w4,#6','b.lo fes_nano_mmu_mailbox_reads_\\phase'];end(name)
    rows+=['mmu_text .Lnewline']
    name=label('scope_after');rows.append('mmu_log_scope');end(name)
    rows+=['.endm','.pushsection .rodata.fes_mmu_mailbox,"a"']
    for phase in range(3):
        rows+=['.Lmailbox_begin_'+str(phase)+': .asciz "FES_NANO_EL3_MMU_MAILBOX phase='+str(phase)+' BEGIN\\r\\n"',
               '.Lmailbox_row_'+str(phase)+': .asciz "FES_NANO_EL3_MMU_MAILBOX phase='+str(phase)+' values="']
    return '\n'.join(rows+['.popsection'])+'\n'


def scope_fingerprint(code,address,park,root):
    if len(code)!=SCOPE['bytes']:raise ValueError('mailbox view guard size differs')
    def signed(value,bits):return value-(1<<bits) if value>>(bits-1) else value
    words=list(struct.unpack('<'+str(len(code)//4)+'I',code));external=[];roots=[]
    for i,word in enumerate(words):
        pc=address+4*i;mask=None
        if word&0xfc000000==0x14000000:mask,shift,bits=0x03ffffff,0,26
        elif word&0xff000010==0x54000000 or word&0x7e000000==0x34000000:mask,shift,bits=0x00ffffe0,5,19
        elif word&0x7e000000==0x36000000:mask,shift,bits=0x0007ffe0,5,14
        if mask is not None:
            target=pc+4*signed((word&mask)>>shift,bits)
            if not address<=target<address+len(code):
                if target!=park:raise ValueError('mailbox view guard escapes its terminal park')
                external.append(i);words[i]=word&~mask
        if word&0x9f000000==0x10000000:
            target=pc+signed(((word>>5)&0x7ffff)*4+((word>>29)&3),21)
            if target!=root or word&31!=3:raise ValueError('mailbox view guard root differs')
            roots.append(i);words[i]=word&~0x60ffffe0
    result=dict(sha256=hashlib.sha256(struct.pack('<'+str(len(words))+'I',*words)).hexdigest(),
                bytes=len(code),external_branch_indices=external,root_indices=roots)
    if result!=SCOPE:raise ValueError('mailbox view CPU/MMU/master guard differs')
    return result


def verify(image,symbols,bounds,file_bytes):
    writer,length=symbols.get('fes_nano_mmu_transition_write',(0,0));sctlr=symbols.get('fes_nano_mmu_transition_sctlr',(0,0))[0]
    park=symbols.get('fes_nano_mmu_transition_park',(0,0))[0];root=bounds['tables']['root'];sites=[];end=writer
    for phase,base in enumerate((PHYSICAL,PHYSICAL,ALIAS)):
        group={}
        for kind in ('scope_before','setup','reads','loop','scope_after'):
            address,size=symbols.get('fes_nano_mmu_mailbox_'+kind+'_'+str(phase),(0,0))
            if not end<=address<address+size<=writer+length:raise ValueError('mailbox view site order or bounds differs')
            end=address+size;code=file_bytes(image,address,size)[0]
            if kind.startswith('scope'):
                scope_fingerprint(code,address,park,root)
            else:
                if kind=='setup':
                    expected=[0xd2800006|((OFFSETS&65535)<<5)]
                    expected += [0xf2800006|((shift//16)<<21)|(((OFFSETS>>shift)&65535)<<5) for shift in (16,32,48)]
                    expected += [0xd2800005|((base&65535)<<5),0xf2a00005|((base>>16)<<5),0x52800004]
                elif kind=='reads':expected=[0xd2800120,0x9b007c80,0x9ac024c0,0x92402000,0x8b0008a0,0xb9400001]
                else:
                    target=group['reads']['address'];delta=(target-address-8)//4
                    expected=[0x11000484,0x7100189f,0x54000003|((delta&0x7ffff)<<5)]
                if code!=struct.pack('<'+str(len(expected))+'I',*expected):
                    raise ValueError('mailbox view setup, read width, address or loop bound differs')
            group[kind]=dict(address=address,bytes=size,sha256=hashlib.sha256(code).hexdigest())
        if phase==0 and end>sctlr or phase and group['scope_before']['address']<sctlr+8:
            raise ValueError('mailbox view crossed its MMU transition phase')
        sites.append(group)
    return dict(contract_sha256=SHA,sites=sites,maximum_reads=18,hardware_authority=False)


BEGIN=re.compile(r'FES_NANO_EL3_MMU_MAILBOX phase=([0-2]) BEGIN')
ROW=re.compile(r'FES_NANO_EL3_MMU_MAILBOX phase=([0-2]) values='+r'([0-9a-f]{16},)'*6)

def state():return dict(records=[],pending=None,valid=True,complete=False,hardware_authority=False)

def observe(s,line,number):
    def bad():
        s['valid']=s['complete']=False
        return 'invalid/out-of-order MMU mailbox view'
    if not s['valid'] or s['complete']:return bad()
    begin=BEGIN.fullmatch(line)
    if begin:
        if s['pending'] is not None or int(begin[1])!=len(s['records']):return bad()
        s['pending']=dict(phase=int(begin[1]),line=number);return None
    match=ROW.fullmatch(line)
    if not match or s['pending'] is None or int(match[1])!=s['pending']['phase']:return bad()
    values={name:int(match[i+2][:-1],16) for i,(name,_) in enumerate(REGISTERS)}
    if any(value>=1<<32 for value in values.values()):return bad()
    s['records'].append(dict(phase=int(match[1]),values=values,line=number,begin_line=s['pending']['line']))
    s['pending']=None;s['complete']=len(s['records'])==3
    return None


HEX=dict(bytes=96,sha256='947af6aae2044f3c13b0615910e9eea07eff20489627804d5dcf876b5e5b68b5',external_branch_indices=[6,12,17])


def verify_hex(image,symbols,bounds,file_bytes):
    if 'fes_nano_mmu_hex_formatter' not in symbols:return None
    address,size=symbols['fes_nano_mmu_hex_formatter'];park=symbols['fes_nano_mmu_transition_park'][0]
    if size!=96 or address&3 or not 0x80000000<=address<address+size<=bounds['ro_end']:
        raise ValueError('MMU UART formatter bounds differ')
    code=file_bytes(image,address,size)[0];words=list(struct.unpack('<24I',code));external=[]
    for i,w in enumerate(words):
        if w&0xfc000000==0x14000000:mask,shift,bits=0x03ffffff,0,26
        elif w&0xff000010==0x54000000 or w&0x7e000000==0x34000000:mask,shift,bits=0x00ffffe0,5,19
        else:continue
        n=(w&mask)>>shift;n=n-(1<<bits) if n>>(bits-1) else n;target=address+4*i+n*4
        if not address<=target<address+size:
            if target!=park:raise ValueError('MMU UART formatter escaped its terminal park')
            external.append(i);words[i]=w&~mask
    proof=dict(bytes=size,sha256=hashlib.sha256(struct.pack('<24I',*words)).hexdigest(),external_branch_indices=external)
    if proof!=HEX:raise ValueError('MMU UART formatter instructions or bounds differ')
    return dict(address=address,bytes=size,sha256=hashlib.sha256(code).hexdigest(),contract=HEX,calls={})


def verify_hex_call(words,index,address,formatter):
    if formatter is None or index<2 or index+1>=len(words):raise ValueError('unselected MMU diagnostic call')
    word=words[index];delta=word&0x3ffffff;delta=delta-(1<<26) if delta>>(26-1) else delta
    source=(words[index-2]>>16)&31
    if (word&0xfc000000!=0x94000000 or address+delta*4!=formatter['address'] or
            words[index-2]&0xffe0ffff!=0xaa0003ee or source not in (*range(8),*range(18,31)) or
            words[index-1]!=0xaa1e03e8 or words[index+1]!=0xaa0803fe):
        raise ValueError('MMU diagnostic call target or scratch/LR preservation differs')
