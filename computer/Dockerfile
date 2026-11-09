FROM ubuntu:26.04

LABEL org.opencontainers.image.title="Inference Gateway Computer"
LABEL org.opencontainers.image.description="A minimal Ubuntu desktop for computer-use agents: infer daemon on an Xvfb display with openbox and a tint2 taskbar. Install what an agent needs with cloud-init user-data."
LABEL org.opencontainers.image.vendor="Inference Gateway"
LABEL org.opencontainers.image.authors="Inference Gateway Team"
LABEL org.opencontainers.image.url="https://github.com/inference-gateway/cli"
LABEL org.opencontainers.image.documentation="https://github.com/inference-gateway/cli/blob/main/docs/computer-use.md"
LABEL org.opencontainers.image.source="https://github.com/inference-gateway/cli"
LABEL org.opencontainers.image.licenses="Apache-2.0"

ARG VERSION=""
ARG REVISION=""
ARG BUILD_DATE=""

LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.revision="${REVISION}"
LABEL org.opencontainers.image.created="${BUILD_DATE}"

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      ca-certificates curl git \
      xvfb x11-xserver-utils openbox python3-xdg tint2 xterm fonts-dejavu-core adwaita-icon-theme && \
    rm -rf /var/lib/apt/lists/* && \
    rm -f /usr/share/applications/tint2*.desktop /usr/share/applications/debian-uxterm.desktop && \
    sed -i 's|</applications>|<application class="*" type="normal"><maximized>yes</maximized></application></applications>|' \
      /etc/xdg/openbox/rc.xml

RUN userdel -r ubuntu && \
    useradd -m -u 1000 -s /bin/bash infer && \
    install -d -m 1777 /tmp/.X11-unix

ARG TARGETARCH
COPY --from=binaries infer-linux-${TARGETARCH} /usr/local/bin/infer
COPY computer/entrypoint.sh /usr/local/bin/computer-entrypoint
COPY computer/tint2rc /etc/xdg/tint2/computer.tint2rc
RUN chmod 0755 /usr/local/bin/infer /usr/local/bin/computer-entrypoint

USER infer
WORKDIR /home/infer

ENV DISPLAY=:99
ENV INFER_IN_CONTAINER=true
ENV INFER_GATEWAY_RUN=false
ENV INFER_GATEWAY_URL=http://inference-gateway:8080

RUN infer init

ENTRYPOINT ["computer-entrypoint"]
