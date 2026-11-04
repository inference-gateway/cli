ARG INFER_IMAGE=ghcr.io/inference-gateway/cli:latest
FROM ${INFER_IMAGE}

USER root
RUN apk --no-cache add xvfb chromium font-dejavu openssl && \
    mkdir -p /tmp/.X11-unix && chmod 1777 /tmp/.X11-unix
COPY entrypoint.sh /usr/local/bin/desktop-entrypoint
COPY scenarios.yaml /opt/infer-desktop/scenarios.yaml
RUN chmod +x /usr/local/bin/desktop-entrypoint
USER infer

ENV DISPLAY=:99

ENTRYPOINT ["desktop-entrypoint"]
