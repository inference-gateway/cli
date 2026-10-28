ARG INFER_IMAGE=ghcr.io/inference-gateway/cli:0.239.0-rc.2
FROM ${INFER_IMAGE}

USER root
RUN apk --no-cache add xvfb chromium font-dejavu ffmpeg minio-client && \
    mkdir -p /tmp/.X11-unix && chmod 1777 /tmp/.X11-unix
COPY entrypoint.sh /usr/local/bin/desktop-entrypoint
COPY upload-artifacts /usr/local/bin/upload-artifacts
COPY scenarios.yaml /opt/infer-desktop/scenarios.yaml
COPY --chown=infer:infer hooks.yaml /home/infer/.infer/hooks.yaml
RUN chmod +x /usr/local/bin/desktop-entrypoint /usr/local/bin/upload-artifacts
USER infer

ENV DISPLAY=:99

ENTRYPOINT ["desktop-entrypoint"]
