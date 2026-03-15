FROM python:3.11-slim-bookworm AS builder

ARG HERMES_REPO=https://github.com/NousResearch/hermes-agent.git
ARG HERMES_REF=main

ENV DEBIAN_FRONTEND=noninteractive \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_NO_CACHE_DIR=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    VIRTUAL_ENV=/opt/venv \
    PATH=/opt/venv/bin:$PATH

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    git \
    libffi-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /opt
RUN git clone --depth 1 --branch ${HERMES_REF} ${HERMES_REPO} hermes-src \
    && cd /opt/hermes-src \
    && git submodule update --init --depth 1 mini-swe-agent

RUN python -m venv ${VIRTUAL_ENV}

WORKDIR /opt/hermes-src
RUN pip install --upgrade pip setuptools wheel \
    && pip install ".[messaging,cron,pty,mcp]" \
    && if [ -f mini-swe-agent/pyproject.toml ]; then pip install ./mini-swe-agent; fi \
    && python -c "import hermes_cli.main, gateway.run; print('Hermes import OK')"

FROM python:3.11-slim-bookworm

ENV DEBIAN_FRONTEND=noninteractive \
    HERMES_HOME=/data/hermes \
    HOME=/home/hermes \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_NO_CACHE_DIR=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    VIRTUAL_ENV=/opt/venv \
    PATH=/opt/venv/bin:$PATH \
    MSWEA_SILENT_STARTUP=1

RUN apt-get update && apt-get install -y --no-install-recommends \
    bash \
    ca-certificates \
    ffmpeg \
    git \
    ripgrep \
    tini \
    && rm -rf /var/lib/apt/lists/*

RUN useradd --create-home --home-dir /home/hermes --shell /bin/bash --uid 10001 hermes

COPY --from=builder /opt/venv /opt/venv
COPY --from=builder /opt/hermes-src /opt/hermes-src
COPY entrypoint.sh /usr/local/bin/hermes-entrypoint

RUN chmod +x /usr/local/bin/hermes-entrypoint \
    && mkdir -p /data \
    && chown -R hermes:hermes /data /home/hermes /opt/hermes-src

USER hermes
WORKDIR /home/hermes

ENTRYPOINT ["tini", "--", "/usr/local/bin/hermes-entrypoint"]
CMD ["hermes", "gateway"]
