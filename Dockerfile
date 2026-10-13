# Stage 1: Build the Go benchmark binary
FROM golang:1.27-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /ovs-ovn-bench .

# Stage 2: Runtime image with Ubuntu packages & jemalloc
FROM ubuntu:26.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    openvswitch-switch \
    ovn-central \
    ovn-host \
    libjemalloc2 \
    procps \
    && mkdir -p /var/run/openvswitch /var/run/ovn /var/log/openvswitch /var/log/ovn \
    && rm -rf /var/cache/apt/* /var/lib/apt/lists/*

WORKDIR /workspace

COPY --from=builder /ovs-ovn-bench /usr/local/bin/ovs-ovn-bench

ENTRYPOINT ["/usr/local/bin/ovs-ovn-bench", "-use-system"]
CMD ["-scales", "1,10,100,1000,10000", "-threads", "4", "-objects", "all"]
