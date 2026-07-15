package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"time"

	"github.com/slng/unmute/internal/generate"
	"github.com/slng/unmute/internal/ir"
	"github.com/slng/unmute/internal/target"
	"github.com/spf13/cobra"
)

// providerBaseURL is the config-plane base for each managed provider. A package
// var so the apply integration test can point it at an httptest server.
// ponytail: only ElevenLabs is wired today; add a row when the next managed
// driver (Vapi) lands.
var providerBaseURL = map[ir.Provider]string{
	ir.ProviderElevenLabs: "https://api.elevenlabs.io",
}

var (
	capturePattern = regexp.MustCompile(`\{\{capture:([^}]+)\}\}`)
	envPattern     = regexp.MustCompile(`\{\{env:([A-Za-z_][A-Za-z0-9_]*)\}\}`)
)

func newApplyCmd() *cobra.Command {
	var names []string
	cmd := &cobra.Command{
		Use:   "apply <package-dir>",
		Short: "Apply a v1 package to its managed (config-plane) targets.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApply(cmd, args[0], names)
		},
	}
	cmd.Flags().StringSliceVar(&names, "target", nil, "target instance name (repeatable; default: all)")
	return cmd
}

func runApply(cmd *cobra.Command, dir string, names []string) error {
	agent, targets, err := loadPackage(dir, names)
	if err != nil {
		return fmt.Errorf("apply %s: %w", dir, err)
	}
	caps := target.Default()
	for _, resolved := range targets {
		artifact, err := generate.Generate(agent, resolved, caps)
		if err != nil {
			return fmt.Errorf("apply %s: %w", dir, err)
		}
		if artifact.Kind != generate.ManagedTarget || artifact.Apply == nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "%s: code target — use `unmute compile`\n", resolved.Name)
			continue
		}
		if err := applyPlan(cmd, resolved, artifact); err != nil {
			return fmt.Errorf("apply %s: %w", resolved.Name, err)
		}
	}
	return nil
}

// applyPlan executes the ordered, branch-aware ApplyPlan: each step pushes a
// payload to the provider with the env-named credential in the auth header
// (never in the body, C9), captures the created resource id, and substitutes
// {{capture:...}} / {{env:...}} placeholders in later steps. Forwarded bindings
// and driver notes are reported so what was sent is always inspectable (V10).
func applyPlan(cmd *cobra.Command, resolved ir.Target, artifact generate.Artifact) error {
	plan := artifact.Apply
	base, ok := providerBaseURL[resolved.Provider]
	if !ok {
		return fmt.Errorf("no config-plane base URL for provider %q", resolved.Provider)
	}
	credential := os.Getenv(plan.CredentialEnv)
	if credential == "" {
		return fmt.Errorf("credential env %s is not set", plan.CredentialEnv)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	captured := map[string]string{}
	out := cmd.OutOrStdout()

	for i, step := range plan.Steps {
		endpoint, err := substitute(step.Endpoint, captured)
		if err != nil {
			return err
		}
		body, err := substitute(string(step.Body), captured)
		if err != nil {
			return err
		}
		endpointURL := base + endpoint
		if step.Branch != "" {
			endpointURL += "?" + url.Values{"branch_id": {step.Branch}}.Encode()
		}
		req, err := http.NewRequest(step.Method, endpointURL, bytes.NewReader([]byte(body)))
		if err != nil {
			return err
		}
		req.Header.Set("xi-api-key", credential)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("step %d %s %s: %w", i+1, step.Method, endpoint, err)
		}
		payload, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			return fmt.Errorf("step %d %s %s: %s: %s", i+1, step.Method, endpoint, resp.Status, string(payload))
		}
		if step.CaptureID != "" {
			id, err := captureAgentID(payload)
			if err != nil {
				return fmt.Errorf("step %d capture %q: %w", i+1, step.CaptureID, err)
			}
			captured[step.CaptureID] = id
		}
		branchNote := ""
		if step.Branch != "" {
			branchNote = " [branch=" + step.Branch + "]"
		}
		fmt.Fprintf(out, "%s step %d: %s %s%s ok\n", resolved.Name, i+1, step.Method, endpoint, branchNote)
	}

	// Report what was forwarded and the reconcile choices; never judge values (V10).
	for _, note := range artifact.Notes.Notes {
		fmt.Fprintf(out, "%s: %s\n", resolved.Name, note)
	}
	for _, fb := range artifact.Notes.ForwardedBindings {
		for _, p := range fb.Params {
			fmt.Fprintf(out, "%s: forwarded %s.%s param %s=%v (sent as-is, not validated)\n",
				resolved.Name, fb.Role, fb.Profile, p.Name, p.Value)
		}
	}
	return nil
}

// substitute resolves {{capture:name}} (a prior step's captured id) and
// {{env:NAME}} (a value from the environment) placeholders. Both must resolve;
// an unresolved reference is an error, not a silent empty value.
func substitute(s string, captured map[string]string) (string, error) {
	var missing string
	s = capturePattern.ReplaceAllStringFunc(s, func(m string) string {
		name := capturePattern.FindStringSubmatch(m)[1]
		if id, ok := captured[name]; ok {
			return id
		}
		if missing == "" {
			missing = "unresolved capture reference " + name + " (target created after this step?)"
		}
		return m
	})
	s = envPattern.ReplaceAllStringFunc(s, func(m string) string {
		name := envPattern.FindStringSubmatch(m)[1]
		if v := os.Getenv(name); v != "" {
			return v
		}
		if missing == "" {
			missing = "environment variable " + name + " is not set"
		}
		return m
	})
	if missing != "" {
		return "", fmt.Errorf("%s", missing)
	}
	return s, nil
}

func captureAgentID(payload []byte) (string, error) {
	var body struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if body.AgentID == "" {
		return "", fmt.Errorf("response has no agent_id")
	}
	return body.AgentID, nil
}
