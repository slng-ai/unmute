package cli

import (
	"bytes"
	"cmp"
	"crypto/hmac"
	"crypto/sha1" // Twilio's request signature is HMAC-SHA1; the algorithm is theirs.
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/slng-ai/unmute/internal/generate"
	"github.com/slng-ai/unmute/internal/ir"
	"github.com/slng-ai/unmute/internal/manifest"
	"github.com/slng-ai/unmute/internal/target"
	"github.com/spf13/cobra"
)

// A twilio deploy points one existing Twilio number at an app the author
// already hosts. It uploads nothing, buys nothing and calls nobody: it checks
// the hosted app is this build, then sets the number's VoiceUrl and VoiceMethod
// and nothing else.
//
// Unlike slng, this talks to the Twilio REST API directly, because there is no
// Twilio CLI step the account boundary could live in. The writes are two fields
// of one IncomingPhoneNumber:
// https://www.twilio.com/docs/phone-numbers/api/incomingphonenumber-resource
//
// The artifact id, the dry run and the rollback snapshot are Unmute's own, not
// Twilio features.
//
// A re-read before the write is not a conditional update: Twilio offers none on
// this resource. Two deploys racing on one number are not supported.

var (
	twilioAPIBase = "https://api.twilio.com"
	// No redirect is followed: every request carries a credential or a
	// signature, and neither should reach a host it was not addressed to.
	twilioHTTP = &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	// twilioRollbackDir is where snapshots of a number's old route go: under
	// the user config root, never next to build output that gets shared.
	twilioRollbackDir = func() (string, error) {
		store, err := manifest.DefaultStore()
		if err != nil {
			return "", err
		}
		return filepath.Join(store.Root, "twilio-rollback"), nil
	}
)

const twilioMaxBody = 1 << 20

var (
	twilioAccountSID = regexp.MustCompile(`^AC[0-9a-fA-F]{32}$`)
	twilioNumberSID  = regexp.MustCompile(`^PN[0-9a-fA-F]{32}$`)
)

// errTwilioBusy is a host that is up and this build, but cannot take a call
// right now. Nothing is written; the fix is to try again later.
var errTwilioBusy = errors.New("the host cannot take a call right now (draining, or every call slot is held); nothing was changed, try again later")

type twilioConfig struct {
	account, token, number string
	origin                 string // https://host[:port], no trailing slash
	envNames               map[string]string
}

// twilioNumber is the part of an IncomingPhoneNumber this command reads.
type twilioNumber struct {
	AccountSID   string `json:"account_sid"`
	SID          string `json:"sid"`
	PhoneNumber  string `json:"phone_number"`
	Capabilities struct {
		Voice bool `json:"voice"`
	} `json:"capabilities"`
	twilioRoute
}

// twilioRoute is everything that decides where a call to the number goes.
// Drift in any of it between the first read and the write refuses the write.
type twilioRoute struct {
	VoiceURL    string `json:"voice_url"`
	VoiceMethod string `json:"voice_method"`
	FallbackURL string `json:"voice_fallback_url"`
	Application string `json:"voice_application_sid"`
	Trunk       string `json:"trunk_sid"`
	ReceiveMode string `json:"voice_receive_mode"`
}

type twilioSnapshot struct {
	TakenAt        string `json:"taken_at"`
	Target         string `json:"target"`
	ArtifactID     string `json:"artifact_id"`
	AccountSID     string `json:"account_sid"`
	PhoneNumberSID string `json:"phone_number_sid"`
	PhoneNumber    string `json:"phone_number"`
	// The exact old values, which may hold a credential in a query string.
	// That is why this file is 0600 and never printed.
	VoiceURL    string `json:"voice_url"`
	VoiceMethod string `json:"voice_method"`
	NewVoiceURL string `json:"new_voice_url"`
}

