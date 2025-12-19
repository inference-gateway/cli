FROM alpine:3.23.0

LABEL org.opencontainers.image.title="Inference Gateway CLI"
LABEL org.opencontainers.image.description="A powerful command-line interface for managing and interacting with the Inference Gateway. Provides interactive chat, autonomous agent capabilities, and extensive tool execution for AI models."
LABEL org.opencontainers.image.vendor="Inference Gateway"
LABEL org.opencontainers.image.authors="Inference Gateway Team"
LABEL org.opencontainers.image.url="https://github.com/inference-gateway/cli"
LABEL org.opencontainers.image.documentation="https://github.com/inference-gateway/cli#readme"
LABEL org.opencontainers.image.source="https://github.com/inference-gateway/cli"
LABEL org.opencontainers.image.licenses="MIT"

ARG VERSION=""
ARG REVISION=""
ARG BUILD_DATE=""

LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.revision="${REVISION}"
LABEL org.opencontainers.image.created="${BUILD_DATE}"

RUN apk --no-cache --no-scripts add ca-certificates jq bash git sqlite-libs
RUN addgroup -g 1000 infer && \
    adduser -u 1000 -G infer -h /home/infer -s /bin/bash -D infer
ARG TARGETARCH
COPY --from=binaries infer-linux-${TARGETARCH} /usr/local/bin/infer
RUN chmod +x /usr/local/bin/infer
WORKDIR /home/infer
RUN mkdir -p .infer && chown -R infer:infer .infer
USER infer

ENV INFER_GATEWAY_RUN=false
ENV INFER_GATEWAY_URL=http://inference-gateway:8080
ENV TERM=xterm-256color
ENV COLORTERM=truecolor

RUN infer init

ENTRYPOINT ["infer"]
