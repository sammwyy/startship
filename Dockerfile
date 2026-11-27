FROM node:22-alpine AS frontend
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/web/dist ./web/dist
ARG VERSION=dev
ARG COMMIT=dev
RUN CGO_ENABLED=0 go build -ldflags "-X github.com/sammwyy/startship/internal/version.Version=${VERSION} -X github.com/sammwyy/startship/internal/version.Commit=${COMMIT}" -o /startshipd ./cmd/startshipd
FROM docker:29-cli
RUN apk add --no-cache git ca-certificates curl
ARG TARGETARCH
RUN case "$TARGETARCH" in \
      amd64) arch=x86_64; checksum=0f55de7874507b9cf7502113120bd96f2ab6979f78d10eaf2eb2ade9207b3af6 ;; \
      arm64) arch=aarch64; checksum=912bd02dd2bb6f9c3a9ed965fe8a68b4aa318dc7a2546e2eca6f2806a894ba39 ;; \
      *) exit 1 ;; \
    esac && \
    curl -fLsS "https://github.com/railwayapp/nixpacks/releases/download/v1.41.0/nixpacks-v1.41.0-$arch-unknown-linux-musl.tar.gz" -o /tmp/nixpacks.tar.gz && \
    echo "$checksum  /tmp/nixpacks.tar.gz" | sha256sum -c - && \
    tar -xzf /tmp/nixpacks.tar.gz -C /usr/local/bin nixpacks && rm /tmp/nixpacks.tar.gz
COPY --from=build /startshipd /usr/local/bin/startshipd
ENV STARTSHIP_DATA=/data
VOLUME /data
EXPOSE 9000 80 443
ENTRYPOINT ["startshipd"]
