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

			// wait for priority updates
			for i := 0; i < 3; i++ {
				gsName := fmt.Sprintf("%s-%d", gss.GetName(), i)
				err = f.WaitForGsUpdatePriorityUpdated(gsName, "20")
				gomega.Expect(err).To(gomega.BeNil())
			}

			// wait for opsState update (currently checks Pod label, which is correct)
			for i := 0; i < 3; i++ {
				gsName := fmt.Sprintf("%s-%d", gss.GetName(), i)
				err = f.WaitForGsOpsStateUpdate(gsName, "Maintaining")
				gomega.Expect(err).To(gomega.BeNil())
			}

			// verify metadata label + annotation on GameServer
			gsName := gss.GetName() + "-0"
			gs, err := f.GetGameServer(gsName)
			gomega.Expect(err).To(gomega.BeNil())

			// Verify labels
			gomega.Expect(gs.Labels["sq"]).To(gomega.Equal("healthy"))

			// Verify annotations
			gomega.Expect(gs.Annotations["note"]).To(gomega.Equal("updated"))

			// Optional: verify opsState is also set correctly in spec
			gomega.Expect(string(gs.Spec.OpsState)).To(gomega.Equal("Maintaining"))
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
