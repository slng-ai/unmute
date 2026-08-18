package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
)

// fakeTwilioAPI serves the two IncomingPhoneNumbers endpoints the webhook
// auto-config uses and records the update form values.
func fakeTwilioAPI(t *testing.T, authToken, existingVoiceURL string, updates *url.Values) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /2010-04-01/Accounts/account/IncomingPhoneNumbers.json", func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "account" || pass != authToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if got := r.URL.Query().Get("PhoneNumber"); got != "+15550001111" {
			http.Error(w, "wrong PhoneNumber filter: "+got, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"incoming_phone_numbers":[{"sid":"PN123","voice_url":"` + existingVoiceURL + `","voice_method":"POST"}]}`))
	})
	mux.HandleFunc("POST /2010-04-01/Accounts/account/IncomingPhoneNumbers/PN123.json", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		*updates = r.PostForm
		_, _ = w.Write([]byte(`{"sid":"PN123"}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	restore := twilioAPIBase
	twilioAPIBase = server.URL
	t.Cleanup(func() { twilioAPIBase = restore })
	return server
}

// V3: lookup by exact number, VoiceUrl set to the tunnel origin plus the
// plan's inbound path, previous voice configuration returned.
func TestConfigureTwilioVoiceWebhookLooksUpAndUpdates(t *testing.T) {
	var updates url.Values
	fakeTwilioAPI(t, "token", "https://old.example/hook", &updates)

	previous, err := configureTwilioVoiceWebhook(context.Background(), "account", "token", "+15550001111", "https://fake.trycloudflare.com/telephony/inbound")
	if err != nil {
		t.Fatal(err)
	}
	if previous.URL != "https://old.example/hook" {
		t.Fatalf("previous voice URL = %q", previous.URL)
	}
	if got := updates.Get("VoiceUrl"); got != "https://fake.trycloudflare.com/telephony/inbound" {
		t.Fatalf("VoiceUrl = %q", got)
	}
	if got := updates.Get("VoiceMethod"); got != "POST" {
		t.Fatalf("VoiceMethod = %q", got)
	}
}

// V3: a number missing from the account is a clear error, and no update is
// attempted.
func TestConfigureTwilioVoiceWebhookReportsUnknownNumber(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			t.Error("update must not run for an unknown number")
		}
		_, _ = w.Write([]byte(`{"incoming_phone_numbers":[]}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	restore := twilioAPIBase
	twilioAPIBase = server.URL
	t.Cleanup(func() { twilioAPIBase = restore })

	_, err := configureTwilioVoiceWebhook(context.Background(), "account", "token", "+15550001111", "https://x.example/inbound")
	if err == nil || !strings.Contains(err.Error(), "was not found on this Twilio account") {
		t.Fatalf("unknown number error = %v", err)
	}
}

func TestConfigureTwilioVoiceWebhookRejectsInvalidPreviousMethods(t *testing.T) {
	for _, method := range []string{"", "DELETE"} {
		name := method
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			posts := 0
			mux := http.NewServeMux()
			mux.HandleFunc("GET /2010-04-01/Accounts/account/IncomingPhoneNumbers.json", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"incoming_phone_numbers":[{"sid":"PN123","voice_url":"https://old.example/hook","voice_method":"` + method + `"}]}`))
			})
			mux.HandleFunc("POST /2010-04-01/Accounts/account/IncomingPhoneNumbers/PN123.json", func(w http.ResponseWriter, _ *http.Request) {
				posts++
				_, _ = w.Write([]byte(`{"sid":"PN123"}`))
			})
			server := httptest.NewServer(mux)
			defer server.Close()
			restoreBase := twilioAPIBase
			twilioAPIBase = server.URL
			defer func() { twilioAPIBase = restoreBase }()

			_, err := configureTwilioVoiceWebhook(
				context.Background(),
				"account",
				"token",
				"+15550001111",
				"https://new.example/hook",
			)
			if err == nil || !strings.Contains(err.Error(), "unsupported prior VoiceMethod") {
				t.Fatalf("invalid prior VoiceMethod error = %v", err)
			}
			if posts != 0 {
				t.Fatalf("Twilio updates = %d, want zero", posts)
			}
		})
	}
}

