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
RUN apk add --no-cache chromium ca-certificates tzdata su-exec \
      font-liberation ttf-dejavu fontconfig \
    && adduser -D tome \
    && mkdir -p /data && chown tome:tome /data
COPY --from=build /out/tome /usr/local/bin/tome
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# The entrypoint starts as root only to chown the /data volume (runtimes like
# Apple's `container` mount named volumes root-owned), then drops to "tome".
ENV TOME_CHROME=/usr/bin/chromium-browser \
    TOME_CHROME_FLAGS="--no-sandbox --disable-dev-shm-usage" \
    TOME_PORT=8080 \
    TOME_DATA_DIR=/data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["entrypoint.sh"]
CMD ["serve"]
