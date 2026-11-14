# syntax=docker/dockerfile:1

# --- Build the Go binary -------------------------------------------------
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/jellyfin-hue-sync ./cmd/jellyfin-hue-sync

# --- Runtime: static binary + ffmpeg --------------------------------------
FROM alpine:3.21
RUN apk add --no-cache ffmpeg
WORKDIR /app
COPY --from=build /out/jellyfin-hue-sync .

ENV CONFIG_DIR=/config \
    PORT=8091

VOLUME ["/config"]
EXPOSE 8091

ENTRYPOINT ["/app/jellyfin-hue-sync"]
