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
	"fmt"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	multiclusterv1alpha1 "github.com/vllry/cluster-reconciler/pkg/api/v1alpha1"
	"github.com/vllry/cluster-reconciler/pkg/helpers"
	"github.com/vllry/cluster-reconciler/pkg/reconcile"
	"github.com/vllry/cluster-reconciler/pkg/restmapper"
	"github.com/vllry/cluster-reconciler/pkg/types"
)

// WorkReconciler reconciles a Work object
type WorkReconciler struct {
	client.Client
	Log                logr.Logger
	Scheme             *runtime.Scheme
	SpokeKubeClient    kubernetes.Interface
	SpokeDynamicClient dynamic.Interface
	RestMapper         *restmapper.Mapper
}

const workFinalizer = "work-clean-up"

// +kubebuilder:rbac:groups=multicluster.x-k8s.io,resources=works,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=multicluster.x-k8s.io,resources=works/status,verbs=get;update;patch

func (r *WorkReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("work", req.NamespacedName)

	work := &multiclusterv1alpha1.Work{}
	err := r.Get(ctx, req.NamespacedName, work)
	if err != nil {
		log.Error(err, "unable to fetch work")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if work.DeletionTimestamp.IsZero() {
		hasFinalizer := false
		for i := range work.Finalizers {
			if work.Finalizers[i] == workFinalizer {
				hasFinalizer = true
				break
			}
		}
		if !hasFinalizer {
			work.Finalizers = append(work.Finalizers, workFinalizer)
			err := r.Update(ctx, work)
			return ctrl.Result{}, err
		}
	}

	// Spoke cluster is deleting, we remove its related resources
	if !work.DeletionTimestamp.IsZero() {
		if err := r.removeWorkResources(ctx, work); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, r.removeWorkFinalizer(ctx, work)
	}

	results, _ := reconcile.ReconcileCluster(r.SpokeKubeClient, r.SpokeDynamicClient, r.fetchFromWork(work))
	desiredManifestConditions := generateAppliedConditionFromResults(results)
	currentManifestConditions := work.Status.ManifestConditions
	desiredManifestConditions = mergeManifestConditions(desiredManifestConditions, currentManifestConditions)
	work.Status.ManifestConditions = desiredManifestConditions
	err = r.Update(ctx, work)

	return ctrl.Result{}, err
}

func (r *WorkReconciler) fetchFromWork(work *multiclusterv1alpha1.Work) reconcile.FetchDesiredObjectFunc {
	return func() (map[types.ResourceIdentifier]types.Semistructured, error) {
		desired := map[types.ResourceIdentifier]types.Semistructured{}
		for index, manifest := range work.Spec.Workload.Manifests {
			unstrcturedObj := &unstructured.Unstructured{}
			err := unstrcturedObj.UnmarshalJSON(manifest.Raw)
			if err != nil {
				identifier := types.ResourceIdentifier{Ordinal: index}
				desired[identifier] = types.Semistructured{Identifier: identifier}
				continue
			}

			semi, err := types.UnstructuredToSemistructured(*unstrcturedObj)
			if err != nil {
				semi.Identifier.Ordinal = index
				desired[semi.Identifier] = semi
				continue
			}
			mapping, err := r.RestMapper.MappingForGVK(semi.Identifier.GroupVersionKind)
			if err != nil {
				semi.Identifier.Ordinal = index
				desired[semi.Identifier] = semi
				continue
			}
			semi.Identifier.GroupVersionResource = mapping.Resource
			desired[semi.Identifier] = semi
		}
		return desired, nil
	}
}

func (r *WorkReconciler) removeWorkResources(ctx context.Context, work *multiclusterv1alpha1.Work) error {
	// TODO
	return nil
}

func (r *WorkReconciler) removeWorkFinalizer(ctx context.Context, work *multiclusterv1alpha1.Work) error {
	copiedFinalizers := []string{}
	for i := range work.Finalizers {
		if work.Finalizers[i] == workFinalizer {
			continue
		}
		copiedFinalizers = append(copiedFinalizers, work.Finalizers[i])
	}

	if len(work.Finalizers) != len(copiedFinalizers) {
		work.Finalizers = copiedFinalizers
		err := r.Update(ctx, work)
		return err
	}

	return nil
}

func (r *WorkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&multiclusterv1alpha1.Work{}).
		Complete(r)
}

func mergeManifestConditions(desired, current []multiclusterv1alpha1.ManifestCondition) []multiclusterv1alpha1.ManifestCondition {
	// TODO
	return desired
}

func generateAppliedConditionFromResults(results []reconcile.ReconcileResult) []multiclusterv1alpha1.ManifestCondition {
	conditions := []multiclusterv1alpha1.ManifestCondition{}
	for _, result := range results {
		condition := multiclusterv1alpha1.ManifestCondition{
			Identifier: multiclusterv1alpha1.ResourceIdentifier{
				Ordinal:   result.Identifier.Ordinal,
				Group:     result.Identifier.GroupVersionKind.Group,
				Version:   result.Identifier.GroupVersionKind.Version,
				Kind:      result.Identifier.GroupVersionKind.Kind,
				Resource:  result.Identifier.GroupVersionResource.Resource,
				Namespace: result.Identifier.NamespacedName.Namespace,
				Name:      result.Identifier.NamespacedName.Name,
			},
			Conditions: []multiclusterv1alpha1.StatusCondition{},
		}

		cond := multiclusterv1alpha1.StatusCondition{
			Type:    "Applied",
			Status:  metav1.ConditionTrue,
			Reason:  "ManifestApplyDone",
			Message: "The manifest is applied successfully",
		}

		if result.Err != nil {
			cond.Status = metav1.ConditionFalse
			cond.Reason = "ManifestApplyFailed"
			cond.Message = fmt.Sprintf("Failed to apply the manifest with err: %v", result.Err)
		}
		helpers.SetWorkCondition(&condition.Conditions, cond)
		conditions = append(conditions, condition)
	}

	return conditions
}
