package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/slng/unmute/internal/generate"
	"github.com/spf13/cobra"
)

const (
	slngAPIKeyEnv      = "SLNG_API_KEY"
	slngAgentsEndpoint = "https://api.agents.slng.ai/v1/agents"
)

var slngApplyHTTPClient = &http.Client{Timeout: 30 * time.Second}

func newApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply <agent-dir> <target>",
		Short: "Apply an agent directory to a config-plane target.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := args[0]
			target := args[1]
			switch target {
			case "slng":
				return runApplySLNG(cmd, root)
			default:
				return fmt.Errorf("unsupported apply target %q", target)
			}
		},
	}
}

func runApplySLNG(cmd *cobra.Command, root string) error {
	apiKey := os.Getenv(slngAPIKeyEnv)
	if apiKey == "" {
		return fmt.Errorf("%s is not set in the environment", slngAPIKeyEnv)
	}

	result, err := renderSLNGPayload(root)
	if err != nil {
		return fmt.Errorf("apply %s slng: %w", root, err)
	}
	for _, warning := range result.Warnings {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning:", warning)
	}

	body, status, err := postSLNGAgent(cmd.Context(), slngApplyHTTPClient, apiKey, result.Content)
	if len(body) > 0 {
		if _, writeErr := cmd.OutOrStdout().Write(body); writeErr != nil {
			return writeErr
		}
		if !bytes.HasSuffix(body, []byte("\n")) {
			fmt.Fprintln(cmd.OutOrStdout())
		}
	}
	if err != nil {
		return fmt.Errorf("apply %s slng: %w", root, err)
	}
	if status < 200 || status > 299 {
		return fmt.Errorf("apply %s slng: SLNG API returned %s", root, http.StatusText(status))
	}
	return nil
}

func renderSLNGPayload(root string) (generate.SLNGResult, error) {
	input, err := loadSLNGInput(root)
	if err != nil {
		return generate.SLNGResult{}, err
	}
	return generate.GenerateSLNG(input)
}

func postSLNGAgent(ctx context.Context, client *http.Client, apiKey string, payload []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, slngAgentsEndpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return body, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}
