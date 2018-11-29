FROM golang:1.11-alpine

RUN apk add --no-cache git

RUN wget -O - https://raw.githubusercontent.com/golang/dep/master/install.sh | sh

WORKDIR /go/src/github.aaf.cloud/platform/websocket-service
COPY . .

RUN dep ensure
RUN go build -o websocket-service ./service

FROM golang:1.11-alpine

WORKDIR /opt/websocket-service/bin

COPY --from=0 /go/src/github.aaf.cloud/platform/websocket-service/websocket-service .
RUN ./websocket-service --help > /dev/null

ENTRYPOINT ["/opt/websocket-service/bin/websocket-service"]
