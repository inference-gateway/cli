FROM ubuntu:26.04

LABEL org.opencontainers.image.title="Inference Gateway Computer"
LABEL org.opencontainers.image.description="The Ubuntu desktop for computer-use agents: infer daemon on a headless GNOME Shell session with the Ubuntu dock and theme. Install what an agent needs with cloud-init user-data."
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
      ca-certificates curl git tmux ffmpeg \
      dbus dbus-user-session libglib2.0-bin python3-dbusmock \
      gnome-shell ubuntu-session ubuntu-settings gnome-shell-extension-ubuntu-dock \
      yaru-theme-gnome-shell yaru-theme-gtk yaru-theme-icon ubuntu-wallpapers fonts-ubuntu \
      nautilus gnome-terminal gnome-text-editor \
      pipewire wireplumber at-spi2-core libgl1-mesa-dri libegl1 libgles2 && \
    rm -rf /var/lib/apt/lists/*

COPY computer/shell-extension /usr/share/gnome-shell/extensions/computer@inference-gateway.com
COPY computer/desktop.gschema.override /usr/share/glib-2.0/schemas/90_computer.gschema.override
RUN glib-compile-schemas /usr/share/glib-2.0/schemas

RUN userdel -r ubuntu && \
    useradd -m -u 1000 -s /bin/bash infer

ARG TARGETARCH
COPY --from=binaries infer-linux-${TARGETARCH} /usr/local/bin/infer
COPY computer/entrypoint.sh /usr/local/bin/computer-entrypoint
RUN chmod 0755 /usr/local/bin/infer /usr/local/bin/computer-entrypoint

USER infer
WORKDIR /home/infer

ENV XDG_RUNTIME_DIR=/run/user/1000
ENV LANG=C.UTF-8
ENV XDG_SESSION_TYPE=wayland
ENV XDG_CURRENT_DESKTOP=ubuntu:GNOME
ENV GNOME_SHELL_SESSION_MODE=ubuntu
ENV WAYLAND_DISPLAY=wayland-0
ENV INFER_IN_CONTAINER=true
ENV INFER_GATEWAY_RUN=false
ENV INFER_GATEWAY_URL=http://inference-gateway:8080

RUN infer init

USER root

ENTRYPOINT ["computer-entrypoint"]
