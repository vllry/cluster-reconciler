package types

import (
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// ResourceIdentifier provides the identifiers needed to interact with any arbitrary object.
type ResourceIdentifier struct {
	GroupVersionKind schema.GroupVersionKind
	NamespacedName   types.NamespacedName
}

// Semistructured provides an Unstructured object, with guaranteed ResourceIdentifier fields.
// TODO: is this necessary? Are Unstructured objects sometimes missing name, namespace, etc?
type Semistructured struct {
	Identifier ResourceIdentifier
	// Unstructured is the full Unstructured object.
	Unstructured unstructured.Unstructured
}

// UnstructuredToSemistructured takes an Unstructured object,
// and returns a Semistructured object.
// The Semistructured object is guaranteed to be valid.
// An error is returned otherwise.
func UnstructuredToSemistructured(obj unstructured.Unstructured) (Semistructured, error) {
	gvk := obj.GroupVersionKind()
	if gvk.Kind == "" || gvk.Version == "" {
		return Semistructured{}, errors.New(fmt.Sprintf("incomplete groupversionkind: %s", gvk))
	}

	ns := types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
	if ns.Name == "" {
		return Semistructured{}, errors.New("missing object name")
	}

	return Semistructured{
		Identifier: ResourceIdentifier{
			GroupVersionKind: gvk,
			NamespacedName:   ns,
		},
		Unstructured: obj,
	}, nil
}

// Gvk2Gvr is a hack to covert GroupVersionKind to GroupVersionResource.
// The former is available in Unstructured objects,
// and the latter is required by the REST client.
// Group and version are the same, resource is NORMALLY lowercase, plural kind.
// TODO avoid the need for this, there is no guarantee that this conversion works for all resources.
func Gvk2Gvr(gvk schema.GroupVersionKind) schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    gvk.Group,
		Version:  gvk.Version,
		Resource: strings.ToLower(gvk.Kind) + "s", // TODO need to find the proper way to fetch the resource (e.g. "configmaps", not "ConfigMap").
	}
}
