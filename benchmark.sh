#!/bin/bash

trap 'kill $(jobs -p)' EXIT
RATE=1600
THREADS=0

[ -z "$1" ] && echo "usage: test2.sh <proto> [udp-thread-num] [PACKET-RATE]" && exit 1
[ -z "$1" ] || PROTO=$1
[ -z "$2" ] || THREADS=$2
[ -z "$3" ] || RATE=$3

go run cmd/stunnerd/main.go -l all:ERROR --udp-thread-num=${THREADS} turn://user:pass@127.0.0.1:5000?transport=${PROTO} &
iperf -s -u -e -i 5 &

go run cmd/turncat/main.go -l all:ERROR udp://127.0.0.1:4999 "turn://user:pass@127.0.0.1:5000?transport=${PROTO}" udp://localhost:5001 &
iperf -c 127.0.0.1 -u -p 4999 -t 0 -l 100 -b $RATE &

go run cmd/turncat/main.go -l all:ERROR udp://127.0.0.1:5999 "turn://user:pass@127.0.0.1:5000?transport=${PROTO}" udp://localhost:5001 &
iperf -c 127.0.0.1 -u -p 5999 -t 0 -l 100 -b $RATE &

go run cmd/turncat/main.go -l all:ERROR udp://127.0.0.1:6999 "turn://user:pass@127.0.0.1:5000?transport=${PROTO}" udp://localhost:5001 &
iperf -c 127.0.0.1 -u -p 6999 -t 0 -l 100 -b $RATE &

go run cmd/turncat/main.go -l all:ERROR udp://127.0.0.1:7999 "turn://user:pass@127.0.0.1:5000?transport=${PROTO}" udp://localhost:5001 &
iperf -c 127.0.0.1 -u -p 7999 -t 0 -l 100 -b $RATE &

go run cmd/turncat/main.go -l all:ERROR udp://127.0.0.1:8999 "turn://user:pass@127.0.0.1:5000?transport=${PROTO}" udp://localhost:5001 &
iperf -c 127.0.0.1 -u -p 8999 -t 0 -l 100 -b $RATE &

go run cmd/turncat/main.go -l all:ERROR udp://127.0.0.1:9999 "turn://user:pass@127.0.0.1:5000?transport=${PROTO}" udp://localhost:5001 &
iperf -c 127.0.0.1 -u -p 9999 -t 0 -l 100 -b $RATE &

go run cmd/turncat/main.go -l all:ERROR udp://127.0.0.1:10999 "turn://user:pass@127.0.0.1:5000?transport=${PROTO}" udp://localhost:5001 &
iperf -c 127.0.0.1 -u -p 10999 -t 0 -l 100 -b $RATE &

go run cmd/turncat/main.go -l all:ERROR udp://127.0.0.1:11999 "turn://user:pass@127.0.0.1:5000?transport=${PROTO}" udp://localhost:5001 &
iperf -c 127.0.0.1 -u -p 11999 -t 0 -l 100 -b $RATE 

exit 0
