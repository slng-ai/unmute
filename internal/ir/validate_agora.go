package ir

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	targetcap "github.com/slng-ai/unmute/internal/target"
)

// Keep this target closed: every nonzero field needs an explicit lowering.
// Reflection makes newly added IR fields fail rather than silently disappear.
func agoraUnsupported(value any, allowed ...string) []string {
	v := reflect.ValueOf(value)
	typ := v.Type()
	var fields []string
	for i := 0; i < v.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		field := v.Field(i)
		empty := field.IsZero() || ((field.Kind() == reflect.Map || field.Kind() == reflect.Slice) && field.Len() == 0)
		if !slices.Contains(allowed, name) && !empty {
			fields = append(fields, name)
		}
	}
	return fields
}

func validateAgoraTarget(agent *Agent, resolved Target, row *TargetValidation) {
	refuse := func(text string) { row.Errors = add(row.Errors, "agora target: "+text) }
	check := func(path string, value any, allowed ...string) {
		for _, field := range agoraUnsupported(value, allowed...) {
			refuse(path + "." + field + " is not supported; remove it")
		}
	}
	check("package", *agent, "manifest", "version", "name", "architecture", "entry_agent", "models", "listen", "agents", "conversation", "channels", "targets", "secrets")
	check("target", resolved, "name", "provider", "version", "sdk_language", "models", "manifest_models")
	if err := targetcap.CheckVersion(targetcap.Agora, resolved.Version); err != nil {
		refuse(err.Error())
	}
	if err := targetcap.CheckSDKLanguage(targetcap.Agora, resolved.SDKLanguage); err != nil {
		refuse(err.Error())
	}
	if agent.Architecture != ArchitectureCascade {
		refuse("architecture must be cascade")
	}
	if len(agent.Agents) != 1 {
		refuse("agents must contain exactly one entry")
	}
	entry, exists := agent.Agents[agent.EntryAgent]
	if !exists {
		refuse("entry_agent must name the single agent")
	}
	check("agents."+agent.EntryAgent, entry, "instructions", "model", "voice")
	if strings.TrimSpace(entry.Instructions) == "" {
		refuse("instructions must not be empty")
	}
	if agent.Name == "" {
		refuse("name is required")
	}
	if len(agent.Channels) != 1 {
		refuse("channels must contain one realtime_audio browser channel")
	}
	for _, name := range sortedKeys(agent.Channels) {
		channel := agent.Channels[name]
		check("channels."+name, channel, "kind")
		if channel.Kind != ChannelRealtimeAudio {
			refuse("channels." + name + " must be realtime_audio; phone routes are not supported")
		}
	}
	for _, secret := range agent.Secrets {
		if !slices.Contains([]string{"AGORA_APP_ID", "AGORA_APP_CERTIFICATE", "AGORA_AREA"}, secret) {
			refuse("secrets includes unused variable " + secret)
		}
	}
	if len(agent.Models) != 3 {
		refuse("models must contain exactly one listen, think and speak entry")
	}
	for _, name := range sortedKeys(agent.Models) {
		model := agent.Models[name]
		check("models."+name, model, "kind", "provider", "model", "voice", "language", "placement", "description")
		if model.Placement != PlacementAPI {
			refuse("models." + name + ".placement must be api")
		}
	}
	check("bindings", resolved.Models, "listen", "reason", "speak")
	if len(resolved.Models.Reason) != 1 || len(resolved.Models.Speak) != 1 {
		refuse("bindings require exactly one think and speak model")
	}
	validateBinding := func(path string, binding *Binding, role targetcap.Role, vendor, model string) {
		if binding == nil {
			refuse(path + " is required")
			return
		}
		allowed := []string{"provider", "model", "placement"}
		if role == targetcap.Listen {
			allowed = append(allowed, "language")
		}
		if role == targetcap.Speak {
			allowed = append(allowed, "voice")
		}
		check(path, *binding, allowed...)
		if binding.Placement != PlacementAPI {
			refuse(path + ".placement must be api")
		}
		if binding.Provider != vendor || binding.Model != model {
			refuse(fmt.Sprintf("%s requires provider %s and model %s", path, vendor, model))
		}
		if role == targetcap.Listen && binding.Language != "en" {
			refuse(path + ".language must be en")
		}
		if role == targetcap.Speak && strings.TrimSpace(binding.Voice) == "" {
			refuse(path + ".voice is required")
		}
	}
	validateBinding("models.listen", resolved.Models.Listen, targetcap.Listen, "deepgram", "nova-3")
	reason, reasonOK := resolved.Models.Reason[entry.Model]
	if !reasonOK {
		refuse("agent think binding is missing")
	} else {
		validateBinding("models.think", &reason, targetcap.Reason, "openai", targetcap.AgoraReasonModel)
	}
	speak, speakOK := resolved.Models.Speak[entry.Voice]
	if !speakOK {
		refuse("agent speak binding is missing")
	} else {
		validateBinding("models.speak", &speak, targetcap.Speak, "minimax", "speech_2_6_turbo")
	}
	c := agent.Conversation
	if c == nil {
		refuse("conversation requires a fixed greeting and explicit interruption.enabled")
		return
	}
	check("conversation", *c, "greeting", "interruption", "max_duration")
	if c.Greeting == nil || c.Greeting.SpeaksFirst != SpeaksFirstAgent || strings.TrimSpace(c.Greeting.Text) == "" {
		refuse("conversation.greeting requires speaks_first: agent and fixed text")
	}
	if c.Interruption == nil || c.Interruption.Enabled == nil {
		refuse("conversation.interruption.enabled must be explicitly true or false")
	} else {
		check("conversation.interruption", *c.Interruption, "enabled")
	}
	if c.MaxDuration != "" {
		duration, err := time.ParseDuration(string(c.MaxDuration))
		if err != nil || duration < time.Second || duration > 600*time.Second || duration%time.Second != 0 {
			refuse("conversation.max_duration must be a whole number of seconds between 1s and 600s")
		}
	}
}
