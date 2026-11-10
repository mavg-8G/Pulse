# syntax=docker/dockerfile:1
# Pulse: imagen final ≈ 12 MB (binario estático + distroless, sin shell).

# CI pasa estos valores desde .node-version y go.mod. Los defaults permiten construir localmente.
ARG NODE_VERSION=26.13.0
ARG GO_VERSION=1.27.3

# 1) Frontend (React + Vite) → internal/webui/dist
FROM node:${NODE_VERSION}-alpine3.24@sha256:8996bb5821d62a8fd9bb27c1cb07462f3fdce6e47d640c6a881ead0b064ce216 AS web
ARG NODE_VERSION
RUN test "$(node --version)" = "v${NODE_VERSION}"
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# 2) Backend Go. SQLite (mattn/go-sqlite3) necesita CGO: se enlaza estático con musl.
FROM golang:${GO_VERSION}-alpine3.24@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS api
ARG GO_VERSION
RUN test "$(go env GOVERSION)" = "go${GO_VERSION}"
RUN apk add --no-cache build-base
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
ENV CGO_ENABLED=1
# Los tests corren dentro del build: si algo falla, no se genera la imagen.
RUN go vet ./... && go test ./...
ARG BUILD_TIMESTAMP
ARG GIT_REVISION
RUN build_timestamp="${BUILD_TIMESTAMP:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}" && \
    go build -trimpath -tags "sqlite_omit_load_extension netgo osusergo" \
      -ldflags "-s -w -linkmode external -extldflags '-static' -X pulse/internal/buildinfo.BuiltAt=${build_timestamp} -X pulse/internal/buildinfo.Revision=${GIT_REVISION}" \
      -o /out/pulse ./cmd/pulse

# 3) Ejecución: sin shell, sin gestor de paquetes, usuario sin privilegios (uid 65532).
FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329
COPY --from=api /out/pulse /pulse
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/pulse"]
CMD ["serve"]
