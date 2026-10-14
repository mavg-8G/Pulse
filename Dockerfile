# syntax=docker/dockerfile:1
# Pulse: imagen final ≈ 12 MB (binario estático + distroless, sin shell).

# CI pasa estos valores desde .node-version y go.mod. Los defaults permiten construir localmente.
ARG NODE_VERSION=24.22.0
ARG GO_VERSION=1.27.2

# 1) Frontend (React + Vite) → internal/webui/dist
FROM node:${NODE_VERSION}-alpine3.24 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# 2) Backend Go. SQLite (mattn/go-sqlite3) necesita CGO: se enlaza estático con musl.
FROM golang:${GO_VERSION}-alpine3.24 AS api
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
RUN go build -trimpath -tags "sqlite_omit_load_extension netgo osusergo" \
      -ldflags "-s -w -linkmode external -extldflags '-static'" -o /out/pulse ./cmd/pulse

# 3) Ejecución: sin shell, sin gestor de paquetes, usuario sin privilegios (uid 65532).
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=api /out/pulse /pulse
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/pulse"]
CMD ["serve"]
