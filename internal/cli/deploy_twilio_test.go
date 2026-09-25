package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/spec"
	"github.com/slng-ai/unmute/internal/target"
)

// The SIDs are assembled, not written out, because a SID-shaped literal trips
// secret scanning on push even when it is made up.
var (
	fakeAccount = "AC" + strings.Repeat("0a", 16)
	fakeNumber  = "PN" + strings.Repeat("f5", 16)
	otherAcct   = "AC" + strings.Repeat("ff", 16)
)

const (
	fakeToken   = "tok5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e"
	oldSecret   = "oldsecret-7d1c"
	oldVoiceURL = "https://old.example.com/hook?token=" + oldSecret
)

// fakeTwilio is one TLS server playing both the Twilio REST API and the hosted
// app, so one client trusts both. It counts everything, and every write.
type fakeTwilio struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	number   map[string]any
	requests int
	reads    int
	writes   []url.Values

	appToken     string // the Auth Token the hosted app validates with
	artifactID   string // what the hosted app's /healthz reports; "" = omitted
	healthStatus int
	voiceBody    string // the TwiML the hosted app answers; "" = this build's
	relay        []byte
	postStatus   int           // non-zero: refuse the write with this status
	postDelay    time.Duration // applied, then answered late
	failAfter    bool          // every read after a write fails
	onRead       func(n int, number map[string]any)
}

func newFakeTwilio(t *testing.T) *fakeTwilio {
	t.Helper()
	f := &fakeTwilio{t: t, appToken: fakeToken, healthStatus: http.StatusOK}
	f.number = map[string]any{
		"account_sid": fakeAccount, "sid": fakeNumber, "phone_number": "+15005550006",
		"voice_url": oldVoiceURL, "voice_method": "GET", "voice_fallback_url": nil,
		"voice_application_sid": nil, "trunk_sid": nil, "voice_receive_mode": "voice",
		"sms_url":      "https://sms.example.com/in",
		"capabilities": map[string]any{"voice": true, "sms": true},
	}
	artifact := twilioFixtureArtifact(t)
	f.artifactID, f.relay = artifact.ArtifactID, artifactContent(artifact, generate.TwilioRelayTemplate)
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)

	client := f.srv.Client()
	client.Timeout = 2 * time.Second
	client.CheckRedirect = twilioHTTP.CheckRedirect
	oldBase, oldHTTP, oldDir := twilioAPIBase, twilioHTTP, twilioRollbackDir
	twilioAPIBase, twilioHTTP = f.srv.URL, client
	rollback := filepath.Join(t.TempDir(), "rollback")
	twilioRollbackDir = func() (string, error) { return rollback, nil }
	t.Cleanup(func() { twilioAPIBase, twilioHTTP, twilioRollbackDir = oldBase, oldHTTP, oldDir })
	return f
}

