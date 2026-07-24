package testcase

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/test/e2e/client"
	"github.com/openkruise/kruise-game/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

func RunServiceQualityScaleDownTest(f *framework.Framework) {
	ginkgo.Describe("ServiceQuality scale down", func() {
		ginkgo.It("deletes GameServers with WaitToBeDeleted ServiceQuality first", func() {
			// 1. Deploy GameServerSet with 3 replicas
			// We need a stable GSS for this test
			gss, err := f.DeployGameServerSet()
			gomega.Expect(err).To(gomega.BeNil(), "Failed to deploy GameServerSet")

			err = f.ExpectGssCorrect(gss, []int{0, 1, 2})
			gomega.Expect(err).To(gomega.BeNil(), "Failed to verify initial GSS state")

			// 2. Define ServiceQuality
			// This ServiceQuality checks for existence of /tmp/wait-to-delete
			// If exists, it sets opsState to WaitToBeDeleted
			probeAction := gameKruiseV1alpha1.ServiceQualityAction{
				State: true,
				GameServerSpec: gameKruiseV1alpha1.GameServerSpec{
					OpsState: gameKruiseV1alpha1.WaitToDelete,
				},
			}

			sq := gameKruiseV1alpha1.ServiceQuality{
				Name:          "wait-to-delete",
				ContainerName: client.GameContainerName,
				Probe: corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						Exec: &corev1.ExecAction{
							Command: []string{"/bin/sh", "-c", "cat /tmp/wait-to-delete"},
						},
					},
					PeriodSeconds: 1,
				},
				ServiceQualityAction: []gameKruiseV1alpha1.ServiceQualityAction{probeAction},
			}

			// 3. Patch GSS with ServiceQuality
			gss, err = f.PatchGssSpec(map[string]interface{}{
				"serviceQualities": []gameKruiseV1alpha1.ServiceQuality{sq},
			})
			gomega.Expect(err).To(gomega.BeNil(), "Failed to patch GSS with ServiceQuality")
			gomega.Expect(f.WaitForGssObservedGeneration(gss.Generation)).To(gomega.BeNil(), "Timeout waiting for GSS generation update")

			// 4. Trigger ServiceQuality on pod 1
			// We create the file /tmp/wait-to-delete in pod 1
			targetPodName := fmt.Sprintf("%s-1", gss.GetName())
			// Ensure pod is ready before exec
			err = f.WaitForPodRunning(targetPodName)
			gomega.Expect(err).To(gomega.BeNil(), "Timeout waiting for pod %s to be ready", targetPodName)

			// Use /bin/sh -c for robustness
			err = execCommandInPod(f, targetPodName, client.GameContainerName, []string{"/bin/sh", "-c", "touch /tmp/wait-to-delete"})
			gomega.Expect(err).To(gomega.BeNil())

			// Verify file exists
			err = execCommandInPod(f, targetPodName, client.GameContainerName, []string{"/bin/sh", "-c", "ls /tmp/wait-to-delete"})
			gomega.Expect(err).To(gomega.BeNil())

			// 5. Wait for opsState to update to WaitToBeDeleted
			err = f.WaitForGsOpsStateUpdate(targetPodName, string(gameKruiseV1alpha1.WaitToDelete))
			gomega.Expect(err).To(gomega.BeNil(), "Timeout waiting for OpsState of %s to become WaitToBeDeleted", targetPodName)

			// 6. Scale down to 2 replicas
			// Since pod 1 is WaitToBeDeleted, it should be prioritized for deletion
			// Normally scale down removes highest ordinal (2), but priority should override this
			gss, err = f.GameServerScale(gss, 2, nil)
			gomega.Expect(err).To(gomega.BeNil(), "Failed to scale down GSS")

			// 7. Verify correct pods remain (should be 0 and 2)
			// Pod 1 should be gone
			err = f.ExpectGssCorrect(gss, []int{0, 2})
			gomega.Expect(err).To(gomega.BeNil(), "Failed to verify correct scaling behavior (expected pods 0 and 2)")
		})
	})
}

// execCommandInPod executes a command in the specified container
func execCommandInPod(f *framework.Framework, podName, containerName string, cmd []string) error {
	req := f.KubeClientSet().CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(client.Namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Container: containerName,
		Command:   cmd,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, scheme.ParameterCodec)

	// Create SPDY executor
	exec, err := remotecommand.NewSPDYExecutor(f.RestConfig, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to init executor: %v", err)
	}

	// Calculate a context with timeout (increased to 30s)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Execute synchronously
	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	if err != nil {
		return fmt.Errorf("failed to execute command %v in pod %s: %v, stdout: %s, stderr: %s", cmd, podName, err, stdout.String(), stderr.String())
	}

	return nil
}
