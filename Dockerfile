FROM golang:1.26 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /gobalancer ./cmd/gobalancer


FROM scratch
COPY --from=builder /gobalancer /gobalancer
ENTRYPOINT ["/gobalancer"]