type twilioDeployReport struct {
	Provider         string   `json:"provider"`
	Target           string   `json:"target"`
	ArtifactID       string   `json:"artifact_id"`
	AccountSID       string   `json:"account_sid"`
	PhoneNumberSID   string   `json:"phone_number_sid"`
	PhoneNumber      string   `json:"phone_number"`
	Destination      string   `json:"destination"`
	PreviousVoiceURL string   `json:"previous_voice_url"` // redacted
	Preflight        []string `json:"preflight"`
	Outcome          string   `json:"outcome"` // unchanged, routed, not_changed, unknown
	Snapshot         string   `json:"rollback_snapshot,omitempty"`
	CallVerified     bool     `json:"call_verified"`
}

// twilioOnlyFlags are the flags that mean something to a slng push and nothing
// to a number route. Accepting one silently would read as having used it.
var twilioOnlyFlags = []string{"profile", "agent-id", "label", "run-samples", "call"}

// twilioDispatch decides whether this run is a twilio deploy, before any
// lookup or request. Twilio needs an explicit --target, so a bare `unmute
// deploy` keeps meaning slng.
func twilioDispatch(cmd *cobra.Command, names []string, selected []ir.Target) (*ir.Target, error) {
	if len(names) == 0 || !slices.ContainsFunc(selected, func(t ir.Target) bool { return t.Provider == ir.ProviderTwilio }) {
		return nil, nil
	}
	if len(selected) != 1 {
		chosen := make([]string, 0, len(selected))
		for _, resolved := range selected {
			chosen = append(chosen, fmt.Sprintf("%s (%s)", resolved.Name, resolved.Provider))
		}
		return nil, fmt.Errorf("twilio target: deploy routes one number per run, and --target selected %s; run once per target", strings.Join(chosen, ", "))
	}
	for _, name := range twilioOnlyFlags {
		if cmd.Flags().Changed(name) {
			return nil, fmt.Errorf("twilio target %s: --%s is a slng deploy flag; a twilio deploy only points a number at the app you host", selected[0].Name, name)
		}
	}
	return &selected[0], nil
}

