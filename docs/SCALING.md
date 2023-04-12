# Scaling

<!-- ## Table of Contents

- [Why](#why)
- [Scaling with STUNner](#scaling-with-stunner)
  - [Scaling-up (out)](#scaling-up-out)
  - [Scaling-down (in)](#scaling-down-in)
  - [Graceful shutdown](#more-on-the-graceful-shutdown)
- [Example](#example) -->

## Why

[Autoscaling](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale) is one of the key features in Kubernetes. It is a feature in which the cluster is capable of increasing the number of pods as the demand for a service increases and decrease the number of pods as the requirement decreases.

The biggest advantage of using Kubernetes for autoscaling is it reduces operational costs and saves you from a lot of head-scratching by simplifying management and improving service quality. You do not need to guess the number of nodes or pods needed to run your workloads. It scales up or down dynamically based on resource utilization, thus saving you dollars.

When defining a Kubernetes deployment's [resource limits and requests](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/), it is a recommended practice to *not* go for an indefinite amount of CPUs ([which would be vertical scaling instead horizontal scaling](https://openmetal.io/docs/edu/openstack/horizontal-scaling-vs-vertical-scaling/)) but to keep the limits down and scale out (increase) the number of running pods if needed. Scaling a service might be scary and complex, this guide aims to overcome the fear.

And the other factors that might boost your need for scaling are as follows:
- smaller load on each instance, might result in better, more stable performance;
- less calls will be affected if an application instance fails for some reason.


## Scaling STUNner

STUNner comes with a full support for horizontal scaling. That means the number of pods can be increased and decreased according to the user's needs. 
In case the user wants to scale the instances of the `stunner` deployment, the Kubernetes built in `HorizontalPodAutoscaler` can be used. The triggering event can be based on arbitrary metrics but it is advised to use the currently utilized amount of CPU. If so, it is important to state that the HPA [uses the requested amount of resources](https://pauldally.medium.com/horizontalpodautoscaler-uses-request-not-limit-to-determine-when-to-scale-97643d808997) to determine when to scale-up or down the number of instances.

## Example

In this part we will walk you through a simple example on how to scale your `stunner` deployment. You might need to install the [metric-server](https://github.com/kubernetes-sigs/metrics-server#installation) in your cluster if it is not already installed.

A simple [HorizontalPodAutoscaler](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale-walkthrough/) config for scaling `stunner` would be:
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
Configured resources in the STUNner deployment are the following.
```yaml
resources:
  limits:
    cpu: 2
    memory: 512Mi
  requests:
    cpu: 500m
    memory: 128Mi
```

Initially there is only a single `stunner` instance in the cluster. As new calls start to come in the amount of utilized CPU is increasing.  

Scale out is triggered when CPU usage of the `stunner` pod reaches 1500m (three times the requested CPU) core CPU.

More calls come in and the summarized CPU usage by `stunner` pods reach 3000m core CPU, this is 1500m core on average. Scale out happens again. 

As participants leave the room, the load will drop and the average CPU will fall under 3000m. Scaling in happens as the `HPA` removes one of the `stunner` instances. And so on...

###  Scaling-up (out)

When adding new instances to the existing `stunner` replica set we don't expect any breaking changes. The infrastructure for existing calls won't get interrupted, it stays the same as before the upscale event. Only when new user calls come in the cloud loadbalancer have an additional endpoint to choose from. This way we can achieve that STUNner is never going to be the bottleneck in the system. Obviously, if you have the needed computational power under your cluster.

Scaling-up the number of instances in the `stunner` deployment should be done based on the CPU usage of the replicas. As it was mentioned in the [section Scaling STUNner](#scaling-stunner) the `HorizontalPodAutoscaler` is using the requested resources to determine when to scale up.

### Scaling-down (in)

When removing existing instances from the `stunner` replica set there are some things to keep in mind. What happens to the existing calls on a to-be-removed pod? Is there a way to keep them? 

In worst case scenario all connections will be dropped and the pod terminates itself, thus we lost all running connections/calls on the removed (scaled down) STUNner instance. A slightly better scenario is that the [ICE restart](https://developer.mozilla.org/en-US/docs/Web/API/RTCPeerConnection/restartIce) mechanism will kick in at the client side, basically it will reset the current ICE candidates and reconnect again the same way it did initially but it takes a second or two and not even supported by all browsers. 

The *default* mode of operation in STUNner is that when it gets removed it will [shutdown gracefully](#more-on-the-graceful-shutdown). This means that `stunner` pods will remain alive as long as there are active allocations via the embedded TURN server, and a pod will automatically remove itself once all allocations through it are deleted or timed out. It is important that the *terminating* pod will not be counted in by the `HorizontalPodAutoscaler` as a running replica, thus its CPU usage won't be taken into account either. Note that one can switch graceful shutdown off and then they will fall back to the mentioned ICE restarts.

#### More on the graceful shutdown

Note that the default TURN refresh lifetime is [10 minutes](https://www.rfc-editor.org/rfc/rfc8656#section-3.2-3) so STUNner may remain alive well after the last client goes away. This occurs when an UDP allocation is left open by a client (spontaneous UDP client-side connection closure cannot be reliably detected by the server). In such cases, after 10 mins the allocation will timeout and get deleted, which will then let `stunnerd` to terminate. 
This feature enables the full support for graceful scale-down: the user can scale the number of `stunner` instances up and down as they wish and no harm should be made to active client connections meanwhile. 
Caveats: 
- currently the max lifetime for `stunner` to remain alive for 1 hour after being deleted: this means that `stunner` will remain active only for 1 hour after it has been deleted/scaled-down. You can always set this in by adjusting the `terminationGracePeriod` on your `stunnerd` pods.
- if there are active (or very recent) TURN allocations then the `stunner` pod may refuse to be removed after a kubectl delete. Use `kubectl delete pod --grace-period=0 --force -n stunner stunner-XXX` to force removal.

## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## License

Copyright 2021-2023 by its authors. Some rights reserved. See [AUTHORS](../AUTHORS).

MIT License - see [LICENSE](../LICENSE) for full text.
