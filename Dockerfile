# Baseline: ubuntu:22.04 so max GLIBC=2.35 → one binary runs 22.04+24.04.
# Official Go tarball (repo Go 1.18 on jammy is too old).
FROM ubuntu:22.04 AS base
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl build-essential pkg-config \
    libx11-dev libxtst-dev libxi-dev libxfixes-dev \
  && rm -rf /var/lib/apt/lists/*
ENV GOTOOLCHAIN=local
ARG GO_VERSION=1.23.4
RUN curl -fsSL https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz \
  | tar -C /usr/local -xz
ENV PATH=/usr/local/go/bin:$PATH

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO variant (input backends) must at least compile on linux.
RUN CGO_ENABLED=1 go vet ./...
RUN CGO_ENABLED=1 go test ./...
RUN CGO_ENABLED=1 go build -o /out/mwb-client ./cmd/mwb-client
# Pure-protocol variant (CGO off) for constrained CI.
RUN CGO_ENABLED=0 go test ./internal/protocol/... ./internal/crypto/... ./internal/clipboard/... ./internal/keymap/... ./internal/util/...

FROM ubuntu:22.04 AS release
RUN apt-get update && apt-get install -y --no-install-recommends \
    libx11-6 libxtst6 libxi6 libxfixes3 ca-certificates \
  && rm -rf /var/lib/apt/lists/*
COPY --from=base /out/mwb-client /usr/local/bin/mwb-client
ENTRYPOINT ["/usr/local/bin/mwb-client"]
CMD ["status"]
