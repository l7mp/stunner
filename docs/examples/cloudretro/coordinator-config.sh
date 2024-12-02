#!/bin/bash
#
# Shellscript for patching multicluster coordinator configmap with additional ICE-server addresses
#
primary_context=$1
secondary_context=$2
if [ $# -eq 0 ]
then
  primary_context=$(kubectl config current-context)
  secondary_context=$(kubectl config current-context)
fi
max=10
raw=$(kubectl get configmap -n cloudretro cloudretro-config-c -o yaml --context $primary_context)
for i in `seq 0 $max`
do
    s="CLOUD_GAME_WEBRTC_ICESERVERS_$(echo $i)_URL"
    c=$(echo $raw | grep $s)
    if [ -z "$c" ]
    then
        contextnumber=$i
        break
    fi
done

username=$(kubectl get gatewayconfig -n stunner stunner-gatewayconfig -o jsonpath='{.spec.userName}' --context $secondary_context)
credential=$(kubectl get gatewayconfig -n stunner stunner-gatewayconfig -o jsonpath='{.spec.password}' --context $secondary_context)
gwip=$(kubectl get service -n stunner udp-gateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}' --context $secondary_context)

kubectl patch configmap -n cloudretro cloudretro-config-c --context $primary_context --patch-file=/dev/stdin <<-EOF
data:
  CLOUD_GAME_WEBRTC_ICESERVERS_$(echo $contextnumber)_CREDENTIAL: $(echo $credential)
  CLOUD_GAME_WEBRTC_ICESERVERS_$(echo $contextnumber)_URL: turn:$(echo $gwip):3478
  CLOUD_GAME_WEBRTC_ICESERVERS_$(echo $contextnumber)_USERNAME: $(echo $username)
EOF
kubectl rollout restart deployment -n cloudretro coordinator-deployment --context $primary_context