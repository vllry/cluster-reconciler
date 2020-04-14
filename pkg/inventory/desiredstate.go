package inventory

import (
	"github.com/vllry/cluster-reconciler/pkg/parse"
	"github.com/vllry/cluster-reconciler/pkg/types"
)

// FetchDesiredResources pretends to fetch a desired state,
// and returns a map of Semistructured objects.
func FetchDesiredResources() (map[types.ResourceIdentifier]types.Semistructured, error) {
	// Yaml would come from... somewhere.
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
