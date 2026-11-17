FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /harbord .
FROM docker:29-cli
RUN apk add --no-cache git ca-certificates
COPY --from=build /harbord /usr/local/bin/harbord
ENV HARBOR_DATA=/data
VOLUME /data
EXPOSE 8080 80 443
ENTRYPOINT ["harbord"]