func twilioFixtureArtifact(t *testing.T) generate.Artifact {
	t.Helper()
	pkg, err := spec.Load(filepath.Join("..", "testdata", "twilio_relay"))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := ir.Build(pkg)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := generate.Generate(agent, agent.Targets["twilio"], target.Default())
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

// renderRelay is what app.py's render_twiml() answers /voice with.
func (f *fakeTwilio) renderRelay() string {
	host := strings.TrimPrefix(f.srv.URL, "https://")
	body := strings.ReplaceAll(string(f.relay), "__PUBLIC_WSS_ORIGIN__", "wss://"+host)
	return strings.ReplaceAll(body, "__PUBLIC_ORIGIN__", f.srv.URL)
}

func (f *fakeTwilio) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	switch r.URL.Path {
	case "/2010-04-01/Accounts/" + fakeAccount + "/IncomingPhoneNumbers/" + fakeNumber + ".json":
		user, pass, ok := r.BasicAuth()
		if !ok || user != fakeAccount || pass != fakeToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			f.writes = append(f.writes, r.PostForm)
			if f.postStatus != 0 {
				w.WriteHeader(f.postStatus)
				_, _ = w.Write([]byte(`{"code": 21999, "message": "echo ` + oldSecret + `"}`))
				return
			}
			f.number["voice_url"], f.number["voice_method"] = r.PostForm.Get("VoiceUrl"), r.PostForm.Get("VoiceMethod")
			if f.postDelay > 0 {
				f.mu.Unlock()
				time.Sleep(f.postDelay)
				f.mu.Lock()
			}
		} else {
			f.reads++
			if f.failAfter && len(f.writes) > 0 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if f.onRead != nil {
				f.onRead(f.reads, f.number)
			}
		}
		_ = json.NewEncoder(w).Encode(f.number)
	case "/healthz":
		w.WriteHeader(f.healthStatus)
		body := map[string]any{"ok": f.healthStatus == http.StatusOK}
		if f.artifactID != "" {
			body["artifact_id"] = f.artifactID
		}
		_ = json.NewEncoder(w).Encode(body)
	case "/voice":
		// The pinned Python RequestValidator: the configured origin plus the
		// path, tried with and without the port.
		_ = r.ParseForm()
		signature := r.Header.Get("X-Twilio-Signature")
		withPort := f.srv.URL + r.URL.RequestURI()
		withoutPort := twilioSignedURL(f.srv.URL, r.URL.RequestURI())
		if signature == "" || (signature != twilioSignature(f.appToken, withPort, r.PostForm) &&
			signature != twilioSignature(f.appToken, withoutPort, r.PostForm)) ||
			r.PostForm.Get("AccountSid") != fakeAccount {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(cmpOr(f.voiceBody, f.renderRelay())))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (f *fakeTwilio) counts() (requests, writes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests, len(f.writes)
}

type twilioRun struct {
	dir, out, errOut string
	err              error
}

// deployTwilio runs `unmute deploy` on a copy of the twilio fixture with no
// voiceai on PATH and no SLNG key, the four Twilio values in the package .env.
func deployTwilio(t *testing.T, f *fakeTwilio, env map[string]string, args ...string) twilioRun {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "twilio_relay"))); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{
		"TWILIO_ACCOUNT_SID": fakeAccount, "TWILIO_AUTH_TOKEN": fakeToken,
		"TWILIO_PHONE_NUMBER_SID": fakeNumber, "TWILIO_PUBLIC_URL": f.srv.URL,
	}
	for name, value := range env {
		values[name] = value
	}
	var dotenv strings.Builder
	for name, value := range values {
		t.Setenv(name, "")
		if value != "" {
			dotenv.WriteString(name + "=" + value + "\n")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(dotenv.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv(target.SlngRouterKeyEnv, "")
	t.Setenv(target.SlngPushCredentialEnv, "")

	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"deploy", dir}, args...))
	err := root.Execute()
	run := twilioRun{dir: dir, out: stdout.String(), errOut: stderr.String(), err: err}
	report, _ := os.ReadFile(filepath.Join(dir, "build", "twilio", "deploy-report.json"))
	for _, text := range []string{run.out, run.errOut, errText(err), string(report)} {
		for _, secret := range []string{fakeToken, oldSecret} {
			if strings.Contains(text, secret) {
				t.Errorf("output or report carries a secret %q:\n%s", secret, text)
			}
		}
	}
	return run
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func twilioReportOf(t *testing.T, dir string) twilioDeployReport {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, "build", "twilio", "deploy-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report twilioDeployReport
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func snapshots(t *testing.T) []string {
	t.Helper()
	dir, _ := twilioRollbackDir()
	matches, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	return matches
}

func TestTwilioDeployRoutesTheNumber(t *testing.T) {
	f := newFakeTwilio(t)
	run := deployTwilio(t, f, nil, "--target", "twilio")
	if run.err != nil {
		t.Fatalf("deploy: %v\n%s", run.err, run.errOut)
	}
	want := f.srv.URL + "/voice"
	if len(f.writes) != 1 {
		t.Fatalf("writes = %d, want 1", len(f.writes))
	}
	if got := f.writes[0]; len(got) != 2 || got.Get("VoiceUrl") != want || got.Get("VoiceMethod") != "POST" {
		t.Errorf("wrote %v, want VoiceUrl=%s and VoiceMethod=POST only", got, want)
	}
	if f.number["sms_url"] != "https://sms.example.com/in" {
		t.Error("the SMS URL changed")
	}
	if !strings.Contains(run.out, "routed +15005550006 ("+fakeNumber+") to POST "+want) {
		t.Errorf("stdout does not name the new route:\n%s", run.out)
	}
	report := twilioReportOf(t, run.dir)
	if report.Outcome != "routed" || report.CallVerified || report.ArtifactID != f.artifactID ||
		report.PreviousVoiceURL != "https://old.example.com/…" || report.Destination != want {
		t.Errorf("report = %+v", report)
	}

	files := snapshots(t)
	if len(files) != 1 || report.Snapshot != files[0] {
		t.Fatalf("snapshots %v, report names %q", files, report.Snapshot)
	}
	info, _ := os.Stat(files[0])
	dirInfo, _ := os.Stat(filepath.Dir(files[0]))
	if info.Mode().Perm() != 0o600 || dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("snapshot mode %v in dir %v, want 0600 in 0700", info.Mode().Perm(), dirInfo.Mode().Perm())
	}
	var saved twilioSnapshot
	body, _ := os.ReadFile(files[0])
	if err := json.Unmarshal(body, &saved); err != nil || saved.VoiceURL != oldVoiceURL || saved.VoiceMethod != "GET" || saved.NewVoiceURL != want {
		t.Errorf("snapshot = %+v (%v), want the exact old route", saved, err)
	}

	// A second run finds the route in place: no snapshot, no write.
	again := deployTwilio(t, f, nil, "--target", "twilio")
	if again.err != nil || !strings.Contains(again.out, "already routes to "+want) {
		t.Fatalf("second deploy: %v\n%s", again.err, again.out)
	}
	if len(f.writes) != 1 || len(snapshots(t)) != 1 {
		t.Errorf("an unchanged route wrote again (%d writes, %d snapshots)", len(f.writes), len(snapshots(t)))
	}
	if twilioReportOf(t, again.dir).Outcome != "unchanged" {
		t.Error("the unchanged run did not report unchanged")
	}
}

