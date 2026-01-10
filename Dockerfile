FROM golang:1.25-alpine

LABEL org.opencontainers.image.source="https://github.com/hairyhenderson/omada_exporter"

RUN apk add --no-cache git ca-certificates

COPY . /app
WORKDIR /app

RUN go build -o /usr/bin/omada-exporter .

CMD ["/usr/bin/omada-exporter"]
