FROM ubuntu:21.04

RUN apt-get update && \
    apt-get install -y wget jq curl && \
    rm -rf /var/lib/apt/lists/*

COPY post-install.sh .
ENTRYPOINT [ "./post-install.sh" ]