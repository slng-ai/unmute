package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/slng-ai/unmute/internal/generate"
)

// Twilio voice webhook auto-configuration for routes whose carrier
// definition carries the fact (SPEC V3). It runs on every start because
// quick tunnel URLs rotate per run, prints the previous webhook so the user
// can restore it, and never creates or buys carrier resources.
//
// API shapes verified 2026-07-22 against
// twilio.com/docs/phone-numbers/api/incomingphonenumber-resource:
//
//	lookup: GET /2010-04-01/Accounts/{sid}/IncomingPhoneNumbers.json?PhoneNumber=<E.164>
//	update: POST /2010-04-01/Accounts/{sid}/IncomingPhoneNumbers/{pn}.json (VoiceUrl, VoiceMethod)
//
// Basic auth is AccountSid:AuthToken.
var twilioAPIBase = "https://api.twilio.com"

var telephonyHTTPClient = &http.Client{Timeout: 15 * time.Second}

// autoConfigureCarrierWebhook dispatches the plan's auto-webhook fact to the
// carrier implementation. A fact without an implementation is a hard error:
// carrier facts are data, and data must not promise what no code does. It
// returns the undo, to be called when the dev process stops (V14).
func autoConfigureCarrierWebhook(ctx context.Context, out io.Writer, targetName string, plan *generate.TelephonyRuntimePlan, public *url.URL, env []string) (func(context.Context) error, error) {
	path := ""
	for _, endpoint := range plan.PublicEndpoints {
		if endpoint.Name == plan.AutoWebhookEndpoint {
			path = endpoint.Path
		}
	}
	if path == "" {
		return nil, fmt.Errorf("auto-webhook endpoint %q is not among the plan's public endpoints", plan.AutoWebhookEndpoint)
	}
	// Signature validation in the generated app derives its URLs only from
	// UNMUTE_PUBLIC_URL, so the webhook must be derived the same way (V3).
	voiceURL := strings.TrimSuffix(public.String(), "/") + path
	switch plan.Route.Carrier {
	case "twilio":
		accountSID := envValue(env, plan.Environment["account_sid"])
		authToken := envValue(env, plan.Environment["auth_token"])
		number := envValue(env, plan.Environment["from_number"])
		previous, err := configureTwilioVoiceWebhook(ctx, accountSID, authToken, number, voiceURL)
		if err != nil {
			return nil, fmt.Errorf("configure Twilio voice webhook: %w", err)
		}
		shown := previous
		if shown == "" {
			shown = "unset"
		}
		fmt.Fprintf(out, "%s: Twilio voice webhook for %s set to %s (was: %s)\n", targetName, number, voiceURL, shown)
		// The dev URL dies with this process, so leaving it on the number aims a
		// real phone line at a dead tunnel until the next run. Hand back the undo
		// (V14/B7).
		return func(restoreCtx context.Context) error {
			if _, err := configureTwilioVoiceWebhook(restoreCtx, accountSID, authToken, number, previous); err != nil {
				return err
			}
			fmt.Fprintf(out, "%s: Twilio voice webhook for %s restored to %s\n", targetName, number, shown)
			return nil
		}, nil
	default:
		return nil, fmt.Errorf("carrier %q declares automatic webhook configuration but no implementation exists", plan.Route.Carrier)
	}
}

// configureTwilioVoiceWebhook looks up the IncomingPhoneNumber by exact
// number, sets its VoiceUrl, and returns the previous VoiceUrl.
func configureTwilioVoiceWebhook(ctx context.Context, accountSID, authToken, number, voiceURL string) (string, error) {
	list := fmt.Sprintf("%s/2010-04-01/Accounts/%s/IncomingPhoneNumbers.json?PhoneNumber=%s",
		twilioAPIBase, url.PathEscape(accountSID), url.QueryEscape(number))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, list, nil)
	if err != nil {
		return "", err
	}
	request.SetBasicAuth(accountSID, authToken)
	var lookup struct {
		IncomingPhoneNumbers []struct {
			SID      string `json:"sid"`
			VoiceURL string `json:"voice_url"`
		} `json:"incoming_phone_numbers"`
	}
	if err := doTelephonyJSON(request, &lookup); err != nil {
		return "", err
	}
	if len(lookup.IncomingPhoneNumbers) == 0 {
		return "", fmt.Errorf("phone number %s was not found on this Twilio account; buy or verify a Voice-capable number in the Twilio Console first", number)
	}
	record := lookup.IncomingPhoneNumbers[0]

	form := url.Values{"VoiceUrl": {voiceURL}, "VoiceMethod": {"POST"}}
	update := fmt.Sprintf("%s/2010-04-01/Accounts/%s/IncomingPhoneNumbers/%s.json",
		twilioAPIBase, url.PathEscape(accountSID), url.PathEscape(record.SID))
	request, err = http.NewRequestWithContext(ctx, http.MethodPost, update, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.SetBasicAuth(accountSID, authToken)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := doTelephonyJSON(request, &struct{}{}); err != nil {
		return "", err
	}
	return record.VoiceURL, nil
}

// doTelephonyJSON performs a request and decodes a JSON body into result,
// surfacing non-2xx responses with a bounded body excerpt.
func doTelephonyJSON(request *http.Request, result any) error {
	response, err := telephonyHTTPClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s: %s", request.Method, request.URL.Path, response.Status, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("decode %s response: %w", request.URL.Path, err)
	}
	return nil
}
