package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// liveKitCreds are the LiveKit server credentials the web dev client needs. URL
// is the room server; the key/secret sign the participant token (C5). Console
// mode reads none of these unless a binding routes through Inference (C2).
type liveKitCreds struct {
	URL       string
	APIKey    string
	APISecret string
}

// liveKitCredsFromEnv reads the creds from a merged env slice (ambient + .env,
// as devChildEnv builds it). Last value wins so a repo .env overrides a stale
// ambient value.
func liveKitCredsFromEnv(env []string) liveKitCreds {
	get := func(key string) string {
		val := ""
		for _, kv := range env {
			if v, ok := strings.CutPrefix(kv, key+"="); ok {
				val = v
			}
		}
		return val
	}
	return liveKitCreds{
		URL:       get("LIVEKIT_URL"),
		APIKey:    get("LIVEKIT_API_KEY"),
		APISecret: get("LIVEKIT_API_SECRET"),
	}
}

// missing lists which required creds are empty, for a preflight error (C7).
func (c liveKitCreds) missing() []string {
	var out []string
	if c.URL == "" {
		out = append(out, "LIVEKIT_URL")
	}
	if c.APIKey == "" {
		out = append(out, "LIVEKIT_API_KEY")
	}
	if c.APISecret == "" {
		out = append(out, "LIVEKIT_API_SECRET")
	}
	return out
}

// liveKitTokenHandler serves GET /api/token → {url, token, room}. Each request
// mints a fresh room so the token's agent dispatch fires at room creation (C5,
// V5); the browser connects to url with token and the agent joins that room.
func liveKitTokenHandler(creds liveKitCreds, agentName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		room, err := randomRoomName("unmute")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		identity, err := randomRoomName("user")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		token, err := mintLiveKitToken(creds.APIKey, creds.APISecret, room, identity, agentName, time.Now(), 30*time.Minute)
		if err != nil {
			http.Error(w, fmt.Sprintf("mint token: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"url":   creds.URL,
			"token": token,
			"room":  room,
		})
	}
}
