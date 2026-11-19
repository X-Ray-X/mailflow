# Tasks

## 1. Runner-safe configuration

- [x] 1.1 Build Prometheus and Grafana images with their configuration copied in, replace bind mounts in `deploy/app/compose.yaml` with the new image variables, and add both builds to `deploy-app.yml`; verify both images build locally and Compose renders with no bind mounts.
- [x] 1.2 Extend the application Compose gate to reject a bind mount on any service; verify by extracting the gate script from `ci.yml` and running it against `.env.example`.
- [x] 1.3 Update `docs/observability.md` and `README.md` with the image-based config and the no-bind-mount invariant; verify the documented commands locally.
- [x] 1.4 Run `./app/scripts/go.sh`, the web build/tests, and `openspec validate --all --strict`; review the diff for secrets and host paths, then commit on `mailflow-runner-safe-config` with the verification result.

## Workflow follow-up

- PR, merge, deploy, then archive the change and sync its delta.
