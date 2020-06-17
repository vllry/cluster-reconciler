package reconcile

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/vllry/cluster-reconciler/pkg/discover"
	"github.com/vllry/cluster-reconciler/pkg/types"
)

const reconcilerAnnotationKey = "cluster-reconciler-managed"
const reconcilerAnnotationValue = "true"

type FetchDesiredObjectFunc func() (map[types.ResourceIdentifier]types.Semistructured, error)

// ReconcileResult is to track the result of result apply
type ReconcileResult struct {
	Identifier types.ResourceIdentifier
	Err        error
	Updated    bool
}

func ReconcileCluster(client kubernetes.Interface, dynamicClient dynamic.Interface, fetchFunc FetchDesiredObjectFunc) ([]ReconcileResult, error) {
	results := []ReconcileResult{}

	//  Fetch resources from the cluster inventory (desired actualState).
	// These will be a set of YAML or JSON Kubernetes Objects.
	desiredObjects, err := fetchFunc()
	if err != nil {
		return results, err
	}

	// TODO inject managed annotation in all objects

	// desiredResourceList contains, for every desired object, all identifiers necessary to fetch the actual object for the cluster.
	desiredResourceList := make([]types.ResourceIdentifier, 0, len(desiredObjects)) // TODO ensure deduplication? User could supply the same object twice.
	for id := range desiredObjects {
		desiredResourceList = append(desiredResourceList, id)
	}

	// Fetch the current actualState of all desired resources.
	currentResources, err := fetchResourceState(dynamicClient, desiredResourceList)
	if err != nil {
		return results, err
	}

	// Create/update desired resources.
	for obj, desiredState := range desiredObjects {
		result := ReconcileResult{}
		if invalidGVR(desiredState.Identifier.GroupVersionResource) {
			result.Identifier = desiredState.Identifier
			result.Err = fmt.Errorf("Invalid gvr")
		} else if actualState, found := currentResources[obj]; found {
			if !sameIntent(desiredState.Unstructured, actualState.Unstructured) {
				// Update resource.
				result.Identifier, result.Err = updateResource(dynamicClient, desiredState)
				result.Updated = true
			} else {
				result.Identifier = actualState.Identifier
			}
		} else {
			result.Identifier, result.Err = createResource(dynamicClient, desiredState)
			result.Updated = true
		}
		results = append(results, result)
	}

	return results, nil
}

// deleteOldManagedResources deletes all managed resources that are not in the provided desired state.
func deleteOldManagedResources(typedClient kubernetes.Interface, dynamicClient dynamic.Interface, desiredObjects map[types.ResourceIdentifier]types.Semistructured) error {
	// Fetch all applicable APIs in tthe cluster.
	clusterResourceTypes, err := discover.FetchApiVersions(typedClient)
	if err != nil {
		return err
	}

	// TODO FetchApiVersions's signature feels wonky to consume.
	for groupName, resources := range clusterResourceTypes {
		for resource, versions := range resources {
			version := versions[0] // Use an arbitrary supported API version.
			gvr := schema.GroupVersionResource{
				Group:    groupName,
				Version:  version,
				Resource: resource,
			}
			items, err := dynamicClient.Resource(gvr).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return errors.Wrap(err, fmt.Sprintf("failed to list %s resources in %s/%s api", resource, groupName, version))
			}

			for _, obj := range items.Items {
				if isReconcilerManaged(obj) {
					/*
						_, found := desiredObjects[gvk]; !found {
							fmt.Printf("Deleting managed object: %v\n", obj.GetName())
							// TODO delete
						}
					*/
				}
			}
		}
	}

	return nil
}

func updateResource(client dynamic.Interface, resource types.Semistructured) (types.ResourceIdentifier, error) {
	_, err := client.Resource(resource.Identifier.GroupVersionResource).Namespace(resource.Identifier.NamespacedName.Namespace).Update(
		context.Background(),
		&resource.Unstructured,
		metav1.UpdateOptions{},
	)
	return resource.Identifier, err
}

func createResource(client dynamic.Interface, resource types.Semistructured) (types.ResourceIdentifier, error) {
	_, err := client.Resource(resource.Identifier.GroupVersionResource).Namespace(resource.Identifier.NamespacedName.Namespace).Create(
		context.Background(),
		&resource.Unstructured,
		metav1.CreateOptions{},
	)
	return resource.Identifier, err
}

// isReconcilerManaged returns true if the reconciler manages the provided object.
func isReconcilerManaged(obj unstructured.Unstructured) bool {
	val := obj.GetAnnotations()[reconcilerAnnotationKey]
	return val == reconcilerAnnotationValue
}

// sameIntent checks that, barring certain ignored fields, the provided objects are semantically equal.
// System metadata, and status fields are ignored.
// Effectively, sameIntent returns true if applying a change would/should be a no-op.
func sameIntent(a unstructured.Unstructured, b unstructured.Unstructured) bool {
	// Check a -> b.
	for aKey, aVal := range a.Object {
		if bVal, found := b.Object[aKey]; found {
			if aKey == "metadata" {
				// TODO check annotations and labels.
			} else if aKey == "status" {
				continue // Ignore status completely.
			} else {
				// Use the Kubernetes Semantic equality check.
				if !equality.Semantic.DeepEqual(aVal, bVal) {
					return false
				}
			}
		}
	}

	// We know everything in exacted is in b.
	// Now we only need to check that b has no extra fields.
	for key, _ := range b.Object {
		if _, found := a.Object[key]; !found {
			return false
		}
	}

	return true
}

// fetchResourceState takes a list of ResourceIdentifiers,
// and returns the current state of each object in the cluster.
// TODO handle unavailable API cases gracefully.
func fetchResourceState(typedClient dynamic.Interface, desiredResources []types.ResourceIdentifier) (map[types.ResourceIdentifier]*types.Semistructured, error) {
	desiredToActual := map[types.ResourceIdentifier]*types.Semistructured{}
	for _, desired := range desiredResources {
		if invalidGVR(desired.GroupVersionResource) {
			continue
		}
		res, err := typedClient.Resource(
			desired.GroupVersionResource).Namespace(
			desired.NamespacedName.Namespace).Get(
			context.Background(), desired.NamespacedName.Name, metav1.GetOptions{})

		if apierrors.IsNotFound(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		semiStructured, err := types.UnstructuredToSemistructured(*res)
		semiStructured.Identifier = desired
		if err != nil {
			return nil, err
		}
		desiredToActual[desired] = &semiStructured
	}

	return desiredToActual, nil
}

func invalidGVR(gvr schema.GroupVersionResource) bool {
	if gvr.Resource == "" {
		return true
	}
	return false
}
