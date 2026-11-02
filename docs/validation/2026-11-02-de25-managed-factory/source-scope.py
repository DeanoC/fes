import hashlib,json,subprocess,sys
from pathlib import Path
root=Path.cwd();phase=Path("/home/deano/fes/out/dev/de25-managed-image/out/de25-managed-factory");sys.path.insert(0,str(root/"image/de25-nano"));import managed_factory as factory
head=subprocess.check_output(["git","rev-parse","HEAD"],text=True).strip()
original="36438ad3ad426a1cb6ce16d4a62b7b6f3e7a6fde"
paths=subprocess.check_output(["git","diff","--name-only",original+"..HEAD"],text=True).splitlines();assert paths==["image/scripts/tests/native-runtime-inputs_test.sh"],paths
def sha(p):
 with Path(p).open("rb") as f:return hashlib.file_digest(f,"sha256").hexdigest()
canonical=json.loads((root/"docs/validation/2026-11-01-de25-managed-fabric/build-inputs.json").read_text())["source_inputs"]
assert all(sha(root/"sources/misteross"/p)==h for p,h in canonical.items())
combined=json.loads((root/"sources/misteross/tests/fixtures/de25-combined-proof.json").read_text())
for key in ("compiler_source_sha256","test_runtime_source_sha256"):assert all(sha(root/p)==h for p,h in combined[key].items())
cache=Path("/home/deano/fes/out/dev/de25-managed-image/out/de25-managed-sdk-integration/linux-base-bootstrap")
sdk=factory.base.kit_software.de25_sdk(root,cache);assert sdk["archive_sha256"]=="5a7fa4bf99cfc660bd157d3ffc79b25807cf93eaa49dab894b579c7c52ea30e4"
record={"source_revision":head,"configuration_proof_source":original,"configuration_implementation_unchanged":True,"changes_since_configuration_proof":paths,"managed_factory_recipe_sha256":factory.recipe_key(),"canonical_menu_inputs_verified":len(canonical),"combined_compiler_inputs_verified":len(combined["compiler_source_sha256"]),"runtime_test_inputs_verified":len(combined["test_runtime_source_sha256"]),"existing_actual_managed_sdk_verified":sdk,"hardware_access":"not-performed"}
(phase/"source-scope.json").write_text(json.dumps(record,indent=2,sort_keys=True)+"\n");print(json.dumps(record,indent=2))
