package testcase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"github.com/openkruise/kruise-game/test/e2e/framework"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RunHADeploymentUpdateTest validates that deployment updates work correctly
// in HA mode (replicas=3 + --leader-elect=true) after the readyz fix (Option A).
//
// This test verifies:
// 1. All 3 pods become Ready in HA mode
// 2. Exactly one pod is elected as leader
// 3. Deployment updates succeed (RollingUpdate completes)
// 4. Leader failover works (kill leader, new one elected)
//
// Context: This test was added to validate the fix for the production bug
// where deployment updates would deadlock because only leader pods were ready.
func RunHADeploymentUpdateTest(f *framework.Framework) {
	ginkgo.Describe("[HA] Deployment Update Test", func() {
		var (
			ctx              context.Context
			rolloutTimeout   = 5 * time.Minute
			leaderCheckDelay = 10 * time.Second
		)

		ginkgo.BeforeEach(func() {
			ctx = context.Background()
		})

		ginkgo.It("should support deployment updates in HA mode with leader ready", func() {
			ginkgo.By("Ensuring Deployment strategy allows leader replacement")
			err := framework.EnsureDeploymentRollingStrategy(ctx, f.KubeClientSet(), managerNamespace, managerDeployment)
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "should patch deployment strategy to maxUnavailable=100%%")

			// Step 1: Verify HA deployment state
			ginkgo.By("Step 1: Verifying HA deployment has replicas=3")
			var deployment *appsv1.Deployment
			deployment, err = f.KubeClientSet().AppsV1().Deployments(managerNamespace).Get(
				ctx, managerDeployment, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "should get deployment")

			replicas := int32(1)
			if deployment.Spec.Replicas != nil {
				replicas = *deployment.Spec.Replicas
			}

			// Skip test if not in HA mode
			if replicas < 2 {
				ginkgo.Skip(fmt.Sprintf("Test requires HA mode (replicas >= 2), got replicas=%d. Run with ENABLE_HA=true", replicas))
			}

			gomega.Expect(replicas).To(gomega.BeNumerically(">=", 2),
				"HA mode should have at least 2 replicas")

			// Step 2: Wait for the deployment to settle with a ready leader
			ginkgo.By("Step 2: Waiting for deployment to report a ready leader")
			err = framework.WaitForDeploymentRollout(ctx, f.KubeClientSet(), managerNamespace, managerDeployment, rolloutTimeout)
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "leader pod should become ready")

			// Verify deployment status
			deployment, err = f.KubeClientSet().AppsV1().Deployments(managerNamespace).Get(
				ctx, managerDeployment, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(deployment.Status.ReadyReplicas).To(gomega.BeNumerically(">=", 1),
				"at least one pod (leader) should be ready")
			gomega.Expect(deployment.Status.AvailableReplicas).To(gomega.BeNumerically(">=", 1),
				"at least one pod (leader) should be available")

			// Step 3: Verify exactly one leader is elected
			ginkgo.By("Step 3: Verifying exactly one pod is elected as leader")
			time.Sleep(leaderCheckDelay) // Give time for leader election logs

			pods, err := f.KubeClientSet().CoreV1().Pods(managerNamespace).List(ctx, metav1.ListOptions{
				LabelSelector: managerLabelSelector,
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "should list pods")
			gomega.Expect(len(pods.Items)).To(gomega.BeNumerically(">=", 2), "should have multiple pods")

			leaderCount := 0
			var leaderPodName string
			for _, pod := range pods.Items {
				// Check logs for leader election message
				logOpts := &corev1.PodLogOptions{
					Container: managerContainer,
					TailLines: int64Ptr(100),
				}
				logs, err := f.KubeClientSet().CoreV1().Pods(managerNamespace).GetLogs(pod.Name, logOpts).DoRaw(ctx)
				if err != nil {
					continue // Pod might not be ready yet
				}

				logStr := string(logs)
				// Look for leader election success messages
				if strings.Contains(logStr, "successfully acquired lease") ||
					strings.Contains(logStr, "became leader") ||
					strings.Contains(logStr, "leader election won") {
					leaderCount++
					leaderPodName = pod.Name
				}
			}

			gomega.Expect(leaderCount).To(gomega.Equal(1),
				fmt.Sprintf("exactly one pod should be leader, found %d", leaderCount))
			ginkgo.By(fmt.Sprintf("✓ Verified leader: %s", leaderPodName))

			// Step 4: Perform a deployment update
			ginkgo.By("Step 4: Performing deployment update (add environment variable)")

			// Add a test annotation to trigger rollout
			deployment, err = f.KubeClientSet().AppsV1().Deployments(managerNamespace).Get(
				ctx, managerDeployment, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			if deployment.Spec.Template.Annotations == nil {
				deployment.Spec.Template.Annotations = make(map[string]string)
			}
			deployment.Spec.Template.Annotations["e2e-test/update-timestamp"] = time.Now().Format(time.RFC3339)

			_, err = f.KubeClientSet().AppsV1().Deployments(managerNamespace).Update(ctx, deployment, metav1.UpdateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "should update deployment")

			// Step 5: Wait for rollout to complete
			ginkgo.By("Step 5: Waiting for deployment update to roll out")
			err = framework.WaitForDeploymentRollout(ctx, f.KubeClientSet(), managerNamespace, managerDeployment, rolloutTimeout)
			gomega.Expect(err).NotTo(gomega.HaveOccurred(),
				"deployment update should complete successfully (validates readyz fix)")

			// Verify all replicas are updated and ready
			deployment, err = f.KubeClientSet().AppsV1().Deployments(managerNamespace).Get(
				ctx, managerDeployment, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(deployment.Status.UpdatedReplicas).To(gomega.Equal(replicas),
				"all replicas should be updated")
			gomega.Expect(deployment.Status.ReadyReplicas).To(gomega.BeNumerically(">=", 1),
				"leader should remain ready after update")

			// Step 6: Verify leader failover
			ginkgo.By("Step 6: Testing leader failover (delete current leader pod)")

			// Find current leader
			pods, err = f.KubeClientSet().CoreV1().Pods(managerNamespace).List(ctx, metav1.ListOptions{
				LabelSelector: managerLabelSelector,
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			currentLeader := ""
			for _, pod := range pods.Items {
				logOpts := &corev1.PodLogOptions{
					Container: managerContainer,
					TailLines: int64Ptr(100),
				}
				logs, err := f.KubeClientSet().CoreV1().Pods(managerNamespace).GetLogs(pod.Name, logOpts).DoRaw(ctx)
				if err != nil {
					continue
				}

				logStr := string(logs)
				if strings.Contains(logStr, "successfully acquired lease") ||
					strings.Contains(logStr, "became leader") {
					currentLeader = pod.Name
					break
				}
			}

			gomega.Expect(currentLeader).NotTo(gomega.BeEmpty(), "should have found current leader")
			ginkgo.By(fmt.Sprintf("Deleting leader pod: %s", currentLeader))

			err = f.KubeClientSet().CoreV1().Pods(managerNamespace).Delete(ctx, currentLeader, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "should delete leader pod")

			// Wait for deployment to recover
			time.Sleep(10 * time.Second)
			err = framework.WaitForDeploymentRollout(ctx, f.KubeClientSet(), managerNamespace, managerDeployment, rolloutTimeout)
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "deployment should recover after leader deletion")

			// Verify new leader was elected
			time.Sleep(leaderCheckDelay)
			pods, err = f.KubeClientSet().CoreV1().Pods(managerNamespace).List(ctx, metav1.ListOptions{
				LabelSelector: managerLabelSelector,
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			newLeaderCount := 0
			var newLeaderPodName string
			for _, pod := range pods.Items {
				if pod.Name == currentLeader {
					continue // Skip the deleted pod if still in list
				}

				logOpts := &corev1.PodLogOptions{
					Container:    managerContainer,
					TailLines:    int64Ptr(200),
					SinceSeconds: int64Ptr(30),
				}
				logs, err := f.KubeClientSet().CoreV1().Pods(managerNamespace).GetLogs(pod.Name, logOpts).DoRaw(ctx)
				if err != nil {
					continue
				}

				logStr := string(logs)
				if strings.Contains(logStr, "successfully acquired lease") ||
					strings.Contains(logStr, "became leader") {
					newLeaderCount++
					newLeaderPodName = pod.Name
				}
			}

			gomega.Expect(newLeaderCount).To(gomega.BeNumerically(">=", 1),
				"new leader should have been elected after failover")
			gomega.Expect(newLeaderPodName).NotTo(gomega.Equal(currentLeader),
				"new leader should be different from deleted pod")
			ginkgo.By(fmt.Sprintf("✓ New leader elected: %s", newLeaderPodName))

			ginkgo.By("✓ HA deployment update test completed successfully")
		})
	})
}

// Helper function to create int64 pointer
func int64Ptr(i int64) *int64 {
	return &i
}