func runTwilioDeploy(cmd *cobra.Command, dir string, agent *ir.Agent, resolved ir.Target, dryRun bool) error {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	fail := func(err error) error { return fmt.Errorf("deploy %s: twilio target %s: %w", dir, resolved.Name, err) }

	report, err := ir.Validate(agent, []ir.Target{resolved}, target.Default())
	printValidationReport(out, errOut, report)
	if err != nil {
		return fmt.Errorf("deploy %s: %w", dir, err)
	}
	artifact, err := generate.Generate(agent, resolved, target.Default())
	if err != nil {
		return fail(err)
	}
	relay := artifactContent(artifact, generate.TwilioRelayTemplate)
	if artifact.ArtifactID == "" || relay == nil {
		return fail(errors.New("the compiled artifact carries no artifact id or TwiML template"))
	}
	cfg, err := twilioConfigFrom(resolved, packageEnv(dir, errOut))
	if err != nil {
		return fail(err)
	}

	number, err := cfg.fetchNumber()
	if err != nil {
		return fail(err)
	}
	if err := cfg.checkNumber(number); err != nil {
		return fail(err)
	}
	preflight := []string{"number " + number.SID + " takes voice calls and has no app, trunk or fallback"}
	if err := cfg.checkHealth(artifact.ArtifactID); err != nil {
		return fail(err)
	}
	preflight = append(preflight, "hosted /healthz names "+artifact.ArtifactID)
	if err := cfg.checkVoice(relay); err != nil {
		return fail(err)
	}
	// A signed /voice proves the HTTP renderer and nothing past it: not the
	// WebSocket through the host's proxy, not speech, not the action callback.
	preflight = append(preflight, "signed POST /voice renders this build's TwiML")

	desired := cfg.origin + "/voice"
	record := twilioDeployReport{
		Provider: string(ir.ProviderTwilio), Target: resolved.Name, ArtifactID: artifact.ArtifactID,
		AccountSID: cfg.account, PhoneNumberSID: cfg.number, PhoneNumber: number.PhoneNumber,
		Destination: desired, PreviousVoiceURL: redactURL(number.VoiceURL), Preflight: preflight,
	}
	unchanged := routesTo(number, desired)
	// A dry run writes nothing at all, here or in build/: not even a report
	// that the route is already right.
	switch {
	case unchanged && dryRun:
		fmt.Fprintf(out, "%s: %s already routes to %s\n", resolved.Name, number.PhoneNumber, desired)
		return nil
	case unchanged:
		record.Outcome = "unchanged"
		fmt.Fprintf(out, "%s: %s already routes to %s\n", resolved.Name, number.PhoneNumber, desired)
		writeTwilioReport(out, errOut, dir, record)
		return nil
	case dryRun:
		fmt.Fprintf(out, "%s: would route %s (%s) to POST %s, now %s %s\n", resolved.Name,
			number.PhoneNumber, cfg.number, desired, cmp.Or(number.VoiceMethod, "-"), redactURL(number.VoiceURL))
		return nil
	}

	again, err := cfg.fetchNumber()
	if err != nil {
		return fail(fmt.Errorf("re-read before writing: %w", err))
	}
	if err := cfg.checkNumber(again); err != nil {
		return fail(fmt.Errorf("re-read before writing, nothing was written: %w", err))
	}
	if again.twilioRoute != number.twilioRoute {
		return fail(errors.New("the number's voice configuration changed while this ran; nothing was written, run deploy again"))
	}
	snapshot, err := saveTwilioSnapshot(twilioSnapshot{
		TakenAt: time.Now().UTC().Format(time.RFC3339), Target: resolved.Name, ArtifactID: artifact.ArtifactID,
		AccountSID: cfg.account, PhoneNumberSID: cfg.number, PhoneNumber: again.PhoneNumber,
		VoiceURL: again.VoiceURL, VoiceMethod: again.VoiceMethod, NewVoiceURL: desired,
	})
	if err != nil {
		return fail(fmt.Errorf("save the rollback snapshot, so nothing was written: %w", err))
	}
	record.Snapshot = snapshot

	// One write, never retried: a timeout does not say whether it landed, and
	// the readback below is what decides. An old route read back after a write
	// that was not refused proves nothing: the write may still land later.
	writeErr := cfg.updateVoice(desired)
	after, readErr := cfg.fetchNumber()
	var conflict error
	if readErr == nil {
		conflict = cfg.checkNumber(after)
	}
	switch {
	case readErr == nil && conflict == nil && routesTo(after, desired):
		record.Outcome = "routed"
	case readErr == nil && twilioRefused(writeErr) && after.twilioRoute == again.twilioRoute:
		record.Outcome = "not_changed"
	default:
		record.Outcome = "unknown"
	}
	if record.Outcome == "routed" {
		fmt.Fprintf(out, "%s: routed %s (%s) to POST %s\n", resolved.Name, number.PhoneNumber, cfg.number, desired)
		fmt.Fprintf(out, "%s: old route saved to %s\n", resolved.Name, snapshot)
	}
	writeTwilioReport(out, errOut, dir, record)
	switch record.Outcome {
	case "routed":
		fmt.Fprintf(out, "%s: no call was placed; call %s to check speech, the WebSocket and the hangup\n", resolved.Name, number.PhoneNumber)
		return nil
	case "not_changed":
		return fail(fmt.Errorf("the number still routes where it did: %w", writeErr))
	default:
		cause := errors.Join(writeErr, readErr, conflict)
		if cause == nil {
			cause = errors.New("the write was answered, and the readback does not show it yet")
		}
		return fail(fmt.Errorf("the write's outcome is unknown: %w\n"+
			"  check the number in the Twilio Console, or run this again with --dry-run.\n"+
			"  the old route is saved in %s; restore it by hand only if the number still routes to POST %s\n"+
			"  and has no TwiML App, SIP trunk or fallback URL",
			cause, snapshot, desired))
	}
}

// routesTo is the route this deploy writes: VoiceUrl and POST.
func routesTo(n twilioNumber, voiceURL string) bool {
	return n.VoiceURL == voiceURL && strings.EqualFold(n.VoiceMethod, http.MethodPost)
}

