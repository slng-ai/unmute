package cli

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplySLNG_postsGeneratedPayload(t *testing.T) { // V55, V56, V57, V58
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := run(t, "init", dir); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Setenv(slngAPIKeyEnv, "sk-slng")

	expected, err := renderSLNGPayload(dir)
	if err != nil {
		t.Fatal(err)
	}

	var gotBody []byte
	called := 0
	withApplyClient(t, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called++
		if req.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", req.Method)
		}
		if req.URL.String() != slngAgentsEndpoint {
			t.Errorf("url = %s, want %s", req.URL.String(), slngAgentsEndpoint)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer sk-slng" {
			t.Errorf("Authorization = %q", got)
		}
		if got := req.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		var err error
		gotBody, err = io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response(201, `{"id":"agent_123"}`), nil
	})})

	output, err := run(t, "apply", dir, "slng")
	if err != nil {
		t.Fatalf("apply slng: %v\n%s", err, output)
	}
	if called != 1 {
		t.Fatalf("POST calls = %d, want 1", called)
	}
	if !bytes.Equal(gotBody, expected.Content) {
		t.Fatalf("posted payload drift:\ngot:\n%s\nwant:\n%s", gotBody, expected.Content)
	}
	if !bytes.Contains(gotBody, []byte(`{{user_name}}`)) {
		t.Fatalf("posted payload missing simple user_name placeholder:\n%s", gotBody)
	}
	if bytes.Contains(gotBody, []byte(`{{ user_name }}`)) {
		t.Fatalf("posted payload contains backend-invalid spaced placeholder:\n%s", gotBody)
	}
	if !strings.Contains(output, `{"id":"agent_123"}`) {
		t.Fatalf("response body missing from output:\n%s", output)
	}
	if !strings.Contains(output, `warning: tool "lookup_order" uses http handler ref "orders" and is omitted`) {
		t.Fatalf("SLNG payload warnings should go to stderr before apply:\n%s", output)
	}
	if _, err := os.Stat(filepath.Join(dir, "targets", "slng")); !os.IsNotExist(err) {
		t.Fatalf("apply should not write or require compiled SLNG JSON, stat err %v", err)
	}
}

func TestApplySLNG_requiresAPIKeyBeforeLoading(t *testing.T) { // V56
	t.Setenv(slngAPIKeyEnv, "")
	withApplyClient(t, &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		t.Fatal("apply should fail before any POST when SLNG_API_KEY is missing")
		return nil, nil
	})})

	_, err := run(t, "apply", filepath.Join(t.TempDir(), "missing-agent"), "slng")
	if err == nil {
		t.Fatal("expected missing API key error")
	}
	if !strings.Contains(err.Error(), "SLNG_API_KEY is not set") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplySLNG_printsNon2xxBodyAndReturnsError(t *testing.T) { // V58, V60
	dir := filepath.Join(t.TempDir(), "support-bot")
	if _, err := run(t, "init", dir); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Setenv(slngAPIKeyEnv, "sk-slng")
	withApplyClient(t, &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return response(400, `{"error":"bad request"}`), nil
	})})

	output, err := run(t, "apply", dir, "slng")
	if err == nil {
		t.Fatal("expected non-2xx error")
	}
	if !strings.Contains(output, `{"error":"bad request"}`) {
		t.Fatalf("non-2xx response body missing from output:\n%s", output)
	}
	if !strings.Contains(err.Error(), "SLNG API returned") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApply_rejectsUnsupportedTarget(t *testing.T) { // V59
	_, err := run(t, "apply", filepath.Join(t.TempDir(), "agent"), "pipecat")
	if err == nil {
		t.Fatal("expected unsupported target error")
	}
	if !strings.Contains(err.Error(), `unsupported apply target "pipecat"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApply_noDeployShapedFlags(t *testing.T) { // V59
	_, err := run(t, "apply", filepath.Join(t.TempDir(), "agent"), "slng", "--runtime", "prod")
	if err == nil {
		t.Fatal("expected unknown flag error")
	}
	if !strings.Contains(err.Error(), "unknown flag: --runtime") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func withApplyClient(t *testing.T, client *http.Client) {
	t.Helper()
	original := slngApplyHTTPClient
	slngApplyHTTPClient = client
	t.Cleanup(func() {
		slngApplyHTTPClient = original
	})
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
