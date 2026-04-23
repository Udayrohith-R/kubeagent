package slack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Udayrohith-R/kubeagent/internal/k8s"
	"github.com/Udayrohith-R/kubeagent/internal/prometheus"
)

const slackAPIURL = "https://slack.com/api/chat.postMessage"

type Client struct {
	token     string
	channelID string
	httpCli   *http.Client
}

func NewClient(token, channelID string) *Client {
	return &Client{
		token:     token,
		channelID: channelID,
		httpCli:   &http.Client{Timeout: 10 * time.Second},
	}
}

type slackBlock struct {
	Type string `json:"type"`
	Text *slackText `json:"text,omitempty"`
	Elements []slackElement `json:"elements,omitempty"`
}

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type slackElement struct {
	Type     string     `json:"type"`
	Text     *slackText `json:"text,omitempty"`
	Style    string     `json:"style,omitempty"`
	ActionID string     `json:"action_id,omitempty"`
	Value    string     `json:"value,omitempty"`
}

type slackPayload struct {
	Channel string       `json:"channel"`
	Blocks  []slackBlock `json:"blocks"`
}

// PostRCAWithApproval posts the LLM-generated RCA and a two-button
// (Approve / Dismiss) action to the configured Slack channel.
// No remediation command runs until an engineer clicks Approve.
func (c *Client) PostRCAWithApproval(alert prometheus.Alert, diag *k8s.DiagnosticResult, rcaSummary string) error {
	headerText := fmt.Sprintf("🚨 *Alert: %s* | `%s/%s`",
		alert.Name, diag.Namespace, diag.PodName)

	statsText := fmt.Sprintf("*Pod Phase:* `%s`   *Restarts:* %d   *Collected:* %s",
		diag.Phase, totalRestarts(diag), diag.CollectedAt.Format(time.RFC3339))

	blocks := []slackBlock{
		{Type: "header", Text: &slackText{Type: "plain_text", Text: "KubeAgent — RCA Report"}},
		{Type: "section", Text: &slackText{Type: "mrkdwn", Text: headerText}},
		{Type: "section", Text: &slackText{Type: "mrkdwn", Text: statsText}},
		{Type: "divider"},
		{Type: "section", Text: &slackText{Type: "mrkdwn", Text: rcaSummary}},
		{Type: "divider"},
		{
			Type: "section",
			Text: &slackText{
				Type: "mrkdwn",
				Text: "⚠️ *Approve to execute the proposed remediation command. This action cannot be undone.*",
			},
		},
		{
			Type: "actions",
			Elements: []slackElement{
				{
					Type:     "button",
					Text:     &slackText{Type: "plain_text", Text: "✅ Approve"},
					Style:    "primary",
					ActionID: "approve_remediation",
					Value:    alert.Name,
				},
				{
					Type:     "button",
					Text:     &slackText{Type: "plain_text", Text: "❌ Dismiss"},
					Style:    "danger",
					ActionID: "dismiss_remediation",
					Value:    alert.Name,
				},
			},
		},
	}

	return c.post(slackPayload{Channel: c.channelID, Blocks: blocks})
}

func (c *Client) PostError(alert prometheus.Alert, err error) {
	msg := fmt.Sprintf("⚠️ KubeAgent failed to process alert *%s*: `%v`", alert.Name, err)
	_ = c.post(slackPayload{
		Channel: c.channelID,
		Blocks: []slackBlock{
			{Type: "section", Text: &slackText{Type: "mrkdwn", Text: msg}},
		},
	})
}

func (c *Client) post(payload slackPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, slackAPIURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return fmt.Errorf("posting to Slack: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Slack API returned %d", resp.StatusCode)
	}
	return nil
}

func totalRestarts(diag *k8s.DiagnosticResult) int32 {
	var total int32
	for _, cs := range diag.ContainerStatuses {
		total += cs.RestartCount
	}
	return total
}
