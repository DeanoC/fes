"""Bounded Kermit transfer into RAM already reserved by the owning caller.

The caller supplies an exclusive Console, a live claim check and a window
established from a verified FAT load and ScratchMemory observation. A successful
sender exit still requires stdout EOF and an empty UART transmit queue. Failures
retain packet/status evidence and leave target recovery to the caller.
"""
import contextlib,hashlib,json,os,re,secrets,select,signal,subprocess,termios,time
from pathlib import Path

def sha(p):return hashlib.sha256(Path(p).read_bytes()).hexdigest()

def _relay(child,fd,window_bytes,check,outgoing,incoming,generated,state):
    """Finish stdout and queued UART writes after a successful sender exit."""
    tx=bytearray();rx=bytearray();txhash=hashlib.sha256();rxhash=hashlib.sha256()
    sent=received=0;tail=b'';stdout_open=True;exit_observation=None
    try:
        while True:
            check();status=child.poll()
            if status is not None and exit_observation is None:
                exit_observation={'returncode':status,'queued_host_bytes':len(tx),
                                  'queued_sender_bytes':len(rx),'stdout_open':stdout_open}
            if status is not None and status!=0:
                raise ValueError('Kermit sender exited with status '+str(status))
            # Sender exit does not imply its pipe or the UART queue is drained.
            if status==0 and not stdout_open and not tx:
                break
            read=[fd]+([child.stdout.fileno()] if stdout_open else [])
            write=([fd] if tx else [])+([child.stdin.fileno()] if rx and status is None else [])
            readable,writable,_=select.select(read,write,[],.05)
            if stdout_open and child.stdout.fileno() in readable:
                data=os.read(child.stdout.fileno(),16384)
                if not data:stdout_open=False
                else:
                    if sent+len(tx)+len(data)>window_bytes:
                        raise ValueError('UART sender exceeds owned RAM wire bound')
                    generated.write(data);generated.flush();tx.extend(data)
            if fd in readable:
                data=os.read(fd,16384)
                if not data:raise OSError('UART disconnected during Kermit')
                received+=len(data)
                if received>1<<20:raise ValueError('UART receiver byte bound')
                incoming.write(data);incoming.flush();rxhash.update(data);rx.extend(data);tail=(tail+data)[-8192:]
            if fd in writable and tx:
                count=os.write(fd,tx);outgoing.write(tx[:count]);outgoing.flush()
                txhash.update(tx[:count]);sent+=count;del tx[:count]
            if child.stdin.fileno() in writable and rx:
                try:count=os.write(child.stdin.fileno(),rx);del rx[:count]
                except BrokenPipeError:
                    if child.poll() is None:raise
        return tail
    finally:
        state.update(sender_returncode=child.poll(),queued_host_bytes=len(tx),queued_sender_bytes=len(rx),
                     stdout_open=stdout_open,sender_exit_observation=exit_observation,
                     host_wire_bytes=sent,target_wire_bytes=received,host_wire_sha256=txhash.hexdigest(),
                     target_wire_sha256=rxhash.hexdigest())