func TestConfigureTwilioVoiceWebhookRollsBackUncertainUpdate(t *testing.T) {
	currentURL := "https://old.example/hook"
	currentMethod := http.MethodGet
	var writes []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /2010-04-01/Accounts/account/IncomingPhoneNumbers.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"incoming_phone_numbers":[{"sid":"PN123","voice_url":"` + currentURL + `","voice_method":"` + currentMethod + `"}]}`))
	})
	mux.HandleFunc("POST /2010-04-01/Accounts/account/IncomingPhoneNumbers/PN123.json", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		currentURL = r.PostForm.Get("VoiceUrl")
		currentMethod = r.PostForm.Get("VoiceMethod")
		writes = append(writes, currentMethod+" "+currentURL)
		if len(writes) == 1 {
			_, _ = w.Write([]byte(`{"sid":`))
			return
		}
		_, _ = w.Write([]byte(`{"sid":"PN123"}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	restoreBase := twilioAPIBase
	twilioAPIBase = server.URL
	t.Cleanup(func() { twilioAPIBase = restoreBase })

	_, err := configureTwilioVoiceWebhook(
		context.Background(),
		"account",
		"token",
		"+15550001111",
		"https://new.example/hook",
	)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("uncertain update error = %v", err)
	}
	wantWrites := []string{
		"POST https://new.example/hook",
		"GET https://old.example/hook",
	}
	if !slices.Equal(writes, wantWrites) {
		t.Fatalf("Twilio writes = %v, want %v", writes, wantWrites)
	}
	if currentURL != "https://old.example/hook" || currentMethod != http.MethodGet {
		t.Fatalf("voice configuration after uncertain update = %s %s", currentMethod, currentURL)
	}
}

