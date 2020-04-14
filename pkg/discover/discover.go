package discover

import (
	"errors"
	"fmt"
	"strings"

	"k8s.io/client-go/kubernetes"
)

type ApiGroupTypeVersions map[string]map[string][]string // Group -> type -> versions

// ignoreResourceTypes is a group->kind map of resources to ignore.
// There are some types of resources (EG nodes) that should not be managed.
var ignoreResourceTypes = map[string]map[string]struct{}{
	"": {
		"nodes": {},
		"pods":  {},
	},
	"leases coordination.k8s.io": {
		"leases": {},
	},
	"metrics.k8s.io": {
		"nodes": {},
		"pods":  {},
	},
}

// FetchApiVersions returns all API groups, kinds, and versions that the reconciler can manage.
func FetchApiVersions(client kubernetes.Interface) (ApiGroupTypeVersions, error) {
	resourceTypes := map[string]map[string][]string{}

	_, resourcesAndGroups, err := client.Discovery().ServerGroupsAndResources()
	if err != nil {
		return nil, err
	}
	for _, groupData := range resourcesAndGroups {
		// GroupVersion formatted as groupData/version, e.g. apps/v1
		gv := strings.Split(groupData.GroupVersion, "/")
		var g, v string   // Group, version.
		if len(gv) == 1 { // "v1"
			v = gv[0]
		} else if len(gv) == 2 {
			g = gv[0]
			v = gv[1]
		} else {
			return nil, errors.New(fmt.Sprintf("unexpected format for groupversion: '%v'", groupData.GroupVersion))
		}

		for _, resource := range groupData.APIResources {
			// Ignore a specific set of resources.
			if _, found := ignoreResourceTypes[g][resource.Name]; found {
				continue
			}

			// Ignore subresources.
			if strings.Contains(resource.Name, "/") {
				continue
			}

			// Ignore non-listable resources.
			canList := false
			for _, verb := range resource.Verbs {
				if verb == "list" {
					canList = true
					break
				}
			}
			if !canList {
				continue
			}

			// Make the group key if it doesn't exist yet.
			if _, found := resourceTypes[g]; !found {
				resourceTypes[g] = make(map[string][]string)
			}

			if _, found := resourceTypes[g][resource.Name]; !found {
				resourceTypes[g][resource.Name] = make([]string, 0, 1)
			}
			resourceTypes[g][resource.Name] = append(resourceTypes[g][resource.Name], v)
		}
	}

	return resourceTypes, nil
}
