# cluster-reconciler

This is prototype for reconciling the desired state of a cluster,
for arbitrary resources.
It pulls desired state from a simple in-progress hub API.

Pull requests + issues welcome, though out-of-the-blue PRs might be hard to reconcile with in-progress work.

* [Original vision](https://timewitch.net/post/2020-03-31-multicluster-workloads/)
* [SIG-Multicluster discussion](https://www.youtube.com/watch?v=c1louM3PoQU&list=PL69nYSiGNLP0HqgyqTby6HlDEz7i1mb0-&index=1&t=325)

# How to test

To build image, run `make docker-build`. Then you could push the image to your own repo.

Ensure you have a k8s cluster (e.g. a kind cluster) and export environment variables as needed:

```
export IMG={your image repo}/cluster-reconciler-controller
export HUBKUBECONFIG={kubeconfig path to the hub k8s cluster}
make deploy
```

The command would generate a controller deployment in `cluster-reconciler-system` namespace. 
Next, you could use the example in `config/sample` to deploy a simple work on the cluster.