# Relay image for MHP.
#
# The relay is the sole service on its dedicated VPS and owns TCP/443. It runs
# as root inside the container so it can bind 443 directly (no
# CAP_NET_BIND_SERVICE is needed). The leaf certificate and the two role tokens
# are mounted read-only from deploy/relay/credentials at runtime; they are
# never baked into the image.
#
# Build from the repo root:
#   docker build -t mhp-relay -f deploy/relay/Dockerfile .
#
# The LGA VPS network blocks proxy.golang.org; pass the mirror via ARG when
# building there (see deploy/relay/compose.yml).
FROM golang:1.27 AS build
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mhp ./cmd/mhp

FROM debian:slim
LABEL org.opencontainers.title="mhp-relay"
LABEL org.opencontainers.description="MHP relay: coordinates browser traffic from proxy clients to exit clients."

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/mhp /usr/local/bin/mhp

# Runs as root to bind TCP/443. The relay is health-checked by client
# connectivity, not an in-container self-check, so there is deliberately no
# HEALTHCHECK (see docs/DEPLOYMENT.md).
EXPOSE 443

ENTRYPOINT ["/usr/local/bin/mhp"]
CMD ["-mode", "relay", "-listen", ":443",
     "-tls-cert", "/credentials/relay.crt",
     "-tls-key", "/credentials/relay.key",
     "-exit-token-file", "/credentials/exit.token",
     "-proxy-token-file", "/credentials/proxy.token"]
