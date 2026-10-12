const assert = require('node:assert/strict');
const {chromium} = require(process.env.UNMUTE_SWEEP_PLAYWRIGHT || 'playwright');

// Typed lines and the text-only call, on both transports. Only the network and
// media boundaries are replaced; the page and the browser DOM stay real.
(async () => {
  const browser = await chromium.launch({headless:true,executablePath:chromium.executablePath()});
  try {
    for (const target of ['pipecat', 'livekit']) {
      const page = await browser.newPage({viewport:{width:1280,height:800}});
      const errors = [];
      page.on('pageerror', error => errors.push(error.message));
      let call = 0;
      await page.route('**/api/session', route => route.fulfill({json:{
        ready:true,dev_events_version:2,call_id:`call-${++call}`,
        kind:target==='livekit'?'livekit':'webrtc-offer',offerUrl:'/api/offer',url:'ws://unused',token:'unused',state_fields:[]}}));
      await page.route('**/api/offer', route => route.fulfill({json:{sdp:'answer',type:'answer',pc_id:'peer'}}));
      await page.addInitScript(() => {
        window.sent = [];
        window.micRequests = 0;
        window.recvOnly = 0;
        window.micEnabled = 0;
        window.EventSource = class extends EventTarget {
          constructor(){ super(); window.feed=this; this.readyState=1; setTimeout(()=>this.dispatchEvent(new MessageEvent('message',{data:JSON.stringify({t:'state',state:'ready',stream_id:'test'})})),0); }
          close(){}
        };
        const track={enabled:true,kind:'audio',stop(){},getSettings(){return {};}};
        const stream={getTracks:()=>[track],getAudioTracks:()=>[track]};
        Object.defineProperty(navigator,'mediaDevices',{value:{getUserMedia:async()=>{window.micRequests++;return stream;},
          enumerateDevices:async()=>[],addEventListener(){}}});
        window.RTCPeerConnection=class extends EventTarget {
          constructor(){super();this.iceGatheringState='complete';this.connectionState='connected';}
          addTrack(){}
          addTransceiver(kind,init){if(kind==='audio'&&init.direction==='recvonly')window.recvOnly++;}
          createDataChannel(){const dc=new EventTarget();dc.readyState='open';dc.send=m=>window.sent.push(JSON.parse(m));dc.close=()=>{};return dc;}
          async createOffer(){return {sdp:'offer',type:'offer'};}
          async setLocalDescription(offer){this.localDescription=offer;}
          async setRemoteDescription(){}
          close(){}
        };
        window.LivekitClient={Room:class {
          constructor(){this.handlers={};this.localParticipant={
            setMicrophoneEnabled:async on=>{if(on)window.micEnabled++;},getTrackPublication:()=>null,
            sendText:async(text,options)=>{window.sent.push({text,options});}};}
          on(event,handler){this.handlers[event]=handler;return this;}
          async connect(){}
          disconnect(){}
        },RoomEvent:{TrackSubscribed:'track',TrackUnsubscribed:'untrack',LocalTrackPublished:'local',Disconnected:'end',ParticipantConnected:'participant'},Track:{Kind:{Audio:'audio'},Source:{Microphone:'mic'}}};
      });
      await page.goto(process.argv[2]);
      await page.locator('#connect:not([disabled])').waitFor();

      // Before a call: the text box is not on the page, the quiet control is.
      assert.equal(await page.locator('#chatform').isVisible(), false);
      assert.equal(await page.locator('#textonly').isVisible(), true);

      // Text only: no microphone is asked for, and the call still connects.
      await page.locator('#textonly').click();
      await page.locator('body[data-state="connected"]').waitFor();
      assert.equal(await page.evaluate(()=>window.micRequests), 0, 'text only never asks for the microphone');
      assert.equal(await page.evaluate(()=>window.micEnabled), 0, 'text only never publishes a microphone');
      if(target==='pipecat') assert.equal(await page.evaluate(()=>window.recvOnly), 1, 'the offer still receives the bot audio');
      assert.equal(await page.locator('#mute').getAttribute('data-mic'), 'unavailable');
      assert.equal(await page.locator('#textonly').isVisible(), false, 'text only is hidden during a call');
      assert.equal(await page.locator('#chatform').isVisible(), true);
      assert.equal(await page.locator('#chatsend').isDisabled(), true, 'send waits for a line');

      // Type a line and press Enter.
      await page.locator('#chatinput').fill('I want a haircut');
      assert.equal(await page.locator('#chatsend').isDisabled(), false);
      await page.locator('#chatinput').press('Enter');
      const sent = await page.evaluate(()=>window.sent.filter(m=>m.label==='rtvi-ai'&&m.type==='send-text'||m.options));
      assert.equal(sent.length, 1);
      if(target==='pipecat') assert.deepEqual(sent[0], {label:'rtvi-ai',type:'send-text',id:sent[0].id,
        data:{content:'I want a haircut',options:{run_immediately:true,audio_response:true}}});
      else assert.deepEqual(sent[0], {text:'I want a haircut',options:{topic:'lk.chat'}});
      assert.equal(await page.locator('#chatinput').inputValue(), '');
      const typed = page.locator('#tlist .turn.you');
      assert.equal(await typed.count(), 1);
      assert.match(await typed.textContent(), /I want a haircut/);

      // The Send button works too, and a blank line sends nothing.
      await page.locator('#chatinput').fill('   ');
      assert.equal(await page.locator('#chatsend').isDisabled(), true);
      await page.locator('#chatinput').fill('second line');
      await page.locator('#chatsend').click();
      assert.equal(await page.evaluate(()=>window.sent.filter(m=>m.label==='rtvi-ai'&&m.type==='send-text'||m.options).length), 2);
      assert.deepEqual(await page.locator('#tlist .turn.you').allTextContents().then(t=>t.map(x=>x.replace(/^you/,''))), ['I want a haircut','second line']);

      // M and Space still belong to the page only outside the text box.
      await page.locator('#chatinput').fill('m m');
      assert.equal(await page.locator('#chatinput').inputValue(), 'm m');

      // End the call: the box goes, the controls come back.
      await page.locator('#connect').click();
      await page.locator('body[data-state="idle"]').waitFor();
      assert.equal(await page.locator('#chatform').isVisible(), false);
      assert.equal(await page.locator('#textonly').isVisible(), true);

      // The ordinary Connect keeps asking for the microphone.
      await page.locator('#connect').click();
      await page.locator('body[data-state="connected"]').waitFor();
      if(target==='pipecat') assert.equal(await page.evaluate(()=>window.micRequests) > 0, true, 'connect still asks for the microphone');
      else assert.equal(await page.evaluate(()=>window.micEnabled), 1);
      assert.equal(await page.locator('#chatform').isVisible(), true, 'typing works on a call with a microphone too');
      assert.deepEqual(errors, []);
      console.log(JSON.stringify({target,textChat:'ok'}));
      await page.close();
    }
  } finally {await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
