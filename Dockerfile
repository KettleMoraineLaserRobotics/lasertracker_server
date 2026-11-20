FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/lasertracker_server ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H -u 2077 laser \
    && mkdir /data && chown laser:laser /data
COPY --from=build /out/lasertracker_server /usr/local/bin/lasertracker_server
USER laser

WORKDIR /data
VOLUME /data
EXPOSE 2077
ENTRYPOINT ["lasertracker_server"]
