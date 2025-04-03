package testcase

import (
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/test/e2e/client"
	"github.com/openkruise/kruise-game/test/e2e/framework"
)

func RunTestCases(f *framework.Framework) {
	ginkgo.Describe("kruise game controllers", func() {

		ginkgo.AfterEach(func() {
			err := f.AfterEach()
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		ginkgo.It("scale", func() {

			// deploy
			gss, err := f.DeployGameServerSet()
			gomega.Expect(err).To(gomega.BeNil())

			err = f.ExpectGssCorrect(gss, []int{0, 1, 2})
			gomega.Expect(err).To(gomega.BeNil())

			// scale up
			gss, err = f.GameServerScale(gss, 5, nil)
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
	})
}
