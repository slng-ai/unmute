package target

// Deepgram matrix rows (call-less: the future bridge driver forwards provider
// names into the Settings JSON, no code injection). Facts from the SCHEMA.md
// 6.2 role table, research pass 2026-07-15: listen accepts Deepgram models
// only; speak accepts Deepgram plus a fixed third-party list. Vendors are our
// canonical spellings; aliases carry Deepgram's own enum spellings.

const deepgramRoleDocs = "https://developers.deepgram.com/docs/voice-agent (SCHEMA.md 6.2 role table)"

var deepgramCatalog = []Entry{
	{Framework: Deepgram, Role: Listen, Vendor: "deepgram",
		Verified: "2026-07-15", Docs: deepgramRoleDocs},
	{Framework: Deepgram, Role: Speak, Vendor: "deepgram",
		Verified: "2026-07-15", Docs: deepgramRoleDocs},
	{Framework: Deepgram, Role: Speak, Vendor: "elevenlabs", Aliases: []string{"eleven_labs"},
		Verified: "2026-07-15", Docs: deepgramRoleDocs},
	{Framework: Deepgram, Role: Speak, Vendor: "cartesia",
		Verified: "2026-07-15", Docs: deepgramRoleDocs},
	{Framework: Deepgram, Role: Speak, Vendor: "openai", Aliases: []string{"open_ai"},
		Verified: "2026-07-15", Docs: deepgramRoleDocs},
	{Framework: Deepgram, Role: Speak, Vendor: "aws_polly",
		Verified: "2026-07-15", Docs: deepgramRoleDocs},
}
