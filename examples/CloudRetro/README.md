#STUNner & CloudRetro: Cloudgaming and their nasty UDP streams in Kubernetes

In this setup documentation, we will install [CloudRetro](https://github.com/giongto35/cloud-game) on an existing Kubernetes cluster, and use STUNner to establish and redirect the UDP connection to its proper Pod(?)

CloudRetro is a simplified cloud-gaming service using WebRTC for multimedia, thus with the need of UDP-forwarding, which can be a pain under RTCP. With STUNner, an in-cluster TURN-server, this issue can be solved.

The upcoming steps are the following:
* Install CloudRetro on your cluster
* Set up CloudRetro configuration and enviroment (TODO)
* Install and config STUNner
* Set up and config STUNNer gateway

##Installation

###Prerequisities

* A functional Kubernetes cluster (autopilot also should be working)
* kubectl
* Docker
* GNU Make
* git
* go
* helm

###Installing CLoudRetro

Lets create a new namespace for CloudRetro;

'''console
kubectl create namespace cloudretro
'''

####CloudRetro config

First step is, to make our lives easier, to create a configMap the CloudRetro pods will be using. For that we need to download the original config file, and make a configMap Kubernetes resource from it. 

'''console
wget https://raw.githubusercontent.com/giongto35/cloud-game/master/configs/config.yaml
kubectl create configmap cloudretro-config --from-file=config.yaml --namespace=cloudretro
'''
For the sepcific config details, please take a look at the original [file](https://github.com/giongto35/cloud-game/blob/master/configs/config.yaml).

####Cloudretro Coordinator deployment & service

Now, we will need the container-image for CloudRetro. The simplest way is to download a very lightly modified version (for easier handling in k8s enviroment), but you can also build your own image [here](https://github.com/giongto35/cloud-game).

After you got your image, we can jump right into deployments and pods. For this, I am using the image I've modified for this purpose (which is far from flawless, but it works).
The following yaml file will make a deployment for the Coordinator, best to leave the scaling at 1 (TODO, worker sign in problems). By default, the Coordinator HTTP service will listen at port 8000, if you changed this in the config, make sure to change it here as well.

'''console
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: coordinator-deployment
  namespace: cloudretro
spec:
  replicas: 1
  selector:
    matchLabels:
      app: coordinator
  template:
    metadata:
      labels:
        app: coordinator
    spec:
      containers:
      - name: coordinator
        image: valniae/snekyrepo:cloudimage4
        volumeMounts:
        - name: cloudretro-confmap-vol
          mountPath: "/usr/local/share/cloud-game/configs/"
          readOnly: true
        command: ["coordinator"]
        args: ["--v=5"]
        ports:
        - containerPort: 8000
      volumes:
      - name: cloudretro-confmap-vol
        configMap:
          name: cloudretro-config
EOF
'''

This should leave us with a working pod of coordinator:

'''console
kubectl get pods -n cloudretro

NAME                                             READY   STATUS    RESTARTS   AGE
coordinator-deployment-54hidoyoureadthis-qpnsx   1/1     Running   0          3m23s
'''

Now that we have a running deployment of Coordinator(s), we will need a service for them. So in case a pod would die for whatever reason, the target address wouldn't change for the users. In case you require, use a firewall-open TCP port for Nodeport, don't forget to uncomment.

'''console
kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: coordinator-lb-svc
  namespace: cloudretro
spec:
  selector:
    app: coordinator
  ports:
    - port: 8000
      targetPort: 8000
      #nodePort: 30001
  type: LoadBalancer
EOF
'''

Now we should be having a fancy LoadBalancer service; whatever NodePort you've decided on (if).

'''console
kubectl get services -n cloudretro

NAME                 TYPE           CLUSTER-IP            EXTERNAL-IP            PORT(S)          AGE
coordinator-lb-svc   LoadBalancer   <YOUR_C_CLUSTER_IP>   <YOUR_C_EXTERNAL_IP>   8000:30618/TCP   2m29s
'''

Now you should be able to successfully connect to <YOUR_EXTERNAL_IP>:8000 service, leaving you on a nice, blank controller page. Have no worries, without workers we didn't expect you to play any games on it.
Great job nonetheless, you are a Kubernetes administrator already.

####Cloudretro Worker deployment & services

Similar to the Coordinator, we need a worker deployment. Only the names and the startup command should differ, the provided container-image contains both the coordinator and worker processes. Although, this time, we scale up the deployment, there is no reason not to. In this example, we will go with 2.

For the client to establish and run a ping-check on the workers, they run a HTTP server as well, in our case, on the port 9000. Other than that, we also need an UDP port it can forward its future stream, in our case 8443;

'''console
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: worker-deployment
  namespace: cloudretro
spec:
  replicas: 2
  selector:
    matchLabels:
      app: worker
  template:
    metadata:
      labels:
        app: worker
    spec:
      containers:
      - name: worker
        image: valniae/snekyrepo:cloudimage4
        volumeMounts:
        - name: cloudretro-confmap-vol
          mountPath: "/usr/local/share/cloud-game/configs/"
          readOnly: true
        command: ["worker"]
        args: ["--v=5"]
        ports:
        - containerPort: 9000
          containerPort: 8443
      volumes:
      - name: cloudretro-confmap-vol
        configMap:
          name: cloudretro-config
EOF
'''

This should leave us with working pods of workers:

'''console
kubectl get pods -n cloudretro

NAME                                      READY   STATUS    RESTARTS   AGE
coordinator-deployment-546ff547cb-qpnsx   1/1     Running   0          18h
worker-deployment-c4d4d8f8f-2dwdn         1/1     Running   0          5m25s
worker-deployment-c4d4d8f8f-f8b4h         1/1     Running   0          5m26s
'''

Now we will need to make a service for the workers HTTP server just like for the coordinator. For this, we will be using the port 9000. Again, if necessary, you can configure the NodePort as well.
(note: we are using LoadBalancer, because we will need to expose that for the public)
(won't a LB kill the purpose of a ping-check though?)

'''console
kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: worker-lb-svc
  namespace: cloudretro
spec:
  selector:
    app: worker
  ports:
    - port: 9000
      targetPort: 9000
      #nodePort: 30002
  type: LoadBalancer
EOF
'''

Another k8s service will be needed for the UDP service, although this time we will use a ClusterIP. Because of STUNner, we will be using the in-cluster network for that.
For the UDP port, you should be using the one you've used at the worker deployment. NodePort is not needed here, because we are not planning to expose this service.

'''console
kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: worker-ci-udp-svc
  namespace: cloudretro
spec:
  selector:
    app: worker
  ports:
    - protocol: UDP
      port: 8443
      targetPort: 8443
  type: ClusterIP
EOF
'''

Now, checking up on services should be looking like this:

'''console
kubectl get services -n cloudretro

NAME                 TYPE           CLUSTER-IP            EXTERNAL-IP             PORT(S)          AGE
coordinator-lb-svc   LoadBalancer   <YOUR_C_CLUSTER_IP>   <YOUR_C_EXTERNAL_IP>    8000:30618/TCP   18h
worker-ci-udp-svc    ClusterIP      10.104.129.8          <none>                  8443/UDP         3m2s
worker-lb-svc        LoadBalancer   <YOUR_W_CLUSTER_IP>   <YOUR_W_EXTERNAL_IP>    9000:32681/TCP   3m10s
'''

Note down <YOUR_C_CLUSTER_IP>, <YOUR_C_EXTERNAL_IP>, <YOUR_W_EXTERNAL_IP>. For the next configuration steps, we will need these.

####CloudRetro config once again

Fortunately we've made our configmap in the first step, so our lives are a little easier now.
We are going to config up CloudRetro with these values:

* <YOUR_C_CLUSTER_IP>  - The in-cluster IP of your coordinator, the workers should connect to this.
* <YOUR_C_EXTERNAL_IP> - The exposed IP of your coordinator, clients should use this to access the service at the sepcified port (8000 in this example).
* <YOUR_W_EXTERNAL_IP> - The exposed IP of the collection of your workers.

To use these values, we need to edit the configmap weve made:

'''console
kubectl edit configmap -n cloudretro cloudretro-config
'''

Then, we should be presented an ugly looking config. Finding the proper attributes and overwriting their values is the next step.
You can find the nice looking version of it [here](https://github.com/giongto35/cloud-game/blob/master/configs/config.yaml), and their line numbers presented as ln.

* (ln. 41) coordinator -> server -> address: Should be the port youve used during the coordinator deployment (8000 in this example)
* (ln. 59) worker -> network -> coordinatorAddress: This should be your <YOUR_C_CLUSTER_IP>:PORT, where the port is the value youve used at the coordinator deployment (8000 in this example)
* (ln. 65) worker -> network -> publicAddress: This should be your <YOUR_W_EXTERNAL_IP>
* (ln. 65) worker -> server -> address: Should be the TCP port youve used during the worker deployment (9000 in this example)

After overwriting and saving these values, you will need to restart the deployments for this changes to take effect:

'''console
kubectl rollout restart deployment -n cloudretro worker-deployment
kubectl rollout restart deployment -n cloudretro coordinator-deployment
'''

Now, if you connect to the coodinator HTTPs, you should be welcomed with the same screen. But if you check your worker list with the little w button on the top-left corner of the console, you should see your workers.
Congratulations, you've (almost) set up the CloudRetro on your cluster!

If you check your browsers console, you could be seeing an ICE-candidate exchange, which ultimately results in fail. That is because if we take a look at the worker-pod log the client is trying to build a connection with, only generates a host type Ice candidate, which is unreachable for the client due to the massive amounts of NATs its probably behind of. This is when STUNner comes into picture.



###Installing STUNner

STUNner is a Kubernetes ingress gateway for WebRTC, it exposes a standards-compliant STUN/TURN gateway for clients to access your virtualized WebRTC infrastructure running in Kubernetes.
We will be using STUNner to forward our WebRTC media stream.

First, we will deploy STUNner as a turn server, and configure it, then create a configurable Gateway for it, so we can forward an UDP Route through that. And all this inside the cluster. Marvelous.

First of all, create a namespace for stunner:

'''console
kubectl create namespace stunner
'''

####Installing STUNner gateway operator

This will be a bit tricky and unlogical, but it wil be fixed asap.

Clone the STUNner gateway operator git repo and enter into the root directory:

'''console
git clone https://github.com/l7mp/stunner-gateway-operator.git
cd stunner-gateway-operator
'''

Install the Kubernetes Gateway CRDs from the official source (these are not part of the STUNner distribution).

'''console
kubectl apply -k "github.com/kubernetes-sigs/gateway-api/config/crd?ref=v0.4.3"
'''

Deploy the STUNner Kubernetes Gateway Operator:

'''console
make install
make deploy
'''

Confirm the operator is running in stunner-gateway namespace:

'''console
kubectl get pods -n stunner-gateway-operator-system

NAME                                                          READY   STATUS    RESTARTS   AGE
stunner-gateway-operator-controller-manager-65dbf8fb4-hjrjr   2/2     Running   0          42m
'''

####Configuring the operator

The STUNner operator (partially) implements the official Kubernetes Gateway API, which allows you to interact with STUNner using the convenience of kubectl and declarative YAML configuration.

Create a GatewayClass. This will serve as the root level configuration for your STUNner deployment and specifies the name and the description of the service implemented by the GatewayClass, as well as a Kubernetes resource (the GatewayConfig resource given under the parametersRef) that will define some general parameters for the data-plane implementing the GatewayClass.

'''console
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: GatewayClass
metadata:
  name: stunner-gatewayclass
  namespace: stunner
spec:
  controllerName: "stunner.l7mp.io/gateway-operator"
  parametersRef:
    group: "stunner.l7mp.io"
    kind: GatewayConfig
    name: stunner-gatewayconfig
    namespace: stunner
  description: "STUNner is a WebRTC ingress gateway for Kubernetes"
EOF
'''

Next, we specify some important configuration for STUNner, by loading a GatewayConfig custom resource into Kubernetes. Make sure to remember the credentials, these will be needed to connect to the TURN server.

'''console
kubectl apply -f - <<EOF
apiVersion: stunner.l7mp.io/v1alpha1
kind: GatewayConfig
metadata:
  name: stunner-gatewayconfig
  namespace: stunner
spec:
  stunnerConfig: "stunnerd-configmap"
  realm: stunner.l7mp.io
  authType: plaintext
  userName: "user-1"
  password: "pass-1"
EOF
'''

####Installing STUNner and setting up the gateway

Now that we have something for the controller and have the stunnerd-config, we can install STUNner:

'''console
helm repo add stunner https://l7mp.io/stunner
helm repo update
helm install stunner stunner/stunner --set stunner.namespace=stunner
'''

And immediatly restart it with the following deployment override to make sure its using the config provided by the controller.

'''console
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: stunner
  namespace: stunner
spec:
  selector:
    matchLabels:
      app: stunner
  template:
    metadata:
      labels:
        app: stunner
    spec:
      containers:
        - command: ["stunnerd"]
          args: ["-w", "-c", "/etc/stunnerd/stunnerd.conf"]
          image: l7mp/stunnerd:latest
          imagePullPolicy: Always
          name: stunnerd
          env:
            - name: STUNNER_ADDR
              valueFrom:
                fieldRef:
                  apiVersion: v1
                  fieldPath: status.podIP
          volumeMounts:
            - name: stunnerd-config-volume
              mountPath: /etc/stunnerd
      volumes:
        - name: stunnerd-config-volume
          configMap:
            name: stunnerd-configmap
EOF
'''

Next up, we will deploy an actual STUNner gateway.
The below Gateway specification will expose the STUNner gateway over the STUN/TURN listener service running on the UDP listener port 3478. STUnner will await clients to connect to this listener port and, once authenticated, let them connect to the services running inside the Kubernetes cluster; meanwhile, the NAT traversal functionality implemented by the STUN/TURN server embedded into STUNner will make sure that clients can connect from behind even the most over-zealous enterprise NAT or firewall.

'''console
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
'''

This will create us a whole new service:

'''console
kubectl get services -n stunner

NAME                              TYPE           CLUSTER-IP      EXTERNAL-IP                  PORT(S)          AGE
stunner                           LoadBalancer   10.85.128.58    <STUNNER_EXTERNAL_IP>        3478:31362/UDP   13m
stunner-gateway-udp-gateway-svc   LoadBalancer   10.85.131.128   <GATEWAY_EXTERNAL_IP>        3478:32576/UDP   30s
'''


Note down <GATEWAY_EXTERNAL_IP> and the port assigned to it (if not using NodePort, 3478 in our case). This will be the address the clients will need to connect to for the TURN server.


Finally, attach a UDP route to the Gateway, so that clients will be able to connect via the public STUN/TURN listener UDP:3478 to the worker LoadBalancer service we've created.
In our case, we named it worker-ci-udp-svc. Don't forget to specify the namespace, even if its in the default one.

'''console
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
'''

To make sure everything is set, you can check if the stunnerd-configmap includes your workers pod IPs as endpoints. If it does, you are almost all set!

'''console
kubectl get configmap -n stunner stunnerd-config -o yaml
'''

####Last touches

Before we can establish an RTCP connection, we will need to configure our CloudRetro pods once again.
To be specific, we need the coordinator to send the TURN server address to the client:

'''console
kubectl edit configmap -n cloudretro cloudretro-config
'''

Here, we are looking for the attribute iceServers: -> url.
To make it work, you should replace:

* stun:stun.l.google.com:19302 (default value) with turn:<GATEWAY_EXTERNAL_IP>:PORT (what you've hopefully noted down in the previous step).

This unfortunately not enough all by itself, you will need to add two extra attributes to the config files, right under the url one.
Change the username and the credential according to what you've specified at the GatewayConfig.

* username: user-1
* credential: pass-1

After saving, you should restart the coordinator deployment to make changes take effect:

'''console
kubectl rollout restart deployment -n cloudretro coordinator-deployment
'''

And there You go! After all set, connecting to the coordinator will... have no change. Seemingly.
But if You open the browser console, You should be seeing a marvelous "[rtcp] connected" log message.
Why doesn't it work then?
It has no games.
That's work under progress.
Hard times over anyway.









