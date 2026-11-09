'use strict';
const test=require('node:test');
const assert=require('node:assert/strict');
const {createClient,mount,pick}=require('./ui_storage');
const bytesReply=(header,body) => ({ok:true,status:200,headers:{get:()=>header},body:new Blob([body]).stream()});

test('drive API encodes relative paths and preserves stored credentials on folder save',async () => {
 const calls=[];
 const api=createClient(async (path,options) => {calls.push({path,options});return {ok:true,status:200,json:async () => path.includes('/entries?')?{entries:[{name:'Game disk.p',size:14}]}:{id:'a'.repeat(24),saved:true}};});
 await api.save({kind:'smb',source:'//nas/Games',password_set:true,bios_folder:'BIOS files'});
 assert.equal(JSON.parse(calls[0].options.body).password_set,true);
 assert.equal(JSON.parse(calls[0].options.body).password,undefined);
 const entries=await api.entries('a'.repeat(24),'Media/Space & time');
 assert.equal(entries[0].name,'Game disk.p');
 assert.match(calls[1].path,/path=Media%2FSpace%20%26%20time$/);
 assert.equal(calls[1].options.redirect,'error');
});
test('file reads reject missing, oversized and truncated response lengths',async () => {
 for(const [header,body] of [[null,''],[String(33*1024*1024),''],['10','short']]){
  const api=createClient(async () => bytesReply(header,body));
  await assert.rejects(api.file('a'.repeat(24),'Media/game.p'));
 }
});
test('file bytes and failed-drive state stay explicit',async () => {
 const api=createClient(async () => bytesReply('14','cassette bytes'));
 assert.equal(await (await api.file('a'.repeat(24),'Media/game.p')).text(),'cassette bytes');
 const offline=createClient(async () => ({ok:false,status:404}));
 await assert.rejects(offline.entries('a'.repeat(24),'Media'),/Drive unavailable/);
});
test('hard-disk reads opt in to the dedicated route and its size bounds',async () => {
 const id='a'.repeat(24), calls=[];
 let size=0, body=new Uint8Array(0);
 const api=createClient(async (path,options) => {
  calls.push({path,options});
  return bytesReply(String(size),body);
 });
 for(const n of [1024*1024,32*1024*1024+512,128*1024*1024]) {
  size=n;body=new Uint8Array(n===1024*1024?n:1);
  if(n===1024*1024) assert.equal((await api.file(id,'Disks/game.img',{purpose:'hard-disk'})).size,n);
  else await assert.rejects(api.file(id,'Disks/game.img',{purpose:'hard-disk'}),/changed or transfer was interrupted/);
  assert.equal(calls.at(-1).path,`/api/v1/storage/drives/${id}/file?path=Disks%2Fgame.img&purpose=hard-disk`);
 }
 for(const n of [0,1024*1024-512,1024*1024+1,128*1024*1024+512]) {
  size=n;body=new Uint8Array(1);
  await assert.rejects(api.file(id,'disk.img',{purpose:'hard-disk'}),/1–128 MiB/);
  assert.equal(calls.at(-1).options.signal.aborted,true);
 }
 size=32*1024*1024+512;body=new Uint8Array(1);
 await assert.rejects(api.file(id,'disk.img'),/up to 32 MiB/);
 assert.equal(calls.at(-1).path,`/api/v1/storage/drives/${id}/file?path=disk.img`);
 size=1024*1024;body=new Uint8Array(1);
 await assert.rejects(api.file(id,'disk.img',{purpose:'hard-disk'}),/changed or transfer was interrupted/);
 body=new Uint8Array(size+1);
 await assert.rejects(api.file(id,'disk.img',{purpose:'hard-disk'}),/changed or transfer was interrupted/);
 assert.equal(calls.at(-1).options.signal.aborted,true);
 assert.throws(() => api.file(id,'disk.img',{purpose:'other'}),/Invalid drive file purpose/);
});

test('disk body uses a size-based transfer budget and external cancellation',async t => {
 const originalSetTimeout=globalThis.setTimeout, budgets=[];
 globalThis.setTimeout=(fn,ms,...args) => {budgets.push(ms);return originalSetTimeout(fn,10000,...args);};
 t.after(()=>{globalThis.setTimeout=originalSetTimeout;});
 const size=128*1024*1024;
 const api=createClient(async () => bytesReply(String(size),new Uint8Array(1)));
 await assert.rejects(api.file('a'.repeat(24),'disk.img',{purpose:'hard-disk'}),/changed or transfer was interrupted/);
 assert.deepEqual(budgets,[1114000,1114000]);
 const abort=new AbortController();
 let signal;
 const pending=createClient(async (_path,options) => {
  signal=options.signal;
  return new Promise((_,reject)=>signal.addEventListener('abort',()=>reject(new Error('aborted')),{once:true}));
 });
 const read=pending.file('a'.repeat(24),'disk.img',{purpose:'hard-disk',signal:abort.signal});
 abort.abort();
 await assert.rejects(read,/aborted/);
 assert.equal(signal.aborted,true);
});

