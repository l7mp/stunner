###########
# BUILD
# The builder runs on the build platform and cross-compiles for the target: Go needs no
# emulation for that, an emulated compiler is an order of magnitude slower.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder
ARG TARGETOS TARGETARCH

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
COPY internal/ internal/
COPY pkg/ pkg/

COPY cmd/ cmd/

COPY .git ./.git/
COPY Makefile ./
RUN apk add --no-cache git make

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH make build-bin

###########
# STUNNERD
FROM scratch

WORKDIR /app

COPY --from=builder /app/bin/stunnerd /usr/bin/
COPY --from=builder /app/cmd/stunnerd/stunnerd.conf /

EXPOSE 3478/udp

CMD [ "stunnerd", "-c", "/stunnerd.conf" ]

# CMD [ "stunnerd", "turn://user1:passwd1@127.0.0.1:3478" ]
