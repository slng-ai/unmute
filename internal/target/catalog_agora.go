package target

// AgoraReasonModel is the model supported by this target's first managed
// configuration. Verified against agora-agents 2.11.0's managed-model set,
// not against the generic OpenAI scaffold: account availability and a real
// voice call remain runtime checks.
const AgoraReasonModel = "gpt-4o-mini"

// These rows describe the hosted SDK configuration, not a local model process.
// IDs and account availability are ultimately checked by the hosted service.
var agoraCatalog = []Entry{
	{Framework: Agora, Role: Listen, Vendor: "deepgram", RequireModel: true, Verified: "2026-09-29", Docs: "https://github.com/AgoraIO/agora-agents-python"},
	{Framework: Agora, Role: Reason, Vendor: "openai", RequireModel: true, Notes: []string{"Managed model: " + AgoraReasonModel}, Verified: "2026-09-29", Docs: "https://github.com/AgoraIO/agora-agents-python"},
	{Framework: Agora, Role: Speak, Vendor: "minimax", RequireModel: true, RequireVoice: true, Verified: "2026-09-29", Docs: "https://github.com/AgoraIO/agora-agents-python"},
}
