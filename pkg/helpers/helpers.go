package helpers

import (
	"time"

	multiclusterv1alpha1 "github.com/vllry/cluster-reconciler/pkg/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func IsConditionTrue(condition *multiclusterv1alpha1.StatusCondition) bool {
	if condition == nil {
		return false
	}
	return condition.Status == metav1.ConditionTrue
}

func FindWorkCondition(conditions []multiclusterv1alpha1.StatusCondition, conditionType string) *multiclusterv1alpha1.StatusCondition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

func SetWorkCondition(conditions *[]multiclusterv1alpha1.StatusCondition, newCondition multiclusterv1alpha1.StatusCondition) {
	if conditions == nil {
		conditions = &[]multiclusterv1alpha1.StatusCondition{}
	}
	existingCondition := FindWorkCondition(*conditions, newCondition.Type)
	if existingCondition == nil {
		newCondition.LastTransitionTime = metav1.NewTime(time.Now())
		*conditions = append(*conditions, newCondition)
		return
	}

	if existingCondition.Status != newCondition.Status {
		existingCondition.Status = newCondition.Status
		existingCondition.LastTransitionTime = metav1.NewTime(time.Now())
	}

	existingCondition.Reason = newCondition.Reason
	existingCondition.Message = newCondition.Message
}
