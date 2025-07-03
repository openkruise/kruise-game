package e2e

import (
	"testing"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/openkruise/kruise-game/test/e2e/framework"
	"github.com/openkruise/kruise-game/test/e2e/testcase"
)

func TestE2E(t *testing.T) {
	var err error

	cfg := config.GetConfigOrDie()
	f := framework.NewFrameWork(cfg)

	gomega.RegisterFailHandler(ginkgo.Fail)

	ginkgo.Describe("Run kruise game manager e2e tests", func() {
		ginkgo.BeforeSuite(func() {
			err = f.BeforeSuit()
			gomega.Expect(err).To(gomega.BeNil())
		})

		ginkgo.AfterSuite(func() {
			err = f.AfterSuit()
			gomega.Expect(err).To(gomega.BeNil())
		})

		testcase.RunTestCases(f)
	})

	ginkgo.RunSpecs(t, "run kgm e2e test")
}
