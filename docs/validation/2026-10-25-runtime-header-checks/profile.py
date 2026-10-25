import hashlib, json, pathlib, re, subprocess, sys, time
root=pathlib.Path(sys.argv[1]); label=sys.argv[2]
module=root/'sources/libmister-runtime'
fes_commit=subprocess.check_output(['git','-C',str(root),'rev-parse','HEAD'],text=True).strip()
text=(module/'Makefile').read_text()
bins=re.search(r'TEST_BINS := (.*?)(?=\nTEST_HEADERS)',text,re.S).group(1).replace('\\\n',' ').replace('$(BUILD_DIR)','build').split()
steps=[('initial-tests',['make','-j2','run-tests']),('header-rebuild',['bash','tests/incremental_build_test.sh',str(module),*bins]),('version-rebuild',['bash','tests/version_build_test.sh',str(module)]),('active-tree',['make','-j2','active-tree-test']),('hardware-support',['make','hardware-support-truth-test'])]
for name,argv in steps:
    log=pathlib.Path('/tmp')/f'fes-runtime-{label}-{name}.log'
    start=time.monotonic()
    with log.open('w') as stream:
        result=subprocess.run(argv,cwd=module,stdout=stream,stderr=subprocess.STDOUT)
    record={'label':label,'stage':name,'elapsed_s':round(time.monotonic()-start,3),'returncode':result.returncode,'argv':argv,'fes_commit':fes_commit,'log':str(log),'log_sha256':hashlib.sha256(log.read_bytes()).hexdigest()}
    with open('/tmp/fes-runtime-profile.jsonl','a') as stream: stream.write(json.dumps(record,sort_keys=True)+'\n')
    print(json.dumps(record,sort_keys=True),flush=True)
    if result.returncode: sys.exit(result.returncode)