// twilioStatusError is an answer from the Twilio API that is not 200. The
// message never quotes the body, which can echo a value back.
type twilioStatusError struct {
	status int
	msg    string
}

func (e *twilioStatusError) Error() string { return e.msg }

// twilioRefused is a write Twilio answered with a 4xx: it validated the
// request and applied nothing. A timeout, a 5xx or a lost connection says
// nothing about whether the write landed.
func twilioRefused(err error) bool {
	statusErr, ok := errors.AsType[*twilioStatusError](err)
	return ok && statusErr.status >= 400 && statusErr.status < 500
}

func artifactContent(artifact generate.Artifact, path string) []byte {
	for _, file := range artifact.Files {
		if file.Path == path {
			return file.Content
		}
	}
	return nil
}

// twilioConfigFrom reads the four environment names the connection declares.
// Names come from the package, values from the same .env files `dev` reads.
// No model key is read: the model runs on the host, not here.
func twilioConfigFrom(resolved ir.Target, env []string) (twilioConfig, error) {
	if resolved.Telephony == nil {
		return twilioConfig{}, errors.New("no telephony plan; the connection must be a twilio_relay connection")
	}
	names := resolved.Telephony.Environment
	var missing []string
	value := func(key string) string {
		v := strings.TrimSpace(envValue(env, names[key]))
		if v == "" {
			missing = append(missing, cmp.Or(names[key], key))
		}
		return v
	}
	cfg := twilioConfig{
		account: value("account_sid"), token: value("auth_token"), number: value("phone_number_sid"),
		envNames: names,
	}
	publicURL := value("public_url")
	if len(missing) > 0 {
		return twilioConfig{}, fmt.Errorf("set %s in the environment or the package .env", strings.Join(missing, ", "))
	}
	if !twilioAccountSID.MatchString(cfg.account) {
		return twilioConfig{}, fmt.Errorf("%s is not an account SID (AC followed by 32 hex characters)", names["account_sid"])
	}
	if !twilioNumberSID.MatchString(cfg.number) {
		return twilioConfig{}, fmt.Errorf("%s is not a phone number SID (PN followed by 32 hex characters)", names["phone_number_sid"])
	}
	origin, err := twilioOrigin(publicURL)
	if err != nil {
		return twilioConfig{}, fmt.Errorf("%s %w", names["public_url"], err)
	}
	cfg.origin = origin
	return cfg, nil
}

// twilioOrigin reads the public URL exactly as app.py's public_origin() does:
// an https origin, a root slash allowed and dropped, the port kept.
func twilioOrigin(value string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("must be an https origin with no path, query or credentials, for example https://relay.example.com")
	}
	return "https://" + u.Host, nil
}

