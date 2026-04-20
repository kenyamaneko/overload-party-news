FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
COPY packages/api-news/go.mod packages/api-news/
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /news ./cmd/server

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=builder /news /app/news
EXPOSE 9008 9108
ENTRYPOINT ["/app/news"]
