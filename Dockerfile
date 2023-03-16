###########
# BUILD
FROM golang:1.19-alpine as builder

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY *.go ./
COPY internal/ internal/
COPY pkg/apis/ pkg/apis/

COPY cmd/stunnerd/main.go cmd/stunnerd/
COPY cmd/stunnerd/stunnerd.conf cmd/stunnerd/

RUN apkArch="$(apk --print-arch)"; \
      case "$apkArch" in \
        aarch64) export GOARCH='arm64' ;; \
        *) export GOARCH='amd64' ;; \
      esac; \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o stunnerd cmd/stunnerd/main.go

###########
# STUNNERD
FROM scratch

WORKDIR /app

COPY --from=builder /app/stunnerd /usr/bin/
COPY --from=builder /app/cmd/stunnerd/stunnerd.conf /

EXPOSE 3478/udp

CMD [ "stunnerd", "-c", "/stunnerd.conf" ]

# CMD [ "stunnerd", "turn://user1:passwd1@127.0.0.1:3478" ]
