FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine3.23 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/hzk-doc ./cmd/hzk-doc

FROM alpine:3.24
RUN addgroup -g 10001 -S hzk-doc && adduser -u 10001 -S -G hzk-doc -h /app hzk-doc
WORKDIR /app
COPY --from=build /out/hzk-doc /app/hzk-doc
RUN mkdir -p /data && chown hzk-doc:hzk-doc /data
USER hzk-doc
EXPOSE 8080
ENTRYPOINT ["/app/hzk-doc"]
CMD ["--docs", "/data/docs", "--listen", "0.0.0.0:8080"]
