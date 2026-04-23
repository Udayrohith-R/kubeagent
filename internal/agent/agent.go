package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Udayrohith-R/kubeagent/internal/k8s"
	"github.com/Udayrohith-R/kubeagent/internal/llm"
	"github.com/Udayrohith-R/kubeagent/internal/prometheus"
	"github.com/Udayrohith-R/kubeagent/internal/slack"
)

// Agent is the top-level coordinator. It receives Prometheus alert webhooks,
// runs read-only Kubernetes diagnostics, generates an RCA summary via LLM,
// and posts a remediation proposal to Slack for human approval.
// Nothing touches the cluster without an engineer approving the Slack action.
type Agent struct {
	cfg      *Config
	slack    *slack.Client
	receiver *prometheus.WebhookReceiver
	k8s      *k8s.Client
	llm      *llm.Client
	log      *slog.Logger
}

func New(cfg *Config, slackClient *slack.Client, receiver *prometheus.WebhookReceiver, log *slog.Logger) *Agent {
	return &Agent{
		cfg:      cfg,
		slack:    slackClient,
		receiver: receiver,
		k8s:      k8s.NewClient(cfg.KubeconfigPath),
		llm:      llm.NewClient(cfg.AnthropicAPIKey, cfg.LLMModel),
		log:      log,
	}
}

func (a *Agent) Run(ctx context.Context) error {
	alerts, err := a.receiver.Start(ctx)
	if err != nil {
		return fmt.Errorf("starting webhook receiver: %w", err)
	}
	a.log.Info("webhook receiver started")

	for {
		select {
		case <-ctx.Done():
			a.log.Info("shutting down")
			return nil
		case alert, ok := <-alerts:
			if !ok {
				return nil
			}
			go a.handleAlert(ctx, alert)
		}
	}
}

func (a *Agent) handleAlert(ctx context.Context, alert prometheus.Alert) {
	log := a.log.With(
		"alert", alert.Name,
		"namespace", alert.Labels["namespace"],
		"pod", alert.Labels["pod"],
	)
	log.Info("handling alert")

	tctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	diag, err := a.runDiagnostics(tctx, alert)
	if err != nil {
		log.Error("diagnostics failed", "error", err)
		a.slack.PostError(alert, err)
		return
	}

	summary, err := a.llm.GenerateRCA(tctx, alert, diag)
	if err != nil {
		log.Error("LLM RCA generation failed", "error", err)
		a.slack.PostError(alert, err)
		return
	}

	if err := a.slack.PostRCAWithApproval(alert, diag, summary); err != nil {
		log.Error("failed to post to Slack", "error", err)
		return
	}

	log.Info("RCA posted to Slack, awaiting human approval")
}

func (a *Agent) runDiagnostics(ctx context.Context, alert prometheus.Alert) (*k8s.DiagnosticResult, error) {
	ns := alert.Labels["namespace"]
	pod := alert.Labels["pod"]

	if ns == "" || pod == "" {
		return nil, fmt.Errorf("alert missing namespace or pod label")
	}

	return a.k8s.Diagnose(ctx, k8s.DiagnosticRequest{
		Namespace: ns,
		PodName:   pod,
		AlertName: alert.Name,
		MaxDepth:  a.cfg.MaxDiagDepth,
	})
}
