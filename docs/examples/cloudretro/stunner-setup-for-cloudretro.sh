#!/bin/bash
#
# Script used to install all the needed STUNner components on a secondary cluster in a multicluster CLoudRetro setup
#
kcontext="--kube-context $(echo $1)"
context="--context $(echo $1)"
if [ -z "$1" ]
then
  context=
  kcontext=
fi
helm install stunner-gateway-operator stunner/stunner-gateway-operator --create-namespace --namespace stunner $kcontext
helm install stunner stunner/stunner --namespace stunner $kcontext
kubectl apply -f stunner-gwcc.yaml $context
kubectl apply $context -f - <<EOF
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
      protocol: TURN-UDP
EOF
kubectl apply $context -f - <<EOF
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