'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const {createController, mount, MAX_PACKAGE_BYTES, MAX_MEDIA_BYTES} = require('./ui_core_library.js');
const A = 'a'.repeat(64), B = 'b'.repeat(64), C = 'c'.repeat(64), M = 'd'.repeat(64);
const copy = value => JSON.parse(JSON.stringify(value));
const pkg = (id, core = 'fes.fixture') => ({package_id:id, descriptor:{core:{id:core, version:'1.0'}}});
const caps = id => ({package_id:id, source:'declared-contract', compatibility:'unknown',
  import_max_bytes:MAX_MEDIA_BYTES, media:[{role:'blob', min_bytes:1,max_bytes:32768,transport:'fixture-stream'}]});
const response = (value, status=200) => ({ok:status >= 200 && status < 300,status,json:async()=>copy(value)});
const deferred = () => { let resolve; const promise=new Promise(r=>{resolve=r;}); return {promise,resolve}; };
function fixture(options = {}) {
  const calls = [];
  const db = {packages:[pkg(A),pkg(B),pkg(C,'fes.other')], entries:[
    {game_id:'entry', title:'My title',core_id:'fes.fixture',package_id:A,media_id:M,media_role:'blob'}]};
  let override = () => undefined;
  const fetchImpl = async (path, options={}) => {
    calls.push({path,options});
    const intercepted=override(path,options);
    if (intercepted !== undefined) return intercepted;
    const method=options.method || 'GET';
    if (path === '/api/v1/core-packages') {
      if (method==='POST') return response(pkg(B),201);
      return response({packages:db.packages});
    }
    if (path.endsWith('/media-capabilities')) return response(caps(path.split('/')[4]));
    if (path.endsWith('/compatibility')) return response({package_id:path.split('/')[4], compatible:false, target:'dev',state:'incompatible'});
    if (path === '/api/v1/core-media') return response({media_id:M,size:options.body.size},201);
    if (path === '/api/v1/library/core-entries') {
      if (method==='POST') {
        const body=JSON.parse(options.body);
        const value={...body,game_id:'new-entry',core_id:'fes.fixture'};
        db.entries.push(value); return response(value,201);
      }
      return response({entries:db.entries});
    }
    if (path.startsWith('/api/v1/library/core-entries/') && method==='PUT') {
      const body=JSON.parse(options.body), entry=db.entries[0];
      if (path.endsWith('/media')) Object.assign(entry,{media_id:body.media_id,media_role:body.media_role});
      else entry.package_id=body.package_id;
      return response(entry);
    }
    throw new Error('Unexpected endpoint '+path);
  };
  const changed=[];
  const controller=createController({fetchImpl,onCatalogChange:()=>changed.push(true),...options});
  return {controller,calls,db,changed,intercept(fn){override=fn;}};
}
for (const phase of ['fetch', 'body']) {
  test('deadline releases mutation lock during pending '+phase+' and ignores late success',async()=>{
    const f=fixture({requestTimeoutMs:20});
    await f.controller.open();
    await f.controller.selectPackage(A);
    const pending=deferred();
    let signal;
    f.intercept((path, options)=>{
      if (path !== '/api/v1/core-media') return undefined;
      signal=options.signal;
      return phase === 'fetch' ? pending.promise : {ok:true,json:()=>pending.promise};
    });
    const operation=f.controller.importMedia({name:'test.rom',size:32768});
    assert.equal(f.controller.snapshot().busy,true);
    await operation;
    assert.equal(signal.aborted,true);
    assert.equal(f.controller.snapshot().busy,false);
    assert.match(f.controller.snapshot().message,/timed out.*may already be saved.*nothing was retried/);
    assert.equal(f.controller.snapshot().media,null);
    assert.equal(f.controller.close(),true);
    const before=f.controller.snapshot();
    pending.resolve(phase === 'fetch' ? response({media_id:M,size:32768}) : {media_id:M,size:32768});
    await new Promise(resolve=>setImmediate(resolve));
    assert.deepEqual(f.controller.snapshot(),before);
    assert.equal(f.calls.filter(c=>c.path==='/api/v1/core-media').length,1);
    assert.deepEqual(f.changed,[]);
  });
}
test('empty/null inventories work without compatibility or lifecycle calls',async()=>{
  const f=fixture();
  f.intercept(path=>path==='/api/v1/core-packages'?response({packages:null}):
    path==='/api/v1/library/core-entries'?response({entries:null}):undefined);
  await f.controller.open();
  assert.deepEqual(f.controller.snapshot().entries,[]);
  assert.deepEqual(f.controller.snapshot().packages,[]);
  assert.equal(f.calls.length,2);
});
test('package import sends the original File body without Content-Length',async()=>{
  const f=fixture(); await f.controller.open();
  const file={name:'new.fcore',size:4096};
  await f.controller.importPackage(file);
  const call=f.calls.find(c=>c.options.method==='POST');
  assert.equal(call.path,'/api/v1/core-packages');
  assert.equal(call.options.body,file);
  assert.deepEqual(call.options.headers,{'Content-Type':'application/octet-stream'});
  assert.equal(call.options.redirect,'error');
  assert.equal(f.controller.snapshot().packageId,B);
  assert.equal(f.db.entries.length,1);
});
test('upload bounds and extension reject locally before POST',async()=>{
  const f=fixture(); await f.controller.open(); await f.controller.selectPackage(A);
  for (const file of [null,{name:'x.fcore',size:0},{name:'x.fcore',size:MAX_PACKAGE_BYTES+1},{name:'x.zip',size:1}])
    await f.controller.importPackage(file);
  for (const file of [null,{size:0},{size:MAX_MEDIA_BYTES+1}]) await f.controller.importMedia(file);
  assert.equal(f.calls.filter(c=>c.options.method==='POST').length,0);
});
test('media import shows digest/size but declared limits prevent selection or creation',async()=>{
  const f=fixture(); await f.controller.open(); await f.controller.selectPackage(A);
  const file={name:'big.rom',size:40000};
  await f.controller.importMedia(file);
  assert.deepEqual(f.controller.snapshot().media,{media_id:M,size:40000});
  await f.controller.createEntry('Too large');
  assert.match(f.controller.snapshot().message,/declared limits/);
  assert.equal(f.calls.filter(c=>c.path==='/api/v1/library/core-entries' && c.options.method==='POST').length,0);
  const upload=f.calls.find(c=>c.path==='/api/v1/core-media');
  assert.equal(upload.options.body,file);
  assert.deepEqual(upload.options.headers,{'Content-Type':'application/octet-stream'});
});
test('creates explicit package-only and media-backed entries, never launches',async()=>{
  for (const media of [false,true]) {
    const f=fixture(); await f.controller.open(); await f.controller.selectPackage(A);
    if(media) await f.controller.importMedia({size:32768});
    await f.controller.createEntry('<b>Title</b>');
    const call=f.calls.find(c=>c.path==='/api/v1/library/core-entries' && c.options.method==='POST');
    assert.deepEqual(JSON.parse(call.options.body),{title:'<b>Title</b>',package_id:A,
      ...(media?{media_role:'blob',media_id:M}:{})});
    assert.equal(f.changed.length,1);
    assert.ok(f.calls.every(c=>!c.path.includes('/session/')));
  }
});
test('same-core package selection uses observed expected ID; clear uses both expected IDs',async()=>{
  const f=fixture(); await f.controller.open(); await f.controller.selectEntry('entry');
  await f.controller.selectPackage(C);
  assert.equal(f.controller.snapshot().packageId,A);
  await f.controller.selectPackage(B); await f.controller.selectEntryPackage();
  const selection=f.calls.find(c=>c.options.method==='PUT');
  assert.deepEqual(JSON.parse(selection.options.body),{expected_package_id:A,package_id:B});
  await f.controller.clearEntryMedia();
  const clearing=f.calls.filter(c=>c.options.method==='PUT').at(-1);
  assert.deepEqual(JSON.parse(clearing.options.body),{expected_package_id:B,expected_media_id:M,media_role:'',media_id:''});
});
test('media selection sends current package/media CAS and imported digest',async()=>{
  const f=fixture(); f.db.entries[0].media_id='';
  await f.controller.open(); await f.controller.selectEntry('entry');
  await f.controller.importMedia({size:512}); await f.controller.selectEntryMedia();
  const call=f.calls.find(c=>c.options.method==='PUT');
  assert.deepEqual(JSON.parse(call.options.body),{expected_package_id:A,expected_media_id:'',media_role:'blob',media_id:M});
});
test('compatibility is unknown until explicit check and incompatible remains distinct',async()=>{
  const f=fixture(); await f.controller.open(); await f.controller.selectPackage(A);
  assert.equal(f.controller.snapshot().compatibility,null);
  assert.equal(f.calls.filter(c=>c.options.method==='POST').length,0);
  await f.controller.checkCompatibility();
  assert.equal(f.controller.snapshot().compatibility.compatible,false);
  assert.match(f.calls.at(-1).path,/compatibility$/);
});
test('conflict refreshes current selections without replaying mutation',async()=>{
  const f=fixture(); await f.controller.open(); await f.controller.selectEntry('entry');
  await f.controller.selectPackage(B);
  f.intercept((path,options)=>{
    if(options.method==='PUT') {
      f.db.entries[0].package_id=B;
      return response({error:{message:'stale selection'}},409);
    }
  });
  await f.controller.selectEntryPackage();
  assert.equal(f.calls.filter(c=>c.options.method==='PUT').length,1);
  assert.equal(f.controller.snapshot().entries[0].package_id,B);
  assert.match(f.controller.snapshot().message,/conflict.*not replayed/);
});
test('saved mutation then refresh failure is not described as rollback or retried',async()=>{
  const f=fixture(); await f.controller.open(); await f.controller.selectEntry('entry');
  await f.controller.selectPackage(B);
  let saved=false;
  f.intercept((path,options)=>{
    if(options.method==='PUT') { saved=true; return response({...f.db.entries[0],package_id:B}); }
    if(saved) return response({error:{message:'offline'}},503);
  });
  await f.controller.selectEntryPackage();
  assert.match(f.controller.snapshot().message,/may already be saved.*Refresh to confirm/);
  assert.equal(f.calls.filter(c=>c.options.method==='PUT').length,1);
});
test('network mutation failure is ambiguous and never retried',async()=>{
  const f=fixture(); await f.controller.open(); await f.controller.selectPackage(A);
  f.intercept((path,options)=>options.method==='POST'?Promise.reject(new Error('lost')):undefined);
  await f.controller.createEntry('Title');
  assert.match(f.controller.snapshot().message,/outcome unknown/);
  assert.equal(f.calls.filter(c=>c.options.method==='POST').length,1);
});
test('stale capability responses cannot overwrite a later package selection',async()=>{
  const f=fixture(); await f.controller.open();
  const pending=deferred();
  f.intercept(path=>path.includes(A+'/media-capabilities')?pending.promise:undefined);
  const first=f.controller.selectPackage(A);
  await f.controller.selectPackage(B);
  pending.resolve(response(caps(A))); await first;
  assert.equal(f.controller.snapshot().capabilities.package_id,B);
});
test('close invalidates pending inventory and overlapping refresh uses latest only',async()=>{
  const f=fixture(), pending=deferred();
  f.intercept(path=>path==='/api/v1/core-packages'?pending.promise:undefined);
  const first=f.controller.open();
  assert.equal(f.controller.close(),true);
  f.intercept(()=>undefined);
  await f.controller.open();
  pending.resolve(response({packages:[]})); await first;
  assert.equal(f.controller.snapshot().packages.length,3);
});
test('mutation locks selection, refresh, close and duplicate actions',async()=>{
  const f=fixture(); await f.controller.open(); await f.controller.selectPackage(A);
  const pending=deferred();
  f.intercept((path,options)=>options.method==='POST'?pending.promise:undefined);
  const action=f.controller.importMedia({size:4});
  assert.equal(f.controller.snapshot().busy,true);
  await f.controller.selectPackage(B); await f.controller.refresh(); await f.controller.createEntry('duplicate');
  assert.equal(f.controller.close(),false);
  assert.equal(f.controller.snapshot().packageId,A);
  assert.equal(f.calls.filter(c=>c.options.method==='POST').length,1);
  pending.resolve(response({media_id:M,size:4})); await action;
  assert.equal(f.controller.snapshot().busy,false);
});
test('unknown/malformed capability response fails closed for media selection',async()=>{
  const f=fixture(); await f.controller.open();
  f.intercept(path=>path.endsWith('/media-capabilities')?response({...caps(A),media:[{role:'blob',min_bytes:1,max_bytes:'unlimited'}]}):undefined);
  await f.controller.selectPackage(A);
  assert.equal(f.controller.snapshot().capabilities,null);
  assert.match(f.controller.snapshot().message,/Invalid declared/);
});

