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
	"time"

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
	desiredManifestConditionsMap := generateAppliedConditionFromResults(results)
	currentManifestConditions := work.Status.ManifestConditions
	if currentManifestConditions == nil {
		currentManifestConditions = []multiclusterv1alpha1.ManifestCondition{}
	}
	desiredManifestConditions := mergeManifestConditions(desiredManifestConditionsMap, currentManifestConditions)
	work.Status.ManifestConditions = desiredManifestConditions
	work.Status.Conditions = generateWorkConditionsFromManifestConditions(desiredManifestConditions)

	err = r.Status().Update(ctx, work)

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

// mergeManifestConditions merges the desired cond from current cond
// If a matched idendifier is found, update the current condition with the new condition,
// else add the new condition.
func mergeManifestConditions(desired map[multiclusterv1alpha1.ResourceIdentifier]multiclusterv1alpha1.ManifestCondition, current []multiclusterv1alpha1.ManifestCondition) []multiclusterv1alpha1.ManifestCondition {
	resultConds := []multiclusterv1alpha1.ManifestCondition{}
	currentCondMap := map[multiclusterv1alpha1.ResourceIdentifier]multiclusterv1alpha1.ManifestCondition{}

	for _, cond := range current {
		currentCondMap[cond.Identifier] = cond
	}

	for _, desiredCond := range desired {
		if curCond, found := currentCondMap[desiredCond.Identifier]; found {
			mergedCond := mergeManifestCondition(curCond, desiredCond)
			resultConds = append(resultConds, mergedCond)
		} else {
			resultConds = append(resultConds, desiredCond)
		}
	}
	return resultConds
}

func mergeManifestCondition(condition, newCondition multiclusterv1alpha1.ManifestCondition) multiclusterv1alpha1.ManifestCondition {
	return multiclusterv1alpha1.ManifestCondition{
		Identifier: newCondition.Identifier,
		Conditions: MergeStatusConditions(condition.Conditions, newCondition.Conditions),
	}
}

// MergeStatusConditions returns a new status condition array with merged status conditions. It is based on newConditions,
// and merges the corresponding existing conditions if exists.
func MergeStatusConditions(conditions []multiclusterv1alpha1.StatusCondition, newConditions []multiclusterv1alpha1.StatusCondition) []multiclusterv1alpha1.StatusCondition {
	merged := []multiclusterv1alpha1.StatusCondition{}

	cm := map[string]multiclusterv1alpha1.StatusCondition{}
	for _, condition := range conditions {
		cm[condition.Type] = condition
	}

	for _, newCondition := range newConditions {
		// merge two conditions if necessary
		if condition, ok := cm[newCondition.Type]; ok {
			merged = append(merged, mergeStatusCondition(condition, newCondition))
			continue
		}

		newCondition.LastTransitionTime = metav1.NewTime(time.Now())
		merged = append(merged, newCondition)
	}

	return merged
}

// mergeStatusCondition returns a new status condition which merges the properties from the two input conditions.
// It assumes the two conditions have the same condition type.
func mergeStatusCondition(condition, newCondition multiclusterv1alpha1.StatusCondition) multiclusterv1alpha1.StatusCondition {
	merged := multiclusterv1alpha1.StatusCondition{
		Type:    newCondition.Type,
		Status:  newCondition.Status,
		Reason:  newCondition.Reason,
		Message: newCondition.Message,
	}

	if condition.Status == newCondition.Status {
		merged.LastTransitionTime = condition.LastTransitionTime
	} else {
		merged.LastTransitionTime = metav1.NewTime(time.Now())
	}

	return merged
}

func generateWorkConditionsFromManifestConditions(manifestConditions []multiclusterv1alpha1.ManifestCondition) []multiclusterv1alpha1.StatusCondition {
	// If all manifests are applied, set work condiition as applied
	workAppliedCondition := multiclusterv1alpha1.StatusCondition{
		Type:               "Applied",
		Status:             metav1.ConditionTrue,
		Reason:             "WorkApplyDone",
		Message:            "Manifests in work are applied",
		LastTransitionTime: metav1.Now(),
	}

	for _, manifestCond := range manifestConditions {
		cond := helpers.FindWorkCondition(manifestCond.Conditions, "Applied")
		if cond == nil {
			workAppliedCondition.Message = fmt.Sprintf("Resource with identifier %#v not applied", manifestCond.Identifier)
			workAppliedCondition.Status = metav1.ConditionFalse
			workAppliedCondition.Reason = "WorkApplyFailed"
		} else if cond.Status == metav1.ConditionFalse {
			workAppliedCondition.Message = fmt.Sprintf("Resource with identifier %#v failed to be applied with err %v", manifestCond.Identifier, cond.Message)
			workAppliedCondition.Status = metav1.ConditionFalse
			workAppliedCondition.Reason = "WorkApplyFailed"
		}
	}

	return []multiclusterv1alpha1.StatusCondition{workAppliedCondition}
}

func generateAppliedConditionFromResults(results []reconcile.ReconcileResult) map[multiclusterv1alpha1.ResourceIdentifier]multiclusterv1alpha1.ManifestCondition {
	conditions := map[multiclusterv1alpha1.ResourceIdentifier]multiclusterv1alpha1.ManifestCondition{}
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
			Type:               "Applied",
			Status:             metav1.ConditionTrue,
			Reason:             "ManifestApplyDone",
			Message:            "The manifest is applied successfully",
			LastTransitionTime: metav1.Now(),
		}

		if result.Err != nil {
			cond.Status = metav1.ConditionFalse
			cond.Reason = "ManifestApplyFailed"
			cond.Message = fmt.Sprintf("Failed to apply the manifest with err: %v", result.Err)
		}
		helpers.SetWorkCondition(&condition.Conditions, cond)
		conditions[condition.Identifier] = condition
	}

	return conditions
}
