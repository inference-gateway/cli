ARG INFER_IMAGE=ghcr.io/inference-gateway/cli:local
FROM ${INFER_IMAGE}

USER root
RUN apk --no-cache add xvfb chromium font-dejavu ffmpeg minio-client && \
    mkdir -p /tmp/.X11-unix && chmod 1777 /tmp/.X11-unix
COPY entrypoint.sh /opt/infer-desktop/bin/infer
COPY upload-artifacts /usr/local/bin/upload-artifacts
COPY scenarios.yaml /opt/infer-desktop/scenarios.yaml
COPY --chown=infer:infer hooks.yaml /home/infer/.infer/hooks.yaml
RUN chmod +x /opt/infer-desktop/bin/infer /usr/local/bin/upload-artifacts
USER infer

ENV DISPLAY=:99
ENV PATH=/opt/infer-desktop/bin:$PATH
