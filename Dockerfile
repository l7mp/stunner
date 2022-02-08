###########
# BUILD
FROM golang:1.17-alpine as builder

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY *.go ./

# RUN go build -o /stunnerd
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o stunnerd .

###########
# STUNNERD
FROM scratch

WORKDIR /app

COPY --from=builder /app/stunnerd /usr/bin/

EXPOSE 3478/udp

CMD [ "stunnerd", "--public-ip", "127.0.0.1", "--users", "test=test" ]
