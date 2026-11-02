from pathlib import Path
import importlib.util,json,subprocess,os
work=Path("/work/build/software");runtime=work/"runtime";prefix=work/"sdk/host/bin/aarch64-buildroot-linux-gnu-"
revision=(work/"source-revision").read_text().strip()
assert len(revision)==40 and all(c in "0123456789abcdef" for c in revision)
names=("de25_computer_test","de25_fpga_manager_test","de25_menu_test","de25_pr_control_test","production_hardware_test")
command=["make","-C",str(runtime),"-j2","CXX="+str(prefix)+"g++","AR="+str(prefix)+"ar","NM="+str(prefix)+"nm","CXXFILT="+str(prefix)+"c++filt","MISTER_RUNTIME_VERSION=git-"+revision,"archive-audit",*("build/tests/unit/"+n for n in names)]
subprocess.run(command,check=True)
sysroot=Path(subprocess.check_output([str(prefix)+"g++","-print-sysroot"],text=True).strip())
qemu=["qemu-aarch64-static","-cpu","cortex-a76","-L","/",str(sysroot/"lib/ld-linux-aarch64.so.1"),"--library-path",str(sysroot/"lib")+":"+str(sysroot/"usr/lib")]
logs=work/"cpu-tests";logs.mkdir(exist_ok=True);checks=[]
for name in names:
 binary=runtime/"build/tests/unit"/name
 with (logs/(name+".log")).open("w") as log:subprocess.run([*qemu,str(binary)],cwd=runtime,stdout=log,stderr=subprocess.STDOUT,check=True,timeout=120)
 checks.append({"name":name,"result":"passed"})
spec=importlib.util.spec_from_file_location("runtime_base_abi","/work/scripts/kit-software.py");module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
binary=runtime/"build/mister-runtime"
contract=module.base_contract(work/"base",[binary],board="de25-nano")
interfaces=module.runtime_menu_interfaces(binary)
record={"classification":"normal runtime build and Cortex-A76 CPU models; no board acceptance","source_revision":revision,"cpu_tests":checks,"base_contract":contract,"menu_interfaces":interfaces,"archive_audit":"canonical29productionmembers passed","qemu_loader":"identity host namespace, direct authenticated SDK loader","hardware_access":"not-performed","factory_bundle":"not-built; sealed MENU and factory metadata still required"}
(work/"runtime-proof.json").write_text(json.dumps(record,indent=2,sort_keys=True)+"\n");print(json.dumps(record,indent=2))
