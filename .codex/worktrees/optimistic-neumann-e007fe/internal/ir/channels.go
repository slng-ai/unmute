package ir

const (
	SIPProviderTwilio    = "twilio"
	ConnectionTypeTwilio = "twilio"
)

// SIPChannelConfig is the shape of agent/channels/sip.yaml.
//
// It declares the telephony surface and references provider auth/config by
// connection name. Directional dial-in/dial-out behavior is a later
// validate/compile concern.
type SIPChannelConfig struct {
	Provider   string `json:"provider" yaml:"provider"`
	Connection string `json:"connection" yaml:"connection"`
}

// TwilioConnectionConfig is the shape of agent/connections/twilio.yaml.
//
// Auth fields are secret references, not secret values.
type TwilioConnectionConfig struct {
	Type string               `json:"type" yaml:"type"`
	Auth TwilioAuthRefs       `json:"auth" yaml:"auth"`
	SIP  TwilioSIPTrunkConfig `json:"sip" yaml:"sip"`
}

// TwilioAuthRefs names the secrets required for Twilio API access.
type TwilioAuthRefs struct {
	AccountSIDSecret string `json:"account_sid_secret" yaml:"account_sid_secret"`
	AuthTokenSecret  string `json:"auth_token_secret" yaml:"auth_token_secret"`
}

// TwilioSIPTrunkConfig holds Twilio SIP trunk capability switches.
type TwilioSIPTrunkConfig struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
}
