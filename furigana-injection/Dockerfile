FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o furigana .

FROM scratch
COPY --from=builder /app/furigana /furigana
EXPOSE 8086
ENTRYPOINT ["/furigana", "serve"]