func TestTwilioDeployDryRunWritesNothing(t *testing.T) {
	f := newFakeTwilio(t)
	run := deployTwilio(t, f, nil, "--target", "twilio", "--dry-run")
	if run.err != nil {
		t.Fatalf("dry run: %v", run.err)
	}
	if _, writes := f.counts(); writes != 0 || len(snapshots(t)) != 0 {
		t.Errorf("dry run wrote %d times and saved %d snapshots", writes, len(snapshots(t)))
	}
	if !strings.Contains(run.out, "would route +15005550006") || !strings.Contains(run.out, "now GET https://old.example.com/…") {
		t.Errorf("dry run does not say what it would change:\n%s", run.out)
	}
	if _, err := os.Stat(filepath.Join(run.dir, "build")); !os.IsNotExist(err) {
		t.Error("dry run wrote a local file")
	}
}

// The public URL's root slash is dropped and its port kept, as app.py does.
func TestTwilioDeployNormalisesTheRootSlash(t *testing.T) {
	f := newFakeTwilio(t)
	run := deployTwilio(t, f, map[string]string{"TWILIO_PUBLIC_URL": f.srv.URL + "/"}, "--target", "twilio")
	if run.err != nil {
		t.Fatal(run.err)
	}
	if got := f.writes[0].Get("VoiceUrl"); got != f.srv.URL+"/voice" {
		t.Errorf("VoiceUrl = %q, want %q", got, f.srv.URL+"/voice")
	}
}

// Every refusal before the write: the reason is named, and nothing is written.
func TestTwilioDeployRefusesBeforeWriting(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*fakeTwilio)
		want  string
	}{
		{"number on another account", func(f *fakeTwilio) { f.number["account_sid"] = otherAcct }, "not " + fakeNumber},
		{"number without voice", func(f *fakeTwilio) { f.number["capabilities"] = map[string]any{"voice": false} }, "cannot take voice calls"},
		{"fax number", func(f *fakeTwilio) { f.number["voice_receive_mode"] = "fax" }, "receives fax"},
		{"TwiML app", func(f *fakeTwilio) { f.number["voice_application_sid"] = "AP0123" }, "TwiML App AP0123"},
		{"SIP trunk", func(f *fakeTwilio) { f.number["trunk_sid"] = "TK0123" }, "SIP trunk TK0123"},
		{"fallback URL", func(f *fakeTwilio) { f.number["voice_fallback_url"] = "https://fallback.example.com/x?k=" + oldSecret }, "fallback URL (https://fallback.example.com/…)"},
		{"health 503", func(f *fakeTwilio) { f.healthStatus = http.StatusServiceUnavailable }, "try again later"},
		{"health names no build", func(f *fakeTwilio) { f.artifactID = "" }, "names no build"},
		{"health names another build", func(f *fakeTwilio) { f.artifactID = "sha256:other" }, "runs build sha256:other"},
		{"signature refused", func(f *fakeTwilio) { f.appToken = "another-token" }, "refused a signed /voice"},
		{"capacity hangup", func(f *fakeTwilio) {
			f.voiceBody = `<?xml version="1.0" encoding="UTF-8"?><Response><Hangup/></Response>`
		}, "try again later"},
		{"unexpected TwiML", func(f *fakeTwilio) {
			f.voiceBody = strings.Replace(f.renderRelay(), `interruptible="speech"`, `interruptible="none"`, 1)
		}, "differs from this build at /Response/Connect/ConversationRelay@interruptible"},
		{"not TwiML", func(f *fakeTwilio) { f.voiceBody = "hello" }, "not TwiML"},
		{"route drift", func(f *fakeTwilio) {
			f.onRead = func(n int, number map[string]any) {
				if n == 2 {
					number["voice_url"] = "https://someone.example.com/else"
				}
			}
		}, "changed while this ran"},
		{"snapshot cannot be saved", func(f *fakeTwilio) {
			// A file where the directory should be: MkdirAll cannot succeed.
			blocker := filepath.Join(f.t.TempDir(), "blocker")
			if err := os.WriteFile(blocker, nil, 0o600); err != nil {
				f.t.Fatal(err)
			}
			twilioRollbackDir = func() (string, error) { return filepath.Join(blocker, "rollback"), nil }
		}, "save the rollback snapshot, so nothing was written"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeTwilio(t)
			tc.setup(f)
			run := deployTwilio(t, f, nil, "--target", "twilio")
			if run.err == nil || !strings.Contains(run.err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", run.err, tc.want)
			}
			if !strings.Contains(run.err.Error(), "twilio target twilio: ") {
				t.Errorf("error does not open with the target: %v", run.err)
			}
			if _, writes := f.counts(); writes != 0 {
				t.Errorf("%d writes after a refusal", writes)
			}
		})
	}
}

