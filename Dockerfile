FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=none
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w \
      -X characterllm/internal/version.Version=${VERSION} \
      -X characterllm/internal/version.Commit=${COMMIT}" \
    -o /out/bot ./cmd/bot

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/bot .
COPY prompts/ ./prompts/
RUN mkdir -p data logs
VOLUME ["/app/data", "/app/logs"]
CMD ["./bot"]
