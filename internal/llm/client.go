package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Udayrohith-R/kubeagent/internal/k8s"
	"github.com/Udayrohith-R/kubeagent/internal/prometheus"
)

const anthropicAPIURL = "https://api.anthropic.com/v1/messages"

type Client struct {
	apiKey  string
	model   string
	httpCli *http.Client
}

func NewClient(apiKey, model string) *Client {
	return &Client{
		apiKey: apiKey,
		model:  model,
		httpCli: &http.Client{Timeout: 60 * time.Second},
	}
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type request struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system"`
	Messages  []message `json:"messages"`
}

type response struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

// GenerateRCA sends diagnostic context to the LLM and gets back a structured
// root cause analysis with a proposed remediation command.
func (c *Client) GenerateRCA(ctx context.Context, alert prometheus.Alert, diag *k8s.DiagnosticResult) (string, error) {
	system := `You are a senior SRE assistant. You receive Kubernetes diagnostic data and produce 
a concise, structured root cause analysis. 

Your output MUST follow this format exactly:

## 🔍 Root Cause Analysis

**Alert:** <alert name and severity>
**Pod:** <namespace/pod>
**Likely Cause:** <one sentence>

## Evidence
- <bullet of key evidence from logs/events>
- <bullet>

## Proposed Remediation
` + "```" + `bash
<single safe kubectl or helm command — read-only preferred, or a rollback command>
` + "```" + `

**Risk:** Low / Medium / High  
**Confidence:** <percentage>

---
⚠️ This action requires manual approval before execution.`

	userMsg := buildDiagnosticPrompt(alert, diag)

	reqBody := request{
		Model:     c.model,
		MaxTokens: 1024,
		System:    system,
		Messages:  []message{{Role: "user", Content: userMsg}},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshalling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPIURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return "", fmt.Errorf("calling LLM API: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LLM API returned %d: %s", resp.StatusCode, string(respBytes))
	}

	var result response
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty response from LLM")
	}

	return result.Content[0].Text, nil
}

func buildDiagnosticPrompt(alert prometheus.Alert, diag *k8s.DiagnosticResult) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "## Alert\nName: %s\nSeverity: %s\nSummary: %s\n\n",
		alert.Name, alert.Labels["severity"], alert.Annotations["summary"])

	fmt.Fprintf(&sb, "## Pod State\nPhase: %s\n", diag.Phase)

	if len(diag.Conditions) > 0 {
		sb.WriteString("Conditions:\n")
		for _, c := range diag.Conditions {
			fmt.Fprintf(&sb, "  - %s\n", c)
		}
	}

	if len(diag.ContainerStatuses) > 0 {
		sb.WriteString("\n## Container Statuses\n")
		for _, cs := range diag.ContainerStatuses {
			fmt.Fprintf(&sb, "- %s: ready=%v restarts=%d state=%s\n",
				cs.Name, cs.Ready, cs.RestartCount, cs.State)
			if cs.LastState != "" {
				fmt.Fprintf(&sb, "  last termination: %s\n", cs.LastState)
			}
		}
	}

	if len(diag.RecentEvents) > 0 {
		sb.WriteString("\n## Recent Pod Events (last 10)\n")
		events := diag.RecentEvents
		if len(events) > 10 {
			events = events[len(events)-10:]
		}
		for _, e := range events {
			fmt.Fprintf(&sb, "  %s\n", e)
		}
	}

	for container, logs := range diag.Logs {
		if logs == "" {
			continue
		}
		lines := strings.Split(logs, "\n")
		if len(lines) > 30 {
			lines = lines[len(lines)-30:]
		}
		fmt.Fprintf(&sb, "\n## Logs: %s (last 30 lines)\n```\n%s\n```\n", container, strings.Join(lines, "\n"))
	}

	return sb.String()
}
