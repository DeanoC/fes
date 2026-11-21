"""Claude #2035 D12: run a DE25 producer compile-only with a given BUILD_ID (main's), on this checkout's tooling.

    python3 d12_repro.py MISTEROSS PRODUCER_MODULE BUILD_ID -- producer args...
"""
import importlib, sys
root, module_name, build_id = sys.argv[1], sys.argv[2], sys.argv[3]
args = sys.argv[sys.argv.index('--') + 1:]
sys.path.insert(0, root)
forced = lambda record: build_id
for name in ('boards.de25nano.producers.build_machine_package', 'boards.de25nano.producers.build_menu_package',
             'boards.de25nano.producers.build_zx81_package'):
    importlib.import_module(name).build_identity = forced
producer = importlib.import_module(module_name)
producer.build_identity = forced
sys.exit(producer.main(args + ['--compile-only']))
