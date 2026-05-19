# build/Dockerfile
# Stage 1: Build
FROM golang:1.26.3-bookworm AS builder

RUN apt-get update && apt-get install -y gcc libc6-dev && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /bin/vaultfleet-master ./cmd/master

# Stage 2: Runtime
FROM debian:bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata && rm -rf /var/lib/apt/lists/*

COPY --from=builder /bin/vaultfleet-master /usr/local/bin/vaultfleet-master

VOLUME ["/data"]
EXPOSE 8080

ENTRYPOINT ["vaultfleet-master"]
CMD ["--data-dir", "/data", "--addr", ":8080"]
