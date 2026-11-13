# AGENTS.md

Mailflow is a homelab email platform used to demonstrate spec-driven development with OpenCode and OpenSpec. It is also an interview portfolio project. Optimise for a small, correct, explainable system over a large one.

## Source of truth

- **OpenSpec is authoritative for intended behavior.** Check `openspec/specs/` for current behavior and `openspec/changes/` for work in flight before writing code. If a change is active, follow its `tasks.md`.
- **Tests are authoritative for actual behavior.** A task is complete when its stated verification passes, not when the code compiles.
- **Do not create competing planning artifacts.** No separate roadmap, phase plan, product spec, or state file. ADRs in `docs/adr/` are allowed only for durable decisions that outlive a change.
- **Specs follow reality.** If implementation proves a design assumption wrong, update the change artifacts in the same piece of work instead of leaving them contradictory.

## Architecture invariants

- **Stalwart owns mail protocols and mailbox state.** It owns messages, folders, flags, and authentication. Nothing else may become a second store of mailbox truth.
- **The Go service owns the published contract.** It is the only HTTP API other components and the future web client use. It adapts to Stalwart through supported interfaces; it never reads or writes Stalwart's storage.
- **PostgreSQL, when it arrives, owns Mailflow application state only** — idempotency records, outbox entries, projections. Not mailbox contents.
- **Mail infrastructure and the application are separate deployment units.** An application deploy or rollback must never recreate, upgrade, or reset mail containers or volumes.
- **Never invent vendor configuration.** Every Stalwart option must come from the documentation of the pinned release, and the pinned release is recorded in `docs/`.

### Replaceability test

The service boundary is only real if these hold. Check them when adding an endpoint:

```
[ ] no vendor object types, method names, or error codes in public JSON
[ ] IDs are Mailflow IDs, not vendor or IMAP identifiers
[ ] no IMAP vocabulary in the public model (map to read/starred/archived)
[ ] no account name or domain used as a primary key
[ ] public contract is versioned and additive-only
```

## Scope

In scope: internal mail exchange between local accounts; standard client access over IMAP and SMTP; the Go service layer; a later React client.

Out of scope unless a change says otherwise: public internet mail, MX/SPF/DKIM/DMARC, deliverability, spam tuning, Kubernetes, service meshes, and any second spec framework.

## Engineering standards

- Idiomatic Go. Explicit errors, wrapped with context. `context.Context` threaded through I/O. No panics for expected failures.
- Bound every retry and every external call with a timeout. State what happens on failure.
- Any operation with an external side effect must be idempotent or deduplicated, and must say which.
- Configuration from the environment, validated at startup. Fail fast on missing or malformed values.
- Local development runs in Docker Compose. No host-level services.
- Secrets never enter the repository, the image, or logs. Tracked files contain names and examples only.
- Prefer the standard library. Add a dependency only when the dependency is smaller than the code it replaces.

## Safety rules

- Never delete or recreate mail volumes, the data directory, or a database volume as part of a restart, deploy, or upgrade.
- Never expose administrative interfaces or the mail server's HTTP API beyond loopback, the tailnet, or an explicitly authorised interface.
- Never configure an open relay, and never enable public mail delivery.
- Never modify Stalwart's internal database or files directly.
- Never claim a check passed without running it. If something could not be verified, say so and say why.
- Ask before destructive operations, before changing the pinned mail server release, and before widening scope.

## Version control

- Work on a feature branch, named after the OpenSpec change it implements (for example `stalwart-local-environment`). `main` stays deployable and only receives reviewed pull requests; commits go directly to `main` only for repository bootstrap.
- End every completed development loop with a commit on that branch. A loop is complete when its stated verification has run and its outcome is known; commit then, rather than letting several loops accumulate.
- One commit per coherent piece of work. Each commit should leave the repository consistent: code, docs, and OpenSpec artifacts agree.
- The message states what changed and what was verified. If something could not be verified, say so in the message.
- The pull request description repeats the verification result and the remaining gaps, so review does not depend on reading the commit log.
- Never commit secrets, `.env`, `data/`, or generated credentials. Review `git status` and the diff first.
- Do not push, open a pull request, merge, or deploy unless asked. Local work stays local until the user says otherwise.
- Do not amend or rewrite history that has already been pushed.

## Completion criteria

Before reporting work as done:

1. Acceptance scenarios for the touched capability pass, or the gap is stated plainly.
2. Formatting, vet, lint, and unit tests pass. Integration checks pass when the change affects real services.
3. The diff is reviewed for secrets, unbounded retries, unhandled errors, and accidental data loss.
4. Documentation affected by the change is updated, including client connection details and port maps.
5. The OpenSpec change reflects what was actually built.
6. The work is committed locally on the change's feature branch, with the verification result in the message, or the reason it was not committed is stated.