// Bad local values stop before any request at all.
func TestTwilioDeployChecksValuesBeforeAnyRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"missing token", map[string]string{"TWILIO_AUTH_TOKEN": ""}, "set TWILIO_AUTH_TOKEN"},
		{"bad account", map[string]string{"TWILIO_ACCOUNT_SID": "AC123"}, "TWILIO_ACCOUNT_SID is not an account SID"},
		{"bad number", map[string]string{"TWILIO_PHONE_NUMBER_SID": "+15005550006"}, "TWILIO_PHONE_NUMBER_SID is not a phone number SID"},
		{"http origin", map[string]string{"TWILIO_PUBLIC_URL": "http://relay.example.com"}, "must be an https origin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeTwilio(t)
			run := deployTwilio(t, f, tc.env, "--target", "twilio")
			if run.err == nil || !strings.Contains(run.err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", run.err, tc.want)
			}
			if requests, _ := f.counts(); requests != 0 {
				t.Errorf("%d requests before the values were checked", requests)
			}
		})
	}
}

// After the write, only the readback decides, and a failure never claims
// that nothing changed.
func TestTwilioDeployReadsBackTheWrite(t *testing.T) {
	t.Run("refused write", func(t *testing.T) {
		f := newFakeTwilio(t)
		f.postStatus = http.StatusBadRequest
		run := deployTwilio(t, f, nil, "--target", "twilio")
		if run.err == nil || !strings.Contains(run.err.Error(), "still routes where it did") ||
			!strings.Contains(run.err.Error(), "answered 400 (error 21999)") {
			t.Fatalf("err = %v", run.err)
		}
		if report := twilioReportOf(t, run.dir); report.Outcome != "not_changed" || report.Snapshot == "" {
			t.Errorf("report = %+v", report)
		}
	})
	t.Run("unreadable after a failed write", func(t *testing.T) {
		f := newFakeTwilio(t)
		f.postStatus = http.StatusInternalServerError
		f.failAfter = true
		run := deployTwilio(t, f, nil, "--target", "twilio")
		if run.err == nil || !strings.Contains(run.err.Error(), "outcome is unknown") ||
			!strings.Contains(run.err.Error(), snapshots(t)[0]) {
			t.Fatalf("err = %v, want an unknown outcome naming the snapshot", run.err)
		}
		if strings.Contains(run.err.Error(), "nothing was") {
			t.Errorf("an unknown outcome claims nothing changed: %v", run.err)
		}
		if report := twilioReportOf(t, run.dir); report.Outcome != "unknown" {
			t.Errorf("outcome = %q", report.Outcome)
		}
	})
	t.Run("write that timed out but landed", func(t *testing.T) {
		f := newFakeTwilio(t)
		f.postDelay = 3 * time.Second
		run := deployTwilio(t, f, nil, "--target", "twilio")
		if run.err != nil {
			t.Fatalf("a write the readback confirms failed: %v", run.err)
		}
		if _, writes := f.counts(); writes != 1 {
			t.Errorf("writes = %d: a timed-out write was retried", writes)
		}
		if report := twilioReportOf(t, run.dir); report.Outcome != "routed" {
			t.Errorf("outcome = %q", report.Outcome)
		}
	})
}

