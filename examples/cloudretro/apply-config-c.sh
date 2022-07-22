#!/bin/bash
# Minimalistic sed usage to edit and re-apply Worker and Coordinator configMap with new env. variables, then restart deployment
#
kubectl get configmap -n cloudretro cloudretro-config-c -o yaml | \
sed 's/CLOUD_GAME_WEBRTC_ICESERVERS_0_URL: default/CLOUD_GAME_WEBRTC_ICESERVERS_0_URL: \"turn:'"$(kubectl get service -n stunner stunner-gateway-udp-gateway-svc -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"':3478\"/' | \
sed 's/CLOUD_GAME_WEBRTC_ICESERVERS_0_URL: default/CLOUD_GAME_WEBRTC_ICESERVERS_0_USERNAME: '"$(kubectl get gatewayconfig stunner-gatewayconfig -o jsonpath='{.spec.userName}')"'/' | \
sed 's/CLOUD_GAME_WEBRTC_ICESERVERS_0_URL: default/CLOUD_GAME_WEBRTC_ICESERVERS_0_CREDENTIAL: '"$(kubectl get gatewayconfig stunner-gatewayconfig -o jsonpath='{.spec.password}')"'/' | \
kubectl apply -f -
kubectl rollout restart deployment -n cloudretro coordinator-deployment
#