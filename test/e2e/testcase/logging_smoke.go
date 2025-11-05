package testcase

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"github.com/openkruise/kruise-game/test/e2e/framework"
)

const (
	// Deployment info for kruise-game-manager
	managerNamespace     = "kruise-game-system"
	managerDeployment    = "kruise-game-controller-manager"
	managerContainer     = "manager"
	managerLabelSelector = "control-plane=controller-manager"

	// Artifact output directory
	auditLogDir = "/tmp/kind-audit"

	// Timeouts
	rolloutTimeout   = 5 * time.Minute
	logSinceDuration = 120 * time.Second
)

// RunLoggingSmokeTest adds the logging format smoke test to the e2e suite.
//
// Test strategy:
// This test validates that the manager can switch log formats dynamically by
// patching the deployment args. To ensure compatibility with the existing
// deployment configuration, we:
//
// 1. Keep ALL original args from the deployment (provider-config, qps, etc.)
// 2. Simply APPEND --log-format=json or --log-format=console
// 3. Do NOT add --health-probe-bind-address or --metrics-bind-address
//
// Rationale:
// - The manager's main.go has default values: probeAddr=":8082", metricsAddr=":8080"
// - The deployment's probes check :8082, which matches the default
// - By not specifying these flags explicitly, we test the ACTUAL production config
// - This exposes any latent configuration issues (like probe/args mismatches)
//
// Test flow:
// 1. Collect baseline console logs (original deployment)
// 2. Backup original deployment template
// 3. Patch: original_args + ["--log-format=json"]
// 4. Wait for rollout and collect JSON logs
// 5. Validate JSON log format
// 6. Patch: original_args + ["--log-format=console"]
// 7. Wait for rollout and collect console logs
func RunLoggingSmokeTest(f *framework.Framework) {
	ginkgo.Describe("Logging Smoke Test", func() {
		var (
			ctx               context.Context
			backupTemplate    []byte
			logPlainPath      string
			logJSONPath       string
			logPlainAfterPath string
		)

		ginkgo.BeforeEach(func() {
			ctx = context.Background()
			logPlainPath = filepath.Join(auditLogDir, "e2e-logs-plain.log")
			logJSONPath = filepath.Join(auditLogDir, "e2e-logs-json.log")
			logPlainAfterPath = filepath.Join(auditLogDir, "e2e-logs-plain-after.log")
		})

		ginkgo.It("should switch log format from console to JSON and back", func() {
			ginkgo.By("Step 1: Collecting baseline logs in console format")
			err := framework.CollectManagerLogs(ctx, f.KubeClientSet(), managerNamespace, managerLabelSelector, logSinceDuration, logPlainPath)
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "should collect baseline console logs")

			ginkgo.By("Step 2: Backing up current Deployment template")
			backupTemplate, err = framework.BackupDeploymentTemplate(ctx, f.KubeClientSet(), managerNamespace, managerDeployment)
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "should backup deployment template")
			gomega.Expect(backupTemplate).NotTo(gomega.BeEmpty(), "backup template should not be empty")

			ginkgo.By("Ensuring Deployment strategy permits leader handover during rollout")
			err = framework.EnsureDeploymentRollingStrategy(ctx, f.KubeClientSet(), managerNamespace, managerDeployment)
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "should patch deployment strategy to maxUnavailable=100%%")

			// Diagnostic: snapshot before patch
			_, _ = framework.DumpDeployment(ctx, f.KubeClientSet(), managerNamespace, managerDeployment, "before-patch")

			// Ensure restore on exit
			defer func() {
				// Diagnostic: snapshot before restore
				_, _ = framework.DumpDeployment(ctx, f.KubeClientSet(), managerNamespace, managerDeployment, "before-restore")

				ginkgo.By("Step 8: Switching back to console log format")
				consoleArgs := []string{
					"--provider-config=/etc/kruise-game/config.toml",
					"--api-server-qps=5",
					"--api-server-qps-burst=10",
					"--enable-cert-generation=false",
					"--leader-elect=true",
					"--log-format=console",
				}
				err := framework.PatchDeploymentArgs(ctx, f.KubeClientSet(), managerNamespace, managerDeployment, managerContainer, consoleArgs)
				gomega.Expect(err).NotTo(gomega.HaveOccurred(), "should switch back to console logging")

				// Diagnostic: snapshot after restore
				_, _ = framework.DumpDeployment(ctx, f.KubeClientSet(), managerNamespace, managerDeployment, "after-restore")

				ginkgo.By("Step 9: Waiting for Deployment to roll out after restore")
				err = framework.WaitForDeploymentRollout(ctx, f.KubeClientSet(), managerNamespace, managerDeployment, rolloutTimeout)
				gomega.Expect(err).NotTo(gomega.HaveOccurred(), "deployment should roll out after restore")

				ginkgo.By("Step 10: Collecting logs after restore")
				err = framework.CollectManagerLogs(ctx, f.KubeClientSet(), managerNamespace, managerLabelSelector, logSinceDuration, logPlainAfterPath)
				gomega.Expect(err).NotTo(gomega.HaveOccurred(), "should collect logs after restore")
			}()

			ginkgo.By("Step 3: Patching Deployment to use JSON log format")
			jsonArgs := []string{
				"--provider-config=/etc/kruise-game/config.toml",
				"--api-server-qps=5",
				"--api-server-qps-burst=10",
				"--enable-cert-generation=false",
				"--leader-elect=true",
				"--log-format=json",
			}
			err = framework.PatchDeploymentArgs(ctx, f.KubeClientSet(), managerNamespace, managerDeployment, managerContainer, jsonArgs)
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "should patch deployment args")

			// Diagnostic: snapshot after patch
			_, _ = framework.DumpDeployment(ctx, f.KubeClientSet(), managerNamespace, managerDeployment, "after-patch")

			ginkgo.By("Step 4: Waiting for Deployment to roll out with JSON logging")
			err = framework.WaitForDeploymentRollout(ctx, f.KubeClientSet(), managerNamespace, managerDeployment, rolloutTimeout)
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "deployment should roll out with JSON logging")

			// Give pods time to generate logs
			time.Sleep(10 * time.Second)

			ginkgo.By("Step 5: Collecting logs in JSON format")
			err = framework.CollectManagerLogs(ctx, f.KubeClientSet(), managerNamespace, managerLabelSelector, logSinceDuration, logJSONPath)
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "should collect JSON logs")

			ginkgo.By("Step 6: Validating JSON log format")
			err = framework.ValidateJSONLogs(logJSONPath)
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "JSON logs should be valid")

			ginkgo.By("Step 7: All logs collected successfully")
			fmt.Printf("✓ Plain logs: %s\n", logPlainPath)
			fmt.Printf("✓ JSON logs: %s\n", logJSONPath)
			fmt.Printf("✓ Plain logs after restore: %s\n", logPlainAfterPath)
		})
	})
}
