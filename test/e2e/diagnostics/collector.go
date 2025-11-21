/*
Copyright 2025 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package diagnostics

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	kruisegameclientset "github.com/openkruise/kruise-game/pkg/client/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

// TestContext tracks metadata for a single test execution
type TestContext struct {
	Name       string    `json:"name"`
	StartTime  time.Time `json:"startTime"`
	EndTime    time.Time `json:"endTime"`
	Status     string    `json:"status"` // "passed", "failed", "skipped"
	FailureMsg string    `json:"failureMessage,omitempty"`
}

// ArtifactCollector collects diagnostic artifacts for E2E tests
type ArtifactCollector struct {
	testContext      *TestContext
	artifactRoot     string
	testDir          string
	kubeClient       kubernetes.Interface
	kruiseGameClient kruisegameclientset.Interface
	namespace        string
	gssLabelKey      string
	gssLabelValue    string
}

// NewArtifactCollector creates a new collector for a test
func NewArtifactCollector(testName string, artifactRoot string, kubeClient kubernetes.Interface, kruiseGameClient kruisegameclientset.Interface, namespace string) *ArtifactCollector {
	now := time.Now()
	testContext := &TestContext{
		Name:      testName,
		StartTime: now,
		Status:    "running",
	}

	// Create test-specific directory: e2e-run/tests/{test-name}/
	testDir := filepath.Join(artifactRoot, "tests", sanitizeFilename(testName))

	return &ArtifactCollector{
		testContext:      testContext,
		artifactRoot:     artifactRoot,
		testDir:          testDir,
		kubeClient:       kubeClient,
		kruiseGameClient: kruiseGameClient,
		namespace:        namespace,
	}
}

// MarkTestStart records test start (already done in NewArtifactCollector)
func (ac *ArtifactCollector) MarkTestStart() {
	ac.testContext.StartTime = time.Now()
	ac.testContext.Status = "running"
}

// MarkTestEnd records test completion
func (ac *ArtifactCollector) MarkTestEnd(passed bool, failureMsg string) {
	ac.testContext.EndTime = time.Now()
	if passed {
		ac.testContext.Status = "passed"
	} else {
		ac.testContext.Status = "failed"
		ac.testContext.FailureMsg = failureMsg
	}
}

// SetGameServerSetLabel sets the label selector for GameServerSet filtering
func (ac *ArtifactCollector) SetGameServerSetLabel(key, value string) {
	ac.gssLabelKey = key
	ac.gssLabelValue = value
}

// CollectAll collects all diagnostic artifacts for the test
func (ac *ArtifactCollector) CollectAll(auditLogPath string) error {
	if err := os.MkdirAll(ac.testDir, 0755); err != nil {
		return fmt.Errorf("failed to create test directory: %w", err)
	}

	// Save test metadata
	if err := ac.saveTestMetadata(); err != nil {
		fmt.Printf("Warning: failed to save test metadata: %v\n", err)
	}

	// Collect K8s resources
	if err := ac.collectK8sResources(); err != nil {
		fmt.Printf("Warning: failed to collect K8s resources: %v\n", err)
	}

	// Collect pod logs with time-based slicing
	if err := ac.collectPodLogs(); err != nil {
		fmt.Printf("Warning: failed to collect pod logs: %v\n", err)
	}

	// Collect audit logs filtered by test time range
	if auditLogPath != "" {
		if err := ac.CollectAuditLogs(auditLogPath); err != nil {
			fmt.Printf("Warning: failed to collect audit logs: %v\n", err)
		}
	}

	return nil
}

// saveTestMetadata saves test-info.json
func (ac *ArtifactCollector) saveTestMetadata() error {
	metadataPath := filepath.Join(ac.testDir, "test-info.json")
	data, err := json.MarshalIndent(ac.testContext, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metadataPath, data, 0644)
}

// collectK8sResources collects GameServerSet, Pods, Services, Events
func (ac *ArtifactCollector) collectK8sResources() error {
	resourcesDir := filepath.Join(ac.testDir, "k8s-resources")
	if err := os.MkdirAll(resourcesDir, 0755); err != nil {
		return err
	}

	ctx := context.TODO()

	// Collect GameServerSet
	if ac.kruiseGameClient != nil {
		gssList, err := ac.kruiseGameClient.GameV1alpha1().GameServerSets(ac.namespace).List(ctx, metav1.ListOptions{})
		if err == nil && len(gssList.Items) > 0 {
			// Save each GameServerSet separately for better readability
			for i := range gssList.Items {
				gss := &gssList.Items[i]
				filename := fmt.Sprintf("gameserverset-%s.yaml", gss.Name)
				if err := ac.saveResourceYAML(filepath.Join(resourcesDir, filename), gss); err != nil {
					fmt.Printf("Warning: failed to save GameServerSet %s: %v\n", gss.Name, err)
				}
			}
		}
	}

	// Collect Pods
	podList, err := ac.kubeClient.CoreV1().Pods(ac.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: ac.getLabelSelector(),
	})
	if err == nil {
		if err := ac.saveResourceYAML(filepath.Join(resourcesDir, "pods.yaml"), podList); err != nil {
			fmt.Printf("Warning: failed to save pods list: %v\n", err)
		}

		// Also save detailed pod info including annotations
		for i := range podList.Items {
			pod := &podList.Items[i]
			filename := fmt.Sprintf("pod-%s.yaml", pod.Name)
			if err := ac.saveResourceYAML(filepath.Join(resourcesDir, filename), pod); err != nil {
				fmt.Printf("Warning: failed to save pod %s: %v\n", pod.Name, err)
			}
		}
	}

	// Collect Services
	svcList, err := ac.kubeClient.CoreV1().Services(ac.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: ac.getLabelSelector(),
	})
	if err == nil {
		if err := ac.saveResourceYAML(filepath.Join(resourcesDir, "services.yaml"), svcList); err != nil {
			fmt.Printf("Warning: failed to save services list: %v\n", err)
		}
	}

	// Collect Events
	eventList, err := ac.kubeClient.CoreV1().Events(ac.namespace).List(ctx, metav1.ListOptions{})
	if err == nil {
		// Filter events by time range
		filteredEvents := &corev1.EventList{
			Items: []corev1.Event{},
		}
		start := ac.testContext.StartTime
		end := ac.testContext.EndTime.Add(5 * time.Second)
		for _, event := range eventList.Items {
			//nolint:staticcheck // Using embedded Time field for comparisons is intentional; metav1.Time does not expose After/Before for time.Time directly
			if event.LastTimestamp.Time.After(start) && event.LastTimestamp.Time.Before(end) {
				filteredEvents.Items = append(filteredEvents.Items, event)
			}
		}
		if err := ac.saveResourceYAML(filepath.Join(resourcesDir, "events.yaml"), filteredEvents); err != nil {
			fmt.Printf("Warning: failed to save events list: %v\n", err)
		}
	}

	return nil
}

// SaveTextArtifact writes textual diagnostics into the test's artifact directory
func (ac *ArtifactCollector) SaveTextArtifact(relPath string, content string) error {
	if ac == nil {
		return fmt.Errorf("artifact collector is nil")
	}
	target := filepath.Join(ac.testDir, relPath)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	return os.WriteFile(target, []byte(content), 0644)
}

// collectPodLogs collects logs from controller pods with time-based slicing
func (ac *ArtifactCollector) collectPodLogs() error {
	logsDir := filepath.Join(ac.testDir, "controller-logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		fmt.Printf("ERROR: failed to create controller-logs directory: %v\n", err)
		return err
	}

	ctx := context.TODO()

	// Try multiple possible namespaces for controller pods
	possibleNamespaces := []string{"kruise-game-system", "kruise-game", "kube-system"}
	possibleLabels := []string{
		"app=kruise-game-controller-manager",
		"control-plane=controller-manager",
		"app.kubernetes.io/name=kruise-game",
	}

	var foundPods []corev1.Pod
	var foundNamespace string
	var foundLabel string

	// Find controller pods by trying different namespace/label combinations
	for _, ns := range possibleNamespaces {
		for _, label := range possibleLabels {
			podList, err := ac.kubeClient.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
				LabelSelector: label,
			})
			if err != nil {
				continue
			}
			if len(podList.Items) > 0 {
				foundPods = podList.Items
				foundNamespace = ns
				foundLabel = label
				break
			}
		}
		if len(foundPods) > 0 {
			break
		}
	}

	if len(foundPods) == 0 {
		fmt.Printf("WARNING: No controller pods found. Tried namespaces: %v, labels: %v\n",
			possibleNamespaces, possibleLabels)
		return fmt.Errorf("no controller pods found")
	}

	fmt.Printf("Found %d controller pod(s) in namespace %s with label %s\n",
		len(foundPods), foundNamespace, foundLabel)

	// Collect logs from each pod
	successCount := 0
	for _, pod := range foundPods {
		logPath := filepath.Join(logsDir, fmt.Sprintf("%s.log", pod.Name))

		// Get logs with timestamps and time filtering
		// Add 10s buffer before test start to capture initialization
		sinceTime := metav1.NewTime(ac.testContext.StartTime.Add(-10 * time.Second))

		logOpts := &corev1.PodLogOptions{
			Timestamps: true,
			SinceTime:  &sinceTime,
		}

		fmt.Printf("Collecting logs from pod %s/%s (since %s)...\n",
			pod.Namespace, pod.Name, sinceTime.Format(time.RFC3339))

		req := ac.kubeClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, logOpts)
		logStream, err := req.Stream(ctx)
		if err != nil {
			fmt.Printf("ERROR: failed to stream logs for pod %s: %v\n", pod.Name, err)
			continue
		}

		// Write logs to file
		logFile, err := os.Create(logPath)
		if err != nil {
			_ = logStream.Close()
			fmt.Printf("ERROR: failed to create log file %s: %v\n", logPath, err)
			continue
		}

		// Copy log stream to file
		bytesWritten, err := logFile.ReadFrom(logStream)
		_ = logStream.Close()
		_ = logFile.Close()

		if err != nil {
			fmt.Printf("ERROR: failed to write logs for pod %s: %v\n", pod.Name, err)
			continue
		}

		fmt.Printf("Successfully collected %d bytes of logs from pod %s\n", bytesWritten, pod.Name)
		successCount++
	}

	if successCount == 0 {
		return fmt.Errorf("failed to collect logs from any of the %d controller pods", len(foundPods))
	}

	fmt.Printf("Successfully collected logs from %d/%d controller pods\n", successCount, len(foundPods))
	return nil
}

// saveResourceYAML saves K8s resource as YAML
func (ac *ArtifactCollector) saveResourceYAML(path string, obj interface{}) error {
	data, err := yaml.Marshal(obj)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// getLabelSelector returns label selector string for filtering resources
func (ac *ArtifactCollector) getLabelSelector() string {
	if ac.gssLabelKey != "" && ac.gssLabelValue != "" {
		return labels.SelectorFromSet(map[string]string{
			ac.gssLabelKey: ac.gssLabelValue,
		}).String()
	}
	return ""
}

// sanitizeFilename replaces characters that are not filesystem-safe
func sanitizeFilename(name string) string {
	// Replace spaces and special chars with underscores
	safe := ""
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			safe += string(r)
		} else {
			safe += "_"
		}
	}
	return safe
}

// GetTestDir returns the test-specific directory path
func (ac *ArtifactCollector) GetTestDir() string {
	return ac.testDir
}

// GetTimeRange returns the test time range
func (ac *ArtifactCollector) GetTimeRange() (time.Time, time.Time) {
	return ac.testContext.StartTime, ac.testContext.EndTime
}

// CollectAuditLogs collects audit logs for the test time range
// auditLogPath should be the full path to the kind cluster's audit.log
func (ac *ArtifactCollector) CollectAuditLogs(auditLogPath string) error {
	if auditLogPath == "" {
		return fmt.Errorf("audit log path not provided")
	}

	// Check if audit log exists
	if _, err := os.Stat(auditLogPath); os.IsNotExist(err) {
		fmt.Printf("WARNING: audit log not found at %s\n", auditLogPath)
		return err
	}

	auditDir := filepath.Join(ac.testDir, "audit")
	if err := os.MkdirAll(auditDir, 0755); err != nil {
		return fmt.Errorf("failed to create audit directory: %w", err)
	}

	// Read full audit log
	content, err := os.ReadFile(auditLogPath)
	if err != nil {
		return fmt.Errorf("failed to read audit log: %w", err)
	}

	// Filter audit events by time range and namespace
	// Add 5s buffer on each side to avoid missing related events
	startTime := ac.testContext.StartTime.Add(-5 * time.Second)
	endTime := ac.testContext.EndTime.Add(5 * time.Second)

	filtered, lineCount, err := filterAuditLogByTimeAndNamespace(
		content,
		startTime,
		endTime,
		ac.namespace,
	)
	if err != nil {
		fmt.Printf("WARNING: failed to filter audit log: %v\n", err)
		// Still save the full log as fallback
		filtered = content
	}

	// Save filtered audit log
	auditPath := filepath.Join(auditDir, "audit-filtered.log")
	if err := os.WriteFile(auditPath, filtered, 0644); err != nil {
		return fmt.Errorf("failed to write filtered audit log: %w", err)
	}

	fmt.Printf("Collected %d lines of audit logs (time range: %s to %s, namespace: %s)\n",
		lineCount, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339), ac.namespace)

	return nil
}

// filterAuditLogByTimeAndNamespace filters audit log entries by time range and namespace
func filterAuditLogByTimeAndNamespace(content []byte, start, end time.Time, namespace string) ([]byte, int, error) {
	lines := strings.Split(string(content), "\n")
	var filtered []string
	lineCount := 0

	for _, line := range lines {
		if line == "" {
			continue
		}

		// Parse JSON to extract timestamp and namespace
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			// Skip malformed lines
			continue
		}

		// Extract timestamp (format: "2024-11-03T01:23:45.123456Z")
		timestampStr, ok := event["requestReceivedTimestamp"].(string)
		if !ok {
			timestampStr, ok = event["stageTimestamp"].(string)
			if !ok {
				continue
			}
		}

		timestamp, err := time.Parse(time.RFC3339Nano, timestampStr)
		if err != nil {
			continue
		}

		// Check if within time range
		if timestamp.Before(start) || timestamp.After(end) {
			continue
		}

		// Check namespace if specified
		if namespace != "" {
			objRef, ok := event["objectRef"].(map[string]interface{})
			if ok {
				ns, _ := objRef["namespace"].(string)
				if ns != namespace {
					continue
				}
			}
		}

		filtered = append(filtered, line)
		lineCount++
	}

	return []byte(strings.Join(filtered, "\n")), lineCount, nil
}
