# Imagens base fixadas por digest (índice multi-arquitetura) para builds reproduzíveis;
# o Dependabot (ecossistema docker) abre PR quando a tag ganha um digest novo.

# ---- frontend ----
FROM node:24-alpine@sha256:ebfe2f90462722a7a4de65e91990e97fe0d401c70e0e762c5b53302f905ec1c1 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---- backend ----
FROM golang:1.27.1-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/server/webdist/dist ./internal/server/webdist/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /filezam ./cmd/filezam
# Empty mount points owned by the runtime user (uid 65532), so named volumes
# created by Docker/Easypanel/Portainer start out writable without a chown.
RUN mkdir -p /empty

# ---- runtime ----
FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
LABEL org.opencontainers.image.title="Filezam" \
      org.opencontainers.image.description="Self-hosted web file manager: one folder, users with scopes, resumable uploads, public links" \
      org.opencontainers.image.source="https://github.com/miguelzamberlan/filezam" \
      org.opencontainers.image.licenses="AGPL-3.0-only"
COPY --from=build /filezam /filezam
COPY --from=build --chown=65532:65532 /empty /data
COPY --from=build --chown=65532:65532 /empty /config
ENV FILEZAM_ROOT=/data \
    FILEZAM_DATA_DIR=/config \
    FILEZAM_LISTEN=:8080
EXPOSE 8080
VOLUME ["/data", "/config"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/filezam", "healthcheck"]
ENTRYPOINT ["/filezam"]
CMD ["serve"]