// twilioSignature is X-Twilio-Signature for a form POST
// (https://www.twilio.com/docs/usage/security): the URL, then each parameter
// name followed by its value, names sorted, HMAC-SHA1 with the Auth Token,
// base64. Values under one name are sorted too, as the pinned Python
// RequestValidator does.
func twilioSignature(token, requestURL string, form url.Values) string {
	var b strings.Builder
	b.WriteString(requestURL)
	for _, name := range slices.Sorted(maps.Keys(form)) {
		values := slices.Clone(form[name])
		slices.Sort(values)
		for _, v := range slices.Compact(values) {
			b.WriteString(name)
			b.WriteString(v)
		}
	}
	mac := hmac.New(sha1.New, []byte(token))
	mac.Write([]byte(b.String()))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// twilioSignedURL is the URL Twilio signs a voice webhook with. For voice over
// HTTPS, Twilio drops the port before signing, and the pinned validator accepts
// either form, so a preflight signed this way is signed the way a call is.
// ponytail: an IPv6 literal host is not handled; app.py's validator does not handle one either.
func twilioSignedURL(origin, path string) string {
	u, err := url.Parse(origin)
	if err != nil {
		return origin + path
	}
	return "https://" + u.Hostname() + path
}

func (c twilioConfig) numberURL() string {
	return twilioAPIBase + "/2010-04-01/Accounts/" + c.account + "/IncomingPhoneNumbers/" + c.number + ".json"
}

func (c twilioConfig) fetchNumber() (twilioNumber, error) {
	req, err := http.NewRequest(http.MethodGet, c.numberURL(), nil)
	if err != nil {
		return twilioNumber{}, err
	}
	var number twilioNumber
	return number, c.twilioAPI(req, &number)
}

// updateVoice writes VoiceUrl and VoiceMethod, and nothing else. Twilio leaves
// every field an update does not name as it was, so SMS and the rest keep
// their settings.
func (c twilioConfig) updateVoice(voiceURL string) error {
	form := url.Values{"VoiceUrl": {voiceURL}, "VoiceMethod": {http.MethodPost}}
	req, err := http.NewRequest(http.MethodPost, c.numberURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.twilioAPI(req, nil)
}

// twilioAPI sends one authenticated request. An error names the status and
// Twilio's error code, never the body: a message can quote a value back.
func (c twilioConfig) twilioAPI(req *http.Request, into any) error {
	req.SetBasicAuth(c.account, c.token)
	req.Header.Set("Accept", "application/json")
	status, body, err := twilioDo(req)
	if err != nil {
		return fmt.Errorf("the Twilio API %s: %w", req.Method, err)
	}
	if status != http.StatusOK {
		var problem struct {
			Code int `json:"code"`
		}
		_ = json.Unmarshal(body, &problem)
		hint := ""
		switch status {
		case http.StatusUnauthorized:
			hint = fmt.Sprintf("; check %s and %s belong together", c.envNames["account_sid"], c.envNames["auth_token"])
		case http.StatusNotFound:
			hint = fmt.Sprintf("; account %s has no number %s", c.account, c.number)
		}
		msg := fmt.Sprintf("the Twilio API %s answered %d%s", req.Method, status, hint)
		if problem.Code != 0 {
			msg = fmt.Sprintf("the Twilio API %s answered %d (error %d)%s", req.Method, status, problem.Code, hint)
		}
		return &twilioStatusError{status: status, msg: msg}
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("the Twilio API %s answered unreadable JSON", req.Method)
	}
	return nil
}

func twilioDo(req *http.Request) (int, []byte, error) {
	resp, err := twilioHTTP.Do(req)
	if err != nil {
		if uerr, ok := errors.AsType[*url.Error](err); ok {
			// The url.Error text repeats the URL; keep only the cause.
			err = uerr.Err
		}
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, twilioMaxBody))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

// checkNumber refuses a number whose calls would not reach VoiceUrl. It clears
// nothing: an application, a trunk or a fallback is the operator's decision.
func (c twilioConfig) checkNumber(n twilioNumber) error {
	switch {
	case n.AccountSID != c.account || n.SID != c.number:
		return fmt.Errorf("the Twilio API returned number %s on account %s, not %s on %s", n.SID, n.AccountSID, c.number, c.account)
	case !n.Capabilities.Voice:
		return fmt.Errorf("number %s cannot take voice calls", n.PhoneNumber)
	case strings.EqualFold(n.ReceiveMode, "fax"):
		return fmt.Errorf("number %s receives fax, not voice; set it to voice in the Twilio Console", n.PhoneNumber)
	case n.Application != "":
		// "If a voice_application_sid is present, we ignore all of the voice urls."
		return fmt.Errorf("number %s is configured by TwiML App %s, which overrides its voice URL; remove the app from the number in the Twilio Console first", n.PhoneNumber, n.Application)
	case n.Trunk != "":
		return fmt.Errorf("number %s is on SIP trunk %s, which ignores its voice URL; take it off the trunk in the Twilio Console first", n.PhoneNumber, n.Trunk)
	case n.FallbackURL != "":
		return fmt.Errorf("number %s has a voice fallback URL (%s), which would answer calls this app refused; clear it in the Twilio Console first", n.PhoneNumber, redactURL(n.FallbackURL))
	}
	return nil
}

// checkHealth compares the hosted build with this one. /healthz and its
// artifact_id are Unmute's, not Twilio's. A body with no id is an older build,
// never a match.
func (c twilioConfig) checkHealth(want string) error {
	req, err := http.NewRequest(http.MethodGet, c.origin+"/healthz", nil)
	if err != nil {
		return err
	}
	status, body, err := twilioDo(req)
	if err != nil {
		return fmt.Errorf("GET %s/healthz: %w", c.origin, err)
	}
	switch status {
	case http.StatusOK:
	case http.StatusServiceUnavailable:
		return errTwilioBusy
	default:
		return fmt.Errorf("GET %s/healthz answered %d; is the app hosted at %s?", c.origin, status, c.envNames["public_url"])
	}
	var health struct {
		ArtifactID string `json:"artifact_id"`
	}
	_ = json.Unmarshal(body, &health)
	switch health.ArtifactID {
	case want:
		return nil
	case "":
		return fmt.Errorf("the app at %s names no build, so it predates this check; recompile, rehost, then deploy", c.origin)
	default:
		return fmt.Errorf("the app at %s runs build %s, and this package compiles to %s; recompile, rehost, then deploy", c.origin, health.ArtifactID, want)
	}
}

// checkVoice sends /voice the request a call would, signed with the Auth
// Token, and reads the TwiML back. It opens no WebSocket and starts no call:
// the app answers /voice statelessly.
func (c twilioConfig) checkVoice(relay []byte) error {
	form := url.Values{"AccountSid": {c.account}}
	req, err := http.NewRequest(http.MethodPost, c.origin+"/voice", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Twilio-Signature", twilioSignature(c.token, twilioSignedURL(c.origin, "/voice"), form))
	status, body, err := twilioDo(req)
	if err != nil {
		return fmt.Errorf("POST %s/voice: %w", c.origin, err)
	}
	switch status {
	case http.StatusOK:
	case http.StatusForbidden:
		return fmt.Errorf("the app at %s refused a signed /voice; the host's %s, %s and %s must be the values used here",
			c.origin, c.envNames["account_sid"], c.envNames["auth_token"], c.envNames["public_url"])
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return errTwilioBusy
	default:
		return fmt.Errorf("POST %s/voice answered %d", c.origin, status)
	}
	got, err := parseTwiML(body)
	if err != nil {
		return fmt.Errorf("POST %s/voice answered something that is not TwiML", c.origin)
	}
	if got.Name == "Response" && len(got.Children) == 1 && got.Children[0].Name == "Hangup" {
		return errTwilioBusy
	}
	want, err := expectedTwiML(relay, c.origin)
	if err != nil {
		return err
	}
	if where := diffTwiML(want, got, ""); where != "" {
		return fmt.Errorf("POST %s/voice answered TwiML that differs from this build at %s; recompile, rehost, then deploy", c.origin, where)
	}
	return nil
}

type twimlNode struct {
	Name     string
	Attrs    map[string]string
	Text     string
	Children []twimlNode
}

// parseTwiML reads exactly one XML document: one root element, and outside it
// only whitespace, comments, processing instructions and, before the root, a
// directive. A second root or stray text is not a document Twilio would run.
func parseTwiML(body []byte) (twimlNode, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	var stack []twimlNode
	var root *twimlNode
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			if root == nil {
				return twimlNode{}, errors.New("no root element")
			}
			return *root, nil
		}
		if err != nil {
			return twimlNode{}, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if root != nil {
				return twimlNode{}, fmt.Errorf("a second root element <%s>", t.Name.Local)
			}
			node := twimlNode{Name: t.Name.Local, Attrs: map[string]string{}}
			for _, a := range t.Attr {
				node.Attrs[a.Name.Local] = a.Value
			}
			stack = append(stack, node)
		case xml.CharData:
			text := strings.TrimSpace(string(t))
			if len(stack) > 0 {
				stack[len(stack)-1].Text += text
			} else if text != "" {
				return twimlNode{}, errors.New("text outside the root element")
			}
		case xml.Directive:
			if root != nil {
				return twimlNode{}, errors.New("a directive after the root element")
			}
		case xml.EndElement:
			node := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				root = &node
				continue
			}
			stack[len(stack)-1].Children = append(stack[len(stack)-1].Children, node)
		}
	}
}

