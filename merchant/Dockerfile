# Build context: repository root — `docker build -f merchant/Dockerfile .`
# The Go module replaces automate-me/ap2core with ../ap2core, so the sibling
# module must be inside the context.

FROM golang:1.26 AS build
ENV CGO_ENABLED=0 GOWORK=off GOFLAGS=-trimpath
WORKDIR /src
COPY ap2core/ ./ap2core/
COPY merchant/go.mod merchant/go.sum ./merchant/
WORKDIR /src/merchant
RUN go mod download
COPY merchant/ ./
RUN go build -o /out/merchant ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/merchant ./merchant
EXPOSE 8080
ENTRYPOINT ["/app/merchant"]