// The choice between slng and twilio is made before voiceai, an SLNG key, or
// any account is touched.
func TestTwilioDeployDispatch(t *testing.T) {
	t.Run("no voiceai and no SLNG key needed", func(t *testing.T) {
		f := newFakeTwilio(t)
		if run := deployTwilio(t, f, nil, "--target", "twilio", "--dry-run"); run.err != nil {
			t.Fatalf("twilio deploy needed something slng: %v", run.err)
		}
	})
	for _, flag := range [][]string{{"--profile", "x"}, {"--agent-id", "x"}, {"--label", "x"}, {"--run-samples"}, {"--call", "+15005550006"}} {
		t.Run("refuses "+flag[0], func(t *testing.T) {
			f := newFakeTwilio(t)
			run := deployTwilio(t, f, nil, append([]string{"--target", "twilio"}, flag...)...)
			if run.err == nil || !strings.Contains(run.err.Error(), flag[0]+" is a slng deploy flag") {
				t.Fatalf("err = %v", run.err)
			}
			if requests, _ := f.counts(); requests != 0 {
				t.Errorf("%d requests before refusing a flag", requests)
			}
		})
	}
	t.Run("no --target keeps the slng path", func(t *testing.T) {
		f := newFakeTwilio(t)
		run := deployTwilio(t, f, nil)
		if run.err == nil || !strings.Contains(run.err.Error(), "no slng target to deploy") ||
			!strings.Contains(run.err.Error(), "unmute deploy --target <name>") {
			t.Fatalf("err = %v", run.err)
		}
		if requests, _ := f.counts(); requests != 0 {
			t.Errorf("%d requests", requests)
		}
	})
}

func TestTwilioDeployRefusesSeveralTargets(t *testing.T) {
	for _, tc := range []struct {
		name, targets string
		args          []string
	}{
		{"two twilio targets", "", []string{"--target", "twilio-openai", "--target", "twilio-gemini"}},
		{"twilio and slng", "  slng:\n    provider: slng\n    deployment_region: eu-north\n", []string{"--target", "twilio-openai", "--target", "slng"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeTwilio(t)
			dir := t.TempDir()
			if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "voice-agents-tests", "relay-desk"))); err != nil {
				t.Fatal(err)
			}
			if tc.targets != "" {
				targets, _ := os.ReadFile(filepath.Join(dir, "targets.yaml"))
				if err := os.WriteFile(filepath.Join(dir, "targets.yaml"), append(targets, tc.targets...), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", t.TempDir())
			root := newRootCmd()
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs(append([]string{"deploy", dir}, tc.args...))
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), "deploy routes one number per run") {
				t.Fatalf("err = %v", err)
			}
			if requests, _ := f.counts(); requests != 0 {
				t.Errorf("%d requests", requests)
			}
		})
	}
}

// Twilio's own worked example, https://www.twilio.com/docs/usage/security.
func TestTwilioSignatureMatchesTheDocumentedExample(t *testing.T) {
	form := url.Values{
		"CallSid": {"CA1234567890ABCDE"}, "Caller": {"+14158675310"}, "Digits": {"1234"},
		"From": {"+14158675310"}, "To": {"+18005551212"},
	}
	if got := twilioSignature("12345", "https://example.com/myapp.php?foo=1&bar=2", form); got != "L/OH5YylLD5NRKLltdqwSvS0BnU=" {
		t.Errorf("signature = %s", got)
	}
	if got := twilioSignedURL("https://relay.example.com:8443", "/voice"); got != "https://relay.example.com/voice" {
		t.Errorf("signed URL = %s: voice over HTTPS is signed without the port", got)
	}
}

func TestTwilioOriginReadsLikeTheApp(t *testing.T) {
	for in, want := range map[string]string{
		"https://relay.example.com":       "https://relay.example.com",
		"https://relay.example.com/":      "https://relay.example.com",
		" https://relay.example.com:8443": "https://relay.example.com:8443",
		"http://relay.example.com":        "",
		"https://u:p@relay.example.com":   "",
		"https://relay.example.com/app":   "",
		"https://relay.example.com/?":     "",
		"https://relay.example.com/?a=1":  "",
		"https://relay.example.com/#x":    "",
		"https://":                        "",
		"relay.example.com":               "",
	} {
		got, err := twilioOrigin(in)
		if (err != nil) != (want == "") || got != want {
			t.Errorf("twilioOrigin(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}
