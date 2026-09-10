const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const evidenceDir = process.env.UNMUTE_DEV_STREAM_EVIDENCE;
if(evidenceDir) fs.mkdirSync(evidenceDir,{recursive:true});
const {chromium} = require(process.env.UNMUTE_SWEEP_PLAYWRIGHT || 'playwright');

// Replay only network/media boundaries. The page, its renderer and the browser
// DOM remain unchanged. No provider, microphone or remote service is contacted.
(async () => {
  const browser = await chromium.launch({headless:true,executablePath:chromium.executablePath()});
  try {
    for (const target of ['pipecat', 'livekit']) {
      const page = await browser.newPage({viewport:{width:1280,height:800}});
      const errors = [];
      page.on('pageerror', error => errors.push(error.message));
      let call = 0, offer, forcedCallID, holdNextBootstrap=false, heldBootstrap, bootstrapHeld;
      await page.route('**/api/session', route => {
        const callID=forcedCallID || `call-${call+1}`;
        call++;
        const session={ready:true,dev_events_version:2,call_id:callID,
          kind:target==='livekit'?'livekit':'webrtc-offer',offerUrl:'/api/offer',
          url:'ws://unused',token:'unused',room:callID};
        if(holdNextBootstrap){
          holdNextBootstrap=false;heldBootstrap=()=>route.fulfill({json:session});bootstrapHeld();return;
        }
        return route.fulfill({json:session});
      });
      await page.route('**/api/offer', route => {
        offer = route.request().postDataJSON();
        return route.fulfill({json:{sdp:'answer',type:'answer',pc_id:'peer'}});
      });
      await page.addInitScript(() => {
        window.mediaConnections = 0;
        window.mediaCloses = 0;
        window.testFeed = class extends EventTarget {
          static CONNECTING = 0;
          static OPEN = 1;
          static CLOSED = 2;
          constructor(){ super(); window.feed=this; this.readyState=1; setTimeout(()=>{
            this.dispatchEvent(new Event('open'));
            this.emit({t:'state',state:'ready',stream_id:'test'});
          },0); }
          emit(event){this.dispatchEvent(new MessageEvent('message',{data:JSON.stringify(event),lastEventId:event.seq?`${event.stream_id}:${event.seq}`:''}));}
          close(){this.readyState=2;}
        };
        window.EventSource = window.testFeed;
        const track={enabled:true,kind:'audio',stop(){},getSettings(){return {};}};
        const stream={getTracks:()=>[track],getAudioTracks:()=>[track]};
        Object.defineProperty(navigator,'mediaDevices',{value:{getUserMedia:async()=>stream,
          enumerateDevices:async()=>[],addEventListener(){}}});
        window.RTCPeerConnection=class extends EventTarget {
          constructor(){super();window.latestPeer=this;window.mediaConnections++;this.iceGatheringState='complete';this.connectionState='connected';}
          addTrack(){}
          createDataChannel(){const dc=new EventTarget();dc.readyState='open';dc.send=()=>{};dc.close=()=>{};return dc;}
          async createOffer(){return {sdp:'offer',type:'offer'};}
          async setLocalDescription(offer){this.localDescription=offer;}
          async setRemoteDescription(){}
          close(){window.mediaCloses++;}
        };
        window.LivekitClient={Room:class {
          constructor(){window.latestRoom=this;this.handlers={};window.mediaConnections++;this.localParticipant={setMicrophoneEnabled:async()=>{},getTrackPublication:()=>null};}
          on(event,handler){this.handlers[event]=handler;return this;}
          async connect(){}
          disconnect(){window.mediaCloses++;}
        },RoomEvent:{TrackSubscribed:'track',TrackUnsubscribed:'untrack',LocalTrackPublished:'local',Disconnected:'end',ParticipantConnected:'participant',TranscriptionReceived:'transcription'},Track:{Kind:{Audio:'audio'},Source:{Microphone:'mic'}}};
        window.arrivalDelays=[];
        window.metricArrivalDelays=[];
      });
      await page.goto(process.argv[2]);
      await page.locator('#connect:not([disabled])').waitFor();
      await page.locator('#connect').click();
      await page.locator('body[data-state="connected"]').waitFor();
      if(target==='pipecat') assert.equal(offer.request_data?.unmute_dev_call_id,'call-1','Pipecat offer must forward the selected call ID');
      let seq=0, order=0, streamID='test';
      const identities=new Map();
      async function emit(kind,id,payload,revision,call_id='call-1') {
        const key=JSON.stringify([call_id,kind,id]);
        const previous=identities.get(key)||{revision:0,order:++order};
        const record={version:2,kind,id,call_id,revision:revision??previous.revision+1,order:previous.order,[kind]:payload};
        const changes=record.revision>previous.revision;
        if(changes) identities.set(key,record);
        const expected=kind==='text'&&changes&&call_id===`call-${call}`?{id,...payload}:null;
        await page.evaluate(async ({event,expected})=>{
          const start=performance.now();window.lastArrival=start;window.feed.emit(event);
          if(!expected) return;
          // Measure the frame where the expected text/finality is actually
          // visible, not merely the first frame after an event was dispatched.
          await new Promise((resolve,reject)=>{
            const check=()=>{
              const node=document.querySelector(`[data-kind="text"][data-id="${expected.id}"]`);
              const delay=performance.now()-start;
              const normal=expected.speaker!=='user'||expected.state!=='final'||
                (node&&node.closest('.said')&&getComputedStyle(node).color===getComputedStyle(node.closest('.said')).color);
              if((!expected.text&&!node)||(node&&node.textContent===(expected.separator_before+expected.text)&&node.dataset.state===expected.state&&normal)){
                window.arrivalDelays.push(delay);resolve();
              }else if(delay>=250){reject(new Error(`text ${expected.id} not visible/final at 250ms`));}
              else requestAnimationFrame(check);
            };
            requestAnimationFrame(check);
          });
        },{event:{t:'metric',stream_id:streamID,seq:++seq,record},expected});
      }
      const segment=(text,state='final',extra={})=>({exchange_id:'input-1',message_id:'message-1',speaker:'user',text,state,origin:'recognition',separator_before:'',...extra});
      await emit('call','call',{target,state:'open',input_boundaries:'known',model_calls:'known',generated_text:'available'});
      await emit('exchange','input-1',{role:'input',state:'open',link_status:'unavailable'});
      await emit('text','segment-1',segment('Yes','provisional'));
      const textNode=page.locator('[data-kind="text"][data-id="segment-1"]');
      assert.equal(await textNode.textContent(),'Yes');
      assert.equal(await textNode.getAttribute('data-state'),'provisional');
      const provisionalColor=await textNode.evaluate(node=>getComputedStyle(node).color);
      await emit('text','segment-1',segment('Yes, go for it.'));
      assert.equal(await textNode.getAttribute('data-state'),'final');
      const finalColors=()=>textNode.evaluate(node=>({segment:getComputedStyle(node).color,
        normal:getComputedStyle(node.closest('.said')).color}));
      const finalized=await finalColors();
      assert.equal(finalized.segment,finalized.normal,'final STT must use normal transcript color immediately');
      assert.notEqual(finalized.segment,provisionalColor,'final STT must stop looking provisional before the reply');
      await page.waitForTimeout(5000);
      assert.equal(await textNode.getAttribute('data-state'),'final');
      assert.deepEqual(await finalColors(),finalized,'final STT must remain normal throughout the response delay');
      assert.equal(await page.locator('[data-speaker="assistant"]').count(),0);
      await emit('text','segment-2',segment('Again','provisional',{separator_before:' '}));
      await emit('text','segment-2',segment('', 'provisional',{separator_before:' '}));
      await emit('text','segment-2',segment('Yes.', 'final',{separator_before:' '}));
      await emit('text','segment-1',segment('Yes, please.'));
      await emit('text','segment-1',segment('stale','provisional'),1);
      assert.equal(await textNode.textContent(),'Yes, please.');
      await emit('exchange','input-2',{role:'input',state:'open',link_status:'unavailable'});
      await emit('text','segment-3',segment('Yes.', 'final',{exchange_id:'input-2',message_id:'message-2'}));
      assert.equal(await page.locator('[data-kind="text"]').count(),3);
      await emit('exchange','empty-input',{role:'input',state:'ended',link_status:'unavailable'});
      assert.equal(await page.locator('[data-exchange="empty-input"]').evaluate(node=>node.getBoundingClientRect().height),0,'empty input exchanges consume no space');
      const callerSpacing=await page.locator('[data-exchange="input-2"]').evaluate(node=>({
        section:node.getBoundingClientRect().height,content:node.querySelector('.exchange-content').getBoundingClientRect().height
      }));
      assert.ok(callerSpacing.section-callerSpacing.content<1,'empty status/activity/details add no gaps to a caller turn');
      // US2: hold the answer back while each independent activity/value arrives.
      // Identity attrs are the same ones used for text; disclosures stay native.
      const identity=(kind,id)=>`[data-kind="${kind}"][data-id="${id}"]`;
      const visible=(selector,pattern)=>({selector,pattern:pattern.source,flags:pattern.flags});
      async function checkpoint(...checks){
        await page.evaluate(async checks=>{
          const start=window.lastArrival;
          await new Promise((resolve,reject)=>{
            const check=()=>{
              const missing=checks.filter(({selector,pattern,flags})=>{
                const node=document.querySelector(selector);
                return !node||!node.checkVisibility()||!new RegExp(pattern,flags).test(node.textContent);
              });
              const delay=performance.now()-start;
              if(!missing.length){window.arrivalDelays.push(delay);window.metricArrivalDelays.push(delay);resolve();}
              else if(delay>=250)reject(new Error(`activity/value not visible at 250ms: ${JSON.stringify(missing)}`));
              else requestAnimationFrame(check);
            };
            requestAnimationFrame(check);
          });
        },checks);
      }
      const replyID='metrics-response';
      const replySelector=`[data-exchange="${replyID}"]`;
      const reply=page.locator(replySelector);
      const response=(state='open',playback='not_started')=>({role:'response',state,link_status:'unavailable',playback});
      const operation=(type,name,extra={})=>({exchange_id:replyID,type,name,state:'running',...extra});
      const measure=(metric,value,extra={})=>({exchange_id:replyID,scope:'response',metric,unit:'seconds',
        state:value==null?'unavailable':'measured',...(value==null?{reason:'not reported'}:{value}),source:'replay-sdk',...extra});
      await emit('exchange',replyID,response());
      await emit('exchange',replyID,response('open','unknown'));
      assert.doesNotMatch(await reply.locator('.exchange-status').textContent(),/audio unknown/);
      await emit('call','call',{target,state:'open',input_boundaries:'known',model_calls:'known',generated_text:'limited'});
      assert.doesNotMatch(await reply.textContent(),/limited|unavailable|unknown/i);
      await emit('call','call',{target,state:'open',input_boundaries:'known',model_calls:'known',generated_text:'available'});
      assert.doesNotMatch(await reply.locator('.exchange-status').textContent(),/limited/i,'fresh coverage removes the source limitation');
      await emit('operation','model-1',operation('llm','same-model',{model:'model-v1',provider:'source-provider',source_request_id:'reused-provider-id'}));
      await checkpoint(visible(identity('operation','model-1'),/LLM\s*1[\s\S]*running/i),
        visible(replySelector+' .metrics-summary',/^1 model call so far$/i));
      assert.equal(await page.locator('[data-speaker="assistant"]').count(),0,'activity must arrive before answer text');
      const details=reply.locator('details').first(),summary=details.locator('summary');
      await summary.click();
      for(const value of ['model-v1','source-provider','reused-provider-id']) assert.ok((await details.textContent()).includes(value),'operation metadata available before any measurement: '+value);
      await summary.focus();
      await details.evaluate(node=>{window.preservedDetails=node;window.preservedSummary=node.querySelector('summary');});
      await emit('measurement','model-1:first',measure('first_response',.88,{scope:'operation',operation_id:'model-1'}));
      await checkpoint(visible(identity('measurement','model-1:first'),/first response[\s\S]*880ms/i));
      assert.match(await details.textContent(),/model-v1/);
      assert.match(await details.textContent(),/source-provider/);
      assert.match(await details.textContent(),/replay-sdk/);
      assert.doesNotMatch(await reply.textContent(),/First response can precede|Full service request|excludes delivery/i,'metric explanations belong in the guide');
      await emit('measurement','model-1:duration',measure('request_duration',1.42,{scope:'operation',operation_id:'model-1'}));
      await checkpoint(visible(identity('measurement','model-1:duration'),/request duration[\s\S]*1\.42s/i),
        visible(identity('measurement','model-1:first'),/first response[\s\S]*880ms/i));
      assert.equal(await details.evaluate(node=>node===window.preservedDetails&&node.open&&document.activeElement===window.preservedSummary),true,'updates preserve open details and focus');
      for(const [ordinal,seconds] of [[2,.89],[3,.93]]){
        const id=`model-${ordinal}`;
        await emit('operation',id,operation('llm','same-model',{model:'model-v1',provider:'source-provider',source_request_id:'reused-provider-id'}));
        await checkpoint(visible(identity('operation',id),new RegExp(`LLM\\s*${ordinal}[\\s\\S]*running`,'i')),
          visible(replySelector+' .metrics-summary',new RegExp(`${ordinal} model calls so far`,'i')));
        await emit('measurement',id+':first',measure('first_response',seconds,{scope:'operation',operation_id:id}));
        await checkpoint(visible(identity('measurement',id+':first'),new RegExp(`first response[\\s\\S]*${Math.round(seconds*1000)}ms`,'i')));
      }
      assert.doesNotMatch(await reply.locator('.metrics-summary').textContent(),/2\.70s|2700ms/,'first-response values cannot be summed into reply latency');
      // Tool rows remain visible while details are closed, even with equal names.
      await summary.click();
      assert.equal(await details.getAttribute('open'),null);
      for (const id of ['model-1:first','model-1:duration','model-2:first','model-3:first']) {
        const metric=page.locator(identity('measurement',id));
        assert.equal(await metric.isVisible(),true,'request latency is visible without expanding debug');
        assert.equal(await metric.locator('xpath=ancestor::*[@data-kind="operation"]').count(),1,'latency belongs beside its own request');
      }
      await emit('measurement','model-2:duration',measure('request_duration',1.2,{scope:'operation',operation_id:'model-2'}));
      await checkpoint(visible(identity('measurement','model-2:duration'),/1\.20s/));
      await summary.focus();
      await page.keyboard.press('Space');
      assert.equal(await details.evaluate(node=>node.open),true,'Space opens native details instead of changing the microphone');
      await page.keyboard.press('Space');
      assert.equal(await details.evaluate(node=>node.open),false);
      for(const id of ['tool-a','tool-b']){
        await emit('operation',id,operation('tool','lookup'));
        await checkpoint(visible(identity('operation',id),/lookup[\s\S]*running/i));
      }
      const toolA=page.locator(identity('operation','tool-a'));
      await toolA.evaluate(node=>window.preservedTool=node);
      assert.equal(await page.locator(identity('operation','tool-a')+','+identity('operation','tool-b')).count(),2);
      await emit('operation','tool-b',operation('tool','lookup',{state:'failed'}));
      await checkpoint(visible(identity('operation','tool-b'),/lookup[\s\S]*failed/i));
      await emit('operation','tool-a',operation('tool','lookup',{state:'returned'}));
      await checkpoint(visible(identity('operation','tool-a'),/lookup[\s\S]*returned/i));
      assert.equal(await toolA.evaluate(node=>node===window.preservedTool),true,'a tool outcome updates its existing row');
      await emit('operation','tool-a',operation('tool','lookup',{state:'returned'}),2);
      assert.equal(await page.locator(identity('operation','tool-a')+','+identity('operation','tool-b')).count(),2,'duplicate tool delivery adds no row');
      // A control gets a row of its own, naming the cause of the model call
      // that follows it, and is never counted as one: the summary below still
      // reads three.
      await emit('operation','handoff-a',operation('handoff','to_billing'));
      await checkpoint(visible(identity('operation','handoff-a'),/handoff[\s\S]*to_billing[\s\S]*running/i));
      await emit('operation','handoff-a',operation('handoff','to_billing',{state:'returned'}));
      await checkpoint(visible(identity('operation','handoff-a'),/to_billing[\s\S]*returned/i));
      await emit('measurement','reply-latency',measure('reply_latency',3.93));
      await checkpoint(visible(replySelector+' .metrics-summary',/reply latency[\s\S]*3\.93s[\s\S]*3 model calls so far/i));
      assert.equal(await details.getAttribute('open'),null,'summary updates do not open details');
      await summary.click();
      await emit('measurement','tool-a:duration',measure('tool_duration',0,{scope:'operation',operation_id:'tool-a'}));
      await checkpoint(visible(identity('measurement','tool-a:duration'),/tool duration[\s\S]*0ms/i));
      await emit('measurement','tool-b:duration',measure('tool_duration',.001,{scope:'operation',operation_id:'tool-b'}));
      await checkpoint(visible(identity('measurement','tool-b:duration'),/tool duration[\s\S]*1ms/i));
      for(const [id,seconds,label] of [['sub-ms',.0004,'<1ms'],['rounded-second',.9996,'1.00s']]){
        await emit('measurement',id,measure('text_aggregation',seconds));
        await checkpoint(visible(identity('measurement',id),new RegExp(label.replace(/[.*+?^${}()|[\]\\]/g,'\\$&'))));
      }
      await emit('measurement','missing-duration',measure('request_duration',null,{scope:'operation',operation_id:'model-3'}));
      assert.equal(await page.locator(identity('measurement','missing-duration')).count(),0,'missing measurements have no placeholder');
      await emit('measurement','missing-duration',measure('request_duration',.5,{scope:'operation',operation_id:'model-3'}));
      await checkpoint(visible(identity('measurement','missing-duration'),/500ms/));
      await emit('measurement','missing-duration',measure('request_duration',null,{scope:'operation',operation_id:'model-3'}));
      assert.equal(await page.locator(identity('measurement','missing-duration')).count(),0,'a corrected missing measurement removes the old value');
      await emit('operation','tts-1',operation('tts','voice-synthesis',{model:'voice-v1',provider:'audio-provider'}));
      await checkpoint(visible(identity('operation','tts-1'),/voice-synthesis[\s\S]*running/i));
      await emit('measurement','tts-1:first',measure('first_response',.24,{scope:'operation',operation_id:'tts-1'}));
      await checkpoint(visible(identity('measurement','tts-1:first'),/TTFB[\s\S]*240ms/i));
      for(const [id,metric,seconds,label] of [
        ['turn-delay','turn_detection',.12,/turn detection[\s\S]*120ms/i],
        ['transcription-delay','transcription_delay',.29,/transcription delay[\s\S]*290ms/i],
        ['aggregation-delay','text_aggregation',.006,/text aggregation[\s\S]*6ms/i],
        ['spoken-duration','speech_duration',2.1,/speech duration[\s\S]*2\.10s/i],
      ]){
        await emit('measurement',id,measure(metric,seconds));
        await checkpoint(visible(identity('measurement',id),label));
      }
      assert.doesNotMatch(await details.textContent(),/excludes.*browser/i);
      for(const id of ['model-1','model-2','model-3']) await emit('operation',id,operation('llm','same-model',{state:'ended',model:'model-v1',provider:'source-provider',source_request_id:'reused-provider-id'}));
      await emit('exchange',replyID,response('ended','ended'));
      await checkpoint(visible(replySelector+' .metrics-summary',/reply latency[\s\S]*3\.93s[\s\S]*3 model calls/i));
      assert.doesNotMatch(await reply.locator('.metrics-summary').textContent(),/so far/);
      await emit('measurement','explicit-owner',measure('first_response',.09,{exchange_id:'input-2',scope:'operation',operation_id:'model-1'}));
      assert.equal(await page.locator(identity('measurement','explicit-owner')).evaluate(node=>node.closest('[data-exchange]').dataset.exchange),'input-2','an explicit measurement owner is never overridden for inline placement');

      // Call and unknown scopes cannot silently become the latest reply's values.
      const firstSpeechScope={scope:'call',exchange_id:undefined,
        source:target==='livekit'?'livekit-session-first':'pipecat-session-first'};
      await emit('measurement','session-first',measure('first_speech',null,{...firstSpeechScope,state:'pending',reason:undefined}));
      const callValue=page.locator(identity('measurement','session-first'));
      const callDetails=callValue.locator('xpath=ancestor::details[1]');
      assert.equal(await callValue.count(),0,'pending values do not create empty details');
      await emit('measurement','session-first',measure('first_speech',.04,firstSpeechScope));
      assert.equal(await page.locator('#tlist [data-exchange="call-details"]').count(),0,'call diagnostics stay outside the conversation');
      assert.equal(await page.locator('#diagnostics').getAttribute('open'),null,'diagnostics start collapsed');
      await page.locator('#diagnostics > summary').click();
      await callDetails.locator('summary').click();
      await checkpoint(visible(identity('measurement','session-first'),/first speech[\s\S]*40ms/i));
      assert.equal(await reply.locator(identity('measurement','session-first')).count(),0);
      assert.match(await callValue.locator('xpath=ancestor::section[1]').textContent(),/call|session/i);
      const unknownScope={scope:'unassigned',exchange_id:undefined,
        source:'source-only-no-clock',model:'model-without-time',provider:'provider-without-time'};
      await emit('measurement','source-only',measure('request_duration',null,{...unknownScope,state:'pending',reason:undefined}));
      const unknownValue=page.locator(identity('measurement','source-only'));
      const unknownDetails=unknownValue.locator('xpath=ancestor::details[1]');
      assert.equal(await unknownValue.count(),0);
      await emit('measurement','source-only',measure('request_duration',null,unknownScope));
      assert.equal(await unknownValue.count(),0);
      await emit('measurement','source-only',measure('request_duration',.2,unknownScope));
      await unknownDetails.locator('summary').click();
      await checkpoint(visible(identity('measurement','source-only'),/200ms/));
      assert.equal(await reply.locator(identity('measurement','source-only')).count(),0);
      const unknownSection=unknownValue.locator('xpath=ancestor::section[1]');
      assert.match(await unknownSection.textContent(),/unassigned/i);
      assert.equal(await unknownSection.evaluate(node=>document.querySelector('#diagnostic-list').contains(node)),true,'unassigned measurements stay in the footer');
      for(const value of ['source-only-no-clock','model-without-time','provider-without-time']) assert.match(await unknownSection.textContent(),new RegExp(value));
      // Opaque native IDs may equal the UI's names for synthetic scope groups.
      await emit('exchange','call-details',response());
      await emit('operation','scope-collision-response',operation('llm','identified response',{exchange_id:'call-details'}));
      const collisionReply=page.locator(identity('operation','scope-collision-response')).locator('xpath=ancestor::section[1]');
      assert.equal(await callValue.evaluate((node,selector)=>node.closest('section')!==document.querySelector(selector).closest('section'),identity('operation','scope-collision-response')),true,'call measurements stay separate from an exchange named call-details');
      assert.match(await callDetails.locator('xpath=ancestor::section[1]').locator('.metrics-summary').textContent(),/call measurements/i);
      assert.match(await collisionReply.locator('.metrics-summary').textContent(),/1 model call/i);
      await emit('exchange','unassigned',{role:'input',state:'open',link_status:'unavailable'});
      await emit('text','scope-collision-input',segment('Identified input','final',{exchange_id:'unassigned',message_id:'scope-collision-input'}));
      assert.equal(await unknownValue.evaluate((node,selector)=>node.closest('section')!==document.querySelector(selector).closest('section'),identity('text','scope-collision-input')),true,'unassigned measurements stay separate from an exchange named unassigned');
      assert.match(await unknownSection.locator('.metrics-summary').textContent(),/unassigned activity and measurements/i);
      await emit('exchange','aggregate-only',response());
      await emit('measurement','aggregate-first',measure('first_response',.72,{exchange_id:'aggregate-only',source:'reply-only-provider'}));
      await checkpoint(visible('[data-exchange="aggregate-only"] .metrics-summary',/^Response measurements$/i));
      const aggregateDetails=page.locator('[data-exchange="aggregate-only"] details').first();
      await aggregateDetails.locator('summary').click();
      assert.match(await aggregateDetails.textContent(),/reply.level|response.level/i);
      assert.match(await page.locator(identity('measurement','aggregate-first')).textContent(),/720ms/);
      assert.equal(await page.locator('[data-exchange="aggregate-only"] [data-kind="operation"]').count(),0);
      for(const [id,metric,value,source,label] of [
        ['node-llm','first_response',.71,'LiveKit llm_node_ttft',/LLM node[\s\S]*TTFT[\s\S]*710ms/i],
        ['node-tts','first_response',.21,'LiveKit tts_node_ttfb',/TTS node[\s\S]*TTFB[\s\S]*210ms/i],
        ['native-playback','playback_delay',.002,'LiveKit playback_latency',/playback delay[\s\S]*2ms/i],
      ]){
        await emit('measurement',id,measure(metric,value,{exchange_id:'aggregate-only',source}));
        await checkpoint(visible(identity('measurement',id),label));
        assert.ok((await page.locator(identity('measurement',id)).textContent()).includes(source));
      }
      assert.match(await page.locator('[data-exchange="aggregate-only"] .metrics-summary').textContent(),/^Response measurements$/i,'node aggregates do not manufacture requests');
      for(const endState of ['ended','interrupted']){
        const id='no-audio-'+endState,selector=`[data-exchange="${id}"]`;
        await emit('exchange',id,response());
        await emit('exchange',id,response(endState,endState==='interrupted'?'interrupted':'not_started'));
        assert.equal(await page.locator(selector+' details').isVisible(),false,'no empty disclosure when no metrics or operations arrived');
        assert.equal(await page.locator(selector).isVisible(),false,'empty ended responses reserve no space');
        await emit('operation',id+':late',operation('llm','late request',{exchange_id:id}));
        await checkpoint(visible(selector+' .metrics-summary',/^1 model call$/i));
        assert.doesNotMatch(await page.locator(selector+' .metrics-summary').textContent(),/pending/i,'late operation cannot revive ended reply latency');
      }
      // Updates received in Logs stay current without reconnecting media.
      const mediaBefore=await page.evaluate(()=>({connections:window.mediaConnections,closes:window.mediaCloses}));
      await page.locator('.vtab[data-view="logs"]').click();
      await page.locator('.kind[data-kind="metric"]').click();
      await emit('measurement','model-1:duration',measure('request_duration',1.72,{scope:'operation',operation_id:'model-1'}));
      await checkpoint(visible('#loglist',/call-1[\s\S]*1\.72s/));
      assert.doesNotMatch(await page.locator('#loglist').textContent(),/Yes, please\.|Yes, go for it\./,'text fragments are not repeated measurement-log rows');
      await page.locator('.vtab[data-view="conversation"]').click();
      assert.equal(await details.evaluate(node=>node===window.preservedDetails&&node.open),true,'view switching preserves disclosure nodes');
      assert.match(await page.locator(identity('measurement','model-1:duration')).textContent(),/1\.72s/);
      assert.match(await page.locator(identity('measurement','model-1:first')).textContent(),/880ms/);
      assert.deepEqual(await page.evaluate(()=>({connections:window.mediaConnections,closes:window.mediaCloses})),mediaBefore);
      assert.equal(await page.locator('body').getAttribute('data-state'),'connected');
      if(evidenceDir)await page.screenshot({path:path.join(evidenceDir,target+'-measurements-streaming.png')});
      await summary.click();
      await page.locator('#diagnostics > summary').click();
      await reply.scrollIntoViewIfNeeded();
      assert.equal(await details.getAttribute('open'),null);
      if(evidenceDir)await page.screenshot({path:path.join(evidenceDir,target+'-latency-at-a-glance.png')});

      await emit('exchange','response-1',{role:'response',state:'open',link_status:'unavailable',playback:'not_started'});
      const answer=text=>({exchange_id:'response-1',message_id:'answer',speaker:'assistant',text,state:'provisional',origin:'generated',separator_before:''});
      for (const text of ['You',"You're", "You're all set."]) {
        await emit('text','answer',answer(text));
        assert.equal(await page.locator('[data-kind="text"][data-id="answer"]').textContent(),text);
      }
      assert.match(await page.locator('#tlist').textContent(),/generated/i);
      await emit('text','answer',{...answer("You're all set."),state:'final'});
      await emit('text','late-first',segment('Linked', 'final',{exchange_id:undefined,message_id:'late-message'}));
      await emit('text','late-second',segment(' words', 'provisional',{exchange_id:undefined,message_id:'late-message'}));
      await emit('text','late-first',segment('Linked', 'final',{exchange_id:'proved-parent',message_id:'late-message'}));
      await emit('text','late-second',segment(' words corrected', 'final',{exchange_id:undefined,message_id:'late-message'}));
      assert.equal(await page.locator('[data-id="late-second"]').evaluate(node=>node.closest('[data-exchange]').dataset.exchange),'proved-parent');
      assert.equal(await page.locator('[data-message="late-message"]').count(),1);
      assert.equal(await page.locator('[data-exchange^="unassigned:"]').count(),0,'proven links must not leave empty fallback groups');
      // LiveKit can create speech before committing its caller's text. A task
      // return also reuses that speech: explicit input links own display order.
      for(const id of ['ordering-first','ordering-second']){
        await emit('exchange',id,response());
        await emit('text',id,{...answer(id),exchange_id:id,message_id:id,state:'final'});
      }
      const linkedReply={...response('ended'),input_id:'ordering-caller',link_status:'known'};
      await emit('exchange','ordering-first',linkedReply); // input not received yet
      await emit('exchange','ordering-caller',{role:'input',state:'ended',link_status:'unavailable'});
      await emit('text','ordering-caller',segment('Yes, please, go for it.','final',{exchange_id:'ordering-caller',message_id:'ordering-caller'}));
      await emit('exchange','ordering-second',linkedReply); // input already received
      await emit('exchange','ordering-next',{role:'input',state:'ended',link_status:'unavailable'});
      await emit('text','ordering-next',segment('Thank you.','final',{exchange_id:'ordering-next',message_id:'ordering-next'}));
      await emit('exchange','ordering-first',{...linkedReply,playback:'ended'});
      assert.deepEqual(await page.locator('#tlist [data-message^="ordering-"]').evaluateAll(nodes=>nodes.map(node=>node.dataset.message)),
        ['ordering-caller','ordering-first','ordering-second','ordering-next'],
        'linked replies follow their caller in stable order, including late input and reply updates');
      if(evidenceDir) await page.screenshot({path:path.join(evidenceDir,target+'-text-streaming.png')});
      await emit('text','unfinished',segment('Still here','provisional',{exchange_id:'input-2',message_id:'message-2',separator_before:' '}));
      await page.locator('#connect').click();
      assert.match(await page.locator('#tlist').textContent(),/Still here/);
      assert.match(await page.locator('#tlist').textContent(),/incomplete/i);
      await emit('call','call',{target,state:'open',input_boundaries:'known',model_calls:'known',generated_text:'available'});
      assert.notEqual(await page.locator('body').getAttribute('data-state'),'connected');
      await page.locator('#connect').click();
      await page.locator('body[data-state="connected"]').waitFor();
      await emit('text','old-call',segment('OLD CALL'));
      assert.doesNotMatch(await page.locator('#tlist').textContent(),/OLD CALL|Still here/);
      // US3: feed recovery changes display freshness, never the audio session.
      const recoveryCall='call-2';
      const inRecovery=(kind,id,data,revision)=>emit(kind,id,data,revision,recoveryCall);
      const control=event=>page.evaluate(event=>{window.lastArrival=performance.now();window.feed.emit(event);},event);
      const mediaSnapshot=()=>page.evaluate(()=>({connections:window.mediaConnections,closes:window.mediaCloses}));
      const closeNative=()=>page.evaluate(target=>{
        if(target==='pipecat'){window.latestPeer.connectionState='disconnected';window.latestPeer.dispatchEvent(new Event('connectionstatechange'));}
        else window.latestRoom.handlers.end();
      },target);
      await inRecovery('call','call',{target,state:'open',input_boundaries:'known',model_calls:'known',generated_text:'available'});
      for(const id of ['recovery-a','recovery-b'])await inRecovery('exchange',id,response());
      const earlyOperation=operation('llm','earlier request',{exchange_id:'recovery-a'});
      await inRecovery('operation','recovery-model',earlyOperation);
      await inRecovery('text','recovery-final',segment('Retained final','final',{exchange_id:'recovery-input',message_id:'recovery-message'}));
      await inRecovery('text','recovery-provisional',segment('unfinished','provisional',{exchange_id:'recovery-input',message_id:'recovery-message',separator_before:' '}));
      const recoveryMedia=await mediaSnapshot();
      await page.evaluate(()=>{window.lastArrival=performance.now();window.feed.readyState=EventSource.CONNECTING;window.feed.dispatchEvent(new Event('error'));});
      await checkpoint(visible('#feed-status',/reconnecting/i));
      assert.equal(await page.locator('body').getAttribute('data-state'),'connected');
      assert.match(await page.locator('#tlist').textContent(),/Retained final/);
      assert.match(await page.locator(identity('operation','recovery-model')).textContent(),/last seen|incomplete|stale/i);
      assert.deepEqual(await mediaSnapshot(),recoveryMedia);
      await page.evaluate(()=>{window.feed.readyState=EventSource.OPEN;window.feed.dispatchEvent(new Event('open'));});
      await control({t:'state',state:'ready',stream_id:streamID}); // ID-less, so the next data sequence remains contiguous.
      await inRecovery('operation','recovery-model',earlyOperation); // retained/new snapshot restores this entity.
      assert.doesNotMatch(await page.locator('#feed-status').textContent(),/incomplete|gap/i);
      const lateValue=measure('first_response',.321,{exchange_id:'recovery-a',scope:'operation',operation_id:'recovery-model'});
      await inRecovery('measurement','late-earlier-value',lateValue);
      const earlier=page.locator('[data-exchange="recovery-a"]');
      await earlier.locator('summary').click();
      assert.match(await earlier.textContent(),/321ms/);
      assert.equal(await page.locator('[data-exchange="recovery-b"] '+identity('measurement','late-earlier-value')).count(),0);
      await inRecovery('measurement','late-earlier-value',lateValue,1);
      await inRecovery('measurement','late-earlier-value',{...lateValue,value:.654});
      await inRecovery('measurement','late-earlier-value',lateValue,1);
      assert.equal(await page.locator(identity('measurement','late-earlier-value')).count(),1);
      assert.match(await page.locator(identity('measurement','late-earlier-value')).textContent(),/654ms/);
      await inRecovery('operation','late-tool',operation('tool','shared-tool',{exchange_id:'recovery-a'}));
      await inRecovery('operation','other-tool',operation('tool','shared-tool',{exchange_id:'recovery-b'}));
      await inRecovery('operation','late-tool',operation('tool','shared-tool',{exchange_id:'recovery-a',state:'returned'}));
      await inRecovery('operation','late-tool',operation('tool','shared-tool',{exchange_id:'recovery-a'}),1);
      assert.match(await earlier.locator(identity('operation','late-tool')).textContent(),/returned/);
      assert.equal(await page.locator(identity('operation','late-tool')).count(),1);
      // A measurement can precede its parent and acquire a proven association later.
      await inRecovery('measurement','orphan-value',measure('first_response',.7,{scope:'operation',exchange_id:undefined,operation_id:'orphan-operation'}));
      await inRecovery('operation','orphan-operation',operation('llm','late parent',{exchange_id:undefined}));
      const unassigned=page.locator('#diagnostic-list [data-exchange="unassigned"]');
      await page.locator('#diagnostics > summary').click();
      await unassigned.locator('summary').click();
      await unassigned.locator('summary').focus();
      const orphan=page.locator(identity('measurement','orphan-value'));
      const selectedValue=await orphan.evaluate(node=>{
        window.preservedOrphan=node;
        const range=document.createRange();range.selectNodeContents(node.firstChild);
        const selection=getSelection();selection.removeAllRanges();selection.addRange(range);return selection.toString();
      });
      await inRecovery('operation','orphan-operation',operation('llm','late parent',{exchange_id:'recovery-b'}));
      assert.equal(await orphan.evaluate(node=>node===window.preservedOrphan&&node.checkVisibility()),true,'late association preserves a visible measurement node');
      assert.equal(await page.evaluate(()=>getSelection().toString()),selectedValue,'late association preserves the selected measurement');
      assert.equal(await page.evaluate(()=>document.activeElement?.tagName==='SUMMARY'&&document.activeElement.checkVisibility()),true,'late association preserves visible disclosure focus');
      assert.equal(await orphan.evaluate(node=>node.closest('[data-exchange]').dataset.exchange),'recovery-b');
      assert.equal(await unassigned.count(),0,'resolved association leaves no empty group');
      await inRecovery('measurement','still-unassigned',measure('request_duration',.2,{scope:'unassigned',exchange_id:undefined}));
      assert.equal(await page.locator(identity('measurement','still-unassigned')).evaluate(node=>node.closest('[data-exchange]').dataset.exchange),'unassigned');
      // Lost history stays explicit after every retained entity reports fresh state.
      await control({t:'gap',reason:'history-evicted',stream_id:streamID});
      await checkpoint(visible('#feed-status',/incomplete|gap/i),visible('[data-exchange="recovery-a"] .metrics-summary',/1 model call observed/i));
      assert.match(await page.locator(identity('operation','other-tool')).textContent(),/last seen|incomplete|stale/i,'a missing outcome cannot leave old activity looking current');
      await inRecovery('call','call',{target,state:'open',input_boundaries:'known',model_calls:'known',generated_text:'available'});
      await inRecovery('operation','recovery-model',{...earlyOperation,state:'ended'});
      await inRecovery('measurement','late-earlier-value',{...lateValue,value:.654});
      await page.evaluate(()=>window.feed.dispatchEvent(new Event('open')));
      assert.match(await page.locator('#feed-status').textContent(),/incomplete|gap/i);
      assert.match(await earlier.locator('.metrics-summary').textContent(),/1 model call observed/i);
      assert.match(await page.locator(identity('operation','other-tool')).textContent(),/last seen|incomplete|stale/i,'reopening the feed does not supply the lost operation outcome');
      await inRecovery('operation','other-tool',operation('tool','shared-tool',{exchange_id:'recovery-b',state:'returned'}));
      assert.match(await page.locator(identity('operation','other-tool')).textContent(),/returned/i,'a later authoritative outcome replaces stale activity');
      assert.deepEqual(await mediaSnapshot(),recoveryMedia);
      await page.evaluate(()=>{window.previousPeer=window.latestPeer;window.previousRoom=window.latestRoom;});
      await page.locator('#connect').click();
      assert.equal(await page.locator(identity('text','recovery-final')).getAttribute('data-state'),'final');
      assert.equal(await page.locator(identity('text','recovery-provisional')).getAttribute('data-state'),'incomplete');
      await inRecovery('call','call',{target,state:'open',input_boundaries:'known',model_calls:'known',generated_text:'available'});
      await inRecovery('operation','recovery-model',earlyOperation);
      assert.notEqual(await page.locator('body').getAttribute('data-state'),'connected');
      assert.match(await page.locator(identity('operation','recovery-model')).textContent(),/ended|incomplete|last seen/i);
      // Cancel a held bootstrap, connect again, then release the stale response.
      const held=new Promise(resolve=>{bootstrapHeld=resolve;});
      holdNextBootstrap=true;
      await page.locator('#connect').click();
      await held;
      await page.locator('#connect').click();
      await page.locator('#connect').click();
      await page.locator('body[data-state="connected"]').waitFor();
      const currentCall=`call-${call}`,currentMedia=await mediaSnapshot();
      const staleResponse=page.waitForResponse(response=>response.url().endsWith('/api/session'));
      await heldBootstrap();
      await (await staleResponse).finished();
      await page.evaluate(()=>new Promise(requestAnimationFrame));
      await page.evaluate(target=>{
        if(target==='pipecat'){window.previousPeer.connectionState='failed';window.previousPeer.dispatchEvent(new Event('connectionstatechange'));}
        else window.previousRoom.handlers.end();
      },target);
      await inRecovery('measurement','old-call-value',lateValue);
      assert.equal(await page.locator(identity('measurement','old-call-value')).count(),0);
      assert.equal(await page.locator(identity('text','recovery-final')).count(),0);
      assert.doesNotMatch(await page.locator('#feed-status').textContent(),/incomplete|gap/i,'a new call clears historical gaps');
      assert.deepEqual(await mediaSnapshot(),currentMedia,'stale setup and media callbacks cannot replace the new call');
      assert.equal(await page.locator('body').getAttribute('data-state'),'connected');
      // A terminal dev snapshot must not suppress the later native close callback.
      await emit('call','call',{target,state:'ended',input_boundaries:'known',model_calls:'known',generated_text:'available'},undefined,currentCall);
      assert.equal(await page.locator('body').getAttribute('data-state'),'connected','feed completion alone does not close active media');
      await closeNative();
      assert.equal(await page.locator('body').getAttribute('data-state'),'idle','a completed agent hangup is not a connection failure');
      assert.equal(await page.locator('#mute').getAttribute('data-mic'),'unavailable');
      assert.equal((await mediaSnapshot()).closes,currentMedia.closes+1);
      // The terminal feed snapshot can arrive just after the native disconnect.
      await page.locator('#connect').click();
      await page.locator('body[data-state="connected"]').waitFor();
      await closeNative();
      const afterRemoteClose=await mediaSnapshot();
      if(target==='pipecat') assert.equal(await page.locator('body').getAttribute('data-state'),'error','unexplained connection loss remains an error');
      await emit('call','call',{target,state:'ended',input_boundaries:'known',model_calls:'known',generated_text:'available'},undefined,`call-${call}`);
      assert.equal(await page.locator('body').getAttribute('data-state'),'idle','late successful completion clears the disconnect error');
      assert.deepEqual(await mediaSnapshot(),afterRemoteClose,'late completion cannot close media twice');
      await page.locator('#connect').click();
      await page.locator('body[data-state="connected"]').waitFor();
      await closeNative();
      await emit('call','call',{target,state:'error',input_boundaries:'known',model_calls:'known',generated_text:'available'},undefined,`call-${call}`);
      if(target==='pipecat') assert.equal(await page.locator('body').getAttribute('data-state'),'error','failed calls must not become successful hangups');
      await page.locator('#connect').click();
      await page.locator('body[data-state="connected"]').waitFor();
      await control({t:'state',state:'failed',stream_id:streamID});
      assert.equal(await page.locator('#connect').isEnabled(),true,'run failure cannot disable ending an active media call');
      const beforeFailedEnd=await mediaSnapshot();
      await page.locator('#connect').click();
      assert.equal((await mediaSnapshot()).closes,beforeFailedEnd.closes+1);
      assert.equal(await page.locator('#mute').getAttribute('data-mic'),'unavailable');
      await control({t:'state',state:'ready',stream_id:streamID});
      await page.locator('#connect').click();
      await page.locator('body[data-state="connected"]').waitFor();
      if(target==='pipecat'){
        await control({t:'state',state:'failed',stream_id:streamID});
        const finished={target,state:'ended',input_boundaries:'known',model_calls:'known',generated_text:'available'};
        await emit('call','call',finished,undefined,`call-${call}`);
        await closeNative();
        assert.equal(await page.locator('body').getAttribute('data-state'),'error','call completion does not hide runtime failure');
        await emit('call','call',finished,undefined,`call-${call}`);
        assert.equal(await page.locator('body').getAttribute('data-state'),'error','late completion does not hide runtime failure');
        await control({t:'state',state:'ready',stream_id:streamID});
        await page.locator('#connect').click();
        await page.locator('body[data-state="connected"]').waitFor();
      }
      seq++; // An unexplained data discontinuity is a gap even without a control.
      await emit('measurement','after-sequence-gap',measure('first_speech',.1,{scope:'call',exchange_id:undefined}),undefined,`call-${call}`);
      await checkpoint(visible('#feed-status',/incomplete|gap/i));
      await control({t:'gap',reason:'stream-restarted',stream_id:'replacement'});
      streamID='replacement';seq=0;
      await control({t:'state',state:'ready',stream_id:streamID});
      await emit('measurement','after-restart',measure('first_speech',.2,{scope:'call',exchange_id:undefined}),undefined,`call-${call}`);
      assert.match(await page.locator('#feed-status').textContent(),/incomplete|gap/i);
      await page.locator('#connect').click();
      await page.locator('#connect').click();
      await page.locator('body[data-state="connected"]').waitFor();
      assert.doesNotMatch(await page.locator('#feed-status').textContent(),/incomplete|gap/i);
      // US4: keep reading position and native controls intact as the page grows.
      assert.ok([null,'off'].includes(await page.locator('#tlist').getAttribute('aria-live')),'the transcript is not a live region for every streamed fragment');
      assert.equal(await page.locator('#tlist').evaluate(node=>{
        for(let element=node;element;element=element.parentElement){
          const live=element.getAttribute('aria-live');
          if((live&&live!=='off')||['log','status','alert'].includes(element.getAttribute('role')))return false;
        }
        return true;
      }),true,'no live ancestor repeatedly announces the transcript');
      assert.equal(await page.locator('#feed-status').getAttribute('role'),'status');
      assert.ok((await page.locator('#feed-status').textContent()).length<160,'feed announcements stay short');
      const frame=()=>page.evaluate(()=>new Promise(requestAnimationFrame));
      const list=page.locator('#tlist'),latestButton=page.locator('#latest');
      const bottomGap=()=>list.evaluate(node=>node.scrollHeight-node.scrollTop-node.clientHeight);
      const geometry=()=>page.evaluate(()=>{
        const stage=document.querySelector('.stage').getBoundingClientRect();
        const transcript=document.querySelector('.transcript').getBoundingClientRect();
        const controls=document.querySelector('.controls').getBoundingClientRect();
        const list=document.querySelector('#tlist');
        return {pageWidth:document.documentElement.scrollWidth,width:innerWidth,height:innerHeight,
          contentWidth:list.scrollWidth,panelWidth:list.clientWidth,stageHeight:stage.height,panelHeight:transcript.height,
          controlTop:controls.top,controlBottom:controls.bottom};
      });
      function assertLayout(layout){
        assert.ok(layout.pageWidth<=layout.width+1,JSON.stringify(layout));
        assert.ok(layout.contentWidth<=layout.panelWidth+1,JSON.stringify(layout));
        assert.ok(layout.controlTop>=0&&layout.controlBottom<=layout.height+1,'call controls remain reachable: '+JSON.stringify(layout));
        assert.ok(layout.stageHeight<=110&&layout.panelHeight>layout.stageHeight*2,'history keeps the stage compact: '+JSON.stringify(layout));
      }
      for(const width of [1280,360]){
        await page.setViewportSize({width,height:800});
        const displayCall=`call-${call}`;
        const show=(kind,id,data,revision)=>emit(kind,id,data,revision,displayCall);
        await show('call','call',{target,state:'open',input_boundaries:'known',model_calls:'known',generated_text:'available'});
        for(let i=0;i<8;i++){
          await show('exchange',`history-${i}`,response('ended','ended'));
          await show('text',`history-text-${i}`,{exchange_id:`history-${i}`,message_id:`history-message-${i}`,speaker:'assistant',
            text:`Earlier exchange ${i}.\nA readable answer kept for review.`,state:'final',origin:'generated',separator_before:''});
        }
        await show('measurement','history-time',measure('reply_latency',.6,{exchange_id:'history-3'}));
        await show('exchange','long-response',response());
        const longText=text=>({exchange_id:'long-response',message_id:'long-message',speaker:'assistant',text,state:'provisional',origin:'generated',separator_before:''});
        let growing='';
        for(let chunk=0;chunk<12;chunk++){
          growing+=Array.from({length:10},(_,line)=>`Streaming line ${chunk*10+line}: the next words arrive now.\n`).join('');
          await show('text','long-text',longText(growing));
          assert.ok(await bottomGap()<=2,'growing text stays at the latest content');
        }
        assert.ok(await page.locator(identity('text','long-text')).evaluate(node=>node.getBoundingClientRect().height>document.querySelector('#tlist').clientHeight*2),'the streamed reply exceeds two panel heights');
        await show('operation','footer-orphan',operation('tool','late parent',{exchange_id:undefined}));
        await latestButton.click();
        await page.locator('#diagnostics > summary').click();
        await frame();
        assert.ok(await bottomGap()<=2,'opening diagnostics preserves following at the latest content');
        growing+='Streaming continues with diagnostics open.\n';
        await show('text','long-text',longText(growing));
        assert.ok(await bottomGap()<=2,'the next update still follows after opening diagnostics');
        await page.locator('#diagnostics > summary').focus();
        await show('operation','footer-orphan',operation('tool','late parent',{exchange_id:'long-response'}));
        await frame();
        assert.equal(await page.locator('#diagnostics').isVisible(),false,'the last reassociated row removes its empty footer');
        assert.equal(await page.evaluate(()=>document.activeElement.matches('.transcript summary,.transcript button')&&document.activeElement.checkVisibility()),true,'hiding the focused footer moves focus to a visible transcript control');
        await show('operation','layout-model',operation('llm','model-with-a-long-name-'.repeat(7),{exchange_id:'long-response',provider:'provider-with-a-long-name-'.repeat(7)}));
        await show('operation','layout-tool',operation('tool','repeated_tool_name_'.repeat(10),{exchange_id:'long-response'}));
        await frame();
        assert.ok(await bottomGap()<=2,'new activity stays in view while following');
        const longDetails=page.locator('[data-exchange="long-response"] details');
        await longDetails.locator('summary').click();
        await show('measurement','layout-time',measure('first_response',.333,{exchange_id:'long-response',scope:'operation',operation_id:'layout-model'}));
        await frame();
        assert.ok(await bottomGap()<=2,'open detail updates stay in view while following');
        assertLayout(await geometry());
        // Read an earlier exchange; focus and selection belong to the reader.
        const anchor=page.locator(identity('text','history-text-3'));
        const anchorDetails=page.locator('[data-exchange="history-3"] details');
        const anchorSummary=anchorDetails.locator('summary');
        await anchorSummary.click();
        await anchorSummary.focus();
        await anchor.scrollIntoViewIfNeeded();
        await frame();
        const selected=await anchor.evaluate(node=>{
          window.readingAnchor=node;window.readingSummary=document.activeElement;
          const range=document.createRange();range.selectNodeContents(node);
          const selection=getSelection();selection.removeAllRanges();selection.addRange(range);return selection.toString();
        });
        const anchorTop=await anchor.evaluate(node=>node.getBoundingClientRect().top);
        assert.equal(await latestButton.isVisible(),true,'Latest is offered while reviewing older content');
        const announcementBefore=await page.locator('#feed-status').textContent();
        const mediaBeforeReview=await mediaSnapshot();
        for(let update=0;update<20;update++){
          growing+=`Later live words ${update}.\n`;
          await show('text','long-text',longText(growing));
          assert.ok(Math.abs((await anchor.evaluate(node=>node.getBoundingClientRect().top))-anchorTop)<=2,'updates keep the reading anchor in place');
        }
        await show('operation','inserted-above',operation('tool','late activity above the reader '.repeat(14),{exchange_id:'history-1'}));
        await frame();
        const anchorAfter=await anchor.evaluate(node=>node.getBoundingClientRect().top);
        assert.ok(Math.abs(anchorAfter-anchorTop)<=2,`late content above the reader preserves the anchor (${target}, ${width}px): ${anchorTop} -> ${anchorAfter}`);
        assert.equal(await page.evaluate(()=>getSelection().toString()),selected,'unrelated updates preserve selected text');
        assert.equal(await anchorSummary.evaluate(node=>node===window.readingSummary&&document.activeElement===node),true,'unrelated updates preserve disclosure focus');
        assert.equal(await anchorDetails.evaluate(node=>node.open),true);
        assert.equal(await page.locator('#feed-status').textContent(),announcementBefore,'fragments do not repeat status announcements');
        assert.deepEqual(await mediaSnapshot(),mediaBeforeReview);
        await latestButton.click();
        await frame();
        assert.ok(await bottomGap()<=2,'one Latest click restores following');
        growing+='Following resumes after one click.\n';
        await show('text','long-text',longText(growing));
        assert.ok(await bottomGap()<=2);
        // Native disclosure/Latest keys leave a muted mic muted; ordinary PTT works.
        await page.locator('#mute').click();
        assert.equal(await page.locator('#mute').getAttribute('data-mic'),'muted');
        await anchorSummary.focus();
        await page.keyboard.press('Enter');
        assert.equal(await anchorDetails.evaluate(node=>node.open),false);
        assert.equal(await anchorSummary.evaluate(node=>document.activeElement===node),true);
        await page.keyboard.press('Space');
        assert.equal(await anchorDetails.evaluate(node=>node.open),true);
        assert.equal(await page.locator('#mute').getAttribute('data-mic'),'muted');
        for(const key of ['Enter','Space']){
          await anchor.scrollIntoViewIfNeeded();
          await frame();
          assert.equal(await latestButton.isVisible(),true);
          await latestButton.focus();
          await page.keyboard.press(key);
          await frame();
          assert.ok(await bottomGap()<=2,`${key} activates Latest`);
          assert.equal(await page.locator('#mute').getAttribute('data-mic'),'muted',`${key} on Latest cannot trigger the mic shortcut`);
        }
        await page.locator('.stage').click();
        await page.keyboard.down('Space');
        assert.equal(await page.locator('#mute').getAttribute('data-mic'),'live','ordinary hold-to-talk still unmutes while held');
        await page.keyboard.up('Space');
        assert.equal(await page.locator('#mute').getAttribute('data-mic'),'muted','hold-to-talk restores the previous mute state');
        await page.locator('#mute').click();
        // View changes keep the same audio session and the latest received values.
        await page.locator('.vtab[data-view="logs"]').click();
        await show('measurement','layout-time',measure('first_response',.444,{exchange_id:'long-response',scope:'operation',operation_id:'layout-model'}));
        await page.locator('.vtab[data-view="conversation"]').click();
        assert.match(await page.locator(identity('measurement','layout-time')).textContent(),/444ms/);
        assert.equal(await longDetails.evaluate(node=>node.open),true);
        assert.deepEqual(await mediaSnapshot(),mediaBeforeReview);
        assertLayout(await geometry());
        if(evidenceDir)await page.screenshot({path:path.join(evidenceDir,`${target}-streaming-${width}.png`)});
        await page.locator('#connect').click();
        await frame();
        assert.equal(await page.locator('body').getAttribute('data-has-history'),'true');
        assert.match(await page.locator('#tlist').textContent(),/Following resumes after one click/);
        assertLayout(await geometry());
        if(evidenceDir)await page.screenshot({path:path.join(evidenceDir,`${target}-ended-${width}.png`)});
        await page.locator('#connect').click();
        await page.locator('body[data-state="connected"]').waitFor();
      }
      await page.setViewportSize({width:1280,height:800});
      // Replay both curated contract files and actual SDK stdout unchanged.
      // Fresh bootstrap selects each call before any record is delivered.
      const sources=[{file:path.resolve(__dirname,`../../devmetrics/testdata/streaming-${target}.jsonl`),native:false}];
      if(evidenceDir) for(const file of fs.readdirSync(evidenceDir).filter(file=>file.endsWith('.jsonl'))) sources.push({file:path.join(evidenceDir,file),native:true});
      let nativeReplayCalls=0,fixtureReplayCalls=0;
      for(const source of sources){
        const rows=fs.readFileSync(source.file,'utf8').split('\n').filter(line=>line.startsWith('UNMUTE_METRIC ')).map(line=>JSON.parse(line.slice(14)));
        const calls=[...new Set(rows.filter(r=>r.kind==='call'&&r.call.target===target).map(r=>r.call_id))];
        for(const callID of calls){
          await page.locator('#connect').click();
          forcedCallID=callID;
          await page.locator('#connect').click();
          await page.locator('body[data-state="connected"]').waitFor();
          const latest=new Map();
          for(const record of rows.filter(r=>r.call_id===callID)){
            const key=record.kind+':'+record.id,prior=latest.get(key);
            if(!prior||record.revision>prior.revision) latest.set(key,record);
            await page.evaluate(async event=>{window.feed.emit(event);await new Promise(requestAnimationFrame);},
              {t:'metric',stream_id:streamID,seq:++seq,record});
          }
          for(const record of latest.values()) if(record.kind==='text'){
            const observed=await page.evaluate(id=>{
              const node=Array.from(document.querySelectorAll('[data-kind="text"]')).find(node=>node.dataset.id===id);
              return node&&{text:node.textContent,state:node.dataset.state};
            },record.id);
            assert.ok(observed,`missing replayed ${source.file} ${record.id}`);
            assert.equal(observed.text,record.text.separator_before+record.text.text);
            if(record.text.state==='final')assert.equal(observed.state,'final');
          }
          for(const record of latest.values()) if(record.kind==='measurement'){
            const row=page.locator(identity('measurement',record.id));
            assert.equal(await row.count(),record.measurement.state==='measured'?1:0,`only captured values render: ${source.file} ${record.id}`);
          }
          for (const record of latest.values()) {
            const m=record.measurement;
            if (!m || m.state!=='measured' || !m.operation_id) continue;
            const op=latest.get('operation:'+m.operation_id)?.operation;
            if (!op || !(['llm','tts'].includes(op.type) && m.metric==='first_response' || op.type==='llm' && m.metric==='request_duration')) continue;
            const metric=page.locator(`[data-kind="measurement"][data-id="${record.id}"]`);
            if (op.exchange_id) assert.equal(await metric.isVisible(),true,'native per-request latency stays visible');
            else assert.equal(await metric.evaluate(node=>document.querySelector('#diagnostic-list').contains(node)),true,'unassigned operations stay accessible in diagnostics');
            if (m.metric==='first_response' && /ttft|ttfb/.test(m.source)) {
              assert.match(await metric.textContent(),m.source.includes('ttft')?/TTFT/:/TTFB/,'use the native first-response label');
            }
          }
          assert.doesNotMatch(await page.locator('#tlist .exchange-status,#tlist summary,#tlist .source-note').allTextContents().then(lines=>lines.join('\n')),/unavailable|unknown|limited by the source/i);
          if(source.native)nativeReplayCalls++;else fixtureReplayCalls++;
        }
      }
      const delays=await page.evaluate(()=>window.arrivalDelays.sort((a,b)=>a-b));
      assert.ok(delays[Math.ceil(delays.length*.95)-1]<=100,JSON.stringify(delays));
      assert.ok(Math.max(...delays)<=250,JSON.stringify(delays));
      assert.deepEqual(errors,[]);
      const metricDelays=await page.evaluate(()=>window.metricArrivalDelays.sort((a,b)=>a-b));
      assert.ok(metricDelays.length>0,'activity and measurement checkpoints ran');
      assert.ok(metricDelays[Math.ceil(metricDelays.length*.95)-1]<=100,JSON.stringify(metricDelays));
      assert.ok(Math.max(...metricDelays)<=250,JSON.stringify(metricDelays));
      const result={target,metricUpdates:metricDelays.length,metricP95:metricDelays[Math.ceil(metricDelays.length*.95)-1],metricMax:Math.max(...metricDelays),updates:delays.length,p95:delays[Math.ceil(delays.length*.95)-1],max:Math.max(...delays),nativeReplayCalls,fixtureReplayCalls};
      console.log(JSON.stringify(result));
      if(evidenceDir)fs.writeFileSync(path.join(evidenceDir,target+'-browser.json'),JSON.stringify(result,null,2)+'\n');
      await page.close();
    }
  } finally {await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