test('mounted panel uses literal text, locks controls and filters same-core versions',async()=>{
  class Node {
    constructor() { this.hidden=true; this.open=false; this.listeners={}; this.children=[]; this.value=''; this.files=[]; }
    set textContent(value) { this.text=String(value); }
    get textContent() { return this.text; }
    set innerHTML(_) { throw new Error('unsafe DOM'); }
    replaceChildren(...children) { this.children=children; }
    addEventListener(name,fn) { this.listeners[name]=fn; }
    setAttribute() {}
    showModal() { this.open=true; }
    close() { this.open=false; }
    focus() {}
  }
  const ids=['open-core-library','core-library','core-library-close','core-library-refresh',
    'core-package-file','core-package-import','core-package-select','core-package-check','core-package-status',
    'core-media-file','core-media-import','core-media-discard','core-media-status','core-entry-title',
    'core-entry-create','core-entry-select','core-entry-current','core-entry-package-save',
    'core-entry-media-save','core-entry-media-clear','core-library-message'];
  const nodes=Object.fromEntries(ids.map(id=>[id,new Node()]));
  nodes['core-library'].querySelectorAll=()=>ids.filter(id=>!id.endsWith('status') && id!=='core-library').map(id=>nodes[id]);
  const document={getElementById:id=>nodes[id],createElement:()=>new Node()};
  const f=fixture();
  f.db.entries[0].title='<img src=x onerror=alert(1)>';
  mount(document,f.controller);
  await f.controller.open(); await f.controller.selectEntry('entry');
  assert.ok(nodes['core-entry-select'].children[1].textContent.startsWith('<img'));
  assert.equal(nodes['core-package-select'].children.length,3);
  const pending=deferred();
  f.intercept((path,options)=>options.method==='POST'?pending.promise:undefined);
  const uploading=f.controller.importMedia({size:4});
  assert.equal(nodes['core-entry-title'].disabled,true);
  assert.equal(nodes['core-library-close'].disabled,true);
  pending.resolve(response({media_id:M,size:4})); await uploading;
  assert.equal(nodes['core-entry-title'].disabled,false);
  assert.equal(nodes['core-library-close'].disabled,false);
  f.controller.close();
  assert.equal(nodes['core-library'].hidden,true);
  assert.equal(nodes['core-library'].open,false);
});
