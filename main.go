package main

import (
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // Needed for misc auth.
	"k8s.io/client-go/tools/clientcmd"

	"github.com/vllry/cluster-reconciler/pkg/reconcile"
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

	err = reconcile.ReconcileCluster(client, dynamicClient)
	if err != nil {
		panic(err)
	}
}
