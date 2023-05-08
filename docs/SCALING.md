# Scaling

[Autoscaling](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale) is one of
the key features in Kubernetes. This means that Kubernetes will automatically increase the number
of pods that run a service as the demand for the service increases, and reduce the number of pods
when the demand drops. This improves service quality, simplifies management, and reduces
operational costs by avoiding the need to over-provision services to the peak load. Most
importantly, autoscaling saves you from having to guess the number of nodes or pods needed to run
your workload: Kubernetes will automatically and dynamically resize your workload based on demand.

Further factors to autoscale your WebRTC workload are:
- smaller load on each instance: this might result in better and more stable performance;
- smaller blast radius: less calls will be affected if a pod fails for some reason.

Autoscaling a production service, especially one as sensitive to latency and performance as WebRTC,
can be challenging. This guide will provide the basics on autoscaling; see the [official
docs](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale) for more detail.

## Horizontal scaling

It is a good practice to scale Kubernetes workloads
[horizontally](https://openmetal.io/docs/edu/openstack/horizontal-scaling-vs-vertical-scaling)
(that is, by adding or removing service pods) instead of vertically (that is, by migrating to a
more powerful server) when demand increases. Correspondingly it is a good advice to set the
[resource limits and
requests](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/) to the
bare minimum and let Kubernetes to automatically scale out the service by adding more pods if
needed.  Note that that HPA [uses the requested amount of
resources](https://pauldally.medium.com/horizontalpodautoscaler-uses-request-not-limit-to-determine-when-to-scale-97643d808997)
to determine when to scale-up or down the number of instances.

STUNner comes with a full support for horizontal scaling using the the Kubernetes built-in
[HorizontalPodAutoscaler](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale)
(HPA). The triggering event can be based on arbitrary metric, say, the [number of active client
connections](#MONITORING.md) per STUNner dataplane pod. Below we use the CPU utilization for
simplicity.

Scaling STUNner *up* occurs by Kubernetes adding more pods to the STUNner dataplane deployment and
load-balancing client requests across the running pods. This should (theoretically) never interrupt
existing calls, but new calls should be automatically routed by the cloud load balancer to the new
endpoint(s). Automatic scale-up means that STUNner should never become the bottleneck in the
system. Note that in certain cases scaling STUNner up would require adding new Kubernetes nodes to
your cluster: most modern hosted Kubernetes services provide horizontal node autoscaling out of the
box to support this.

Scaling STUNner *down*, however, is trickier. Intuitively, when a running STUNner dataplane pod is
terminated on scale-down, all affected clients with active TURN allocations on the terminating pod
would be disconnected. This would then require clients to go through an [ICE
restart](https://developer.mozilla.org/en-US/docs/Web/API/RTCPeerConnection/restartIce) to
re-connect, which may cause prolonged connection interruption and may not even be supported by all
browsers.

In order to avoid client disconnects on scale-down, STUNner supports a feature called [graceful
shutdown](https://cloud.google.com/blog/products/containers-kubernetes/kubernetes-best-practices-terminating-with-grace). This
means that `stunnerd` pods would refuse to terminate as long as there are active TURN allocations
on them, and automatically remove themselves only once all allocations are deleted or timed out. It
is important that *terminating* pods will not be counted by the HorizontalPodAutoscaler towards the
average CPU load, and hence would not affect autoscaling decisions. In addition, new TURN
allocation requests would never be routed by Kubernetes to terminating `stunnerd` pods.

Graceful shutdown enables full support for scaling STUNner down without affecting active client
connections. As usual, however, some caveats apply:
1. Currently the max lifetime for `stunnerd` to remain alive is 1 hour after being deleted: this
   means that `stunnerd` will remain active only for 1 hour after it has been deleted/scaled-down
   even if active allocations would last longer. You can always set this by adjusting the
   `terminationGracePeriod` on your `stunnerd` pods.
2. STUNner pods may remain alive well after the last client connection goes away. This occurs when
   an TURN/UDP allocation is left open by a client (spontaneous UDP client-side connection closure
   cannot be reliably detected by the server). As the default TURN refresh lifetime is [10
   minutes](https://www.rfc-editor.org/rfc/rfc8656#section-3.2-3), it may take 10 minutes until all
   allocations time out, letting `stunnerd` to finally terminate.
3. If there are active (or very recent) TURN allocations then the `stunnerd` pod may refuse to be
   removed after a `kubectl delete`. Use `kubectl delete pod --grace-period=0 --force stunner-XXX`
   to force removal.

### Example

Below is a simple
[HorizontalPodAutoscaler](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale-walkthrough/)
config for autoscaling `stunnerd`. The example assumes that the [Kubernetes metric
server](https://github.com/kubernetes-sigs/metrics-server#installation) is available in the
cluster.

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: hpa-stunner
  namespace: stunner
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: stunner
  minReplicas: 1
  maxReplicas: 10
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 300
```

Here, `scaleTargetRef` selects the STUNner Deployment named `stunnerd` as the scaling target and
the deployment will always run at least 1 pod and at most 10 pods. Understanding how Kubernetes
chooses the number of running pods is, however, a bit tricky.

Suppose that the configured resources in the STUNner deployment are the following.

```yaml
resources:
  limits:
    cpu: 2
    memory: 512Mi
  requests:
    cpu: 500m
    memory: 128Mi
```

Suppose that, initially, there is only a single `stunnerd` pod in the cluster. As new calls come
in, CPU utilization is increasing. Scale out will be triggered when CPU usage of the `stunnerd` pod
reaches 1500 millicore CPU (three times the requested CPU). If more calls come and the total CPU
usage of the `stunnerd` pods reaches 3000 millicore, which amounts to 1500 millicore on average,
scale out would happen again. When users leave, load will drop and the total CPU utilization will
fall under 3000 millicore. At this point Kubernetes will automatically scale-in and remove one of
the `stunnerd` instances. Recall, this would never affect existing connections thanks to graceful
shutdown.

