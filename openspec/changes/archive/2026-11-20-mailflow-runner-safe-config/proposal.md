# Proposal

## Why

The first production deploy of the observability stack failed at container
startup. The runner's checkout path is not visible to the homelab Docker daemon,
so the bind mounts of `prometheus.yml` and Grafana provisioning files resolved
to directories and were rejected. The application Compose project previously
had no bind mounts for exactly this reason.

## What Changes

- Build Prometheus and Grafana images from pinned upstream bases with their
  configuration copied in at build time, and deploy those commit-tagged images.
- Remove all bind mounts from the application Compose project; only named
  volumes remain.
- Update the Compose gate to assert that no application service uses a bind
  mount, including the observability services.
- Document that the runner streams source into builds instead of relying on
  host paths.

Out of scope: changing application code, the `/v1` contract, mail infrastructure,
or adding a registry. Load testing and alerting stay out of scope.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `service-deployment`: requires runner-safe images with no host bind mounts.

## Impact

- `deploy/app/prometheus.Dockerfile`, `deploy/app/grafana.Dockerfile`,
  `deploy/app/compose.yaml`, `.env.example`, and `deploy-app.yml`.
- `.github/workflows/ci.yml` Compose gate and `docs/`.
- No application code or mail deployment change.
