package controllers

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	multiclusterv1alpha1 "github.com/vllry/cluster-reconciler/pkg/api/v1alpha1"
	"github.com/vllry/cluster-reconciler/pkg/helpers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Work Controller", func() {
	const workNamespace = "cluster"
	const timeout = time.Second * 30
	const interval = time.Second * 1

	BeforeEach(func() {
		// Create namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: workNamespace,
			},
		}
		_, err := k8sClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		// Add any teardown steps that needs to be executed after each test
		err := k8sClient.CoreV1().Namespaces().Delete(context.Background(), workNamespace, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
	})
	Context("Deploy a work", func() {
		It("Should have a configmap deployed correctly", func() {
			cmName := "testcm"
			cmNamespace := "default"
			cm := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: cmNamespace,
				},
				Data: map[string]string{
					"test": "test",
				},
			}

			work := &multiclusterv1alpha1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "comfigmap-work",
					Namespace: workNamespace,
				},
				Spec: multiclusterv1alpha1.WorkSpec{
					Workload: multiclusterv1alpha1.WorkloadTemplate{
						Manifests: []multiclusterv1alpha1.Manifest{
							{
								RawExtension: runtime.RawExtension{Object: cm},
							},
						},
					},
				},
			}

			workClient := workManager.GetClient()
			err := workClient.Create(context.Background(), work)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				_, err := k8sClient.CoreV1().ConfigMaps(cmNamespace).Get(context.Background(), cmName, metav1.GetOptions{})
				return err
			}, timeout, interval).Should(Succeed())

			Eventually(func() error {
				resultWork := &multiclusterv1alpha1.Work{}
				err := workClient.Get(context.Background(), types.NamespacedName{Name: work.Name, Namespace: work.Namespace}, resultWork)
				if err != nil {
					return err
				}
				if len(resultWork.Status.ManifestConditions) != 1 {
					return fmt.Errorf("Expect the 1 manifest condition is updated")
				}

				cond := helpers.FindWorkCondition(resultWork.Status.ManifestConditions[0].Conditions, "Applied")
				if cond == nil {
					return fmt.Errorf("Failed to find applied condition")
				}
				if !helpers.IsConditionTrue(cond) {
					return fmt.Errorf("Exepect condition statuso to be true")
				}

				cond = helpers.FindWorkCondition(resultWork.Status.Conditions, "Applied")
				if cond == nil {
					return fmt.Errorf("Failed to find applied condition")
				}
				if !helpers.IsConditionTrue(cond) {
					return fmt.Errorf("Exepect condition statuso to be true")
				}

				return nil
			}, timeout, interval).Should(Succeed())
		})
	})
})
