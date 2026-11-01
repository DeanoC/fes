const test=require('node:test');const assert=require('node:assert/strict');
const {parseSession,mouseEventRequest,createAppController}=require('./ui_app.js');
const core={package_id:'a'.repeat(64),generation:7,abi:{id:'fes.computer',major:1,minor:0},active_interfaces:[{id:'fes.mouse.relative',major:1,minor:0}]};
test('mouse capability and signed packed request',()=>{
 assert.equal(parseSession({state:'active',core_package:core}).mouse_relative,true);
 assert.equal(parseSession({state:'active',core_package:{...core,abi:{...core.abi,minor:1}}}).mouse_relative,true);
 assert.equal(parseSession({state:'active',core_package:{...core,abi:{...core.abi,minor:2}}}).mouse_relative,undefined);
 assert.equal(parseSession({state:'active',core_package:{...core,active_interfaces:[{id:'fes.mouse.relative',major:1,minor:1}]}}).mouse_relative,undefined);
 const event=JSON.parse(mouseEventRequest(-32768,32767,3).options.body).event;
 assert.deepEqual(event,{Player:0,Device:2,Kind:4,Action:3,Code:3,Value:2147450880});
});
test('uncertain motion is sent once',async()=>{
 let posts=0;const input={state:'attached',ready:true,metrics:{frames_sent:0,state_resyncs:0,sequence_gaps:0,releases:0,capture_to_bridge_p95_ms:0,bridge_to_uinput_p95_ms:0,rtt_ms:0,bridge_to_uinput_measurable:false}};
 const controller=createAppController({fetchImpl:async(path)=>{if(path==='/api/v1/session')return {ok:true,status:200,json:async()=>({state:'active',core_package:core,input})};posts++;throw new Error('lost response');}});
 await controller.loadSession();assert.equal(controller.mouseAllowed(),true);
 assert.equal(await controller.sendMouseRelative(2,-3,1),false);assert.equal(posts,1);
 assert.equal(await controller.sendMouseRelative(0,0,4),false);assert.equal(posts,1);
});
test('a full queue drops motion but keeps every button change',async()=>{
 const input={state:'attached',ready:true,metrics:{frames_sent:0,state_resyncs:0,sequence_gaps:0,releases:0,capture_to_bridge_p95_ms:0,bridge_to_uinput_p95_ms:0,rtt_ms:0,bridge_to_uinput_measurable:false}};
 let open;const gate=new Promise(resolve=>{open=resolve;});const sent=[];
 const controller=createAppController({fetchImpl:async(path,options)=>{
  if(path==='/api/v1/session')return {ok:true,status:200,json:async()=>({state:'active',core_package:core,input})};
  sent.push(JSON.parse(options.body).event);await gate;return {ok:true,status:200,json:async()=>({ok:true})};
 }});
 await controller.loadSession();
 // Both buttons down, then motion until 64 requests wait on the host.
 const queued=[controller.sendMouseRelative(0,0,3)];
 for(let n=1;n<64;n++)queued.push(controller.sendMouseRelative(1,0,3));
 assert.equal(await controller.sendMouseRelative(1,0,3),false,'pure motion at depth 64 is dropped');
 const release=controller.sendMouseRelative(0,0,1);
 assert.equal(await controller.sendMouseRelative(0,0,1),false,'an unchanged repeat is dropped');
 open();
 assert.deepEqual(await Promise.all(queued),new Array(64).fill(true));
 assert.equal(await release,true);
 assert.equal(sent.length,65);
 assert.deepEqual(sent[64],{Player:0,Device:2,Kind:4,Action:3,Code:1,Value:0});
});
