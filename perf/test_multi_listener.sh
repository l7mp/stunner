#!/bin/bash -x

# test.sh [PACKET-RATE]

trap 'kill $(jobs -p)' EXIT

RATE=8000
[ -z "$1" ] || RATE=$1

go run ../cmd/stunnerd/main.go -l all:ERROR -c multi_listener.conf &
iperf -s -u -e -i 5 &

# 5000
go run ../cmd/turncat/main.go -l all:ERROR udp://127.0.0.1:4999 "turn://user:pass@127.0.0.1:5000?transport=udp" udp://localhost:5001 &
iperf -c 127.0.0.1 -u -p 4999 -t 0 -l 100 -b $RATE &

# 6000
go run ../cmd/turncat/main.go -l all:ERROR udp://127.0.0.1:5999 "turn://user:pass@127.0.0.1:6000?transport=udp" udp://localhost:5001 &
iperf -c 127.0.0.1 -u -p 5999 -t 0 -l 100 -b $RATE &

# 7000
go run ../cmd/turncat/main.go -l all:ERROR udp://127.0.0.1:6999 "turn://user:pass@127.0.0.1:7000?transport=udp" udp://localhost:5001 &
iperf -c 127.0.0.1 -u -p 6999 -t 0 -l 100 -b $RATE &

# 8000
go run ../cmd/turncat/main.go -l all:ERROR udp://127.0.0.1:7999 "turn://user:pass@127.0.0.1:8000?transport=udp" udp://localhost:5001 &
iperf -c 127.0.0.1 -u -p 7999 -t 0 -l 100 -b $RATE

