import hashlib, os, shutil, socket, sys
from pathlib import Path
run, cache = map(Path, sys.argv[1:])
host = socket.gethostname().split(".", 1)[0]
router = {"powerboat": "HIP", "chip-30x0-ai-dev": "CUDA"}[host]
archive = Path("/data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip")
source = Path("/data/de25-2353-diag/pc386-rom-abe6f29c.hex")
assert hashlib.sha256(source.read_bytes()).hexdigest() == "abe6f29c14ad3cc354f13408004a5c1868e706fd04a375a1c32c3beaea5ac28a"
target = Path("build/firmware/pc386/pc386-rom.hex")
target.parent.mkdir(parents=True, exist_ok=True)
shutil.copyfile(source, target)
producer = Path("boards/de25nano/producers/build_pc386_package.py").resolve(strict=True)
os.execv(sys.executable, [sys.executable, str(producer), "--model", "486sx", "--gpu-router", router, "--vendor-archive", str(archive), "--quartus-root", "/data/altera_pro/26.3", "--cache-root", str(cache), "--package-output", str(run / "package")])
