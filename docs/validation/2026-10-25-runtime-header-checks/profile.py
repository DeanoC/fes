import json, pathlib, re, subprocess, sys, time
root=pathlib.Path(sys.argv[1]); label=sys.argv[2]
module=root/'sources/libmister-runtime'
text=(module/'Makefile').read_text()
bins=re.search(r'TEST_BINS := (.*?)(?=\nTEST_HEADERS)',text,re.S).group(1).replace('\\\n',' ').replace('$(BUILD_DIR)','build').split()
steps=[('initial-tests',['make','-j2','run-tests']),('header-rebuild',['bash','tests/incremental_build_test.sh',str(module),*bins]),('version-rebuild',['bash','tests/version_build_test.sh',str(module)]),('active-tree',['make','-j2','active-tree-test']),('hardware-support',['make','hardware-support-truth-test'])]
for name,argv in steps:
    log=pathlib.Path('/tmp')/f'fes-runtime-{label}-{name}.log'
    start=time.monotonic()
    with log.open('w') as stream:
        result=subprocess.run(argv,cwd=module,stdout=stream,stderr=subprocess.STDOUT)
    record={'label':label,'stage':name,'elapsed_s':round(time.monotonic()-start,3),'returncode':result.returncode,'argv':argv,'log':str(log)}
    with open('/tmp/fes-runtime-profile.jsonl','a') as stream: stream.write(json.dumps(record)+'\n')
    print(json.dumps(record),flush=True)
    if result.returncode: sys.exit(result.returncode)
