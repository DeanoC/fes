"""Bind the real staged programmer to one fresh vendor-client scan."""
import json,os,resource,subprocess,time
from pathlib import Path
from common import BASE,ROOT,SOURCE,adapter_cable,claim,sha,parse_jtag_clock
import jtag_programming as policy

def identity():os.setgroups([]);os.setgid(1000);os.setuid(1000)

def traced_identity():
    identity()
    resource.setrlimit(resource.RLIMIT_FSIZE,(8<<20,8<<20))

def traced_arguments(arguments,cable,label):
    assert claim().owns() and adapter_cable()==cable
    assert len(arguments)==4 and arguments[:3]==['-m','jtag','-o']
    assert arguments[3].startswith('p;/') and arguments[3].endswith('.sof@1')
    assert Path(label).name==label
    trace=BASE/(label+'.strace')
    with trace.open('xb'):pass
    return ['/usr/bin/strace','--kill-on-exit','-I','1','-f','-qq','-ttt','-s','256','-yy',
            '-e','trace=execve,openat,connect,mmap','-o',str(trace),'--',
            '/data/altera_pro/26.3/quartus/bin/quartus_pgm','-c',cable,*arguments]

def program_once(phase,image,program,pump):
    preflight=json.loads((BASE/'preflight.json').read_text())
    assert preflight['host_programming_policy_sha256']==sha(Path(policy.__file__))
    selected=preflight['vendor_scan_probe']
    probe=Path(selected['path']);assert probe.is_absolute()
    counter=0
    def observe():
        nonlocal counter
        owner=claim();assert owner.owns()
        cable=adapter_cable();r=json.loads((BASE/'transport.json').read_text())
        assert r['status']=='ready' and r['source_commit']==SOURCE
        assert sha(probe)==selected['sha256']
        libraries={p:sha(Path(p)) for p in policy.LIBRARIES};assert libraries==selected['client_sha256']
        path=BASE/(phase+'-live-clock-'+str(counter)+'.log');counter+=1
        with path.open('xb') as log:
            value=subprocess.run(['/data/altera_pro/26.3/quartus/bin/jtagconfig','--getparam',cable,'JtagClock'],stdout=log,stderr=subprocess.STDOUT,preexec_fn=identity,timeout=30)
        assert value.returncode==0
        return dict(serial=r['serial'],source_commit=SOURCE,claim_token=owner.token,
                    pid=r['owned_pid'],start_ticks=r['process_start_ticks'],cable=cable,
                    config_sha256=sha(BASE/'private-client.conf'),clock=parse_jtag_clock(path.read_text()),
                    client_sha256=libraries,probe_sha256=sha(probe))
    def scan(cable):
        path=BASE/(phase+'-live-scan.log');child=None
        try:
            with path.open('xb') as log:
                child=subprocess.Popen([str(probe),cable],stdout=log,stderr=subprocess.STDOUT,preexec_fn=identity)
                until=time.monotonic()+30
                while child.poll() is None:
                    if time.monotonic()>until:raise TimeoutError('fresh vendor scan30s bound')
                    pump(.1)
                assert child.returncode==0,'fresh vendor scan failed'
            return path.read_bytes()
        finally:
            if child is not None and child.poll() is None:child.kill();child.wait(timeout=5)
    return policy.ProgramAttempt(BASE/(phase+'-program-consumed.json'),observe,scan,program).run(phase,sha(image))
