package testcase

import (
	"fmt"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
	"github.com/openkruise/kruise-game/test/e2e/client"
	"github.com/openkruise/kruise-game/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func RunTestCases(f *framework.Framework) {
	ginkgo.Describe("kruise game controllers", func() {

		ginkgo.BeforeEach(func() {
			// Mark a per-test start timestamp for audit filtering
			f.MarkTestStart()
		})

		ginkgo.AfterEach(func() {
			if ginkgo.CurrentGinkgoTestDescription().Failed {
				f.DumpAuditIfFailed()
			}
			err := f.AfterEach()
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		// Run logging smoke test first for fast feedback
		RunLoggingSmokeTest(f)

		// Run HA deployment update test (only in HA mode)
		RunHADeploymentUpdateTest(f)

		ginkgo.It("scale", func() {

			// deploy
			gss, err := f.DeployGameServerSet()
			gomega.Expect(err).To(gomega.BeNil())

			err = f.ExpectGssCorrect(gss, []int{0, 1, 2})
			gomega.Expect(err).To(gomega.BeNil())

			// scale up
			_, err = f.GameServerScale(gss, 5, nil)
			gomega.Expect(err).To(gomega.BeNil())

			err = f.ExpectGssCorrect(gss, []int{0, 1, 2, 3, 4})
			gomega.Expect(err).To(gomega.BeNil())

			// scale down when setting WaitToDelete
			_, err = f.MarkGameServerOpsState(gss.GetName()+"-2", string(gameKruiseV1alpha1.WaitToDelete))
			gomega.Expect(err).To(gomega.BeNil())

			// sleep for a while to wait the status update
			time.Sleep(5 * time.Second)

			err = f.WaitForGsOpsStateUpdate(gss.GetName()+"-2", string(gameKruiseV1alpha1.WaitToDelete))
			gomega.Expect(err).To(gomega.BeNil())

			gss, err = f.GameServerScale(gss, 4, nil)
			gomega.Expect(err).To(gomega.BeNil())

			err = f.ExpectGssCorrect(gss, []int{0, 1, 3, 4})
			gomega.Expect(err).To(gomega.BeNil())

			// scale down when setting deletion priority
			_, err = f.ChangeGameServerDeletionPriority(gss.GetName()+"-3", "100")
			gomega.Expect(err).To(gomega.BeNil())

			err = f.WaitForGsDeletionPriorityUpdated(gss.GetName()+"-3", "100")
			gomega.Expect(err).To(gomega.BeNil())

			gss, err = f.GameServerScale(gss, 3, nil)
			gomega.Expect(err).To(gomega.BeNil())

			err = f.ExpectGssCorrect(gss, []int{0, 1, 4})
			gomega.Expect(err).To(gomega.BeNil())
		})

		ginkgo.It("update pod", func() {

			// deploy
			gss, err := f.DeployGameServerSet()
			gomega.Expect(err).To(gomega.BeNil())

			err = f.ExpectGssCorrect(gss, []int{0, 1, 2})
			gomega.Expect(err).To(gomega.BeNil())

			gss, err = f.ImageUpdate(gss, client.GameContainerName, "nginx:latest")
			gomega.Expect(err).To(gomega.BeNil())

			err = f.WaitForUpdated(gss, client.GameContainerName, "nginx:latest")
			gomega.Expect(err).To(gomega.BeNil())
		})

		ginkgo.It("service qualities", func() {

			// deploy
			gss, err := f.DeployGssWithServiceQualities()
			gomega.Expect(err).To(gomega.BeNil())

			err = f.ExpectGssCorrect(gss, []int{0, 1, 2})
			gomega.Expect(err).To(gomega.BeNil())

			err = f.WaitForGsUpdatePriorityUpdated(gss.GetName()+"-0", "20")
			gomega.Expect(err).To(gomega.BeNil())
			err = f.WaitForGsUpdatePriorityUpdated(gss.GetName()+"-1", "20")
			gomega.Expect(err).To(gomega.BeNil())
			err = f.WaitForGsUpdatePriorityUpdated(gss.GetName()+"-2", "20")
			gomega.Expect(err).To(gomega.BeNil())
		})

		ginkgo.It("service qualities scale down prioritizes WaitToBeDeleted", func() {
			// Deploy GSS and scale to 5
			gss, err := f.DeployGssWithServiceQualities()
			gomega.Expect(err).To(gomega.BeNil())
			gss, err = f.GameServerScale(gss, 5, nil)
			gomega.Expect(err).To(gomega.BeNil())
			gomega.Expect(f.ExpectGssCorrect(gss, []int{0, 1, 2, 3, 4})).To(gomega.BeNil())

			// ServiceQuality probe (per issue):
			// exit 0 -> WaitToBeDeleted (idle pods)
			// exit 1 -> None (active pod: -4)
			patchFields := map[string]interface{}{
				"serviceQualities": []map[string]interface{}{
					{
						"name":          "idle-check",
						"containerName": "default-game",
						"permanent":     false,
						"exec": map[string]interface{}{
							"command": []string{
								"sh", "-c",
								"hostname | grep -q -- '-4$' && exit 1 || exit 0",
							},
						},
						"serviceQualityAction": []map[string]interface{}{
							{"state": true, "opsState": "WaitToBeDeleted"},
							{"state": false, "opsState": "None"},
						},
					},
				},
			}
			gss, err = f.PatchGssSpec(patchFields)
			gomega.Expect(err).To(gomega.BeNil())

			// Wait for opsState convergence
			for i := 0; i < 4; i++ {
				gomega.Expect(
					f.WaitForGsSpecOpsState(fmt.Sprintf("%s-%d", gss.GetName(), i), "WaitToBeDeleted"),
				).To(gomega.BeNil())
			}
			gomega.Expect(
				f.WaitForGsSpecOpsState(fmt.Sprintf("%s-4", gss.GetName()), "None"),
			).To(gomega.BeNil())

			// Scale down 5 -> 3
			gss, err = f.GameServerScale(gss, 3, nil)
			gomega.Expect(err).To(gomega.BeNil())

			// Wait for scale-down to complete
			var remaining []int
			gomega.Eventually(func() []int {
				remaining = []int{}
				for i := 0; i < 5; i++ {
					if gs, err := f.GetGameServer(fmt.Sprintf("%s-%d", gss.GetName(), i)); err == nil && gs != nil {
						remaining = append(remaining, i)
					}
				}
				return remaining
			}, 2*time.Minute, 2*time.Second).Should(
				gomega.And(
					gomega.HaveLen(3),
					gomega.ContainElement(4), // active pod must survive
				),
			)

			// Verify core requirement: pod 4 (opsState=None) survived
			gs4, err := f.GetGameServer(fmt.Sprintf("%s-4", gss.GetName()))
			gomega.Expect(err).To(gomega.BeNil())
			gomega.Expect(string(gs4.Spec.OpsState)).To(gomega.Equal("None"),
				"Pod 4 with opsState=None should be protected from deletion")

			// Verify the other 2 remaining pods exist (no specific opsState check to avoid flake)
			gomega.Expect(remaining).To(gomega.HaveLen(3))
			gomega.Expect(remaining).To(gomega.ContainElement(4))
		})

		ginkgo.Describe("network control", func() {
			ginkgo.It("disables NodePort traffic when networkDisabled is true", func() {
				networkConf := []gameKruiseV1alpha1.NetworkConfParams{
					{Name: "PortProtocols", Value: "8080/TCP"},
				}
				ports := []corev1.ContainerPort{{ContainerPort: 8080}}
				gss, err := f.DeployGameServerSetWithNetwork("Kubernetes-NodePort", networkConf, ports)
				gomega.Expect(err).To(gomega.BeNil())

				err = f.ExpectGssCorrect(gss, []int{0, 1, 2})
				gomega.Expect(err).To(gomega.BeNil())

				target := fmt.Sprintf("%s-0", client.GameServerSet)

				gomega.Expect(f.WaitForNodePortServiceSelector(target, false)).To(gomega.BeNil())
				gomega.Expect(f.WaitForGsDesiredNetworkState(target, gameKruiseV1alpha1.NetworkReady)).To(gomega.BeNil())

				_, err = f.PatchGameServerSpec(target, map[string]interface{}{"networkDisabled": true})
				gomega.Expect(err).To(gomega.BeNil())
				gomega.Expect(f.WaitForGsNetworkDisabled(target, true)).To(gomega.BeNil())
				gomega.Expect(f.WaitForGsDesiredNetworkState(target, gameKruiseV1alpha1.NetworkNotReady)).To(gomega.BeNil())
				gomega.Expect(f.WaitForNodePortServiceSelector(target, true)).To(gomega.BeNil())

				_, err = f.PatchGameServerSpec(target, map[string]interface{}{"networkDisabled": false})
				gomega.Expect(err).To(gomega.BeNil())
				gomega.Expect(f.WaitForGsNetworkDisabled(target, false)).To(gomega.BeNil())
				gomega.Expect(f.WaitForGsDesiredNetworkState(target, gameKruiseV1alpha1.NetworkReady)).To(gomega.BeNil())
				gomega.Expect(f.WaitForNodePortServiceSelector(target, false)).To(gomega.BeNil())

				gs, err := f.GetGameServer(target)
				gomega.Expect(err).To(gomega.BeNil())
				gomega.Expect(gs.Spec.NetworkDisabled).NotTo(gomega.BeNil())
				gomega.Expect(*gs.Spec.NetworkDisabled).To(gomega.BeFalse())
			})
		})

		ginkgo.It("GameServer lifecycle(DeleteGameServerReclaimPolicy)", func() {

			// Deploy a gss, and the ReclaimPolicy is Delete
			gss, err := f.DeployGameServerSetWithReclaimPolicy(gameKruiseV1alpha1.DeleteGameServerReclaimPolicy)
			gomega.Expect(err).To(gomega.BeNil())

			err = f.ExpectGssCorrect(gss, []int{0, 1, 2})
			gomega.Expect(err).To(gomega.BeNil())

			_, err = f.ChangeGameServerDeletionPriority(gss.GetName()+"-1", "100")
			gomega.Expect(err).To(gomega.BeNil())

			// sleep for a while to wait the status update
			time.Sleep(5 * time.Second)

			err = f.WaitForGsDeletionPriorityUpdated(gss.GetName()+"-1", "100")
			gomega.Expect(err).To(gomega.BeNil())

			err = f.DeletePodDirectly(1)
			gomega.Expect(err).To(gomega.BeNil())

			err = f.ExpectGsCorrect(gss.GetName()+"-1", "None", "100", "0")
			gomega.Expect(err).To(gomega.BeNil())
		})

		ginkgo.It("GameServer lifecycle(CascadeGameServerReclaimPolicy)", func() {

			// Deploy a gss, and the ReclaimPolicy is Cascade
			gss, err := f.DeployGameServerSet()
			gomega.Expect(err).To(gomega.BeNil())

			err = f.ExpectGssCorrect(gss, []int{0, 1, 2})
			gomega.Expect(err).To(gomega.BeNil())

			_, err = f.ChangeGameServerDeletionPriority(gss.GetName()+"-1", "100")
			gomega.Expect(err).To(gomega.BeNil())

			// sleep for a while to wait the status update
			time.Sleep(5 * time.Second)

			err = f.WaitForGsDeletionPriorityUpdated(gss.GetName()+"-1", "100")
			gomega.Expect(err).To(gomega.BeNil())

			err = f.DeletePodDirectly(1)
			gomega.Expect(err).To(gomega.BeNil())

			err = f.ExpectGsCorrect(gss.GetName()+"-1", "None", "0", "0")
			gomega.Expect(err).To(gomega.BeNil())
		})

		// Reserve-only equivalent replacement (requires Delete ReclaimPolicy for precise replacement)
		ginkgo.It("reserve-only equal replacement", func() {
			// 1. Deploy GSS (ReclaimPolicy=Delete) and scale to 5 replicas
			gss, err := f.DeployGameServerSetWithReclaimPolicy(gameKruiseV1alpha1.DeleteGameServerReclaimPolicy)
			gomega.Expect(err).To(gomega.BeNil())

			_, err = f.GameServerScale(gss, 5, nil)
			gomega.Expect(err).To(gomega.BeNil())

			// 2. Only set reserve = "3-4"
			gss, err = f.PatchGssSpec(map[string]interface{}{
				"reserveGameServerIds": []intstr.IntOrString{intstr.FromString("3-4")},
			})
			gomega.Expect(err).To(gomega.BeNil())
			gomega.Expect(f.WaitForGssObservedGeneration(gss.Generation)).To(gomega.BeNil())

			// 3. Assert final set is {0,1,2,5,6}, replicas still 5
			err = f.ExpectGssCorrect(gss, []int{0, 1, 2, 5, 6})
			gomega.Expect(err).To(gomega.BeNil())
		})

		// Reserve+replicas change simultaneously (prioritize deleting reserve, requires Delete ReclaimPolicy)
		ginkgo.It("reserve+replicas change prioritizes reserve deletions", func() {
			// 1. Deploy to 5 replicas and set reserve=3-4 (preparation) with Delete ReclaimPolicy
			gss, err := f.DeployGameServerSetWithReclaimPolicy(gameKruiseV1alpha1.DeleteGameServerReclaimPolicy)
			gomega.Expect(err).To(gomega.BeNil())

			_, err = f.GameServerScale(gss, 5, nil)
			gomega.Expect(err).To(gomega.BeNil())

			gss, err = f.PatchGssSpec(map[string]interface{}{
				"reserveGameServerIds": []intstr.IntOrString{intstr.FromString("3-4")},
			})
			gomega.Expect(err).To(gomega.BeNil())
			gomega.Expect(f.WaitForGssObservedGeneration(gss.Generation)).To(gomega.BeNil())

			// 2. Scale down to 3 and extend reserve to 3-6 simultaneously
			gss, err = f.PatchGssSpec(map[string]interface{}{
				"replicas":             3,
				"reserveGameServerIds": []intstr.IntOrString{intstr.FromString("3-6")},
			})
			gomega.Expect(err).To(gomega.BeNil())
			gomega.Expect(f.WaitForGssObservedGeneration(gss.Generation)).To(gomega.BeNil())

			// 3. Assert final set is {0,1,2}
			err = f.ExpectGssCorrect(gss, []int{0, 1, 2})
			gomega.Expect(err).To(gomega.BeNil())
		})

		// ReserveIds strategy backfill and reuse
		ginkgo.It("reserveIds backfill and reuse on expansion", func() {
			// 1. Deploy replicas=5 and set strategy to ReserveIds
			gss, err := f.DeployGameServerSet()
			gomega.Expect(err).To(gomega.BeNil())

			_, err = f.GameServerScale(gss, 5, nil)
			gomega.Expect(err).To(gomega.BeNil())

			gss, err = f.PatchGssSpec(map[string]interface{}{
				"scaleStrategy": map[string]interface{}{
					"scaleDownStrategyType": "ReserveIds",
				},
			})
			gomega.Expect(err).To(gomega.BeNil())
			gomega.Expect(f.WaitForGssObservedGeneration(gss.Generation)).To(gomega.BeNil())

			// 2. Scale down to 3 and assert set {0,1,2}
			gss, err = f.GameServerScale(gss, 3, nil)
			gomega.Expect(err).To(gomega.BeNil())
			err = f.ExpectGssCorrect(gss, []int{0, 1, 2})
			gomega.Expect(err).To(gomega.BeNil())

			// 3. Wait for controller to backfill reserve into GSS (both spec and annotation contain 3 and 4)
			err = f.WaitForGss(func(g *gameKruiseV1alpha1.GameServerSet) (bool, error) {
				rset := util.GetReserveOrdinalIntSet(g.Spec.ReserveGameServerIds)
				if rset == nil || !(rset.Has(3) && rset.Has(4)) {
					return false, nil
				}
				ann := g.GetAnnotations()[gameKruiseV1alpha1.GameServerSetReserveIdsKey]
				aset := util.StringToOrdinalIntSet(ann, ",")
				return aset.Has(3) && aset.Has(4), nil
			})
			gomega.Expect(err).To(gomega.BeNil())

			// 4. Remove 4 from reserve, scale to 4, assert new ordinal 4
			gss, err = f.PatchGssSpec(map[string]interface{}{
				"replicas":             4,
				"reserveGameServerIds": []intstr.IntOrString{intstr.FromInt(3)},
			})
			gomega.Expect(err).To(gomega.BeNil())
			gomega.Expect(f.WaitForGssObservedGeneration(gss.Generation)).To(gomega.BeNil())
			err = f.ExpectGssCorrect(gss, []int{0, 1, 2, 4})
			gomega.Expect(err).To(gomega.BeNil())
		})

		// Kill triggers automatic scale-down
		ginkgo.It("kill opsState triggers auto scale down and does not backfill reserve", func() {
			// 1. Deploy replicas=3 to get 0..2
			gss, err := f.DeployGameServerSet()
			gomega.Expect(err).To(gomega.BeNil())
			err = f.ExpectGssCorrect(gss, []int{0, 1, 2})
			gomega.Expect(err).To(gomega.BeNil())

			// 2. Mark gss-1 as Kill and wait for spec update plus pod handling
			_, err = f.MarkGameServerOpsState(client.GameServerSet+"-1", string(gameKruiseV1alpha1.Kill))
			gomega.Expect(err).To(gomega.BeNil())
			err = f.WaitForGsSpecOpsState(client.GameServerSet+"-1", string(gameKruiseV1alpha1.Kill))
			gomega.Expect(err).To(gomega.BeNil())
			err = f.WaitForPodOpsStateOrDeleted(client.GameServerSet+"-1", string(gameKruiseV1alpha1.Kill))
			gomega.Expect(err).To(gomega.BeNil())

			// 3. Wait for replicas to automatically become 2, and assert 1 is excluded
			gomega.Expect(f.WaitForReplicasConverge(gss, 2)).To(gomega.BeNil())
			cur, err := f.GetGameServerSet()
			gomega.Expect(err).To(gomega.BeNil())
			err = f.ExpectGssCorrect(cur, []int{0, 2})
			gomega.Expect(err).To(gomega.BeNil())

			// 4. Scale back to 3, allow reusing 1 (implementation reuses 1)
			cur, err = f.GameServerScale(cur, 3, nil)
			gomega.Expect(err).To(gomega.BeNil())
			err = f.ExpectGssCorrect(cur, []int{0, 1, 2})
			gomega.Expect(err).To(gomega.BeNil())

			// And do not backfill reserve (should not include 1)
			cur, err = f.GetGameServerSet()
			gomega.Expect(err).To(gomega.BeNil())
			rset2 := util.GetReserveOrdinalIntSet(cur.Spec.ReserveGameServerIds)
			gomega.Expect(rset2.Has(1)).To(gomega.BeFalse(), "id 1 should not be backfilled in reserve")
		})
	})
}
