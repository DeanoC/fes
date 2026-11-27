import json, sys
from pathlib import Path
src=Path(sys.argv[1]).resolve(); sys.path.insert(0,str(src))
import boards.de25nano.producers.build_pc386_package as m
repo,rev=m._require_clean_source(src)
tools={n:'selection-only' for n in ('quartus','qsys','ipgenerate','pfg','yosys')}
r=json.loads(m.create_build_record(src,repo,rev,tools,execution={"selection_only":True,"gpu_device":"lease","toolchain":"quartus"},model='486sx'))
print(rev, r['identities']['design'][:32], r['parameters'].get('machine_clock_hz'))
