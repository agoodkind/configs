FROM ghcr.io/actions/actions-runner:2.336.0@sha256:a1919047b038c38871d667c58cfdc7a878452711ab1212fb6036188f27a7ab16

LABEL org.opencontainers.image.source="https://github.com/agoodkind/configs"

USER root

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        build-essential \
        docker-compose-v2 \
    && mkdir -p /usr/local/lib/docker/cli-plugins \
    && ln -s \
        /usr/libexec/docker/cli-plugins/docker-compose \
        /usr/local/lib/docker/cli-plugins/docker-compose \
    && rm -rf /var/lib/apt/lists/*

USER runner
