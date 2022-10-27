#!/bin/bash

num_of_processes=$1
eval_time=$2
packet_size=$3
bandwidth=$4
platform=$5

echo "Number of concurrent turncat clients: $num_of_processes"
echo "Evaluation time: $eval_time sec"
echo "Packet size: $packet_size bytes"
echo "Bandwidth: $(($bandwidth / 1000)) Kbits/sec or $(($bandwidth / 1000 / 1000)) Mbits/sec per turncat client"
echo "Platform: $platform"

if [[ $platform == "local" ]]; then
    STUNNER_PUBLIC_ADDR="127.0.0.1"
    STUNNER_PUBLIC_PORT="5001"
    STUNNER_REALM="stunner.l7mp.io"
    STUNNER_USERNAME="user1"
    STUNNER_PASSWORD="passwd1"
    UDP_ECHO_IP="127.0.0.1"
    IPERF_PORT="5000"

    go run ../../cmd/stunnerd/main.go --log=all:INFO turn://${STUNNER_USERNAME}:${STUNNER_PASSWORD}@${STUNNER_PUBLIC_ADDR}:${STUNNER_PUBLIC_PORT} &> /dev/null 2>&1 &
    iperf -s -p 5000 -u -e &
    sleep 2

elif [[ $platform == "k8s" ]]; then
    STUNNER_PUBLIC_ADDR=$(kubectl get cm stunnerd-config -n stunner -o jsonpath='{.data.stunnerd\.conf}' | jq -r .listeners[0].public_address)
    STUNNER_PUBLIC_PORT=$(kubectl get cm stunnerd-config -n stunner -o jsonpath='{.data.stunnerd\.conf}' | jq -r .listeners[0].public_port)
    STUNNER_REALM=$(kubectl get cm stunnerd-config -n stunner -o jsonpath='{.data.stunnerd\.conf}' | jq -r .auth.realm)
    STUNNER_PASSWORD=$(kubectl get cm stunnerd-config -n stunner -o jsonpath='{.data.stunnerd\.conf}' | jq -r .auth.credentials.password)
    STUNNER_USERNAME=$(kubectl get cm stunnerd-config -n stunner -o jsonpath='{.data.stunnerd\.conf}' | jq -r .auth.credentials.username)
    UDP_ECHO_IP=$(kubectl get svc iperf-server -o jsonpath='{.spec.clusterIP}')
    IPERF_PORT="5000"
else
    echo "Platform '$platform' is invalid, only 'local' and 'k8s' are valid"
    exit
fi

for i in $(seq $num_of_processes); 
do
    port=$((8999+$i))
    go run ../../cmd/turncat/main.go --log=all:INFO udp://127.0.0.1:$port \
    turn://${STUNNER_USERNAME}:${STUNNER_PASSWORD}@${STUNNER_PUBLIC_ADDR}:${STUNNER_PUBLIC_PORT} udp://${UDP_ECHO_IP}:$IPERF_PORT &> /dev/null 2>&1 &
done

sleep 2

for i in $(seq $num_of_processes); 
do
    port=$((8999+$i))
    iperf -c 127.0.0.1 -u -p $port -t $eval_time -i 1 -b $bandwidth -l $packet_size &> /dev/null &
    # iperf -c 127.0.0.1 -u -p $port -t $eval_time -i 1 -b $bandwidth -l $packet_size &

done

sleep $(($eval_time+2))
killall main -w &> /dev/null
killall iperf -w &> /dev/null
exit
