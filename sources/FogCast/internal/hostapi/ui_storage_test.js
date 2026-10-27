'use strict';
const test=require('node:test');
const assert=require('node:assert/strict');
const {createClient}=require('./ui_storage');

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
  const api=createClient(async () => ({ok:true,status:200,headers:{get:() => header},blob:async () => new Blob([body])}));
  await assert.rejects(api.file('a'.repeat(24),'Media/game.p'));
 }
});
test('file bytes and failed-drive state stay explicit',async () => {
 const api=createClient(async () => ({ok:true,status:200,headers:{get:() => '14'},blob:async () => new Blob(['cassette bytes'])}));
 assert.equal(await (await api.file('a'.repeat(24),'Media/game.p')).text(),'cassette bytes');
 const offline=createClient(async () => ({ok:false,status:404}));
 await assert.rejects(offline.entries('a'.repeat(24),'Media'),/Drive unavailable/);
});