// expectedTwiML is the relay template with the two URLs app.py's
// render_twiml() sets at startup.
func expectedTwiML(relay []byte, origin string) (twimlNode, error) {
	root, err := parseTwiML(relay)
	if err != nil {
		return twimlNode{}, fmt.Errorf("read the compiled TwiML template: %w", err)
	}
	host := strings.TrimPrefix(origin, "https://")
	for i, child := range root.Children {
		if child.Name != "Connect" {
			continue
		}
		root.Children[i].Attrs["action"] = origin + "/connect-action"
		for j, relay := range child.Children {
			if relay.Name == "ConversationRelay" {
				root.Children[i].Children[j].Attrs["url"] = "wss://" + host + "/conversation"
				return root, nil
			}
		}
	}
	return twimlNode{}, errors.New("the compiled TwiML template has no Connect/ConversationRelay")
}

// diffTwiML names the first element or attribute where got leaves want, or "".
// Attribute order and whitespace are not meaning, so neither is compared.
func diffTwiML(want, got twimlNode, at string) string {
	at += "/" + want.Name
	if want.Name != got.Name {
		return at
	}
	for _, name := range slices.Sorted(maps.Keys(want.Attrs)) {
		if got.Attrs[name] != want.Attrs[name] {
			return at + "@" + name
		}
	}
	for _, name := range slices.Sorted(maps.Keys(got.Attrs)) {
		if _, ok := want.Attrs[name]; !ok {
			return at + "@" + name
		}
	}
	if want.Text != got.Text || len(want.Children) != len(got.Children) {
		return at
	}
	for i := range want.Children {
		if where := diffTwiML(want.Children[i], got.Children[i], at); where != "" {
			return where
		}
	}
	return ""
}

