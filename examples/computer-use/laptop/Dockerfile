ARG INFER_IMAGE=ghcr.io/inference-gateway/cli:latest
FROM ${INFER_IMAGE}

USER root
RUN apk --no-cache add openssl
COPY entrypoint.sh /usr/local/bin/laptop-entrypoint
COPY daemon.yaml /opt/infer-laptop/daemon.yaml
RUN chmod +x /usr/local/bin/laptop-entrypoint
USER infer

ENTRYPOINT ["laptop-entrypoint"]
