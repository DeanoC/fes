from pathlib import Path
import hashlib,json,os,shutil,subprocess,sys
root=Path.cwd();phase=Path("/home/deano/fes/out/dev/de25-managed-image/out/de25-managed-sdk-integration");base=phase/"linux-base-bootstrap"
sys.path.insert(0,str(root/"scripts"));import kit_software as kit
revision=subprocess.check_output(["git","rev-parse","HEAD"],text=True).strip()
assert revision=="cad04bd65d82c98f0e375de927051d215dd63af1"
assert json.loads((phase/"base-bootstrap-job/job.json").read_text())["state"]=="passed"
parent=phase/"runtime-source-cad04bd65";parent.mkdir(exist_ok=True);snapshot=parent/"source"
assert not snapshot.exists(),"retain any previous actual runtime proof"
subprocess.run(["git","clone","--shared","--no-checkout",str(root),str(snapshot)],check=True)
subprocess.run(["git","-C",str(snapshot),"checkout","--detach",revision],check=True)
work=snapshot/"image/build/software";work.mkdir(parents=True)
record=json.loads((base/"base.json").read_text());shutil.copyfile(base/"rootfs.ext4",work/"base.img")
assert kit.sha(work/"base.img")==record["files"]["rootfs.ext4"]
output=phase/"runtime-proof";output.mkdir(exist_ok=True)
original=kit.run
with (output/"normal-builder.log").open("w") as log:
 def logged(args,**kwargs):
  log.write("+ "+json.dumps([str(x) for x in args])+"\n");log.flush()
  try: result=original(args,**kwargs)
  except subprocess.CalledProcessError as error:
   log.write(str(error.output));log.flush();raise
  log.write(result+"\n");log.flush();return result
 kit.run=logged
 toolchain=kit.build_runtime(snapshot,base,revision,kit.build_environment(),None,"de25-nano")
 selected=toolchain["de25_image_sdk"];assert selected["profile"]=="managed-base"
 shutil.copy2(phase/"runtime-cpu-proof.py",work/"proof-runtime.py")
 (work/"source-revision").write_text(revision+"\n")
 volume="fes-de25-sdk-tests-cad04bd65"
 assert subprocess.run(["docker","volume","inspect",volume],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode!=0,"do not claim an existing volume"
 env=kit.build_environment();env.update(TARGET_IMAGE_CONTAINER_RUNTIME="docker",TARGET_IMAGE_OUTPUT_VOLUME=volume)
 try:kit.run([snapshot/"image/scripts/target-image-container.sh","run","/usr/bin/python3","/work/build/software/proof-runtime.py"],env=env)
 finally:subprocess.run(["docker","volume","rm",volume],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,check=False)
checks=json.loads((work/"runtime-proof.json").read_text());assert all(r["result"]=="passed" for r in checks["cpu_tests"])
for name in ("mister-runtime","libmister-runtime.a"):shutil.copy2(work/"runtime/build"/name,output/name)
shutil.copytree(work/"cpu-tests",output/"cpu-tests",dirs_exist_ok=True)
shutil.copy2(work/"runtime-proof.json",output/"runtime-proof.json")
checks.update(sdk_selection=selected,toolchain=toolchain,base_rootfs_sha256=record["files"]["rootfs.ext4"],artifacts={name:{"sha256":kit.sha(output/name),"size":(output/name).stat().st_size} for name in ("mister-runtime","libmister-runtime.a")})
(output/"receipt.json").write_text(json.dumps(checks,indent=2,sort_keys=True)+"\n");print(json.dumps(checks,indent=2))