def send(console,sender,payload,*,expected_sha256,window_bytes,owns,stderr,timeout=150,control_timeout=5):
    """Use an exclusive console and its caller's verified live RAM reservation."""
    payload=Path(payload);sender=Path(sender)
    if (payload.is_symlink() or not payload.is_file() or not 0<payload.stat().st_size<=2<<20 or
            sha(payload)!=expected_sha256 or type(window_bytes) is not int or not 0<window_bytes<=8<<20 or
            4*payload.stat().st_size+65536>window_bytes):
        raise ValueError('bounded regular source and previously owned UART RAM window required')
    if not console.parked or not owns():raise ValueError('exclusive parked console required')
    length=payload.stat().st_size;started=time.monotonic();deadline=started+timeout
    original=termios.tcgetattr(console.fd);child=None
    directory=Path(str(stderr)+'.wire');directory.mkdir(mode=0o700,exist_ok=False)
    stack=contextlib.ExitStack()
    r={'source_sha256':expected_sha256,'source_bytes':length,'address':0x82000000,'baud':230400,
       'reserved_bytes':window_bytes,'maximum_host_wire_bytes':window_bytes,'pass':False,'stage':'begin',
       'diagnostic_directory':str(directory),'protocol':'kermit-stop-and-wait'}
    def check():
        if time.monotonic()>=deadline:raise TimeoutError('UART transfer overall deadline')
        if not owns():raise ValueError('UART transfer lost kit ownership')
        debug=directory/'gkermit.debug'
        if debug.exists() and debug.stat().st_size>4<<20:raise ValueError('Kermit debug output byte bound')
    def control(pattern,raw=b''):
        until=min(deadline,time.monotonic()+control_timeout)
        while True:
            check()
            if re.search(pattern,raw):return raw
            if time.monotonic()>=until:raise TimeoutError('UART rate/prompt handshake')
            if len(raw)>16384:raise ValueError('UART control output byte bound')
            if select.select([console.fd],[],[],.05)[0]:
                data=os.read(console.fd,16384)
                if not data:raise OSError('UART disconnected')
                incoming.write(data);incoming.flush();console.log.write(data);console.log.flush();raw+=data
    token='FES_UART_'+secrets.token_hex(8)
    try:
        outgoing=stack.enter_context((directory/'host.bin').open('xb'))
        incoming=stack.enter_context((directory/'target.bin').open('xb'))
        generated=stack.enter_context((directory/'sender-stdout.bin').open('xb'))
        log=stack.enter_context(Path(stderr).open('xb'))
        console.parked=False
        r['stage']='switch-to-transfer-rate'
        os.write(console.fd,('loadb 82000000 230400; echo '+token+'=$?\r').encode())
        control(rb'(?m)^## Switch baudrate to 230400 bps and press ENTER \.\.\.\r?$')
        attrs=original.copy();attrs[6]=original[6].copy();attrs[4]=attrs[5]=termios.B230400
        termios.tcsetattr(console.fd,termios.TCSANOW,attrs)
        # Pinned load.c waits 50ms before and 50ms after setting the rate.
        time.sleep(.15);check();os.write(console.fd,b'\r')
        control(rb'(?m)^## Ready for binary \(kermit\) download to 0x82000000 at 230400 bps\.\.\.\r?$')
        env=os.environ.copy();env.pop('GKERMIT',None);r['stage']='kermit'
        r['sender_argv']=[str(sender),'-i','-S','-q','-X','-b','2','-d',str(directory/'gkermit.debug'),'-a','U.BIN','-s',str(payload)]
        child=subprocess.Popen(r['sender_argv'],
            stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=log,env=env,start_new_session=True)
        os.set_blocking(child.stdin.fileno(),False);os.set_blocking(child.stdout.fileno(),False)
        tail=_relay(child,console.fd,window_bytes,check,outgoing,incoming,generated,r)
        r['stage']='switch-to-console-rate'
        ending=control(rb'(?m)^## Switch baudrate to 115200 bps and press ESC \.\.\.\r?$',tail)
        count=re.findall(rb'(?:^|[\r\n])## Total Size\s+= 0x([0-9a-fA-F]+) = ([0-9]+) Bytes(?=[\r\n])',ending)
        if len(count)!=1 or int(count[0][0],16)!=length or int(count[0][1])!=length:
            raise ValueError('UART loader length observation differs')
        termios.tcsetattr(console.fd,termios.TCSANOW,original);time.sleep(.15);check()
        os.write(console.fd,b'\x1b')
        output=console._receive(control_timeout)
        check()
        if len(re.findall(r'(?m)^'+re.escape(token)+r'=0\r?$',output))!=1 or not console.parked:
            raise ValueError('UART loader final acknowledgement differs')
        if sha(payload)!=expected_sha256 or payload.stat().st_size!=length:
            raise ValueError('UART source changed during transfer')
        r.update(pass_=True,elapsed_seconds=time.monotonic()-started,prompt_parked=True)
        r['pass']=r.pop('pass_');r['stage']='complete';return r
    except BaseException as error:
        r['error_type']=type(error).__name__;r['error']=str(error);raise
    finally:
        if child is not None:
            r['sender_returncode_before_cleanup']=child.poll()
            if child.poll() is None:
                r['sender_terminated_by_controller']=True
                os.killpg(child.pid,signal.SIGTERM)
                try:child.wait(timeout=2)
                except subprocess.TimeoutExpired:os.killpg(child.pid,signal.SIGKILL);child.wait(timeout=2)
            for pipe in (child.stdin,child.stdout):pipe.close()
        try:
            termios.tcsetattr(console.fd,termios.TCSANOW,original);r['host_baud_restored']=True
        finally:
            stack.close()
            r['elapsed_seconds']=time.monotonic()-started;r['prompt_parked']=console.parked
            r['trace_files']={p.name:{'bytes':p.stat().st_size,'sha256':sha(p)} for p in directory.iterdir() if p.is_file()}
            Path(str(stderr)+'.json').write_text(json.dumps(r,indent=2)+'\n')
