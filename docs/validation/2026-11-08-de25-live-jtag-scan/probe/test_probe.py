import json, os, re, subprocess
from pathlib import Path
s=Path(__file__).resolve().parent
flags=['-std=c++17','-Wall','-Wextra','-Werror','-Wno-unknown-pragmas','-DPORT=2','-DUNIX=2','-DWINDOWS=1','-DSYS=3','-DLINUX=3','-DMODE_64_BIT','-I',str(s)]
subprocess.run(['g++',*flags,str(s/'scan_probe.cpp'),str(s/'mock_aji.cpp'),'-o',str(s/'mock-probe')],check=True,timeout=30)
env=os.environ.copy();env.update(QUARTUS_JTAG_CLIENT_CONFIG=str(s/'fake-private.conf'),QUARTUS_JTAG_CLIENT_NO_LOCAL_SERVER='1')
cable='DE25-Nano on localhost:1234 [3-11.1-iface0]'
all_calls=['find','name','lock','clock','forced-scan','cached-after','unlock']
cases=[('',all_calls)]
for phase in all_calls:
 end=all_calls.index(phase)+1
 calls=all_calls[:end]
 if end>3 and phase!='unlock':calls=calls+['unlock']
 cases.append((phase,calls))
cases += [('wrong-name',all_calls[:2]),('wrong-clock',all_calls[:4]+['unlock']),('wrong-count',all_calls),('wrong-id',all_calls),('wrong-ir',all_calls)]
records=[]
for failure,expected in cases:
 e=env|{'FES_MOCK_FAIL':failure}
 r=subprocess.run([str(s/'mock-probe'),cable],env=e,capture_output=True,text=True,timeout=5)
 calls=re.findall(r'^MOCK_CALL (.+)$',r.stdout,re.M)
 assert calls==expected,(failure,calls,expected)
 assert r.returncode==(0 if not failure else 1),(failure,r.returncode,r.stdout)
 assert ('RESULT pass=1' in r.stdout)==(not failure)
 records.append({'failure':failure or None,'calls':calls,'returncode':r.returncode})
for name,args,changes in [('bottom',[cable.replace('3-11.1','3-11.2')],{}),('local',['DE25-Nano'],{}),('missing-env',[cable],{'QUARTUS_JTAG_CLIENT_NO_LOCAL_SERVER':'0'}),('extra-argument',[cable,'extra'],{})]:
 r=subprocess.run([str(s/'mock-probe'),*args],env=env|changes,capture_output=True,text=True,timeout=5)
 assert r.returncode==2 and 'MOCK_CALL' not in r.stdout and 'REFUSE' in r.stdout,(name,r.stdout)
 records.append({'refusal':name,'calls':[],'returncode':r.returncode})
(s/'probe-test-proof.json').write_text(json.dumps({'pass':True,'cases':records},indent=2)+'\n')
print('PASS',len(records),'mock API cases: exactly one forced scan, fail-stop, unlock and wrong-target/environment refusal; no hardware access.')
