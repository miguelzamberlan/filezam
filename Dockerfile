# ---- frontend ----
FROM node:22-alpine AS web
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

# ---- runtime ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /filezam /filezam
ENV FILEZAM_ROOT=/data \
    FILEZAM_DATA_DIR=/config \
    FILEZAM_LISTEN=:8080
EXPOSE 8080
VOLUME ["/data", "/config"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/filezam", "healthcheck"]
ENTRYPOINT ["/filezam"]
CMD ["serve"]
