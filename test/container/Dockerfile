# golang:X-alpine can't be used since it does not support the race detector flag which assumes a glibc based system, whereas alpine linux uses musl libc
# https://github.com/golang/go/issues/14481
FROM golang:1.25

COPY --from=quay.io/ongres/kubectl:v1.33.6-build-6.46 /usr/local/bin/kubectl /usr/local/bin/kubectl

ADD ./ /go/src/github.com/coreos/etcd-operator

ADD ./_test/aws /aws

WORKDIR /go/src/github.com/coreos/etcd-operator

RUN rm -rf _output _test .git .gitignore

ENTRYPOINT ["./test/container/run"]
