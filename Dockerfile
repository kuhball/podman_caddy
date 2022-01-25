# build stage
FROM golang:alpine as build-env

COPY ./ /go/src/github.com/scouball/podman_caddy
WORKDIR /go/src/github.com/scouball/podman_caddy

RUN apk add git
RUN GO111MODULE=on go get github.com/urfave/cli/v2

RUN GO111MODULE=on CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s" -o $GOPATH/bin/podman_caddy


# final stage
FROM scratch
WORKDIR /go/bin
COPY --from=build-env /go/bin/ /go/bin

ENTRYPOINT ["/go/bin/podman_caddy"]
