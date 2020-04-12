package main

import (
	"context"
	"fmt"
	"github.com/vllry/cluster-reconciler/pkg/parse"
	"github.com/vllry/cluster-reconciler/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp" // Needed for GCP auth.
	"k8s.io/client-go/tools/clientcmd"
	"reflect"
)

func main() {
	config, _ := clientcmd.BuildConfigFromFlags("", "/Users/vallery/.kube/config")
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	err = reconcileCluster(client, dynamicClient)
	if err != nil {
		panic(err)
	}
}

const reconcilerAnnotationKey = "cluster-reconciler-managed"
const reconcilerAnnotationValue = "true"

func fetchDesiredResources() (map[types.ResourceIdentifier]types.Semistructured, error) {
	cm := `apiVersion: v1
kind: ConfigMap
metadata:
  name: reconcile-demo
  namespace: default
data:
  ui.properties: |
    color=pink
`

	resourceList, err := parse.ParseYaml(cm)
	if err != nil {
		return nil, err
	}

	resources := make(map[types.ResourceIdentifier]types.Semistructured)
	for _, obj := range resourceList {
		semistructured, err := types.UnstructuredToSemistructured(obj)
		if err != nil {
			return nil, err
		}
		resources[semistructured.Identifier] = semistructured
	}

	return resources, nil
}

// fetchResourceState takes a list of ResourceIdentifiers,
// and returns the current state of each object in the cluster.
// TODO handle unavailable API cases gracefully.
func fetchResourceState(client dynamic.Interface, desiredResources []types.ResourceIdentifier) (map[types.ResourceIdentifier]*types.Semistructured, error) {
	desiredToActual := map[types.ResourceIdentifier]*types.Semistructured{}
	for _, desired := range desiredResources {
		desiredToActual[desired] = nil // Indicate nothing was found.

		res, err := client.Resource(
			types.Gvk2Gvr(desired.GroupVersionKind)).Namespace(
				desired.NamespacedName.Namespace).Get(
					context.Background(), desired.NamespacedName.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		semiStructured, err := types.UnstructuredToSemistructured(*res)
		if err != nil {
			return nil, err
		}
		desiredToActual[desired] = &semiStructured
	}

	return desiredToActual, nil
}

func reconcileCluster(client kubernetes.Interface, dynamicClient dynamic.Interface) error {
	//  Fetch resources from the cluster inventory (desired actualState).
	// These will be a set of YAML or JSON Kubernetes Objects.
	desiredObjects, err := fetchDesiredResources()
	if err != nil {
		return err
	}

	_, err = fetchApiVersions(client)
	if err != nil {
		return err
	}

	// desiredResourceList contains, for every desired object, all identifiers necessary to fetch the actual object for the cluster.
	desiredResourceList := make([]types.ResourceIdentifier, 0, len(desiredObjects)) // TODO ensure deduplication? User could supply the same object twice.
	for id := range desiredObjects {
		desiredResourceList = append(desiredResourceList, id)
	}

	// Fetch the current actualState of all desired resources.
	currentResources, err := fetchResourceState(dynamicClient, desiredResourceList)
	if err != nil {
		return err
	}

	// Create/update desired resources.
	//desiredRes := make(map[ResourceIdentifier]unstructured.Unstructured) // TODO pick better typing, populate
	for obj, desiredState := range desiredObjects {
		if actualState, found := currentResources[obj]; found {
			// sameIntent(desiredState.unstructured, actualState.unstructured)
			//!equality.Semantic.DeepEqual(desiredState, actualState)
			if true {
				fmt.Println("Needs update")
				fmt.Println(desiredState)
				fmt.Println(actualState)
				// Update resource.
				err = updateResource(dynamicClient, desiredState)
				if err != nil {
					return err
				}
			} else {
				fmt.Println("No update needed")
			}
		} else {
			fmt.Println("Needs creating")
			err = createResource(dynamicClient, desiredState)
			if err != nil {
				return err
			}
		}
	}

	// Do a diff for any actual resources that are missing from the desired actualState.
	// DELETE them.

	// Do a diff for any desired resources that are MISSING or DIFFERENT in the actual actualState.
	// CREATE the resources that are missing.
	// UPDATE the ones that are different.

	return nil
}

func updateResource(client  dynamic.Interface, resource types.Semistructured) error {
	_, err := client.Resource(types.Gvk2Gvr(resource.Identifier.GroupVersionKind)).Namespace(resource.Identifier.NamespacedName.Namespace).Update(
		context.Background(),
		&resource.Unstructured,
		metav1.UpdateOptions{},
		)
	return err
}

func createResource(client  dynamic.Interface, resource types.Semistructured) error {
	_, err := client.Resource(types.Gvk2Gvr(resource.Identifier.GroupVersionKind)).Namespace(resource.Identifier.NamespacedName.Namespace).Create(
		context.Background(),
		&resource.Unstructured,
		metav1.CreateOptions{},
	)
	return err
}

// sameIntent returns true if, barring specific exceptions, there are unequal fields between expected and actual.
func sameIntent(expected unstructured.Unstructured, actual unstructured.Unstructured) bool {
	// Check expect -> actual.
	for key, expectVal := range expected.Object {
		if actualVal, found := actual.Object[key]; found {

			if key == "metadata" {
				// Ignore metadata FOR NOW
			} else if key == "status" {
				continue // Ignore status.
			} else {
				/*if interfacesDifferent(expectVal, actualVal) {
					return true
				}*/
				if !reflect.DeepEqual(expectVal, actualVal) {
					return false
				}
			}

		}
	}

	// We know everything in exacted is in actual.
	// Now we only need to check that actual has no extra fields.
	for key, _ := range actual.Object {
		if _, found := expected.Object[key]; !found {
			return false
		}
	}

	return true
}

func interfacesDifferent(a interface{}, b interface{}) bool {
	if reflect.TypeOf(a) != reflect.TypeOf(b) {
		return true
	}

	switch a.(type) {
	case int:
		return false
	default:
		return false
	}
}