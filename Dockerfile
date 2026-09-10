# Imagens base fixadas por digest (índice multi-arquitetura) para builds reproduzíveis;
# o Dependabot (ecossistema docker) abre PR quando a tag ganha um digest novo.

# ---- frontend ----
FROM node:22-alpine@sha256:c610fcdfb1d5b4740dd70c284ed3cb16bb857e0f7166196e36a5501df7a3aa32 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---- backend ----
FROM golang:1.26.8-alpine@sha256:ce864e7223ac17b1775e6fd0b4c0db580c2eb50e7953a427916379e4b92a1628 AS build
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
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
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
