# KubeAgent

An agentic SRE first-responder for Kubernetes. When Prometheus fires an alert, KubeAgent automatically collects diagnostic context, generates a root cause analysis, and posts a remediation proposal to Slack — with a human approval step before anything touches the cluster.

## How it works

```
Prometheus alert fires
        │
        ▼
Alertmanager webhook → KubeAgent
        │
        ▼
Read-only kubectl diagnostics
(pod state, logs, events, recent deployments)
        │
        ▼
LLM generates RCA + remediation command
        │
        ▼
Slack message with Approve / Dismiss buttons
        │
        ▼
Engineer clicks Approve → command runs
```

The agent never takes a destructive action autonomously. Every proposed remediation sits behind a Slack approval gate.

## Folder structure

```
kubeagent/
├── cmd/
│   └── agent/          # Entrypoint — wires everything together
│       └── main.go
├── internal/
│   ├── agent/          # Core orchestration logic + config
│   ├── k8s/            # Read-only Kubernetes diagnostics client
│   ├── llm/            # Anthropic API client for RCA generation
│   ├── prometheus/     # Alertmanager webhook receiver
│   └── slack/          # Slack client — posts RCA + approval buttons
├── manifests/          # Kubernetes deployment manifests + RBAC
├── configs/            # Alertmanager routing config example
├── Dockerfile
└── README.md
```

## Alerts handled

| Alert | Diagnostics collected |
|---|---|
| `KubePodCrashLooping` | Pod state, container restart count, last termination reason, recent logs |
| `KubePodNotReady` | Pod conditions, events, container status |
| `HighLatency` | Pod events, recent deployment events, container logs |

## Setup

### Prerequisites

- Kubernetes cluster with Prometheus + Alertmanager
- Slack bot token with `chat:write` scope
- Anthropic API key

### Deploy

```bash
# Create secrets
kubectl create secret generic kubeagent-secrets \
  --namespace monitoring \
  --from-literal=slack-bot-token=xoxb-... \
  --from-literal=slack-channel-id=C... \
  --from-literal=anthropic-api-key=sk-ant-...

# Deploy
kubectl apply -f manifests/deployment.yaml
```

### Configure Alertmanager

Add the webhook route from `configs/alertmanager.yaml` to your Alertmanager config.

## Local development

```bash
export SLACK_BOT_TOKEN=xoxb-...
export SLACK_CHANNEL_ID=C...
export ANTHROPIC_API_KEY=sk-ant-...
export KUBECONFIG=~/.kube/config

go run ./cmd/agent
```

## RBAC

The agent uses a `ClusterRole` scoped to read-only verbs (`get`, `list`, `watch`) on pods, logs, events, deployments, and replicasets. It cannot modify any cluster resource.

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `WEBHOOK_PORT` | `8080` | Port to listen for Alertmanager webhooks |
| `KUBECONFIG` | in-cluster | Path to kubeconfig (leave unset in-cluster) |
| `SLACK_BOT_TOKEN` | required | Slack bot OAuth token |
| `SLACK_CHANNEL_ID` | `#incidents` | Channel to post RCA reports |
| `ANTHROPIC_API_KEY` | required | Anthropic API key for LLM RCA generation |
| `LLM_MODEL` | `claude-3-5-sonnet-20241022` | Model to use |
| `MAX_DIAG_DEPTH` | `3` | Max diagnostic recursion depth |
