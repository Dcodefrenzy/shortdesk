# Spike: Shortdesk desk image v0
# Debian 13 XFCE desktop in the browser (Selkies) + Chromium + Node + Claude Code.
FROM lscr.io/linuxserver/webtop:debian-xfce

RUN apt-get update \
 && apt-get install -y --no-install-recommends nodejs jq tmux curl ca-certificates \
 && npm install -g @anthropic-ai/claude-code @playwright/mcp \
 && apt-get clean && rm -rf /var/lib/apt/lists/*

# Links (xdg-open, `claude` login, etc.) must use the same wrapper as the desktop icon.
# Plain /usr/bin/chromium dies in a container with "No usable sandbox!".
RUN update-alternatives --install /usr/bin/x-www-browser x-www-browser /usr/local/bin/wrapped-chromium 300 \
 && update-alternatives --set x-www-browser /usr/local/bin/wrapped-chromium
ENV BROWSER=/usr/local/bin/wrapped-chromium

ENV TITLE="Shortdesk"
