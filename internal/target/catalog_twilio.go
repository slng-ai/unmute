package target

// The twilio target's catalogue. Two kinds of row live here, and the field names
// mean different things for each.
//
// The think rows are real SDK calls made by the emitted app.py: OpenAI Chat
// Completions or native Gemini generateContent. The Class is the SDK client.
//
// The listen and speak rows are not code the app runs. ConversationRelay does
// the speech on Twilio's side, so the "constructor" is the <ConversationRelay>
// TwiML noun and each Arg is the attribute a binding field lands on. That keeps
// the shared language-slot check in internal/ir working unchanged: the language
// slot is a TwiML attribute here, not a keyword argument.
var twilioCatalog = []Entry{
	{
		Framework: Twilio, Role: Reason, Vendor: "openai",
		Verified: TwilioTargetVerified, Docs: "https://developers.openai.com/api/reference/resources/chat",
		Install: InstallSpec{Package: "openai"},
		Import:  "from openai import AsyncOpenAI",
		Call: &CallSpec{
			Class: "AsyncOpenAI", APIKeyArg: "api_key", APIKeyEnv: "OPENAI_API_KEY",
			Model:  FieldSpec{Arg: "model", Required: true},
			Params: ParamsKwargs,
		},
	},
	{
		Framework: Twilio, Role: Reason, Vendor: "google", Aliases: []string{"gemini"},
		Verified: TwilioTargetVerified, Docs: "https://ai.google.dev/api/generate-content",
		Install: InstallSpec{Package: "google-genai"},
		Import:  "from google import genai",
		Call: &CallSpec{
			Class: "genai.Client", APIKeyArg: "api_key", APIKeyEnv: "GOOGLE_API_KEY",
			Model:  FieldSpec{Arg: "model", Required: true},
			Params: ParamsKwargs,
		},
	},
	{
		Framework: Twilio, Role: Listen, Vendor: "deepgram",
		Verified: TwilioTargetVerified, Docs: TwilioDocs.ConversationRelay,
		Call: &CallSpec{
			Class:    "ConversationRelay",
			Model:    FieldSpec{Arg: "speechModel", Required: true},
			Language: FieldSpec{Arg: "transcriptionLanguage"},
		},
		Notes: []string{"transcriptionProvider=\"Deepgram\"; ConversationRelay transcribes, the app installs no Deepgram SDK and reads no Deepgram key"},
	},
	{
		Framework: Twilio, Role: Speak, Vendor: "elevenlabs",
		Verified: TwilioTargetVerified, Docs: TwilioDocs.ConversationRelay,
		Call: &CallSpec{
			Class: "ConversationRelay",
			// The model is not an attribute of its own: the driver joins it to
			// the voice id as the voice attribute's suffix.
			Model:    FieldSpec{Arg: "model", Required: true},
			Voice:    FieldSpec{Arg: "voice", Required: true},
			Language: FieldSpec{Arg: "ttsLanguage"},
		},
		Notes: []string{"ttsProvider=\"ElevenLabs\"; voice is \"<voice id>-<model>\", for example UgBBYS2sOqTuMpoF3BR0-flash_v2_5; the app installs no ElevenLabs SDK and reads no ElevenLabs key"},
	},
}
