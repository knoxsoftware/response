FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o respond ./cmd/respond && \
    CGO_ENABLED=0 GOOS=linux go build -o migrate ./cmd/migrate

FROM gcr.io/distroless/static-debian12
COPY --from=builder /app/respond /respond
COPY --from=builder /app/migrate /migrate
COPY --from=builder /app/migrations /migrations
ENTRYPOINT ["/respond"]
