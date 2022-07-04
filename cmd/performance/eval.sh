#!/bin/bash

num_of_processes=$1
eval_time=$2
packet_size=$3
bandwidth=$4
platform=$5

echo "Number of concurrent turncat processes: $num_of_processes"
echo "Evaluation time: $eval_time s"
echo "Packet size: $packet_size bytes"
echo "Platform: $platform"

if [[ $platform == "local" ]];
then
    STUNNER_PUBLIC_ADDR="127.0.0.1"
    STUNNER_PUBLIC_PORT="5000"
    STUNNER_REALM="stunner.l7mp.io"
    STUNNER_USERNAME="user1"
    STUNNER_PASSWORD="passwd1"
    UDP_ECHO_IP="127.0.0.1"
    IPERF_PORT="8999"
else
    STUNNER_PUBLIC_ADDR=$(kubectl get cm stunner-config -o jsonpath='{.data.STUNNER_PUBLIC_ADDR}')
    STUNNER_PUBLIC_PORT=$(kubectl get cm stunner-config -o jsonpath='{.data.STUNNER_PUBLIC_PORT}')
    STUNNER_REALM=$(kubectl get cm stunner-config -o jsonpath='{.data.STUNNER_REALM}')
    STUNNER_PASSWORD=$(kubectl get cm stunner-config -o jsonpath='{.data.STUNNER_PASSWORD}')
    STUNNER_USERNAME=$(kubectl get cm stunner-config -o jsonpath='{.data.STUNNER_USERNAME}')
    UDP_ECHO_IP=$(kubectl get svc udp-echo -o jsonpath='{.spec.clusterIP}')
    IPERF_PORT="9001"
fi

for i in $(seq $num_of_processes); 
do
    port=$((8999+$i))
    go run ../turncat/main.go --realm $STUNNER_REALM --log=all:INFO udp://127.0.0.1:$port \
    turn://${STUNNER_USERNAME}:${STUNNER_PASSWORD}@${STUNNER_PUBLIC_ADDR}:${STUNNER_PUBLIC_PORT} udp://${UDP_ECHO_IP}:$IPERF_PORT &
done

sleep 3

for i in $(seq $num_of_processes); 
do
    port=$((8999+$i))
    # port=8999
    iperf -c 127.0.0.1 -u -p $port -t $eval_time -i 1 -b $bandwidth -l $packet_size &> /dev/null &
    # echo "hello" | socat -d -d - udp:127.0.0.1:$port
done

sleep $(($eval_time+1))
killall main &> /dev/null
# killall iperf
exit
