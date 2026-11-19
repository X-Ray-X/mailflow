# Design

## Context

See `proposal.md` for motivation. The observability change introduced bind mounts of `prometheus.yml` and Grafana provisioning into `deploy/app/compose.yaml`. On the homelab runner, the checkout path exists inside the runner container but not on the host daemon; Docker created a directory instead of using the file and failed to start Prometheus. The earlier app services deliberately avoided bind mounts for this reason.

## Goals / Non-Goals

**Goals:** remove every bind mount from the application project; make the deploy independent of checkout paths on the host daemon; keep pinned upstream images.

**Non-Goals:** registries, Helm, config reload without redeploy, mail changes, or altering observability behaviour.

## Decisions

### Bake configuration into small derived images

`prometheus.Dockerfile` starts from the pinned `prom/prometheus:v3.5.0` and copies `prometheus.yml`. `grafana.Dockerfile` starts from `grafana/grafana:12.1.0` and copies the provisioning tree. The deploy builds both with the commit tag, just like the API and web images. The alternative — `docker cp` into a volume from the runner — would reintroduce host path assumptions and add an imperative deploy step.

### The Compose gate forbids all bind mounts

The existing gate already forbade bind mounts on the API and database. It now iterates every service and asserts `type != bind`. The alternative was an allow-list for the observability files; it would let the original failure return through a different path.

### The image pins stay in one place

The base image tags live in `.env.example` and are passed as build arguments to the derived Dockerfiles, so upgrading Prometheus or Grafana still requires an explicit value change. No `:latest` tag is used.

## Risks / Trade-offs

- **Config changes now need a rebuild.** That is desirable: config is versioned with the commit and deployed atomically with the service.
- **Two more images built per deploy.** Both are small and cached; the deploy already builds two images.
- **No live config reload.** Prometheus and Grafana read their files at startup; a config change redeploys them.
