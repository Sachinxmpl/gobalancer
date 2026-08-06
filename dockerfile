FROM golang:1.26.5 AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o bin/gobalancer ./cmd/gobalancer

FROM debian:bookworm-slim
COPY --from=build /app/bin/gobalancer /gobalancer
ENTRYPOINT ["/gobalancer", "run", "-c", "/config.yaml"]