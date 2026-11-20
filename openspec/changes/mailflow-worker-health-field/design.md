# Design

## Context

The previous change added a zero-restart requirement for the worker in the
deploy health loop. It used `.State.RestartCount`, but the Docker inspect schema
used on the runner places `RestartCount` at the top level. The template error
was written to stderr, the command substitution produced an empty string, and
the comparison against `0` failed.

## Goals / Non-Goals

Fix the field path and keep the gate strict. No other changes.

## Decisions

### Use `{{.RestartCount}}`

This field is present in the inspect output on the runner and locally. The
alternative — treating an empty value as zero — would hide template errors and
weaken the gate, so it was rejected.

## Risks / Trade-offs

A Docker version that moves the field again would fail the deploy with an
explicit error, which is preferable to a false green.