test('picker advertises the selected limit and reads saved USB and SMB hard disks',async t => {
 const NativeFile=globalThis.File;
 if(!NativeFile)globalThis.File=require('node:buffer').File;
 t.after(() => { if(!NativeFile)delete globalThis.File; });
 class Node {
  constructor(tag='') { this.tag=tag; this.children=[]; this.listeners={}; this.textContent=''; this.disabled=false; }
  append(...children) { this.children.push(...children); }
  replaceChildren(...children) { this.children=children; }
  addEventListener(name,fn) { this.listeners[name]=fn; }
  setAttribute() {}
  querySelectorAll() { return []; }
  showModal() { this.open=true; }
  close() { this.open=false; }
  focus() {}
  click() { if(this.listeners.click)this.listeners.click({target:this}); }
 }
 const waitFor=async pred => {
  for(let i=0;i<200;i++) { if(pred())return; await new Promise(resolve=>setTimeout(resolve,1)); }
  assert.fail('picker did not reach expected state');
 };
 for(const kind of ['usb','smb']) {
  const id=(kind==='usb'?'a':'b').repeat(24), calls=[];
  let blockFile=false, readSignal=null;
  const drive={id,kind,label:kind.toUpperCase(),saved:true,media_folder:'Disks'};
  const launch=new Node('button'), body=new Node('body');
  const doc={body,createElement:tag=>new Node(tag),getElementById:id=>id==='storage-manage'?launch:null};
  const fetch=async (path,options={}) => {
   calls.push(path);
   if(path==='/api/v1/storage/drives?discover=true')return {ok:true,status:200,json:async()=>({drives:[drive]})};
   if(path.includes('/entries?'))return {ok:true,status:200,json:async()=>({entries:[
    {name:'oversize.img',size:129*1024*1024,directory:false},
    {name:'disk.img',size:1024*1024,directory:false},
   ]})};
   if(path.includes('/file?')) {
    if(blockFile) {
     readSignal=options.signal;
     return new Promise((_,reject)=>readSignal.addEventListener('abort',()=>reject(new Error('aborted')),{once:true}));
    }
    return bytesReply(String(1024*1024),new Uint8Array(1024*1024));
   }
   throw Error('unexpected '+path);
  };
  mount(doc,fetch);
  const dialog=body.children[0],content=dialog.children[1],status=dialog.children[2];
  let selected=null;
  pick({purpose:'media',filePurpose:'hard-disk',onSelect:file => {selected=file;}});
  await waitFor(() => content.children.some(n=>n.textContent.includes('1–128 MiB')) && content.children.some(n=>n.textContent.includes(kind.toUpperCase())));
  content.children.find(n=>n.tag==='button'&&n.textContent.includes(kind.toUpperCase())).click();
  await waitFor(() => content.children.some(n=>n.textContent.includes('disk.img')));
  assert.ok(content.children.some(n=>n.textContent.includes('whole 512-byte sectors')));
  content.children.find(n=>n.tag==='button'&&n.textContent.includes('oversize.img')).click();
  await waitFor(() => status.textContent.includes('1–128 MiB'));
  assert.equal(calls.filter(path=>path.includes('/file?')).length,0);
  content.children.find(n=>n.tag==='button'&&n.textContent.includes('disk.img')).click();
  await waitFor(() => selected !== null);
  assert.equal(selected.name,'disk.img');assert.equal(selected.size,1024*1024);
  assert.ok(calls.includes(`/api/v1/storage/drives/${id}/file?path=Disks%2Fdisk.img&purpose=hard-disk`));
  // BIOS selection cannot opt into the hard-disk allowance even if supplied
  // a disk purpose accidentally.
  await new Promise(resolve=>setTimeout(resolve,0));
  selected=null;
  pick({purpose:'bios',filePurpose:'hard-disk',onSelect:file => {selected=file;}});
  await waitFor(() => content.children.some(n=>n.textContent.includes('up to 32 MiB')) && content.children.some(n=>n.textContent.includes(kind.toUpperCase())));
  content.children.find(n=>n.tag==='button'&&n.textContent.includes(kind.toUpperCase())).click();
  await waitFor(() => content.children.some(n=>n.textContent.includes('disk.img')));
  content.children.find(n=>n.tag==='button'&&n.textContent.includes('oversize.img')).click();
  await waitFor(() => status.textContent.includes('up to 32 MiB'));
  assert.equal(calls.filter(path=>path.includes('/file?')).length,1);
  content.children.find(n=>n.tag==='button'&&n.textContent.includes('disk.img')).click();
  await waitFor(() => selected !== null);
  assert.ok(calls.includes(`/api/v1/storage/drives/${id}/file?path=disk.img`));
  // Dismissing a large transfer aborts its request and never calls onSelect.
  await new Promise(resolve=>setTimeout(resolve,0));
  selected=null;blockFile=true;
  pick({purpose:'media',filePurpose:'hard-disk',onSelect:file => {selected=file;}});
  await waitFor(() => content.children.some(n=>n.tag==='button'&&n.textContent.includes(kind.toUpperCase())));
  content.children.find(n=>n.tag==='button'&&n.textContent.includes(kind.toUpperCase())).click();
  await waitFor(() => content.children.some(n=>n.textContent.includes('disk.img')));
  content.children.find(n=>n.tag==='button'&&n.textContent.includes('disk.img')).click();
  await waitFor(() => readSignal !== null);
  dialog.children[3].click();
  assert.equal(readSignal.aborted,true);
  await new Promise(resolve=>setTimeout(resolve,0));
  assert.equal(selected,null);assert.equal(dialog.open,false);
 }
});
