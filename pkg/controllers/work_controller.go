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
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multiclusterv1alpha1 "github.com/vllry/cluster-reconciler/pkg/api/v1alpha1"
	"github.com/vllry/cluster-reconciler/pkg/reconcile"
	"github.com/vllry/cluster-reconciler/pkg/types"
)

// WorkReconciler reconciles a Work object
type WorkReconciler struct {
	client.Client
	Log                logr.Logger
	Scheme             *runtime.Scheme
	SpokeKubeClient    kubernetes.Interface
	SpokeDynamicClient dynamic.Interface
}

// +kubebuilder:rbac:groups=multicluster.k8s.io,resources=works,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=multicluster.k8s.io,resources=works/status,verbs=get;update;patch

func (r *WorkReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("work", req.NamespacedName)

	var work *multiclusterv1alpha1.Work
	err := r.Get(ctx, req.NamespacedName, work)
	if err != nil {
		log.Error(err, "unable to fetch work")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	err = reconcile.ReconcileCluster(r.SpokeKubeClient, r.SpokeDynamicClient, fetchFromWork(work))
	if err != nil {
		log.Error(err, "unable to reconcile work")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *WorkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&multiclusterv1alpha1.Work{}).
		Complete(r)
}

func fetchFromWork(work *multiclusterv1alpha1.Work) reconcile.FetchDesiredObjectFunc {
	return func() (map[types.ResourceIdentifier]types.Semistructured, error) {
		desired := map[types.ResourceIdentifier]types.Semistructured{}
		for _, manifest := range work.Spec.Workload.Manifests {
			unstrcturedObj := &unstructured.Unstructured{}
			err := unstrcturedObj.UnmarshalJSON(manifest.Raw)
			if err != nil {
				return nil, err
			}

			semi, err := types.UnstructuredToSemistructured(*unstrcturedObj)
			if err != nil {
				return nil, err
			}
			desired[semi.Identifier] = semi
		}
		return desired, nil
	}
}
