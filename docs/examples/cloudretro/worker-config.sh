#!/bin/bash
# Minimalistic sed usage to edit and re-apply Worker configMap with new env. variables, then restart deployment
#
primary_context=$1
secondary_context=$2
if [ $# -eq 0 ]
then
  primary_context=$(kubectl config current-context)
  secondary_context=$(kubectl config current-context)
fi

coordaddr=$(kubectl get service -n cloudretro coordinator-lb-svc -o jsonpath='{.status.loadBalancer.ingress[0].ip}' --context $primary_context)
publicaddr=$(kubectl get service -n cloudretro worker-lb-svc -o jsonpath='{.status.loadBalancer.ingress[0].ip}' --context $secondary_context)

kubectl patch configmap -n cloudretro cloudretro-config-w --context $secondary_context --patch-file=/dev/stdin <<-EOF
data:
  CLOUD_GAME_WORKER_NETWORK_COORDINATORADDRESS: $(echo $coordaddr):8000
  CLOUD_GAME_WORKER_NETWORK_PUBLICADDRESS: $(echo $publicaddr)
EOF
kubectl rollout restart deployment -n cloudretro worker-deployment --context $secondary_context