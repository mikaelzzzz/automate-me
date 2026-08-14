.PHONY: test lint fmt run-app run-merchant run-web build deploy-app deploy-merchant

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

run-app:
	cd app && go run ./cmd/server

run-merchant:
	cd merchant && go run ./cmd/server

run-web:
	cd app/web && npm run dev

build:
	cd app/web && npm run build
	cd app && go build ./...
	cd merchant && go build ./...

# GCP_PROJECT and REGION must be set; see README spin-up.
deploy-app:
	gcloud run deploy automate-me --source app --project $(GCP_PROJECT) --region $(REGION) --allow-unauthenticated

deploy-merchant:
	gcloud run deploy merchant-agent --source merchant --project $(GCP_PROJECT) --region $(REGION) --no-allow-unauthenticated
