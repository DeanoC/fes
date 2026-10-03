"""Private-host normal-library admission cases for incomplete native video inventories."""
import argparse
from pathlib import Path
from run_library import Driver, require

class PartialDriver(Driver):
    def run(self):
        self.start()
        expected=self.request('GET','/diagnostic/expectations')
        before=self.idle('initial-idle')
        require(self.request('GET','/api/v1/core-packages')['packages'] == [], 'Private catalog must start empty')
        installed=self.request('POST','/api/v1/core-packages',self.args.package.read_bytes(),201)
        require(installed['package_id'] == expected['package_id'],'Wrong native package')
        media=self.request('POST','/api/v1/core-media',self.args.rom.read_bytes(),201)
        entry=self.request('POST','/api/v1/library/core-entries',{'title':'Native missing-output admission', 'package_id':expected['package_id'], 'media_role':'blob', 'media_id':media['media_id']},201)
        self.profile('direct')
        resolution=self.request('GET',f'/api/v1/library/core-entries/{entry["game_id"]}/video')
        require(not resolution['builtin'] and not resolution.get('part_id'), 'Vacant native output was misrepresented')
        require(next(x for x in resolution['choices'] if x['profile']=='direct')['available'] is False, 'Vacant native Direct was advertised as available')
        self.active=True  # A failed/lost launch reply can still leave our session active.
        rejection=self.request('POST','/api/v1/session/launch',{'game_id':entry['game_id'],'target':self.args.target_name},400)
        require(rejection['error']['code']=='BAD_REQUEST' and rejection['error'].get('phase')=='admission', 'Missing Direct failed for another reason')
        require(self.observation()==before,'Missing Direct launch mutated target status')
        self.idle('missing-direct')
        self.active=False
        self.save('missing-direct-rejected',{'resolution':resolution,'error':rejection,'status_unchanged':True})
        part=self.request('POST','/api/v1/library/video-parts/direct',self.args.direct.read_bytes(),200)
        expected_direct=expected['compositions']['direct']
        video_id=next(x['part_id'] for x in expected_direct['parts'] if x['role']=='video')
        require(part['part_id']==video_id,'Wrong fallback Direct part')
        self.profile('scanlines')
        resolution=self.request('GET',f'/api/v1/library/core-entries/{entry["game_id"]}/video')
        require(resolution['preferred_profile']=='scanlines' and resolution['effective_profile']=='direct' and
                resolution['part_id']==video_id and not resolution['builtin'] and resolution.get('fallback_reason'), 'Missing Scanlines failed to select Direct')
        self.active=True
        self.request('POST','/api/v1/session/launch',{'game_id':entry['game_id'],'target':self.args.target_name})
        active=self.observation()
        cp=active['core_package']
        require(active['state']=='active' and cp['package_id']==expected['package_id'] and
                cp['build_id']==expected['build_id'] and cp['abi']==expected['abi'] and
                cp['persistence_mode']=='volatile' and cp['generation']>0 and cp['parts_composition']==expected_direct,'Fallback programmed wrong composition')
        self.save('scanlines-fallback-direct',{'resolution':resolution,'runtime':active})
        self.stop('fallback')
        self.save('result',{'passed':True,'missing_direct_rejected_without_mutation':True,'missing_scanlines_falls_back_to_linked_direct':True,'final_idle':True})

if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__)
    for arg in ('host-binary','state','catalog','rom','output','package','direct'):
        p.add_argument('--'+arg,type=Path,required=True)
    p.add_argument('--target-id',required=True)
    p.add_argument('--target-name',default='kit2')
    p.add_argument('--port',type=int,default=18789)
    p.add_argument('--image-sha256',required=True)
    args=p.parse_args()
    d=PartialDriver(args)
    try:d.run()
    finally:
        if d.process:
            try:
                if d.active:d.stop('cleanup')
            finally:d.shutdown()
