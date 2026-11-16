"""Exact full-SOF native boot selection, independent of live programming authority."""
from pathlib import Path
import importlib.util,json,stat
from transport import require,sha
from fit_swap import Bundle,member
B=Path(__file__).resolve().parent

def plain(p):
 p=Path(p);require(p.is_absolute() and '..' not in p.parts and not any(q.is_symlink() for q in (p,*p.parents)) and p.is_file() and stat.S_ISREG(p.stat().st_mode),'plain selected image input required');return p

def validate(image,spec,plan):
 require(spec['scope']=='matched-native-full-SOF-selection-v1' and spec['source_head']==plan['candidate_source_head'] and spec['source_root']==plan['candidate_root'],'different selected native source')
 composition=plain(Path(spec['composition'])/'result.json');require(sha(composition)==spec['composition_result_sha256'],'composition receipt changed')
 c=json.loads(composition.read_text());base=composition.parent;sof=plain(base/'native-hps-first.sof');spl=plain(base/'u-boot-spl-dtb.hex');phase1=plain(base/'native-hps-first.hps.rbf')
 expected=dict(path=str(sof),sha256=c['sof_sha256'],bytes=sof.stat().st_size,kind='matched-native-sof',operation='p;'+str(sof)+'@1',volatile=True,flash_operation=False)
 require(image==spec['image']==expected,'explicit complete native SOF required; no phase-one fallback')
 member(image);require(c['phase1_sha256']==sha(phase1) and c['phase1_sha256']!=image['sha256'],'native split pair differs')
 require(c['fit_sha256']==spec['fit_sha256']==plan['forward']['target_boot']['sha256'] and c['spl_sha256']==spec['spl_sha256']==sha(spl),'FIT/SPL composition differs')
 require(c['status']=='prepared' and c['hps_initialization']=='HPS FIRST' and c['paired_programs'] and c['source_context_bound'] and c['core_payload_unchanged'] and c['hardware_acceptance']=='not-run' and c['physical_programming_admission'] is False,'composition provenance or acceptance scope differs')
 files={str(plain(p)):h for p,h in c['inputs'].items()};require(all(sha(p)==h for p,h in files.items()),'composition input changed')
 refs=[Path(p) for p,h in files.items() if h==c['reference_combined_sof_sha256']];require(len(refs)==1,'unique sealed reference required')
 inspector=plain(spec['inspector_path']);require(sha(inspector)==spec['inspector_sha256'],'selected inspector changed')
 loader=importlib.util.spec_from_file_location('full_sof_byte_inspector',inspector);m=importlib.util.module_from_spec(loader);loader.loader.exec_module(m)
 observed=m.inspect(sof.read_bytes(),spl.read_bytes(),refs[0].read_bytes());require(observed==c['embedded_spl_identity'],'embedded native SPL or preserved FPGA sections differ')
 files.update({str(p):sha(p) for p in [composition,sof,spl,phase1,inspector]})
 return dict(scope='verified-file-only-matched-native-full-SOF',image=image,fit_sha256=c['fit_sha256'],spl_sha256=c['spl_sha256'],embedded_SPL=observed,files=files,hardware_actions=False,programming_authority=False,hardware_acceptance=False,phase_one_fallback=False)

def check(bundle=None):
 bundle=Bundle(B/'plan.json',sha(B/'plan.json')) if bundle is None else bundle;bundle.verify()
 rows=[v for v in bundle.plan['roles'] if v['role']=='native-diagnostic'];require(len(rows)==1 and rows[0]['program_phase']=='native','one native image role required')
 path=plain(B/'native-image-selection.json');result=validate(rows[0]['image'],json.loads(path.read_text()),bundle.plan);result['files'][str(path)]=sha(path);return result
