/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"fmt"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/discovery"
	cacheddiscovery "k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	multiclusterv1alpha1 "github.com/vllry/cluster-reconciler/pkg/api/v1alpha1"
	"github.com/vllry/cluster-reconciler/pkg/restmapper"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient kubernetes.Interface
var testEnv *envtest.Environment
var workManager ctrl.Manager

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Controller Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func(done Done) {
	logf.SetLogger(zap.LoggerTo(GinkgoWriter, true))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("../../", "config", "crd", "bases")},
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = multiclusterv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sClient, err = kubernetes.NewForConfig(cfg)
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).ToNot(BeNil())

	dynamicClient, err := dynamic.NewForConfig(cfg)
	Expect(err).ToNot(HaveOccurred())
	Expect(dynamicClient).ToNot(BeNil())

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	Expect(err).ToNot(HaveOccurred())
	Expect(discoveryClient).ToNot(BeNil())

	cachedSpokeDiscoveryClient := cacheddiscovery.NewMemCacheClient(discoveryClient)
	restMapper := restmapper.NewMapper(cachedSpokeDiscoveryClient)

	stopCh := ctrl.SetupSignalHandler()
	go restMapper.Run(stopCh)

	workManager, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme:             scheme.Scheme,
		MetricsBindAddress: "0",
	})
	Expect(err).ToNot(HaveOccurred())
	err = (&WorkReconciler{
		Client:             workManager.GetClient(),
		Log:                ctrl.Log.WithName("controllers").WithName("Work"),
		Scheme:             workManager.GetScheme(),
		SpokeKubeClient:    k8sClient,
		SpokeDynamicClient: dynamicClient,
		RestMapper:         restMapper,
	}).SetupWithManager(workManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		err = workManager.Start(stopCh)
		fmt.Printf("failed to starrt manager, %v\n", err)
		Expect(err).ToNot(HaveOccurred())
	}()

	close(done)
}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})
