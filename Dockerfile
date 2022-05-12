###########
# BUILD
FROM golang:1.18-alpine as builder

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY *.go ./
COPY utils/stunnerd/main.go stunnerd/
COPY utils/stunnerd/stunnerd.conf ./

# RUN go build -o /stunnerd
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-w -s" -o stunnerd  ./...

###########
# STUNNERD
FROM scratch

WORKDIR /app

COPY --from=builder /app/stunnerd /usr/bin/
COPY --from=builder /app/stunnerd.conf /

EXPOSE 3478/udp

CMD [ "stunnerd", "-c", "/stunnerd.conf" ]

# CMD [ "stunnerd", "turn://user1:passwd1@127.0.0.1:3478" ]
