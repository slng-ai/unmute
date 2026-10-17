package cli

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestMintLiveKitToken (V6): the hand-minted JWT decodes to the claims a LiveKit
// server expects — iss/sub, the video grant, and the roomConfig agent dispatch —
// and its HS256 signature verifies against the secret. No LiveKit SDK, no Python.
func TestMintLiveKitToken(t *testing.T) {
	const key, secret, room, id, agent = "APIabc", "s3cr3t", "room-xyz", "user-1", "remy-dev"
	now := time.Unix(1_700_000_000, 0)

	tok, err := mintLiveKitToken(key, secret, room, id, agent, now, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d segments, want 3", len(parts))
	}

	// Signature: recompute HS256 over header.payload and compare (proves a
	// server would accept it).
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil)); want != parts[2] {
		t.Errorf("signature mismatch:\n got %s\nwant %s", parts[2], want)
	}

	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var c lkClaims
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	if c.Iss != key || c.Sub != id {
		t.Errorf("iss/sub = %q/%q, want %q/%q", c.Iss, c.Sub, key, id)
	}
	if c.Nbf != now.Unix() || c.Exp != now.Add(15*time.Minute).Unix() {
		t.Errorf("nbf/exp = %d/%d, want %d/%d", c.Nbf, c.Exp, now.Unix(), now.Add(15*time.Minute).Unix())
	}
	if !c.Video.RoomJoin || c.Video.Room != room || !c.Video.CanPublish || !c.Video.CanSubscribe {
		t.Errorf("video grant = %+v", c.Video)
	}
	if len(c.RoomConfig.Agents) != 1 || c.RoomConfig.Agents[0].AgentName != agent {
		t.Errorf("roomConfig agents = %+v, want one dispatch for %q", c.RoomConfig.Agents, agent)
	}

	// The camelCase keys the server actually reads must be present verbatim.
	for _, want := range []string{`"roomJoin":true`, `"canPublishData":true`, `"roomConfig"`, `"agentName":"remy-dev"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("payload missing %q; got %s", want, raw)
		}
	}
}

func TestMintLiveKitTokenRequiresCreds(t *testing.T) {
	if _, err := mintLiveKitToken("", "s", "r", "i", "a", time.Now(), time.Minute); err == nil {
		t.Error("missing api key must error")
	}
}

func TestRandomRoomNameUnique(t *testing.T) {
	a, err := randomRoomName("unmute")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := randomRoomName("unmute")
	if a == b {
		t.Errorf("room names collided: %q", a)
	}
	if !strings.HasPrefix(a, "unmute-") {
		t.Errorf("room name %q missing prefix", a)
	}
}