func TestConfigureTwilioVoiceWebhookRollsBackServerError(t *testing.T) {
	currentURL := "https://old.example/hook"
	currentMethod := http.MethodGet
	var writes []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /2010-04-01/Accounts/account/IncomingPhoneNumbers.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"incoming_phone_numbers":[{"sid":"PN123","voice_url":"` + currentURL + `","voice_method":"` + currentMethod + `"}]}`))
	})
	mux.HandleFunc("POST /2010-04-01/Accounts/account/IncomingPhoneNumbers/PN123.json", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		currentURL = r.PostForm.Get("VoiceUrl")
		currentMethod = r.PostForm.Get("VoiceMethod")
		writes = append(writes, currentMethod+" "+currentURL)
		if len(writes) == 1 {
			http.Error(w, "upstream failed after applying update", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"sid":"PN123"}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	restoreBase := twilioAPIBase
	twilioAPIBase = server.URL
	t.Cleanup(func() { twilioAPIBase = restoreBase })

	_, err := configureTwilioVoiceWebhook(
		context.Background(),
		"account",
		"token",
		"+15550001111",
		"https://new.example/hook",
	)
	if err == nil || !strings.Contains(err.Error(), "502 Bad Gateway") {
		t.Fatalf("uncertain server error = %v", err)
	}
	wantWrites := []string{
		"POST https://new.example/hook",
		"GET https://old.example/hook",
	}
	if !slices.Equal(writes, wantWrites) {
		t.Fatalf("Twilio writes = %v, want %v", writes, wantWrites)
	}
	if currentURL != "https://old.example/hook" || currentMethod != http.MethodGet {
		t.Fatalf("voice configuration after server error = %s %s", currentMethod, currentURL)
	}
}

func TestConfigureTwilioVoiceWebhookReportsRollbackFailure(t *testing.T) {
	posts := 0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /2010-04-01/Accounts/account/IncomingPhoneNumbers.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"incoming_phone_numbers":[{"sid":"PN123","voice_url":"https://old.example/hook","voice_method":"GET"}]}`))
	})
	mux.HandleFunc("POST /2010-04-01/Accounts/account/IncomingPhoneNumbers/PN123.json", func(w http.ResponseWriter, _ *http.Request) {
		posts++
		if posts == 1 {
			_, _ = w.Write([]byte(`{"sid":`))
			return
		}
		http.Error(w, "restore refused", http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	restoreBase := twilioAPIBase
	twilioAPIBase = server.URL
	t.Cleanup(func() { twilioAPIBase = restoreBase })

	_, err := configureTwilioVoiceWebhook(
		context.Background(),
		"account",
		"token",
		"+15550001111",
		"https://new.example/hook",
	)
	if err == nil {
		t.Fatal("uncertain update and failed rollback returned nil")
	}
	for _, want := range []string{"decode", "restore previous Twilio voice configuration", "500 Internal Server Error"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("combined error missing %q: %v", want, err)
		}
	}
	if posts != 2 {
		t.Fatalf("Twilio updates = %d, want failed update and rollback", posts)
	}
}

// V14: the undo restores every voice field changed for the dev session.
func TestAutoConfigureCarrierWebhookRestoresTwilioVoiceConfiguration(t *testing.T) {
	currentURL := "https://old.example/hook"
	currentMethod := http.MethodGet
	updates := 0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /2010-04-01/Accounts/account/IncomingPhoneNumbers.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"incoming_phone_numbers":[{"sid":"PN123","voice_url":"` + currentURL + `","voice_method":"` + currentMethod + `"}]}`))
	})
	mux.HandleFunc("POST /2010-04-01/Accounts/account/IncomingPhoneNumbers/PN123.json", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		updates++
		currentURL = r.PostForm.Get("VoiceUrl")
		currentMethod = r.PostForm.Get("VoiceMethod")
		_, _ = w.Write([]byte(`{"sid":"PN123"}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	restoreBase := twilioAPIBase
	twilioAPIBase = server.URL
	t.Cleanup(func() { twilioAPIBase = restoreBase })

	plan := pipecatTwilioPlan()
	plan.AutoWebhookEndpoint = "inbound"
	public, err := url.Parse("https://fake.trycloudflare.com")
	if err != nil {
		t.Fatal(err)
	}
	undo, err := autoConfigureCarrierWebhook(
		context.Background(),
		&strings.Builder{},
		"phone",
		plan,
		public,
		[]string{
			"TWILIO_ACCOUNT_SID=account",
			"TWILIO_AUTH_TOKEN=token",
			"TWILIO_PHONE_NUMBER=+15550001111",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if updates != 2 {
		t.Fatalf("Twilio updates = %d, want set then restore", updates)
	}
	if currentURL != "https://old.example/hook" || currentMethod != http.MethodGet {
		t.Fatalf("restored voice configuration = %s %s, want GET https://old.example/hook", currentMethod, currentURL)
	}
}

// V3 guard: a carrier fact without a CLI implementation fails loudly instead
// of silently skipping configuration.
func TestAutoConfigureCarrierWebhookRejectsUnimplementedCarrier(t *testing.T) {
	plan := pipecatTwilioPlan()
	plan.Route.Carrier = "telnyx"
	plan.AutoWebhookEndpoint = "inbound"
	public, _ := url.Parse("https://fake.trycloudflare.com")
	_, err := autoConfigureCarrierWebhook(context.Background(), os.Stderr, "phone", plan, public, nil)
	if err == nil || !strings.Contains(err.Error(), "no implementation exists") {
		t.Fatalf("unimplemented carrier error = %v", err)
	}
}

// V3 end to end through the post-gate core: the webhook update runs after
// the graph is up, uses the managed tunnel origin, and the previous webhook
// is printed for restore.
func TestExecDevTelephonyConfiguresTwilioWebhookAfterReady(t *testing.T) {
	var updates url.Values
	fakeTwilioAPI(t, "sekrit-auth-77", "https://old.example/hook", &updates)
	root, trace := fakeTelephonyRoot(t, "TWILIO_ACCOUNT_SID=account\nTWILIO_AUTH_TOKEN=sekrit-auth-77\nTWILIO_PHONE_NUMBER=+15550001111\n")
	fakeDocker(t, root)
	cloudflared := root + "/cloudflared"
	if err := os.WriteFile(cloudflared, []byte("#!/bin/sh\necho 'INF |  https://fake-zero.trycloudflare.com  |'\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	restoreLook := tunnelLookPath
	tunnelLookPath = func(string) (string, error) { return cloudflared, nil }
	t.Cleanup(func() { tunnelLookPath = restoreLook })

	plan := pipecatTwilioPlan()
	plan.AutoWebhookEndpoint = "inbound"
	cmd, out := telephonyTestCommand(t)
	if err := execDevTelephony(cmd, root, "phone", plan, composeFiles, devTelephonyOptions{botPort: "7861"}); err != nil {
		t.Fatalf("execDevTelephony: %v\n%s", err, out.String())
	}
	// V14: the last write the carrier saw is the *restore*. The dev URL dies
	// with this process, so leaving it on a real number aims a phone line at a
	// dead tunnel (B7). The set itself is proven by the printed line below.
	if got := updates.Get("VoiceUrl"); got != "https://old.example/hook" {
		t.Fatalf("webhook was not restored on exit; final VoiceUrl = %q", got)
	}
	printed := out.String()
	if !strings.Contains(printed, "Twilio voice webhook for +15550001111 set to https://fake-zero.trycloudflare.com/telephony/inbound (was: https://old.example/hook)") {
		t.Fatalf("output missing webhook report:\n%s", printed)
	}
	if !strings.Contains(printed, "Twilio voice webhook for +15550001111 restored to https://old.example/hook") {
		t.Fatalf("output missing webhook restore report:\n%s", printed)
	}
	raw, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), " up ") {
		t.Fatalf("compose up never ran:\n%s", raw)
	}
	// No secret value may appear in printed output (V6).
	if strings.Contains(printed, "sekrit-auth-77") {
		t.Fatalf("printed output leaks the auth token:\n%s", printed)
	}
}

// V14: --no-webhook leaves the carrier's number configuration completely
// alone. A shared or production number must survive a dev run untouched, so
// there is no set and therefore nothing to restore.
func TestExecDevTelephonyNoWebhookLeavesCarrierUntouched(t *testing.T) {
	var updates url.Values
	fakeTwilioAPI(t, "sekrit-auth-77", "https://old.example/hook", &updates)
	root, _ := fakeTelephonyRoot(t, "TWILIO_ACCOUNT_SID=account\nTWILIO_AUTH_TOKEN=sekrit-auth-77\nTWILIO_PHONE_NUMBER=+15550001111\n")
	fakeDocker(t, root)
	cloudflared := root + "/cloudflared"
	if err := os.WriteFile(cloudflared, []byte("#!/bin/sh\necho 'INF |  https://fake-zero.trycloudflare.com  |'\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	restoreLook := tunnelLookPath
	tunnelLookPath = func(string) (string, error) { return cloudflared, nil }
	t.Cleanup(func() { tunnelLookPath = restoreLook })

	plan := pipecatTwilioPlan()
	plan.AutoWebhookEndpoint = "inbound"
	cmd, out := telephonyTestCommand(t)
	opts := devTelephonyOptions{botPort: "7861", noWebhook: true}
	if err := execDevTelephony(cmd, root, "phone", plan, composeFiles, opts); err != nil {
		t.Fatalf("execDevTelephony: %v\n%s", err, out.String())
	}
	if len(updates) != 0 {
		t.Fatalf("--no-webhook must not write to the carrier, got %v", updates)
	}
	printed := out.String()
	if !strings.Contains(printed, "--no-webhook, carrier number left untouched") {
		t.Fatalf("output must say the number was left alone:\n%s", printed)
	}
	if strings.Contains(printed, "webhook for +15550001111 set to") {
		t.Fatalf("--no-webhook must not report a set:\n%s", printed)
	}
}
