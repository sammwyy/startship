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
RUN apk add --no-cache git ca-certificates
COPY --from=build /startshipd /usr/local/bin/startshipd
ENV STARTSHIP_DATA=/data
VOLUME /data
EXPOSE 8080 80 443
ENTRYPOINT ["startshipd"]
