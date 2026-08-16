// greeting-probe: drives one `unmute dev` browser session unattended and
// reports whether the agent's voice actually arrived.
//
// Output is one JSON line for the Go sweep harness to parse.
//   argv:   <page-url> <timeout-seconds>
//   stdout: one JSON line
//   exit:   0 on inbound audio, 1 otherwise
//
// The page is loaded exactly as shipped. The only instrumentation is a wrapper
// around the browser's own RTCPeerConnection constructor, installed before any
// page script runs: the page holds its connection in a module-scoped variable,
// and a page edited to expose it would no longer be the page under test. One
// probe covers both transports because pipecat and livekit both end in inbound
// WebRTC audio — the same reason the shipped page has one adapter.

const PLAYWRIGHT = process.env.UNMUTE_SWEEP_PLAYWRIGHT || "playwright";
const { chromium } = require(PLAYWRIGHT);

// Fixed flags: no permission prompt, no real capture device, no gesture
// requirement. Together they are what lets thirteen sessions run with nobody
// at the keyboard.
const FLAGS = [
  "--use-fake-device-for-media-stream",
  "--use-fake-ui-for-media-stream",
  "--autoplay-policy=no-user-gesture-required",
];

// Runs in the page before any of its own scripts. Keeps every peer connection
// the page creates, whichever transport made it.
const KEEP_PEER_CONNECTIONS = () => {
  window.__unmutePCs = [];
  const Native = window.RTCPeerConnection;
  if (!Native) return;
  window.RTCPeerConnection = function (...args) {
    const pc = new Native(...args);
    window.__unmutePCs.push(pc);
    return pc;
  };
  window.RTCPeerConnection.prototype = Native.prototype;
  Object.assign(window.RTCPeerConnection, Native);
};

// Sums inbound audio across every connection. Returns null until some track has
// carried bytes, so the caller can poll it until the timeout.
const INBOUND_AUDIO = async () => {
  const pcs = window.__unmutePCs || [];
  let total = 0;
  for (const pc of pcs) {
    let report;
    try {
      report = await pc.getStats();
    } catch {
      continue;
    }
    report.forEach((s) => {
      if (s.type === "inbound-rtp" && s.kind === "audio" && s.bytesReceived > 0) {
        total += s.bytesReceived;
      }
    });
  }
  return total > 0 ? total : null;
};

const STATE = () => ({
  state: document.body.dataset.state || "",
  hint: (document.getElementById("hint") || {}).textContent || "",
  connections: (window.__unmutePCs || []).length,
  connectionState: ((window.__unmutePCs || [])[0] || {}).connectionState || "none",
});

function say(payload, code) {
  process.stdout.write(JSON.stringify(payload) + "\n");
  process.exit(code);
}

async function main() {
  const [url, seconds] = process.argv.slice(2);
  if (!url) say({ ok: false, reason: "usage: greeting-probe.js <url> <timeout-seconds>" }, 2);
  const timeoutMs = (Number(seconds) || 60) * 1000;

  const browser = await chromium.launch({ args: FLAGS });
  const context = await browser.newContext({ permissions: ["microphone"] });
  await context.addInitScript(KEEP_PEER_CONNECTIONS);
  const page = await context.newPage();

  const consoleErrors = [];
  page.on("pageerror", (e) => consoleErrors.push(String(e.message || e)));

  const started = Date.now();
  try {
    await page.goto(url, { waitUntil: "domcontentloaded", timeout: 30000 });
    await page.click("#connect", { timeout: 15000 });

    while (Date.now() - started < timeoutMs) {
      const bytes = await page.evaluate(INBOUND_AUDIO);
      if (bytes) {
        const out = { ok: true, bytesReceived: bytes, msToFirstAudio: Date.now() - started };
        await browser.close();
        say(out, 0);
      }
      await page.waitForTimeout(500);
    }

    const observed = await page.evaluate(STATE);
    await browser.close();
    say({ ok: false, reason: "no-inbound-audio", ...observed, pageErrors: consoleErrors }, 1);
  } catch (err) {
    let observed = {};
    try {
      observed = await page.evaluate(STATE);
    } catch {
      /* the page is already gone; the error below is the whole story */
    }
    await browser.close().catch(() => {});
    say({ ok: false, reason: String((err && err.message) || err), ...observed, pageErrors: consoleErrors }, 1);
  }
}

main().catch((err) => say({ ok: false, reason: "probe crashed: " + String((err && err.message) || err) }, 1));
