FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o bulk-api cmd/server/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
RUN mkdir -p /app/uploads /app/exports

WORKDIR /app
COPY --from=builder /app/bulk-api .

# Create uploads and exports directories
RUN mkdir -p uploads exports

EXPOSE 8080

CMD ["./bulk-api"]