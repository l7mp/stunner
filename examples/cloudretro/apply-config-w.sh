#!/bin/bash
# Minimalistic sed usage to edit and re-apply Worker configMap with new env. variables, then restart deployment
#
kubectl get configmap -n cloudretro cloudretro-config-w -o yaml | \
sed 's/CLOUD_GAME_WORKER_NETWORK_COORDINATORADDRESS: NULL/CLOUD_GAME_WORKER_NETWORK_COORDINATORADDRESS:'"$(kubectl get service -n cloudretro coordinator-lb-svc -o jsonpath='{.spec.clusterIP}')"':8000/' | \
sed 's/CLOUD_GAME_WORKER_NETWORK_PUBLICADDRESS: NULL/CLOUD_GAME_WORKER_NETWORK_PUBLICADDRESS: '"$(kubectl get service -n cloudretro worker-lb-svc -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"'/' | \
kubectl apply -f -
kubectl rollout restart deployment -n cloudretro worker-deployment
#