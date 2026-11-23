import json,sys,pathlib
P=pathlib.Path('/data/projects/i486dx-plan/fits')
rows=[]
for d in sorted(P.iterdir()):
    f=d/'fit.json'
    if not f.exists(): print(d.name,'no fit.json'); continue
    j=json.load(open(f)); r=j['resources']; t=j['timing']
    fm=min(float(c['restricted_fmax']) for x in t['fmax'] for c in x['clocks'])
    ws=t.get('worst_setup_ns', t.get('setup',{}) if isinstance(t.get('setup'),(int,float)) else None)
    print(f"{d.name:10} alms={r['alms']['used']:8.1f} vpin={r['alms_with_virtual_pins']['used']:6.0f} regs={r['registers']['used']:6.0f} dsp={r['dsp_blocks']['used']:3.0f} m20k={r['ram_blocks']['used']:3.0f} status={j['status']} fmin={fm:.2f}")
