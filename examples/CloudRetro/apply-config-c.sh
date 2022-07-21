#!/bin/bash
# Minimalistic sed usage to edit and re-apply Worker and Coordinator configMap with new env. variables, then restart deployment
#
kubectl get configmap -n cloudretro cloudretro-config-c -o yaml | \
sed 's/CLOUD_GAME_WEBRTC_ICESERVERS_0_URL: NULL/CLOUD_GAME_WEBRTC_ICESERVERS_0_URL: '"$(kubectl get service TODO -o jsonpath='{TODO}')"'/' | \
sed 's/CLOUD_GAME_WEBRTC_ICESERVERS_0_URL: NULL/CLOUD_GAME_WEBRTC_ICESERVERS_0_USERNAME: '"$(kubectl get service TODO -o jsonpath='{TODO}')"'/' | \
sed 's/CLOUD_GAME_WEBRTC_ICESERVERS_0_URL: NULL/CLOUD_GAME_WEBRTC_ICESERVERS_0_CREDENTIAL: '"$(kubectl get service TODO -o jsonpath='{TODO}')"'/' | \
kubectl apply -f -
kubectl rollout restart deployment -n cloudretro coordinator-deployment