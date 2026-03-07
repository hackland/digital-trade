.PHONY: build run test lint clean docker-up docker-down migrate backtest klinebackfill dashboard-build dashboard-dev

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOVET=$(GOCMD) vet
BINARY_NAME=btc-trader
BACKTESTER_NAME=backtester
KLINEBACKFILL_NAME=klinebackfill

# Build flags
LDFLAGS=-ldflags "-s -w"

build:
	$(GOBUILD) $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/trader
	$(GOBUILD) $(LDFLAGS) -o bin/$(BACKTESTER_NAME) ./cmd/backtester
	$(GOBUILD) $(LDFLAGS) -o bin/$(KLINEBACKFILL_NAME) ./cmd/klinebackfill

run:
	$(GOBUILD) -o bin/$(BINARY_NAME) ./cmd/trader && ./bin/$(BINARY_NAME)

test:
	$(GOTEST) -race -v ./...

test-short:
	$(GOTEST) -short ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/

# Docker
docker-up:
	docker compose -f deployments/docker/docker-compose.yml up -d

docker-down:
	docker compose -f deployments/docker/docker-compose.yml down

# Database
migrate:
	$(GOBUILD) -o bin/$(BINARY_NAME) ./cmd/trader && ./bin/$(BINARY_NAME) -migrate

# Backtest
backtest:
	$(GOBUILD) -o bin/$(BACKTESTER_NAME) ./cmd/backtester && ./bin/$(BACKTESTER_NAME)

# K-line backfill (pull 1yr from Binance into DB)
klinebackfill:
	$(GOBUILD) -o bin/$(KLINEBACKFILL_NAME) ./cmd/klinebackfill && ./bin/$(KLINEBACKFILL_NAME) -days 365

# Frontend
frontend-install:
	cd web/dashboard && pnpm install

frontend-build:
	cd web/dashboard && pnpm build

frontend-dev:
	cd web/dashboard && pnpm dev

# Dashboard (full build: frontend + embedded Go binary)
dashboard-build: frontend-build
	rm -rf internal/web/dist
	cp -r web/dashboard/dist internal/web/dist
	$(GOBUILD) $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/trader

# Dashboard dev mode (Go with -tags dev, frontend served by Vite)
dashboard-dev:
	$(GOBUILD) -tags dev -o bin/$(BINARY_NAME) ./cmd/trader && ./bin/$(BINARY_NAME)