// redactURL keeps the scheme and host of a URL an operator set, and drops the
// rest: a path or query can carry a credential.
func redactURL(raw string) string {
	if raw == "" {
		return "(none)"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "(set)"
	}
	shown := u.Scheme + "://" + u.Hostname()
	if strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" {
		shown += "/…"
	}
	return shown
}

// saveTwilioSnapshot writes the old route to a new private file, whole or not
// at all: a temporary file is written, synced and renamed into place. Older
// snapshots are never touched.
func saveTwilioSnapshot(record twilioSnapshot) (string, error) {
	dir, err := twilioRollbackDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	body, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".partial-*") // 0600
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(append(body, '\n')); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	unique := strings.TrimPrefix(filepath.Base(tmp.Name()), ".partial-")
	final := filepath.Join(dir, fmt.Sprintf("%s-%s-%s.json", record.PhoneNumberSID, time.Now().UTC().Format("20060102T150405Z"), unique))
	if err := os.Rename(tmp.Name(), final); err != nil {
		return "", err
	}
	return final, nil
}

// writeTwilioReport records what this run did beside the build. It holds IDs,
// the redacted old URL and the outcome; no token and no provider body.
// call_verified stays false: only a real call verifies a call.
func writeTwilioReport(out, errOut io.Writer, dir string, record twilioDeployReport) {
	body, err := json.MarshalIndent(record, "", "  ")
	if err == nil {
		outDir := filepath.Join(dir, "build", record.Target)
		if err = os.MkdirAll(outDir, 0o755); err == nil {
			path := filepath.Join(outDir, "deploy-report.json")
			if err = os.WriteFile(path, append(body, '\n'), 0o644); err == nil {
				fmt.Fprintf(out, "%s: wrote %s\n", record.Target, displayDir(path))
				return
			}
		}
	}
	warnf(errOut, "%s: could not write deploy-report.json: %v\n", record.Target, err)
}
