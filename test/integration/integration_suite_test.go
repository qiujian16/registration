package integration_test

import (
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var cfg *rest.Config
var testEnv *envtest.Environment
var k8sClient client.Client

func TestIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	// TODO test cases
	// - spoke registration agent creates CSR, hub authorizes, spoke agent creates hub kubeconfig and connects back to hub for successful join
	// - spoke registration agent recovery from invalid bootstrap kubeconfig
	// - spoke registration agent recovery from invalid hub kubeconfig
	// - spoke registration rotate its certificate after its certificate is expired
	RunSpecsWithDefaultAndCustomReporters(t, "Integration Suite", []Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func(done Done) {
	logf.SetLogger(zap.LoggerTo(GinkgoWriter, true))

	By("bootstrapping test environment")

	// install cluster CRD and start a local kube-apiserver
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "vendor", "github.com", "open-cluster-management", "api", "cluster", "v1"),
		},
	}
	cfg, err := testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = clusterv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).ToNot(BeNil())
	close(done)
}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})
