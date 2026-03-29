# golang:X-alpine can't be used since it does not support the race detector flag which assumes a glibc based system, whereas alpine linux uses musl libc
# https://github.com/golang/go/issues/14481
FROM golang:1.25 as builder
ENV GOOS=linux
ENV GOARCH=amd64

COPY --from=quay.io/ongres/kubectl:v1.33.6-build-6.46 /usr/local/bin/kubectl /bin/kubectl

ADD ./ /go/src/github.com/coreos/etcd-operator

WORKDIR /go/src/github.com/coreos/etcd-operator

RUN go test ./test/e2e/ -c -o /bin/etcd-operator-e2e --race

FROM ubuntu:24.04

COPY --from=builder /bin/etcd-operator-e2e /bin
COPY --from=builder /bin/kubectl /bin
COPY --from=builder /go/src/github.com/coreos/etcd-operator/test/pod/simple/run-e2e /bin/run-e2e

CMD ["/bin/run-e2e"]
