package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

// ensureDebugDir creates a timestamped debug directory under /tmp/kind-audit and returns its path.
func ensureDebugDir() (string, error) {
	base := "/tmp/kind-audit"
	dir := filepath.Join(base, fmt.Sprintf("logging-debug-%d", time.Now().Unix()))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// dumpJSONToFile writes an object as indented JSON to the specified file.
func dumpJSONToFile(dir, prefix, name string, obj interface{}) error {
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.json", prefix, name))
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(obj)
}

// DumpDeployment writes the Deployment spec+status JSON to a debug file and returns the debug dir path.
func DumpDeployment(ctx context.Context, kube clientset.Interface, ns, name, prefix string) (string, error) {
	dir, err := ensureDebugDir()
	if err != nil {
		return "", err
	}
	dep, err := kube.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		// write an error marker file
		_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-deployment-error.txt", prefix)), []byte(err.Error()), 0644)
		return dir, err
	}
	_ = dumpJSONToFile(dir, prefix, "deployment", dep)
	return dir, nil
}

// DumpReplicaSetsForDeployment writes all ReplicaSets owned by the Deployment to debug files and returns their names.
func DumpReplicaSetsForDeployment(ctx context.Context, kube clientset.Interface, ns, depName, prefix string) ([]string, error) {
	dir, err := ensureDebugDir()
	if err != nil {
		return nil, err
	}
	rsList, err := kube.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	written := []string{}
	for _, rs := range rsList.Items {
		for _, owner := range rs.OwnerReferences {
			if strings.EqualFold(owner.Kind, "Deployment") && owner.Name == depName {
				name := rs.Name
				_ = dumpJSONToFile(dir, prefix, "replicaset-"+name, rs)
				written = append(written, name)
			}
		}
	}
	return written, nil
}

// DumpPodsForSelector writes pods matching a label selector to debug files and returns pod names written.
func DumpPodsForSelector(ctx context.Context, kube clientset.Interface, ns, labelSelector, prefix string) ([]string, error) {
	dir, err := ensureDebugDir()
	if err != nil {
		return nil, err
	}
	pods, err := kube.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, pod := range pods.Items {
		names = append(names, pod.Name)
		_ = dumpJSONToFile(dir, prefix, "pod-"+pod.Name, pod)
		// also write a human readable summary
		summary := fmt.Sprintf("Pod: %s\nPhase: %s\nLabels: %v\nContainers:\n", pod.Name, pod.Status.Phase, pod.Labels)
		for _, c := range pod.Spec.Containers {
			summary += fmt.Sprintf("  - %s\n    args=%v\n    env=%v\n", c.Name, c.Args, c.Env)
		}
		_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-pod-%s.txt", prefix, pod.Name)), []byte(summary), 0644)
	}
	return names, nil
}

// DumpEventsForObject writes recent events for the given object (by name/kind) to a text file and returns the file path.
func DumpEventsForObject(ctx context.Context, kube clientset.Interface, ns, involvedKind, involvedName, prefix string) (string, error) {
	dir, err := ensureDebugDir()
	if err != nil {
		return "", err
	}
	evList, err := kube.CoreV1().Events(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	out := []string{}
	for _, ev := range evList.Items {
		if ev.InvolvedObject.Kind == involvedKind && ev.InvolvedObject.Name == involvedName {
			out = append(out, fmt.Sprintf("%s %s %s %s %s\n", ev.LastTimestamp.String(), ev.Type, ev.Reason, ev.Source.Component, ev.Message))
		}
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-events-%s.txt", prefix, involvedName))
	_ = os.WriteFile(path, []byte(strings.Join(out, "")), 0644)
	return path, nil
}
