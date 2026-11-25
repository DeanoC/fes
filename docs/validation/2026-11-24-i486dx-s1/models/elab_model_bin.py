import sys, hashlib, types
from pathlib import Path
sys.path.insert(0, '.')
import importlib.util
spec = importlib.util.spec_from_file_location('pc386build', 'boards/de25nano/pc386/build.py')
b = importlib.util.module_from_spec(spec); spec.loader.exec_module(b)
model, binary = sys.argv[1], Path(sys.argv[2])
b.FRONT_END = types.SimpleNamespace(path=binary, identity='sha256=' + hashlib.sha256(binary.read_bytes()).hexdigest())
sources = b.board.capture(b)
files, derived = b.component_files(sources, model)
out = Path(sys.argv[3]); out.write_bytes(files['pc386_system.v'])
print(model, b.FRONT_END.identity[:23], 'netlist', hashlib.sha256(files['pc386_system.v']).hexdigest())
