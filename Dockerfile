###########
# BUILD
FROM golang:1.24-alpine as builder

WORKDIR /app

COPY go.mod ./
COPY go.sum ./

COPY *.go ./
COPY internal/ internal/
COPY pkg/ pkg/

COPY cmd/ cmd/

COPY .git ./
COPY Makefile ./
RUN apk add --no-cache git make

RUN apkArch="$(apk --print-arch)"; \
      case "$apkArch" in \
        aarch64) export GOARCH='arm64' ;; \
        *) export GOARCH='amd64' ;; \
      esac; \
    export CGO_ENABLED=0; \
    export GOOS=linux; \
    make build-bin

###########
# STUNNERD
FROM scratch

WORKDIR /app

COPY --from=builder /app/bin/stunnerd /usr/bin/
COPY --from=builder /app/cmd/stunnerd/stunnerd.conf /

EXPOSE 3478/udp

CMD [ "stunnerd", "-c", "/stunnerd.conf" ]

# CMD [ "stunnerd", "turn://user1:passwd1@127.0.0.1:3478" ]
