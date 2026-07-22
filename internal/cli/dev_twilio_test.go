package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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
		_, _ = w.Write([]byte(`{"incoming_phone_numbers":[{"sid":"PN123","voice_url":"` + existingVoiceURL + `"}]}`))
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
// plan's inbound path, previous URL returned.
func TestConfigureTwilioVoiceWebhookLooksUpAndUpdates(t *testing.T) {
	var updates url.Values
	fakeTwilioAPI(t, "token", "https://old.example/hook", &updates)

	previous, err := configureTwilioVoiceWebhook(context.Background(), "account", "token", "+15550001111", "https://fake.trycloudflare.com/telephony/inbound")
	if err != nil {
		t.Fatal(err)
	}
	if previous != "https://old.example/hook" {
		t.Fatalf("previous voice URL = %q", previous)
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

// V3 guard: a carrier fact without a CLI implementation fails loudly instead
// of silently skipping configuration.
func TestAutoConfigureCarrierWebhookRejectsUnimplementedCarrier(t *testing.T) {
	plan := pipecatTwilioPlan()
	plan.Route.Carrier = "telnyx"
	plan.AutoWebhookEndpoint = "inbound"
	public, _ := url.Parse("https://fake.trycloudflare.com")
	err := autoConfigureCarrierWebhook(context.Background(), os.Stderr, "phone", plan, public, nil)
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
	if got := updates.Get("VoiceUrl"); got != "https://fake-zero.trycloudflare.com/telephony/inbound" {
		t.Fatalf("VoiceUrl = %q", got)
	}
	printed := out.String()
	if !strings.Contains(printed, "Twilio voice webhook for +15550001111 set to https://fake-zero.trycloudflare.com/telephony/inbound (was: https://old.example/hook)") {
		t.Fatalf("output missing webhook report:\n%s", printed)
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
