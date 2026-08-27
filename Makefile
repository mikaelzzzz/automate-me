.PHONY: test lint fmt run-app run-merchant run-web build docker-build gcp-setup deploy deploy-app deploy-merchant

test:
	cd app && go test -race ./...
	cd merchant && go test -race ./...

lint:
	cd app && go vet ./... && test -z "$$(gofmt -l .)"
	cd merchant && go vet ./... && test -z "$$(gofmt -l .)"
	golangci-lint run ./app/... ./merchant/... || true

fmt:
	gofmt -w app merchant
	cd app && go fix ./...
	cd merchant && go fix ./...

# Loads app/.env (GOOGLE_API_KEY etc.) if present. PORT/MERCHANT_URL from the environment win.
run-app:
	cd app && (set -a; [ -f .env ] && . ./.env; set +a; go run ./cmd/server)

run-merchant:
	cd merchant && go run ./cmd/server

run-web:
	cd app/web && npm run dev

build:
	cd app/web && npm run build
	cd app && go build ./...
	cd merchant && go build ./...

# Local validation of the Cloud Build images (context = repo root).
docker-build:
	docker build -f app/Dockerfile -t automate-me/app .
	docker build -f merchant/Dockerfile -t automate-me/merchant .

# One-time project bootstrap; GCP_PROJECT defaults to automate-me-hack (never ecosistema-karol-prod).
gcp-setup:
	./infra/gcp-setup.sh

# Cloud Build both images, roll out merchant (private) then app (public). GCP_PROJECT required.
deploy:
	./infra/deploy.sh

deploy-app:
	ONLY=app ./infra/deploy.sh

deploy-merchant:
	ONLY=merchant ./infra/deploy.sh
