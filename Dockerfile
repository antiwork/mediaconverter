FROM golang:1.23 AS builder

WORKDIR /app

COPY ./src /app

ENV CGO_ENABLED=0 GOOS=linux

RUN go build -o mediaconverter

FROM ubuntu:24.04

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    ffmpeg \
    bc \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /app/mediaconverter /app/mediaconverter

COPY ./ffmpeg /app/ffmpeg
RUN chmod -R +x /app/ffmpeg/*.sh

EXPOSE 80
EXPOSE 7454

CMD ["/app/mediaconverter"]
