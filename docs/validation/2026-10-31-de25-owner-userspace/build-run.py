import pathlib,sys,os
sys.path.insert(0,str(pathlib.Path.cwd()))
from scripts.de25_kernel_platform import run_container
out=pathlib.Path('out/owner-client-build-d64ced1ea-v2').resolve();root=pathlib.Path.cwd();base=pathlib.Path('/home/deano/fes/out/artifacts/de25-kernel-modules/0da63d1044cc7ecf9908efba52af17b3e55e6271/linux-image')
run_container(['docker','run','--rm','--name','fes-de25-owner-user-client-d64ced1ea-v2','--network','none','--cap-drop=ALL','--security-opt=no-new-privileges','--user',f'{os.getuid()}:{os.getgid()}','--volume',f'{root}:/src:ro','--volume',f'{base}:/image:ro','--volume',f'{out}:/export','sha256:ea200655b92ab1211bc8b326c6e799815f1d648b2d5e5a6c982f597cb2b9650d','python3','/export/inside.py'],'docker','fes-de25-owner-user-client-d64ced1ea-v2',out,240)
