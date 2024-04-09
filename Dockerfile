# build stage
FROM docker.io/golang:1.22 as build-env

COPY ./ /go/src/github.com/scouball/podman_caddy
WORKDIR /go/src/github.com/scouball/podman_caddy

RUN go mod download -x

ARG GOARCH=amd64
RUN GO111MODULE=on CGO_ENABLED=0 GOOS=linux GOARCH=$GOARCH go build -a -installsuffix cgo -ldflags="-w -s" -o $GOPATH/bin/podman_caddy


# final stage
FROM scratch
WORKDIR /go/bin
COPY --from=build-env /go/bin/ /go/bin

ENTRYPOINT ["/go/bin/podman_caddy"]
