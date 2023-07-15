ARG DOCKER_ARCH
ARG GO_VERSION
ARG IMAGE_VERSION
ARG DOCKER_OS
FROM --platform=${DOCKER_OS}/${DOCKER_ARCH} golang:${GO_VERSION}-alpine${IMAGE_VERSION} as builder

ADD . /gateway-mt
WORKDIR /gateway-mt

RUN apk update
RUN apk add --no-cache git gcc musl-dev
RUN go build -v -o /tmp/gateway-mt ./cmd/gateway-mt

FROM --platform=${DOCKER_OS}/${DOCKER_ARCH} alpine:${IMAGE_VERSION}

EXPOSE 20010
WORKDIR /app

COPY --from=builder /tmp/gateway-mt /app/gateway-mt

RUN apk update && \
    apk add --no-cache libc6-compat gcompat ca-certificates tzdata && \
    update-ca-certificates

COPY --from=builder /gateway-mt/cmd/gateway-mt/etc/nsswitch.conf /etc/nsswitch.conf
COPY --from=builder /gateway-mt/cmd/gateway-mt/entrypoint /entrypoint

RUN chmod +x /app/gateway-mt

ENTRYPOINT ["/entrypoint"]

ENV STORJ_CONFIG_DIR=/root/.local/share/storj/gateway-mt
ENV STORJ_SERVER_ADDRESS=0.0.0.0:20010

# Healthcheck URL: http://<host>:20010/-/health
