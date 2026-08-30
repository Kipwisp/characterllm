FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bot ./cmd/bot

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/bot .
COPY prompts/ ./prompts/
RUN mkdir -p data logs
VOLUME ["/app/data", "/app/logs"]
CMD ["./bot"]
