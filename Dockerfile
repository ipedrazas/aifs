# syntax=docker/dockerfile:1.10

# Base images are pinned by digest; Dependabot keeps them current.

# --- frontend --------------------------------------------------------------
# Built on the build platform only: the output is platform independent.
FROM --platform=$BUILDPLATFORM node:26-alpine@sha256:6bd9ab43c0ec0190ca7b2d193906cb8b7edf6e7f4afa43ddeccd6452710bffb5 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# --- backend ---------------------------------------------------------------
# Cross-compiles on the build platform (no QEMU): CGO is off.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:eb61c3873c97416fff0116553fff65d67a3549f72e11d9758849a689cdd8b523 AS api
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
WORKDIR /src/api
COPY api/go.mod api/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY api/ ./
COPY --from=web /src/web/dist/ ./internal/webui/dist/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath \
      -ldflags "-s -w \
        -X github.com/ipedrazas/aifs/api/internal/version.Version=${VERSION} \
        -X github.com/ipedrazas/aifs/api/internal/version.Commit=${COMMIT}" \
      -o /out/aifs ./cmd/aifs \
 && mkdir -p /out/data

# --- runtime ---------------------------------------------------------------
FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329
LABEL org.opencontainers.image.title="AI Fashion Studio" \
      org.opencontainers.image.source="https://github.com/ipedrazas/aifs" \
      org.opencontainers.image.licenses=""
COPY --from=api /out/aifs /usr/local/bin/aifs
# The data directory must exist and belong to the nonroot user (65532) so a
# fresh named volume inherits that ownership.
COPY --from=api --chown=65532:65532 /out/data /data
ENV AIFS_ADDR=:8080 \
    AIFS_DATA_DIR=/data \
    AIFS_ENV=production
USER 65532:65532
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/aifs", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/aifs"]
CMD ["serve"]
