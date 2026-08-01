# Tome server — self-hostable image.
#
# Bundles Chromium (PDF rendering works out of the box) and the browser
# extension (served at /extension.zip, install guide at /install).
#
# Build from the REPO ROOT so the extension can be bundled:
#   docker    : docker build -t tome -f server/Dockerfile .
#   apple     : container build -t tome -f server/Dockerfile .
# Run with a named volume for the account database:
#   container run -d --name tome -p 8080:8080 -v tome-data:/data tome

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/tome ./cmd/tome/

# Package the extension for self-distribution (GET /extension.zip).
COPY extension/ /ext/tome-extension/
RUN apk add --no-cache zip \
    && cd /ext && zip -qr /out/extension.zip tome-extension -x "*.DS_Store"

FROM alpine:3.21
RUN apk add --no-cache chromium ca-certificates tzdata su-exec \
      font-liberation ttf-dejavu fontconfig \
    && adduser -D tome \
    && mkdir -p /data && chown tome:tome /data
COPY --from=build /out/tome /usr/local/bin/tome
COPY --from=build /out/extension.zip /opt/tome/extension.zip
COPY PRIVACY.md /opt/tome/PRIVACY.md
COPY server/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# The entrypoint starts as root only to chown the /data volume (runtimes like
# Apple's `container` mount named volumes root-owned), then drops to "tome".
ENV TOME_CHROME=/usr/bin/chromium-browser \
    TOME_CHROME_FLAGS="--no-sandbox --disable-dev-shm-usage" \
    TOME_PORT=8080 \
    TOME_DATA_DIR=/data \
    TOME_EXTENSION_PATH=/opt/tome/extension.zip
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["entrypoint.sh"]
CMD ["serve"]
