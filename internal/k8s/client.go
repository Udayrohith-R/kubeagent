package k8s

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// Client wraps the Kubernetes client. All operations are strictly read-only.
// We never write to the cluster — remediation actions go through Slack approval first.
type Client struct {
	cs *kubernetes.Clientset
}

func NewClient(kubeconfigPath string) *Client {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		rules.ExplicitPath = kubeconfigPath
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules,
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		// In-cluster fallback
		cfg, err = clientcmd.BuildConfigFromFlags("", "")
		if err != nil {
			panic(fmt.Sprintf("cannot build kubeconfig: %v", err))
		}
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		panic(fmt.Sprintf("cannot create k8s client: %v", err))
	}
	return &Client{cs: cs}
}

type DiagnosticRequest struct {
	Namespace string
	PodName   string
	AlertName string
	MaxDepth  int
}

type DiagnosticResult struct {
	PodName      string
	Namespace    string
	Phase        string
	Conditions   []string
	RecentEvents []string
	ContainerStatuses []ContainerStatus
	Logs         map[string]string // container name -> last N lines
	RecentDeploymentEvents []string
	CollectedAt  time.Time
}

type ContainerStatus struct {
	Name         string
	Ready        bool
	RestartCount int32
	State        string
	LastState    string
}

func (c *Client) Diagnose(ctx context.Context, req DiagnosticRequest) (*DiagnosticResult, error) {
	result := &DiagnosticResult{
		PodName:     req.PodName,
		Namespace:   req.Namespace,
		Logs:        make(map[string]string),
		CollectedAt: time.Now().UTC(),
	}

	pod, err := c.cs.CoreV1().Pods(req.Namespace).Get(ctx, req.PodName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get pod %s/%s: %w", req.Namespace, req.PodName, err)
	}

	result.Phase = string(pod.Status.Phase)

	for _, cond := range pod.Status.Conditions {
		result.Conditions = append(result.Conditions,
			fmt.Sprintf("%s=%s (reason: %s)", cond.Type, cond.Status, cond.Reason))
	}

	for _, cs := range pod.Status.ContainerStatuses {
		cst := ContainerStatus{
			Name:         cs.Name,
			Ready:        cs.Ready,
			RestartCount: cs.RestartCount,
		}
		if cs.State.Running != nil {
			cst.State = "Running"
		} else if cs.State.Waiting != nil {
			cst.State = fmt.Sprintf("Waiting: %s — %s", cs.State.Waiting.Reason, cs.State.Waiting.Message)
		} else if cs.State.Terminated != nil {
			cst.State = fmt.Sprintf("Terminated: exit %d — %s", cs.State.Terminated.ExitCode, cs.State.Terminated.Reason)
		}
		if cs.LastTerminationState.Terminated != nil {
			lt := cs.LastTerminationState.Terminated
			cst.LastState = fmt.Sprintf("exit %d at %s — %s", lt.ExitCode, lt.FinishedAt.Format(time.RFC3339), lt.Reason)
		}
		result.ContainerStatuses = append(result.ContainerStatuses, cst)

		// Fetch last 100 lines of logs per container — read-only
		logLines := int64(100)
		logReq := c.cs.CoreV1().Pods(req.Namespace).GetLogs(req.PodName, &corev1.PodLogOptions{
			Container: cs.Name,
			TailLines: &logLines,
		})
		logBytes, logErr := logReq.DoRaw(ctx)
		if logErr == nil {
			result.Logs[cs.Name] = string(logBytes)
		}
	}

	events, err := c.cs.CoreV1().Events(req.Namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s", req.PodName),
	})
	if err == nil {
		for _, ev := range events.Items {
			result.RecentEvents = append(result.RecentEvents,
				fmt.Sprintf("[%s] %s: %s", ev.LastTimestamp.Format(time.RFC3339), ev.Reason, ev.Message))
		}
	}

	// Also pull recent ReplicaSet/Deployment events to spot bad rollouts
	depEvents, err := c.cs.CoreV1().Events(req.Namespace).List(ctx, metav1.ListOptions{
		FieldSelector: "involvedObject.kind=Deployment",
	})
	if err == nil {
		for _, ev := range depEvents.Items {
			if strings.Contains(ev.Message, req.PodName) || ev.Type == corev1.EventTypeWarning {
				result.RecentDeploymentEvents = append(result.RecentDeploymentEvents,
					fmt.Sprintf("[%s] %s/%s: %s", ev.LastTimestamp.Format(time.RFC3339),
						ev.InvolvedObject.Name, ev.Reason, ev.Message))
			}
		}
	}

	return result, nil
}
