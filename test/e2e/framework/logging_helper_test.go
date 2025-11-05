package framework

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestBackupRestoreDeploymentTemplate(t *testing.T) {
	// Test backup/restore JSON serialization roundtrip
	original := corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "manager",
					Image: "test-image:latest",
					Args:  []string{"--log-format=console", "--enable-leader-election"},
				},
			},
		},
	}

	// Simulate backup
	templateBytes, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal template: %v", err)
	}

	// Simulate restore
	var restored corev1.PodTemplateSpec
	if err := json.Unmarshal(templateBytes, &restored); err != nil {
		t.Fatalf("unmarshal template: %v", err)
	}

	// Verify roundtrip
	if restored.Spec.Containers[0].Name != "manager" {
		t.Errorf("expected container name 'manager', got %s", restored.Spec.Containers[0].Name)
	}
	if len(restored.Spec.Containers[0].Args) != 2 {
		t.Errorf("expected 2 args, got %d", len(restored.Spec.Containers[0].Args))
	}
}

func TestPatchDeploymentArgsMerge(t *testing.T) {
	// Test patch construction (actual patching requires live cluster)
	newArgs := []string{"--log-format=json", "--enable-leader-election"}
	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []map[string]interface{}{
						{
							"name": "manager",
							"args": newArgs,
						},
					},
				},
			},
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		t.Fatalf("marshal patch: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(patchBytes, &decoded); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}

	// Verify patch structure
	spec := decoded["spec"].(map[string]interface{})
	template := spec["template"].(map[string]interface{})
	tSpec := template["spec"].(map[string]interface{})
	containers := tSpec["containers"].([]interface{})
	firstContainer := containers[0].(map[string]interface{})
	args := firstContainer["args"].([]interface{})

	if len(args) != 2 {
		t.Errorf("expected 2 args in patch, got %d", len(args))
	}
	if args[0] != "--log-format=json" {
		t.Errorf("expected first arg '--log-format=json', got %v", args[0])
	}
}
