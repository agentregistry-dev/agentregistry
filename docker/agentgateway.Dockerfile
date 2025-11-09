FROM ubuntu:24.04

# Install Node.js, Python, uv, and dependencies
RUN apt-get update && apt-get install -y \
    curl \
    ca-certificates \
    gnupg \
    python3 \
    python3-pip \
    && mkdir -p /etc/apt/keyrings \
    && curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key | gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg \
    && echo "deb [signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_20.x nodistro main" | tee /etc/apt/sources.list.d/nodesource.list \
    && apt-get update \
    && apt-get install -y nodejs \
    && curl -LsSf https://astral.sh/uv/install.sh | sh \
    && mv /root/.local/bin/uv /usr/local/bin/uv \
    && mv /root/.local/bin/uvx /usr/local/bin/uvx \
    && rm -rf /var/lib/apt/lists/*

# Copy agentgateway binary from official image
COPY --from=ghcr.io/agentgateway/agentgateway:0.10.5 /app/agentgateway /usr/local/bin/agentgateway

WORKDIR /app

ENTRYPOINT ["/usr/local/bin/agentgateway"]

# The config file will be mounted via volume
CMD ["-f", "/config/agent-gateway.yaml"]

