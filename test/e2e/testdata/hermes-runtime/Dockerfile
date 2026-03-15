FROM debian:bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive \
    HERMES_HOME=/data/hermes \
    HOME=/home/hermes

RUN apt-get update && apt-get install -y --no-install-recommends \
    bash \
    coreutils \
    procps \
    && rm -rf /var/lib/apt/lists/*

RUN useradd --create-home --home-dir /home/hermes --shell /bin/bash --uid 10001 hermes

COPY hermes /usr/local/bin/hermes
RUN chmod +x /usr/local/bin/hermes \
    && mkdir -p /data \
    && chown -R hermes:hermes /data /home/hermes

USER hermes
WORKDIR /home/hermes

ENTRYPOINT ["/usr/local/bin/hermes"]
CMD ["gateway"]
