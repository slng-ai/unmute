// Offline browser lifecycle checks; no browser SDK, microphone, or network access.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const source = fs.readFileSync(path.join(__dirname, '../internal/generate/templates/agora_v1/client.js.tmpl'), 'utf8');

function fixture(pending) {
  let release;
  const gate = new Promise(resolve => { release = resolve; });
  const calls = { join: 0, publish: 0, start: 0, stop: 0, leave: 0, close: 0 };
  const elements = Object.fromEntries(['#start', '#stop', '#status'].map(name => [name, {
    disabled: name === '#stop', textContent: '',
    addEventListener(event, handler) { this[event] = handler; },
  }]));
  const events = {};
  const timers = new Set();
  const pause = async phase => { if (pending === phase) await gate; };
  const track = { stop() {}, close() { calls.close++; } };
  const client = {
    on() {}, removeAllListeners() {}, subscribe: async () => {},
    async join() { calls.join++; await pause('join'); },
    async publish() { calls.publish++; await pause('publish'); },
    async leave() { calls.leave++; },
  };
  const AgoraRTC = {
    createClient: () => client,
    async createMicrophoneAudioTrack() { await pause('microphone'); return track; },
  };
  vm.runInNewContext(source, {
    document: { querySelector: name => elements[name] },
    window: { AgoraRTC, addEventListener: (event, handler) => { events[event] = handler; } },
    AgoraRTC,
    setTimeout(callback) { timers.add(callback); return callback; },
    clearTimeout(timer) { timers.delete(timer); },
    async fetch(url) {
      if (url === '/sessions') {
        await pause('prepare');
        return { ok: true, json: async () => ({ id: 'session', key: 'capability', expires_in: 120 }) };
      }
      if (url.endsWith('/start')) { calls.start++; await pause('start'); }
      if (url.endsWith('/stop')) calls.stop++;
      return { ok: true, json: async () => ({ status: 'ok' }) };
    },
  });
  return { release, calls, elements, events, timers };
}

for (const phase of ['prepare', 'microphone', 'join', 'publish', 'start']) {
  test(`page exit cancels pending ${phase} and restores usable UI`, async () => {
    const f = fixture(phase);
    const startup = f.elements['#start'].click();
    await new Promise(setImmediate);
    f.events.pagehide();
    f.events.pageshow({ persisted: true });
    const restoredStatus = f.elements['#status'].textContent;
    f.release();
    await startup;
    assert.equal(f.elements['#start'].disabled, false);
    assert.equal(f.elements['#stop'].disabled, true);
    assert.equal(f.elements['#status'].textContent, restoredStatus);
    assert.equal(f.timers.size, 0);
    assert.equal(f.calls.stop, 1);
    if (phase === 'prepare' || phase === 'microphone') assert.equal(f.calls.join, 0);
    if (phase === 'join') assert.equal(f.calls.publish, 0);
    if (phase !== 'start') assert.equal(f.calls.start, 0);
    if (phase !== 'prepare') assert.equal(f.calls.close, 1);
  });
}

test('active conversation releases resources and permits restart after BFCache restore', async () => {
  const f = fixture();
  await f.elements['#start'].click();
  assert.equal(f.timers.size, 1);
  f.events.pagehide();
  f.events.pageshow({ persisted: true });
  await new Promise(setImmediate);
  assert.equal(f.timers.size, 0);
  assert.equal(f.calls.stop, 1);
  assert.equal(f.calls.close, 1);
  await f.elements['#start'].click();
  assert.equal(f.calls.start, 2);
  assert.equal(f.elements['#stop'].disabled, false);
});
