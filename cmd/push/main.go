// Command push is a result delivery / notifier that sends reports to various targets.
// It reads a PushInput JSON from stdin and outputs a PushOutput JSON.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// PushInput is the input schema for the push tool.
type PushInput struct {
	Target string     `json:"target"` // stdout, github, slack, file
	Result PushResult `json:"result"`
	Config PushConfig `json:"config"`
}

// PushResult holds the content to deliver.
type PushResult struct {
	Goal   string `json:"goal"`
	Diff   string `json:"diff"`
	Report string `json:"report"`
}

// PushConfig holds target-specific configuration.
type PushConfig struct {
	Repo       string `json:"repo"`        // owner/repo for github
	PR         int    `json:"pr"`          // PR/issue number for github
	WebhookURL string `json:"webhook_url"` // webhook URL for slack
	FilePath   string `json:"file_path"`   // output file path for file target
}

// PushOutput is the output of the push tool.
type PushOutput struct {
	OK     bool   `json:"ok"`
	Target string `json:"target"`
	Err    string `json:"err,omitempty"`
}

func main() {
	var input PushInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	var result PushOutput
	result.Target = input.Target

	switch input.Target {
	case "stdout":
		result = pushStdout(input)
	case "github":
		result = pushGitHub(input)
	case "slack":
		result = pushSlack(input)
	case "file":
		result = pushFile(input)
	default:
		result = PushOutput{
			OK:     false,
			Target: input.Target,
			Err:    fmt.Sprintf("unknown target: %s", input.Target),
		}
	}

	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func formatReport(r PushResult) string {
	var sb strings.Builder

	if r.Goal != "" {
		sb.WriteString("## Goal\n\n")
		sb.WriteString(r.Goal)
		sb.WriteString("\n\n")
	}

	if r.Report != "" {
		sb.WriteString("## Report\n\n")
		sb.WriteString(r.Report)
		sb.WriteString("\n\n")
	}

	if r.Diff != "" {
		sb.WriteString("## Diff\n\n```diff\n")
		sb.WriteString(r.Diff)
		sb.WriteString("\n```\n")
	}

	return sb.String()
}

func pushStdout(input PushInput) PushOutput {
	report := formatReport(input.Result)
	fmt.Fprint(os.Stderr, report)
	return PushOutput{OK: true, Target: "stdout"}
}

func pushGitHub(input PushInput) PushOutput {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return PushOutput{OK: false, Target: "github", Err: "GITHUB_TOKEN not set"}
	}

	if input.Config.Repo == "" {
		return PushOutput{OK: false, Target: "github", Err: "config.repo is required"}
	}
	if input.Config.PR == 0 {
		return PushOutput{OK: false, Target: "github", Err: "config.pr is required"}
	}

	// Build comment body
	body := formatReport(input.Result)

	// POST /repos/{owner}/{repo}/issues/{pr}/comments
	url := fmt.Sprintf("https://api.github.com/repos/%s/issues/%d/comments",
		input.Config.Repo, input.Config.PR)

	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return PushOutput{OK: false, Target: "github", Err: fmt.Sprintf("marshal: %v", err)}
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		return PushOutput{OK: false, Target: "github", Err: fmt.Sprintf("new request: %v", err)}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return PushOutput{OK: false, Target: "github", Err: fmt.Sprintf("http: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return PushOutput{
			OK:     false,
			Target: "github",
			Err:    fmt.Sprintf("github API %d: %s", resp.StatusCode, string(respBody)),
		}
	}

	return PushOutput{OK: true, Target: "github"}
}

func pushSlack(input PushInput) PushOutput {
	if input.Config.WebhookURL == "" {
		return PushOutput{OK: false, Target: "slack", Err: "config.webhook_url is required"}
	}

	report := formatReport(input.Result)
	payload, err := json.Marshal(map[string]string{"text": report})
	if err != nil {
		return PushOutput{OK: false, Target: "slack", Err: fmt.Sprintf("marshal: %v", err)}
	}

	req, err := http.NewRequest("POST", input.Config.WebhookURL, bytes.NewReader(payload))
	if err != nil {
		return PushOutput{OK: false, Target: "slack", Err: fmt.Sprintf("new request: %v", err)}
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return PushOutput{OK: false, Target: "slack", Err: fmt.Sprintf("http: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return PushOutput{
			OK:     false,
			Target: "slack",
			Err:    fmt.Sprintf("slack webhook %d: %s", resp.StatusCode, string(respBody)),
		}
	}

	return PushOutput{OK: true, Target: "slack"}
}

func pushFile(input PushInput) PushOutput {
	if input.Config.FilePath == "" {
		return PushOutput{OK: false, Target: "file", Err: "config.file_path is required"}
	}

	report := formatReport(input.Result)

	// Ensure parent directory exists
	dir := input.Config.FilePath
	if idx := strings.LastIndex(dir, "/"); idx >= 0 {
		if err := os.MkdirAll(dir[:idx], 0755); err != nil {
			return PushOutput{OK: false, Target: "file", Err: fmt.Sprintf("mkdir: %v", err)}
		}
	}

	if err := os.WriteFile(input.Config.FilePath, []byte(report), 0644); err != nil {
		return PushOutput{OK: false, Target: "file", Err: fmt.Sprintf("write: %v", err)}
	}

	return PushOutput{OK: true, Target: "file"}
}
