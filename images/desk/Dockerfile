# Shortdesk desk image.
# A Linux desktop (XFCE) viewed in the browser through KasmVNC (seamless clipboard with the
# viewer's browser), with Chromium and agent CLIs.
# Starts itself with start-desk, so it runs as a normal child process: under plain
# Docker, or under OpenSandbox's execd bootstrap (which must stay PID 1).
FROM debian:trixie-slim

ARG TARGETARCH
ARG KASMVNC_VERSION=1.5.2

ENV DEBIAN_FRONTEND=noninteractive \
    LANG=en_US.UTF-8 \
    LC_ALL=en_US.UTF-8 \
    TZ=Etc/UTC

RUN apt-get update \
 && apt-get install -y --no-install-recommends locales curl ca-certificates \
 && sed -i 's/^# *en_US.UTF-8/en_US.UTF-8/' /etc/locale.gen && locale-gen \
 && curl -fsSL -o /tmp/kasmvnc.deb \
      "https://github.com/kasmtech/KasmVNC/releases/download/v${KASMVNC_VERSION}/kasmvncserver_debian_trixie_${KASMVNC_VERSION}_${TARGETARCH}.deb" \
 && apt-get install -y --no-install-recommends /tmp/kasmvnc.deb \
 && rm /tmp/kasmvnc.deb \
 && apt-get install -y --no-install-recommends \
      xfce4 xfce4-terminal dbus-x11 thunar \
      chromium firefox-esr fonts-dejavu-core fonts-noto-color-emoji \
      xclip xdotool x11-xserver-utils sqlite3 \
      nodejs npm git jq tmux tzdata \
 && npm install -g @anthropic-ai/claude-code @playwright/mcp \
 && npm cache clean --force \
 && apt-get clean && rm -rf /var/lib/apt/lists/*

# Free Claude Code: a local proxy that lets Claude Code use other models with the
# owner's own keys (DeepSeek, Gemini, Groq, ...). Started per task by Shortdesk only when
# the router picks it. It needs an exact Python, which uv fetches without touching the
# system Python.
COPY --from=ghcr.io/astral-sh/uv:0.13.5 /uv /usr/local/bin/uv
ARG FCC_VERSION=v6.9.2
RUN UV_PYTHON_INSTALL_DIR=/opt/uv/python UV_TOOL_DIR=/opt/uv/tools UV_TOOL_BIN_DIR=/usr/local/bin \
      uv tool install --python 3.14.7 \
      "free-claude-code @ git+https://github.com/Alishahryar1/free-claude-code@${FCC_VERSION}" \
 && chmod -R a+rX /opt/uv \
 && rm -rf /root/.cache/uv

# Non-root desk user. Its home is the desk's persistent storage
# (browser profile, logins, Claude config).
RUN groupadd -g 1000 desk && useradd -m -u 1000 -g desk -G audio,video -s /bin/bash desk \
 && install -d -o desk -g desk /home/desk/Developer /home/desk/Files /home/desk/outputs /home/desk/.phone \
 && mkdir -p /tmp/.X11-unix && chmod 1777 /tmp/.X11-unix

COPY rootfs/ /
RUN chmod +x /usr/local/bin/* \
 && update-alternatives --install /usr/bin/x-www-browser x-www-browser /usr/local/bin/desk-chromium 300 \
 && update-alternatives --set x-www-browser /usr/local/bin/desk-chromium

ENV BROWSER=/usr/local/bin/desk-chromium \
    DISPLAY=:0

USER desk
WORKDIR /home/desk
EXPOSE 6080
ENTRYPOINT ["/usr/local/bin/start-desk"]
