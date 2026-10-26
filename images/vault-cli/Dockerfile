# Bitwarden's official CLI, used by Shortdesk to read and write the Vaultwarden vault.
# Runs as a long-lived helper container next to the vault; Shortdesk calls `bw` in it.
FROM node:22-bookworm-slim
ARG BW_VERSION=2026.9.1
RUN npm install -g @bitwarden/cli@${BW_VERSION} && npm cache clean --force \
 && apt-get update && apt-get install -y --no-install-recommends jq ca-certificates \
 && rm -rf /var/lib/apt/lists/*
ENV BITWARDENCLI_APPDATA_DIR=/data
VOLUME /data
ENTRYPOINT ["sleep", "infinity"]
