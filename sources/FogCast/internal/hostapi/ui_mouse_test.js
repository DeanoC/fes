const test=require('node:test');const assert=require('node:assert/strict');
const {parseSession,mouseEventRequest,createAppController}=require('./ui_app.js');
const core={package_id:'a'.repeat(64),generation:7,abi:{id:'fes.computer',major:1,minor:0},active_interfaces:[{id:'fes.mouse.relative',major:1,minor:0}]};
test('mouse capability and signed packed request',()=>{
 assert.equal(parseSession({state:'active',core_package:core}).mouse_relative,true);
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
