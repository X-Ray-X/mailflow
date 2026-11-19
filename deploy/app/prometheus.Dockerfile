# Configuration is baked into the image because the runner's checkout path is
# not visible to the homelab Docker daemon. No bind mounts at deploy time.
ARG PROMETHEUS_BASE=prom/prometheus:v3.5.0
FROM ${PROMETHEUS_BASE}

COPY prometheus.yml /etc/prometheus/prometheus.yml
