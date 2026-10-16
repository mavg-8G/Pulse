# Pulse — tareas comunes
.PHONY: dev web build test race cover fuzz web-test e2e lint check docker clean

GO_TAGS := sqlite_omit_load_extension
BUILD_TIMESTAMP := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_REVISION := $(shell git rev-parse HEAD 2>/dev/null)
BUILD_LDFLAGS := -X pulse/internal/buildinfo.BuiltAt=$(BUILD_TIMESTAMP) -X pulse/internal/buildinfo.Revision=$(GIT_REVISION)

dev: ## API en :8080 (modo desarrollo). En otra terminal: cd web && npm run dev
	@mkdir -p .dev
	@test -f .dev/pulse_kek || (head -c32 /dev/urandom | base64 > .dev/pulse_kek)
	PULSE_ENV=development PULSE_BASE_URL=http://localhost:5173 PULSE_DB_PATH=.dev/pulse.db \
	PULSE_KEK_FILE=.dev/pulse_kek go run -tags $(GO_TAGS) ./cmd/pulse serve

web: ## Compila el frontend dentro de internal/webui/dist
	cd web && npm ci && npm run build

build: web ## Binario completo en ./bin/pulse
	CGO_ENABLED=1 go build -trimpath -tags $(GO_TAGS) -ldflags "$(BUILD_LDFLAGS)" -o bin/pulse ./cmd/pulse

test: ## Tests de Go
	go test -tags $(GO_TAGS) ./...

race: ## Tests con detector de carreras
	go test -race -tags $(GO_TAGS) ./...

cover: ## Cobertura por paquete
	go test -tags $(GO_TAGS) -coverprofile=cover.out ./... && go tool cover -func=cover.out | tail -1

fuzz: ## 30 s de fuzzing por objetivo
	go test ./internal/crypto -run=^$$ -fuzz=FuzzOpen -fuzztime=30s
	go test ./internal/passkey -run=^$$ -fuzz=FuzzCOSEyAuthData -fuzztime=30s
	go test ./internal/budget -run=^$$ -fuzz=FuzzCompute -fuzztime=30s
	go test ./internal/mailparse -run=^$$ -fuzz=FuzzParseAmount -fuzztime=30s
	go test ./internal/mailparse -run=^$$ -fuzz=FuzzParseDate -fuzztime=30s
	go test ./internal/mailparse -run=^$$ -fuzz=FuzzEML -fuzztime=30s
	go test ./internal/gmail -run=^$$ -fuzz=FuzzVerifyJWT -fuzztime=30s
	go test ./internal/gmail -run=^$$ -fuzz=FuzzCheckState -fuzztime=30s

web-test: ## Tipos + tests unitarios del frontend
	cd web && npx tsc --noEmit && npx vitest run

e2e: web ## Playwright (escritorio y móvil) contra el binario real
	cd web && npx playwright test

lint:
	gofmt -l . | grep -v node_modules | (! grep .) && go vet ./...

check: lint race web-test e2e ## Todo lo que corre la CI

docker:
	docker build --build-arg BUILD_TIMESTAMP="$(BUILD_TIMESTAMP)" --build-arg GIT_REVISION="$(GIT_REVISION)" -t pulse:latest .

clean:
	rm -rf bin .dev cover.out web/.e2e web/test-results
