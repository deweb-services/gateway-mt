ARG DOCKER_ARCH
ARG GO_VERSION
ARG IMAGE_VERSION
ARG DOCKER_OS
FROM --platform=${DOCKER_OS}/${DOCKER_ARCH} golang:${GO_VERSION}-alpine${IMAGE_VERSION} as builder

ADD . /authservice
WORKDIR /authservice

RUN apk update
RUN apk add --no-cache git gcc musl-dev
RUN go build -v -o /tmp/authservice ./cmd/authservice

FROM --platform=${DOCKER_OS}/${DOCKER_ARCH} alpine:${IMAGE_VERSION}

ENV GOARCH ${GOARCH}

RUN apk update && \
    apk add --no-cache ca-certificates tzdata && \
    update-ca-certificates

EXPOSE 20000
WORKDIR /app

COPY --from=builder /tmp/authservice /app/authservice

COPY --from=builder /authservice/cmd/authservice/etc/nsswitch.conf /etc/nsswitch.conf
COPY --from=builder /authservice/cmd/authservice/entrypoint /entrypoint

ENTRYPOINT ["/entrypoint"]

ENV STORJ_CONFIG_DIR=/root/.local/share/storj/authservice
ENV STORJ_LISTEN_ADDR=0.0.0.0:20000

# Healthcheck URLs:
#  * Startup successful: https://<host>:20000/v1/health/startup
#  * Able to hit DB: https://<host>:20000/v1/health/live
