# Build from the repository root: docker build -t mhp:latest .
FROM golang:1.27.1 AS build
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mhp ./cmd/mhp

FROM debian:trixie-slim
LABEL org.opencontainers.image.title="mhp"
LABEL org.opencontainers.image.description="MHP relay, exit-node, and loopback SOCKS5 proxy."
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/mhp /usr/local/bin/mhp

# Select the mode and mount its credentials at runtime.
ENTRYPOINT ["/usr/local/bin/mhp"]
