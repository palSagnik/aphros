FROM golang:1.23-alpine AS build
WORKDIR /go/src/aphros

COPY . .
RUN CGO_ENABLED=0 go build -o /go/bin/aphros ./cmd/aphros
RUN GRPC_HEALTH_PROBE_VERSION=v0.4.24 && \
    GHP_ARCH=linux-arm64 && \
    wget -qO /go/bin/grpc_health_probe \
    https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/${GRPC_HEALTH_PROBE_VERSION}/grpc_health_probe-${GHP_ARCH} && \
    chmod +x /go/bin/grpc_health_probe

FROM golang:1.23-alpine
COPY --from=build /go/bin/aphros /bin/aphros
COPY --from=build /go/bin/grpc_health_probe /bin/grpc_health_probe

ENTRYPOINT ["/bin/aphros"]