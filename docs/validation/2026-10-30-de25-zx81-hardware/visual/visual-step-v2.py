import sys,importlib.util,json,pathlib,hashlib,subprocess,tempfile,time,shlex,os,struct,gzip,re
ROOT=pathlib.Path('/home/deano/.codex/worktrees/c134/fes')
HERE=ROOT/'out/dev/de25-zx81-hardware'
DATA=HERE/'docs/validation/2026-10-30-de25-zx81-hardware'
OUT=HERE/('out/hdmi/steps/'+sys.argv[3])
OUT.mkdir(parents=True,exist_ok=False)
spec=importlib.util.spec_from_file_location('replay',ROOT/'out/dev/de25-board-cpu-proofs/docs/validation/2026-10-30-de25-board-cpu-proofs/replay.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
cfg=json.loads((ROOT/'out/dev/de25-reboot-investigation/out/reboot/private.json').read_text());board=m.Board(cfg['boards'][0],cfg['known_hosts'])
sha=lambda p:hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
programmer_log=gzip.decompress((DATA/'program.log.gz').read_bytes()).decode()
programmed_checksum=re.search(r'with checksum (0x[0-9A-Fa-f]+) for device A5EB013BB23BCS@1',programmer_log)[1]
board_identity={'quartus_cable_name':'DE25-Nano [3-11.3-iface0]','fpga_part':'A5EB013BB23BCS','usb_serial':None,'board_uid':None,'jtag_idcode':None,'physical_board_binding':'not-established'}
inputs=json.loads((HERE/'out/zx81-jtag/hps/inputs.json').read_text());build=inputs['computer']['build_id']
generation=json.loads((HERE/'out/zx81-jtag/hps/generation.json').read_text());base=generation['computer']['jtag_computer_base'];assert base==inputs['computer']['jtag_control_bridge_base']+inputs['computer']['peripheral_offset']
client=HERE/'out/client-visual-aarch64';elf=client.read_bytes();assert elf[:6]==b'\x7fELF\x02\x01' and struct.unpack_from('<H',elf,18)[0]==183
phoff=struct.unpack_from('<Q',elf,32)[0];phsize,phcount=struct.unpack_from('<HH',elf,54);assert all(struct.unpack_from('<I',elf,phoff+i*phsize)[0]!=3 for i in range(phcount))
image=HERE/'docs/validation/2026-10-30-de25-board-cpu-proofs/TRWU8J8G-internal-saved.p';assert sha(image)=='b4e6e04e3063149a0e7232f23e4aa24b96d0631d557920e97c65d1e01dfb3c71'
remote=board.run('mktemp -d /tmp/fes-de25-zx81-XXXXXXXX').stdout.decode().strip();assert remote.startswith('/tmp/fes-de25-zx81-') and remote.replace('/','').replace('-','').isalnum()
(OUT/'prepared.json').write_text(json.dumps({'remote':remote,'client_sha256':sha(client),'image_sha256':sha(image),'build_id':build},indent=2)+'\n')
board.upload(client,remote+'/client');board.upload(image,remote+'/saved.p')
started=time.time()
results=[]
device='/devices/A5E(A013BB23B|B013BB23BCS)|..@1#3-11.3-iface0#DE25-Nano'
master=device+'/(link)/JTAG/alt_sld_fab_0_alt_sld_fab_0_sldfabric.node_0/phy_0/subsys_debug_fpga_m.master'
defs=(HERE/'sources/misteross/boards/de25nano/zx81-hps/generated/fes_de25_computer.vh').read_text();import re
constant=lambda n:int(re.search(r'`define FES_DE25_COMPUTER_'+n+r" 32'h([0-9a-f]+)",defs)[1],16)
for scenario, argument in [(sys.argv[1],sys.argv[2])]:
 out=OUT/scenario;out.mkdir()
 with tempfile.TemporaryDirectory(prefix='fes-de25-live-') as tmp:
  endpoint=pathlib.Path(tmp)/'register.sock'
  args=['python3',str(DATA/'proxy.py'),'--socket',str(endpoint),'--output',str(out/'proxy.json'),'--log',str(out/'console.log'),'--sof',str(HERE/'out/zx81-jtag-with-spl.sof'),'--device',device,'--master',master,'--console','/data/altera_pro/26.3/syscon/bin/system-console','--base',str(base),'--identity',str(constant('IDENTITY')),'--version',str(constant('VERSION'))]
  with (out/'proxy.log').open('wb') as log:
   proxy=subprocess.Popen(args,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
   try:
    deadline=time.monotonic()+45
    while not endpoint.exists():
     if proxy.poll() is not None or time.monotonic()>deadline:raise RuntimeError('physical proxy did not become ready: '+scenario)
     time.sleep(.1)
    command=' '.join(shlex.quote(x) for x in [remote+'/client',remote+'/'+scenario+'.sock',build,scenario,(remote+'/saved.p' if scenario=='media' else argument)])
    reply=board.run(command,forward=remote+'/'+scenario+'.sock:'+str(endpoint),timeout=180,log=out/'client.log')
    if proxy.wait(timeout=15)!=0:raise RuntimeError('physical proxy failed to close: '+scenario)
    row=json.loads(reply.stdout);actual=json.loads((out/'proxy.json').read_text());assert row['status']=='passed' and actual['status']=='completed';assert row['register_reads']==actual['reads'] and row['register_writes']==actual['writes']
    results.append({'scenario':scenario,'client':row,'proxy':actual});print(scenario,'PASS',flush=True)
   finally:
    if proxy.poll() is None:
     import signal
     os.killpg(proxy.pid,signal.SIGTERM)
     try:proxy.wait(timeout=5)
     except subprocess.TimeoutExpired:os.killpg(proxy.pid,signal.SIGKILL);proxy.wait()
(OUT/'proof.json').write_text(json.dumps({'scenario':sys.argv[1],'argument':sys.argv[2],'started_unix':started,'completed_unix':time.time(),'board_identity':board_identity,'programmed_sof_quartus_checksum':programmed_checksum,'programmer_log':'program.log.gz','build_id':build,'client_sha256':sha(client),'saved_image_sha256':sha(image),'driver_sha256':sha(__file__),'results':results,'cpu_physical_mmio':False,'fpga_access':'actual selected fabric JTAG master','basic_output_observed':False},indent=2)+'\n')

board.run('rm -rf -- '+shlex.quote(remote)+'\ntest ! -e '+shlex.quote(remote))
print('Owned visual-step uploads removed',flush=True)
