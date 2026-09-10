# ---- frontend ----
FROM node:26-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---- backend ----
FROM golang:1.26.8-alpine AS build
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
FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.title="Filezam" \
      org.opencontainers.image.description="Self-hosted web file manager: one folder, users with scopes, resumable uploads, public links" \
      org.opencontainers.image.source="https://github.com/miguelzamberlan/filezam" \
      org.opencontainers.image.licenses="MIT"
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
