package main

import (
	"errors"
	"fmt"
	"k8s.io/client-go/kubernetes"
	"strings"
)

type apiGroupTypeVersions map[string]map[string][]string // Group -> type -> versions

func fetchApiVersions(client kubernetes.Interface) (apiGroupTypeVersions, error) {
	resourceTypes := map[string]map[string][]string{}

	_, resourcesAndGroups, err := client.Discovery().ServerGroupsAndResources()
	if err != nil {
		return nil, err
	}
	for _, group := range resourcesAndGroups {
		// GroupVersion formatted as group/version, e.g. apps/v1
		gv := strings.Split(group.GroupVersion, "/")
		var g, v string   // Group, version.
		if len(gv) == 1 { // "v1"
			v = gv[0]
		} else if len(gv) == 2 {
			g = gv[0]
			v = gv[1]
		} else {
			return nil, errors.New(fmt.Sprintf("unexpected format for groupversion: '%v'", group.GroupVersion))
		}

		if _, found := resourceTypes[g]; !found {
			resourceTypes[g] = make(map[string][]string)
		}
		for _, resource := range group.APIResources {
			if _, found := resourceTypes[g][resource.Name]; !found {
				resourceTypes[g][resource.Name] = make([]string, 0, 1)
			}
			resourceTypes[g][resource.Name] = append(resourceTypes[g][resource.Name], v)
		}
	}

	return resourceTypes, nil
}
