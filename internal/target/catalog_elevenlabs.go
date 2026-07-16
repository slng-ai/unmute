package target

// ElevenLabs matrix rows (call-less: the managed driver builds
// conversation_config JSON, no code injection). ElevenLabs runs only its own
// voices (SCHEMA.md 6.2 role table); listen and turn are integrated roles and
// carry no rows here. reason has no rows either: the supported-LLM list plus
// the custom-endpoint exception stay unvalidated identities (D10).

var elevenlabsCatalog = []Entry{
	{Framework: ElevenLabs, Role: Speak, Vendor: "elevenlabs", Aliases: []string{"eleven_labs"},
		Verified: "2026-07-15", Docs: "https://elevenlabs.io/docs/agents-platform (SCHEMA.md 6.2 role table)"},
}
