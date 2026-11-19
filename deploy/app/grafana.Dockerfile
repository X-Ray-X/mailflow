# Provisioned datasource and dashboard travel with the image; the Compose
# project has no host bind mounts.
ARG GRAFANA_BASE=grafana/grafana:12.1.0
FROM ${GRAFANA_BASE}

COPY grafana/provisioning/datasources /etc/grafana/provisioning/datasources
COPY grafana/provisioning/dashboards /etc/grafana/provisioning/dashboards
COPY grafana/provisioning/dashboards/mailflow.json /var/lib/grafana/dashboards/mailflow.json
