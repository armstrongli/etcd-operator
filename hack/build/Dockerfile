FROM golang:1.25 as build-bins
WORKDIR /app
COPY . .
ENV GOOS=linux
ENV GOARCH=amd64
ENV CGO_ENABLED=0
RUN go build -o etcd-operator ./cmd/operator
RUN go build -o etcd-backup-operator ./cmd/backup-operator
RUN go build -o etcd-restore-operator ./cmd/restore-operator

FROM alpine:3.23 as default
COPY --from=build-bins /app/etcd-backup-operator /usr/local/bin/etcd-backup-operator
COPY --from=build-bins /app/etcd-restore-operator /usr/local/bin/etcd-restore-operator
COPY --from=build-bins /app/etcd-operator /usr/local/bin/etcd-operator

FROM scratch as slim
COPY --from=build-bins /app/etcd-backup-operator /usr/local/bin/etcd-backup-operator
COPY --from=build-bins /app/etcd-restore-operator /usr/local/bin/etcd-restore-operator
COPY --from=build-bins /app/etcd-operator /usr/local/bin/etcd-operator