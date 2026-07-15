package ir

import "testing"

func TestSIPChannelConfigTwilioTrunk(t *testing.T) { // V30, V32, V33
	got := SIPChannelConfig{
		Provider:   SIPProviderTwilio,
		Connection: "twilio",
	}

	if got.Provider != "twilio" {
		t.Errorf("provider = %q, want twilio", got.Provider)
	}
	if got.Connection != "twilio" {
		t.Errorf("connection = %q, want twilio", got.Connection)
	}
}

func TestTwilioConnectionConfigUsesSecretRefs(t *testing.T) { // V31, V33
	got := TwilioConnectionConfig{
		Type: ConnectionTypeTwilio,
		Auth: TwilioAuthRefs{
			AccountSIDSecret: "TWILIO_ACCOUNT_SID",
			AuthTokenSecret:  "TWILIO_AUTH_TOKEN",
		},
		SIP: TwilioSIPTrunkConfig{Enabled: true},
	}

	if got.Type != "twilio" {
		t.Errorf("type = %q, want twilio", got.Type)
	}
	if got.Auth.AccountSIDSecret != "TWILIO_ACCOUNT_SID" {
		t.Errorf("account SID secret ref = %q", got.Auth.AccountSIDSecret)
	}
	if got.Auth.AuthTokenSecret != "TWILIO_AUTH_TOKEN" {
		t.Errorf("auth token secret ref = %q", got.Auth.AuthTokenSecret)
	}
	if !got.SIP.Enabled {
		t.Error("SIP trunk should be enabled in scaffold shape")
	}
}
