FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /startshipd .
FROM docker:29-cli
RUN apk add --no-cache git ca-certificates
COPY --from=build /startshipd /usr/local/bin/startshipd
ENV STARTSHIP_DATA=/data
VOLUME /data
EXPOSE 8080 80 443
ENTRYPOINT ["startshipd"]
