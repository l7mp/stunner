###########
# BUILD
FROM golang:1.18-alpine as builder

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY *.go ./
COPY internal/manager/ internal/manager/
COPY internal/object/ internal/object/
COPY internal/resolver/ internal/resolver/
COPY internal/util/ internal/util/
COPY pkg/apis/v1alpha1/ pkg/apis/v1alpha1/

COPY cmd/stunnerd/main.go cmd/stunnerd/
COPY cmd/stunnerd/stunnerd.conf cmd/stunnerd/

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -trimpath -o stunnerd cmd/stunnerd/main.go

###########
# STUNNERD
FROM scratch

WORKDIR /app

COPY --from=builder /app/stunnerd /usr/bin/
COPY --from=builder /app/cmd/stunnerd/stunnerd.conf /

EXPOSE 3478/udp

CMD [ "stunnerd", "-c", "/stunnerd.conf" ]

# CMD [ "stunnerd", "turn://user1:passwd1@127.0.0.1:3478" ]
