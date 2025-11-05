package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
)

// BackupDeploymentTemplate returns JSON bytes of the Deployment's PodTemplate for later rollback.
func BackupDeploymentTemplate(ctx context.Context, kube clientset.Interface, ns, name string) ([]byte, error) {
	dep, err := kube.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get deployment %s/%s: %w", ns, name, err)
	}
	templateBytes, err := json.Marshal(dep.Spec.Template)
	if err != nil {
		return nil, fmt.Errorf("marshal deployment template: %w", err)
	}
	return templateBytes, nil
}

// PatchDeploymentArgs merges or replaces args for the specified container in a Deployment.
// If containerName is empty, patches the first container.
// newArgs completely replaces the container's args array.
func PatchDeploymentArgs(ctx context.Context, kube clientset.Interface, ns, name string, containerName string, newArgs []string) error {
	dep, err := kube.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get deployment %s/%s: %w", ns, name, err)
	}

	containers := dep.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		return fmt.Errorf("deployment %s/%s has no containers", ns, name)
	}

	targetIdx := 0
	if containerName != "" {
		found := false
		for i, c := range containers {
			if c.Name == containerName {
				targetIdx = i
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("container %s not found in deployment %s/%s", containerName, ns, name)
		}
	}

	// Construct strategic merge patch
	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []map[string]interface{}{
						{
							"name": containers[targetIdx].Name,
							"args": newArgs,
						},
					},
				},
			},
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal patch: %w", err)
	}

	_, err = kube.AppsV1().Deployments(ns).Patch(ctx, name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("patch deployment %s/%s: %w", ns, name, err)
	}
	return nil
}

// EnsureDeploymentRollingStrategy enforces RollingUpdate strategy with maxUnavailable=100% to avoid deadlocks
// when only the elected leader reports readiness.
func EnsureDeploymentRollingStrategy(ctx context.Context, kube clientset.Interface, ns, name string) error {
	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"strategy": map[string]interface{}{
				"type": "RollingUpdate",
				"rollingUpdate": map[string]interface{}{
					"maxUnavailable": "100%",
					"maxSurge":       0,
				},
			},
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal strategy patch: %w", err)
	}

	_, err = kube.AppsV1().Deployments(ns).Patch(ctx, name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("patch deployment strategy %s/%s: %w", ns, name, err)
	}
	return nil
}

// WaitForDeploymentRollout waits until the Deployment reaches Available condition.
// On timeout, writes debug snapshots to /tmp/kind-audit/logging-debug-<ts>/ for post-mortem analysis.
func WaitForDeploymentRollout(ctx context.Context, kube clientset.Interface, ns, name string, timeout time.Duration) error {
	var lastSummary string
	pollErr := wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (done bool, err error) {
		dep, err := kube.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, nil // transient error, keep polling
		}

		// Collect rollout summary on each poll
		lastSummary = fmt.Sprintf("generation=%d observedGeneration=%d replicas=%d updatedReplicas=%d availableReplicas=%d unavailableReplicas=%d readyReplicas=%d",
			dep.Generation,
			dep.Status.ObservedGeneration,
			*dep.Spec.Replicas,
			dep.Status.UpdatedReplicas,
			dep.Status.AvailableReplicas,
			dep.Status.UnavailableReplicas,
			dep.Status.ReadyReplicas,
		)

		// Check if rollout is complete:
		// 1. ObservedGeneration matches Generation (controller saw the update)
		// 2. UpdatedReplicas == Replicas (all pods running new version)
		// 3. At least one replica is Ready / Available (leader elected). In HA mode
		//    only the leader reports Ready, so we do not require all replicas.
		// 4. Available condition is True
		if dep.Status.ObservedGeneration != dep.Generation {
			return false, nil // Controller hasn't seen latest spec yet
		}

		if dep.Status.UpdatedReplicas != *dep.Spec.Replicas {
			return false, nil // Not all replicas updated yet
		}

		if dep.Status.ReadyReplicas < 1 || dep.Status.AvailableReplicas < 1 {
			return false, nil // no ready/available replica yet
		}

		// Finally check Available condition
		for _, cond := range dep.Status.Conditions {
			if cond.Type == appsv1.DeploymentAvailable && cond.Status == corev1.ConditionTrue {
				return true, nil // Rollout complete!
			}
		}
		return false, nil
	})

	// On timeout, dump diagnostics
	if pollErr != nil {
		prefix := fmt.Sprintf("timeout-%d", time.Now().Unix())
		_, _ = DumpDeployment(ctx, kube, ns, name, prefix)
		_, _ = DumpReplicaSetsForDeployment(ctx, kube, ns, name, prefix)

		// Get label selector from deployment
		dep, err := kube.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err == nil && dep.Spec.Selector != nil {
			selector := metav1.FormatLabelSelector(dep.Spec.Selector)
			_, _ = DumpPodsForSelector(ctx, kube, ns, selector, prefix)
		}
		_, _ = DumpEventsForObject(ctx, kube, ns, "Deployment", name, prefix)

		// Write timeout summary
		summaryPath := filepath.Join("/tmp/kind-audit", "timeout-summary.txt")
		summaryContent := fmt.Sprintf("Deployment: %s/%s\nTimestamp: %s\nLast rollout summary:\n  %s\nError: %v\n",
			ns, name, time.Now().Format(time.RFC3339), lastSummary, pollErr)
		_ = os.WriteFile(summaryPath, []byte(summaryContent), 0644)

		return fmt.Errorf("deployment rollout timeout (%s): %w", lastSummary, pollErr)
	}

	return nil
}

// CollectManagerLogs writes logs from all pods matching labelSelector to outPath (one file per pod or combined).
// since specifies how far back to collect logs (e.g., 60s).
func CollectManagerLogs(ctx context.Context, kube clientset.Interface, ns string, labelSelector string, since time.Duration, outPath string) error {
	pods, err := kube.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("list pods with selector %s in %s: %w", labelSelector, ns, err)
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no pods found with selector %s in namespace %s", labelSelector, ns)
	}

	// Ensure output directory exists
	dir := filepath.Dir(outPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	outFile, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create output file %s: %w", outPath, err)
	}
	defer outFile.Close()

	sinceSeconds := int64(since.Seconds())
	for _, pod := range pods.Items {
		req := kube.CoreV1().Pods(ns).GetLogs(pod.Name, &corev1.PodLogOptions{
			SinceSeconds: &sinceSeconds,
		})
		stream, err := req.Stream(ctx)
		if err != nil {
			// Log error but continue with other pods
			fmt.Fprintf(outFile, "# Error getting logs for pod %s: %v\n", pod.Name, err)
			continue
		}
		fmt.Fprintf(outFile, "# Logs from pod: %s\n", pod.Name)
		_, _ = io.Copy(outFile, stream)
		stream.Close()
		fmt.Fprintln(outFile) // blank line between pods
	}
	return nil
}

// RestoreDeploymentTemplate restores the Deployment's PodTemplate from a backup JSON.
func RestoreDeploymentTemplate(ctx context.Context, kube clientset.Interface, ns, name string, templateJSON []byte) error {
	var podTemplate corev1.PodTemplateSpec
	if err := json.Unmarshal(templateJSON, &podTemplate); err != nil {
		return fmt.Errorf("unmarshal template JSON: %w", err)
	}

	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"template": podTemplate,
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal restore patch: %w", err)
	}

	_, err = kube.AppsV1().Deployments(ns).Patch(ctx, name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("restore deployment %s/%s: %w", ns, name, err)
	}
	return nil
}
