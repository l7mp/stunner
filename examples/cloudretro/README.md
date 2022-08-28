# STUNner & CloudRetro: Cloudgaming and their nasty UDP streams in Kubernetes

In this demo, we will install [CloudRetro](https://github.com/giongto35/cloud-game) on an existing Kubernetes cluster, and use STUNner to establish and redirect the UDP connection to its proper endpoint.

CloudRetro is a simplified cloud-gaming service using WebRTC for multimedia, thus with the need of UDP-forwarding, which can be a pain under RTP.
With STUNner, this issue can be solved.

![STUNner & CloudRetro architecture](../../doc/stunner_cloudretro.svg)

The upcoming steps are the following:
* Install CloudRetro demo on your cluster
* Install and configure STUNner with Gateway Operator
* Configure CloudRetro to use STUNner

## Installation

### Prerequisities

* A functional Kubernetes cluster (autopilot works as well)
* kubectl
* Docker
* git
* helm

### Additional preparations

This demo provides some helpful scripts for the setup, so this repository should be cloned;

```console
git clone https://github.com/l7mp/stunner
cd stunner/examples/cloudretro
```

### Quick-installing CLoudRetro

The included script sets up a demo CloudRetro service, with it's enviroment.
For a more-detailed setup and architecture-overview, please visit the [CloudRetro](https://github.com/giongto35/cloud-game) repository.
For this demo, we are going to use a forked-image, you can find it [here](https://github.com/l7mp/cloudretro-demo-build).

```console
kubectl apply -f cloudretro-setup-coordinator.yaml
kubectl apply -f cloudretro-setup-workers.yaml
```

After it's complete, configuring and restarting the Worker deployment is needed, for that purpose a script is included:

```console
chmod +x worker-config.sh
./worker-config.sh
```

This will configure our Workers to successfully find their Coordinator.

In the CloudRetro demo, we will be having multiple HTTPS web services, one linked to port 8000, and the other is to port 9000.
From this, port 8000 is essential, while without port 9000 which is necessary for CloudRetro to work as intended, the demo should still work.

If everything is successful, Kubernetes should assign an external address to the exposed service of the Coordinator, which clients will connect to.
Running to following command will result the assigned address in a decimal four-octet format:

```console
# Cat is present because some terminals do not breakline  ^._.^
cat | kubectl get service -n cloudretro coordinator-lb-svc -o jsonpath='{.status.loadBalancer.ingress[0].ip}'
```

In cases Kubernetes won't assign an external IP to your service, You will need to use NodePort service instead.

Clients connecting to this URL on a browser; `http://<service-ip>:8000` (don't forget to swap <service-ip> with the one mentioned above) will be presented a console-looking website, with seemingly no active additional service. The CloudRetro is running and working, but the endpoints can not establish an ICE connection. You can take a look at it in the console as well, this is because ICE can not create acceptable candidates through Kubernetes NATs, and not even a STUN server would help. RTP is unfit to handle this issue.
That's why we need STUNner to make it work.


### Installing STUNner

To install STUNner with a Gateway Operator, a helm chart is used. STUNner comes with an Operatorless Mode as well, for that please refer to [STUNner installation](https://github.com/l7mp/stunner#getting-started).
For more details about Gateway Operator and it's use, please visit [here](https://github.com/l7mp/stunner-gateway-operator).

```console
helm repo add stunner https://l7mp.io/stunner
helm repo update

helm install stunner-gateway-operator stunner/stunner-gateway-operator --create-namespace --namespace stunner

helm install stunner stunner/stunner --namespace stunner
```

By default, it will install STUNner with a Gateway Operator.

Next step is to register STUNner with the Kubernetes Gateway API, with a GatewayClass what we are going to instantiate later on, and a default GatewayConfig for it.

```console
kubectl apply -f stunner-gwcc.yaml
```

This script will install these in no time.
The default config has the 'username' as 'user-1' and 'password' as 'pass-1'. Feel free to modify these values.

Now we are going to apply an instance of a Gateway, which will serve as a... yes, you got it right, a gateway for our WebRTC streams.
The below Gateway specification will expose the STUNner gateway over the STUN/TURN listener service running on the UDP listener port 3478. STUNner will await clients to connect to this listener port and, once authenticated, let them connect to the services running inside the Kubernetes cluster; meanwhile, the NAT traversal functionality implemented by the STUN/TURN server embedded into STUNner will make sure that clients can connect from behind even the most over-zealous enterprise NAT or firewall.

```console
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: Gateway
metadata:
  name: udp-gateway
  namespace: stunner
spec:
  gatewayClassName: stunner-gatewayclass
  listeners:
    - name: udp-listener
      port: 3478
      protocol: UDP
EOF
```

With this, our Gateway Operator will create us a whole new LoadBalancer service for this Gateway, from which we can establish connection with STUNner. Although, this does not specify an endpoint for the UDP streams, so we are going to need an attached UDProute as well.

Attaching an UDP route to the Gateway, so that clients will be able to connect via the public STUN/TURN listener UDP:3478 to the Worker LoadBalancer service we've created earlier (optionally with the cloudretro-setup.yaml).
In our case, we named it worker-ci-udp-svc. Don't forget to specify the namespace, even if its in the default one.
This is where we are connecting CloudRetro and STUNner.

```console
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: UDPRoute
metadata:
  name: worker-udp-route
  namespace: stunner
spec:
  parentRefs:
    - name: udp-gateway
  rules:
    - backendRefs:
        - name: worker-ci-udp-svc
          namespace: cloudretro
EOF
```

### Configure CloudRetro to use STUNner

Now that we've set up STUNner, it is ready for action. Although, for this to work, we have to configure CloudRetro to use it as well.
Running the following, minimalistic script will do it for You;

```console
chmod +x coordinator-config.sh
./coordinator-config.sh
```

It will configure the Coordinator to provide STUNner as a TURN server for the clients, with the proper credentials for authentication.

### Joy

And now everything is set as it should be, You are able to play SuperMario on Your CloudRetro installed in Kubernetes.
Thanks STUNner.

![Supre Maro let's go](mario-super.gif)

## But if You want more...

Time to upgrade Your whole service to multi-cluster. The process itself is not so hard, You can do it all with an extra few steps.
To make this context more clear, we will be talking about PRIMARY (the one You already have set up, having a coordinator) and SECONDARY clusters.

For the next steps, You might want to make your life easier and use the included scripts later on, but for that You need to set up [contexts](https://kubernetes.io/docs/tasks/access-application-cluster/configure-access-multiple-clusters/) for your clusters, so kubectl can handle both at the same time.
The naming `'primary'` and `'secondary1'` is completely custom, just make sure to have distinguishable names, the scripts use these context names to determine which cluster it should apply to.

(Note; You can always do the following steps manually, completely eliminating the need for contexts)

### CloudRetro setup on SECONDARY clusters

Once You have a new SECONDARY cluster You want to integrate the CloudRetro with your already working one, the below yaml script will deploy workers only without a coordinator.

```console
kubectl apply -f cloudretro-setup-workers.yaml --context secondary1
```

Next, to make these workers connect to the coordinator, we are going to need the Coordinators address from the PRIMARY cluster. Remember, this time we don't have a coordinator locally. This address is also the one clients can connect to, or the external IP of your coordinator-lb-svc LoadBalancer service on Your PRIMARY cluster.
The script below will configure the SECONDARY workers to connect to the remote-cluster coordinator;

```console
chmod +x worker-config.sh
./worker-config.sh primary secondary1
```

### STUNner setup on SECONDARY clusters

This time we won't get into the details, for a detailed installation see the [Installing STUNner](https://github.com/l7mp/stunner/tree/main/examples/cloudretro#installing-stunner) chapter.
The following script will repeat the exact same steps on Your SECONDARY cluster;

```console
chmod +x stunner-setup-for-cloudretro.sh
./stunner-setup-for-cloudretro.sh secondary1
```

TODO: shared controll-plane setup, so one instance of STUNner will be enough

### Configure Coordinator for SECONDARY cluster workers

Fortunately CloudRetro now supports multiple ICE-servers. In this step You will need to add the address of our SECONDARY STUNner gateway service as well.
Running the following script will do that for You; although, You need to specify the PRIMARY and SECONDARY cluster contexts as arguments.
The script supports up to 10 SECONDARY clusters, but You can always just change the limit.

```console
chmod +x coordinator-config.sh
./coordinator-config.sh primary secondary1
```

### Joy

And Your CloudRetro is now officially a multicluster service! Congratulations!
Keep in mind, that CloudRetro will connect to the relatively closest cluster if it's multiregion. This happens by a HTTP request from the Client to all known worker-groups, which the client chooses the closest one from.

Although, you can manually choose workers by clicking the little `w` button under `options`.

For example, two videos are included. Both were recorded from a client in the European region with 240 FPS.
It is obvious, that the one made with an US-region worker (48 frame ~200,00ms) has latency between invoke and response much larger than in the European one (20 frame ~83,33ms).

| US video | EU video |
|---|---|
| ![cloudretro_us.mp4](https://github.com/l7mp/stunner/raw/main/examples/cloudretro/cloudretro_us.mp4) | ![cloudretro_eu.mp4](https://github.com/l7mp/stunner/raw/main/examples/cloudretro/cloudretro_eu.mp4) |


## Clean up

Delete the demo deployments and services created, and also the gateway and UDPRoute we have made for STUNner using the below commands.

```console
kubectl delete -f cloudretro-setup-coordinator.yaml
kubectl delete -f cloudretro-setup-workers.yaml
kubectl delete -f cloudretro-stunner-cleanup.yaml
```

## Help

STUNner development is coordinated in Discord, feel free to [join](https://discord.gg/DyPgEsbwzc).

## License

Copyright 2021-2022 by its authors. Some rights reserved. See [AUTHORS](../../AUTHORS).

MIT License - see [LICENSE](../../LICENSE) for full text.

## Acknowledgments

Demo adopted from [CloudRetro](https://github.com/giongto35/cloud-game).

Please note that this demo is only for showcasing STUNner in this enviroment.
Many CloudRetro functions do not work; shared-save, shared-roms, areas, etc.
