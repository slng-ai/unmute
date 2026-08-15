package cli

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// LiveKit access token, minted by hand so `unmute dev` needs no LiveKit Go SDK
// (the whole token is a signed JSON object). Claim shape verified against
// livekit-api's AccessToken.to_jwt (livekit/api/access_token.py, 2026-07-19):
// HS256 over the api secret, camelCase claim keys, sub=identity, iss=api key,
// nbf/exp unix seconds (no iat), the video grant, and roomConfig agent dispatch.

type lkVideoGrant struct {
	Room           string `json:"room"`
	RoomJoin       bool   `json:"roomJoin"`
	CanPublish     bool   `json:"canPublish"`
	CanSubscribe   bool   `json:"canSubscribe"`
	CanPublishData bool   `json:"canPublishData"`
}

type lkAgentDispatch struct {
	AgentName string `json:"agentName"`
}

type lkRoomConfig struct {
	Agents []lkAgentDispatch `json:"agents"`
}

type lkClaims struct {
	Iss        string       `json:"iss"`
	Sub        string       `json:"sub"`
	Nbf        int64        `json:"nbf"`
	Exp        int64        `json:"exp"`
	Video      lkVideoGrant `json:"video"`
	RoomConfig lkRoomConfig `json:"roomConfig"`
}

// mintLiveKitToken returns a participant JWT for `room` that also carries an
// agent-dispatch entry for `agentName`. The agent has agent_name set (so it
// won't auto-dispatch), and token dispatch fires only when the room is first
// created — so the caller must use a fresh room name per token (C5).
func mintLiveKitToken(apiKey, apiSecret, room, identity, agentName string, now time.Time, ttl time.Duration) (string, error) {
	if apiKey == "" || apiSecret == "" {
		return "", errors.New("LIVEKIT_API_KEY and LIVEKIT_API_SECRET must be set")
	}
	claims := lkClaims{
		Iss: apiKey,
		Sub: identity,
		Nbf: now.Unix(),
		Exp: now.Add(ttl).Unix(),
		Video: lkVideoGrant{
			Room: room, RoomJoin: true,
			CanPublish: true, CanSubscribe: true, CanPublishData: true,
		},
		RoomConfig: lkRoomConfig{Agents: []lkAgentDispatch{{AgentName: agentName}}},
	}
	return signJWT(apiSecret, claims)
}

// signJWT is the whole of the hand-rolled JWT: marshal, fixed HS256 header,
// HMAC, base64url concat. Three tokens are minted in this package — participant,
// SIP admin, agent dispatch — and they differ only in their claims, so the
// signing lives here once. Key order in the header is irrelevant to
// verification.
func signJWT(apiSecret string, claims any) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := b64url([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + b64url(payload)
	mac := hmac.New(sha256.New, []byte(apiSecret))
	mac.Write([]byte(signingInput))
	return signingInput + "." + b64url(mac.Sum(nil)), nil
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// randomRoomName returns a fresh room name so each token creates a new room and
// its agent dispatch fires (C5). Crypto/rand keeps it collision-free without a
// clock or global counter.
func randomRoomName(prefix string) (string, error) {
	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s", prefix, b64url(buf[:])), nil
}
