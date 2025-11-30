FROM golang:1.23.3-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/goshort_binary main.go

# ---- Stage 2: Minimal Runner ----
FROM alpine:3.22

RUN apk --no-cache add ca-certificates

WORKDIR /app
COPY --from=builder /app/goshort_binary .

EXPOSE 8080

CMD ["./goshort_binary"]
