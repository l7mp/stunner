#!/bin/bash
set -e
set -o pipefail

trap 'last_command=$current_command; current_command=$BASH_COMMAND' DEBUG
trap 'killall main -w &> /dev/null && killall iperf -w &> /dev/null && rm log.tmp &> /dev/null' EXIT



usage="$(basename "$0") [-h] [-np n] [-t n] [-ps n] [-bw n] [-pl local|k8s]   -- a helper script for executing performance measurements using STUNner in or outside Kubernetes

where:
    -h      Show help text
    -n      Number of 'turncat' clients (more of them can be used, 
            this way each client will forward lesser traffic and 
            none of them becomes the bottleneck while measuring) [Default: 1]
    -t      Time in seconds to transmit for [Default: 10]
    -s      Size of the packet in bytes [Default: 1200]
    -b      Bandwidth to send in bits/sec [Default: 100000]
    -p      Platform, can be 'local' or 'k8s' [Default: local]"

num_of_processes=1
eval_time=10
packet_size=1200
bandwidth=100000
platform=local


while getopts ":hn:t:s:b:p:" option; do
    case $option in
        h) # display Help
            echo "$usage"
            exit
            ;;
        n) num_of_processes=$OPTARG
            ;;
        t)  eval_time=$OPTARG
            ;;
        s) packet_size=$OPTARG
            ;;
        b) bandwidth=$OPTARG
            ;;
        p) platform=$OPTARG
            if [[ $platform != "k8s" && $platform != "local" ]]; then
                printf "wrong argument value for platform '-p %s' can be 'local' or 'k8s'\n", "$platform" >&2
                exit 1
            fi
            ;;
        :)  printf "missing argument for -%s\n" "$OPTARG" >&2
            echo "$usage" >&2
            exit 1
            ;;
        \?) printf "illegal option: -%s\n" "$OPTARG" >&2
            echo "$usage" >&2
            exit 1
            ;;
    esac
done
shift $((OPTIND - 1))

echo "Number of concurrent turncat clients: $num_of_processes"
echo "Evaluation time: $eval_time sec"
echo "Packet size: $packet_size bytes"
echo "Bandwidth: $((bandwidth / 1000)) Kbits/sec or $((bandwidth / 1000 / 1000)) Mbits/sec per turncat client"
echo "Platform: $platform"

if [[ $platform == "local" ]]; then
    STUNNER_PUBLIC_ADDR="127.0.0.1"
    STUNNER_PUBLIC_PORT="5001"
    STUNNER_USERNAME="user1"
    STUNNER_PASSWORD="passwd1"
    UDP_ECHO_IP="127.0.0.1"
    IPERF_PORT="5000"

    go run ../../../cmd/stunnerd/main.go --log=all:INFO \
    turn://${STUNNER_USERNAME}:${STUNNER_PASSWORD}@${STUNNER_PUBLIC_ADDR}:${STUNNER_PUBLIC_PORT} &> /dev/null 2>&1 &
    iperf -s -p 5000 -u -e > log.tmp 2>&1 &
    sleep 2

elif [[ $platform == "k8s" ]]; then
    # STUNNER_PUBLIC_ADDR=$(kubectl get cm stunnerd-config -n stunner -o jsonpath='{.data.stunnerd\.conf}' | jq -r .listeners[0].public_address)
    # STUNNER_PUBLIC_PORT=$(kubectl get cm stunnerd-config -n stunner -o jsonpath='{.data.stunnerd\.conf}' | jq -r .listeners[0].public_port)
    # STUNNER_PASSWORD=$(kubectl get cm stunnerd-config -n stunner -o jsonpath='{.data.stunnerd\.conf}' | jq -r .auth.credentials.password)
    # STUNNER_USERNAME=$(kubectl get cm stunnerd-config -n stunner -o jsonpath='{.data.stunnerd\.conf}' | jq -r .auth.credentials.username)
    UDP_ECHO_IP=$(kubectl get svc iperf-server -o jsonpath='{.spec.clusterIP}')
    IPERF_PORT="5000"
else
    echo "Platform '$platform' is invalid, only 'local' and 'k8s' are valid"
    exit
fi

for i in $(seq "$num_of_processes"); 
do
    port=$((8999+i))
    go run ../../../cmd/turncat/main.go --log=all:INFO udp://127.0.0.1:$port \
        k8s://stunner/udp-gateway:udp-listener udp://"${UDP_ECHO_IP}":$IPERF_PORT >/dev/null 2>&1 &
done

sleep 2

for i in $(seq "$num_of_processes"); 
do
    port=$((8999+i))
    iperf -c 127.0.0.1 -u -p $port -t "$eval_time" -i 1 -b "$bandwidth" -l "$packet_size" &> /dev/null &
done

sleep $((eval_time+2))

# In case of K8s take iperf server logs from inside the cluster pod
if [[ $platform == "k8s" ]]; then
    kubectl logs "$(kubectl get pods -l app="iperf-server" -o go-template --template '{{range .items}}{{.metadata.name}}{{"\n"}}{{end}}')" > log.tmp
fi

echo
echo "Results"
< log.tmp grep -E -i 'pps|\[SUM\]'
exit
