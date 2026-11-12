import hashlib,json,os,subprocess,sys
from pathlib import Path
source=Path.cwd();phase=Path("/home/deano/fes/out/dev/de25-managed-image/out/de25-managed-factory");sys.path.insert(0,str(source/"image/de25-nano"));import managed_base as base
volume="fes-de25-factory-config-36438ad3a"
builder=base.image.ensure_builder(source,"docker",volume);assert builder==base.sdk_lock()["builder_image"]
output=phase/"config-proof";output.mkdir(exist_ok=True)
subprocess.run(["docker","run","--rm","--name","fes-de25-factory-config-36438ad3a","--network","none","--user",str(os.getuid())+":"+str(os.getgid()),"--ulimit","core=0:0","--ulimit","nofile=65536:65536","--volume",str(source)+":/src:ro","--volume",str(phase)+":/proof:ro","--volume",str(output)+":/export","--volume","/home/deano/fes/out/cache/de25-vendor:/cache:ro","--volume",volume+":/build","--volume","/home/deano/fes/out/dev/de25-managed-image/out/de25-managed-sdk-integration/runtime-proof:/runtime-input:ro",builder,"/usr/bin/python3","/proof/config-inside.py"],check=True)
