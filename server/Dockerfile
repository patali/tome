# Tome server — self-hostable image.
#
# Bundles Chromium so PDF rendering works out of the box. Inside a container
# delivery is SMTP-only (no Mail.app); without SMTP env vars /convert still
# works and /send-to-kindle reports "none".
#
#   docker    : docker build -t tome . && docker run --rm -p 8080:8080 tome
#   apple     : container build -t tome . && container run --rm -p 8080:8080 tome

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/tome ./cmd/tome/

FROM alpine:3.21
RUN apk add --no-cache chromium ca-certificates tzdata \
      font-liberation ttf-dejavu fontconfig \
    && adduser -D tome
COPY --from=build /out/tome /usr/local/bin/tome

USER tome
ENV TOME_CHROME=/usr/bin/chromium-browser \
    TOME_CHROME_FLAGS="--no-sandbox --disable-dev-shm-usage" \
    TOME_PORT=8080
EXPOSE 8080
ENTRYPOINT ["tome"]
