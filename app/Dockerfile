# Build context: repository root — `docker build -f app/Dockerfile .`
# The Go module replaces automate-me/ap2core with ../ap2core, so the sibling
# module must be inside the context.

# --- web build ---
FROM node:24-slim AS web
WORKDIR /web
COPY app/web/package.json app/web/package-lock.json ./
RUN npm ci --no-fund --no-audit
COPY app/web/ ./
RUN npm run build

# --- go build ---
FROM golang:1.26 AS build
ENV CGO_ENABLED=0 GOWORK=off GOFLAGS=-trimpath
WORKDIR /src
COPY ap2core/ ./ap2core/
COPY app/go.mod app/go.sum ./app/
WORKDIR /src/app
RUN go mod download
COPY app/ ./
RUN go build -o /out/server ./cmd/server

# --- runtime ---
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/server ./server
COPY --from=web /web/dist ./web/dist
ENV WEB_DIST=/app/web/dist
EXPOSE 8080
ENTRYPOINT ["/app/server"]
