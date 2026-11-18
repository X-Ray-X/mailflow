# Spec Delta

## ADDED Requirements

### Requirement: Pull requests run code-quality gates

The `CI — mail infrastructure` workflow MUST verify the Go service and the web
client on every pull request, using pinned toolchain images. It MUST run Go
formatting, vet, and unit tests; client dependency install, TypeScript build,
and unit tests; Python syntax checks for tracked scripts; and
`openspec validate --all --strict`. A failure in any gate MUST fail the workflow.

#### Scenario: Go code regresses on a pull request

- **WHEN** a pull request changes Go code that is unformatted, fails vet, or
  fails a unit test
- **THEN** the Go quality gate fails and the workflow reports the failing step

#### Scenario: Web client does not build

- **WHEN** a pull request changes client code that fails the TypeScript build or
  a unit test
- **THEN** the web quality gate fails and the workflow reports the failing step

#### Scenario: Specs and code disagree

- **WHEN** an OpenSpec change is left invalid or incomplete
- **THEN** the OpenSpec gate fails and the workflow reports the validation output

### Requirement: Application compose gate covers both services

The application Compose gate MUST assert that the project contains exactly the
API, database, and static client services; that every published port uses
loopback; that the database publishes nothing and mounts no host path; and that
no mail data directory is mounted. The client's frontend network MUST NOT be
internal so its loopback port can be published, and the data network MUST remain
internal.

#### Scenario: A new service is added to the application project

- **WHEN** a pull request adds a service beyond api, db, and web
- **THEN** the application compose gate fails until the declaration is updated

#### Scenario: The database becomes reachable from the host

- **WHEN** a pull request publishes a database port or bind-mounts its data
- **THEN** the application compose gate fails and names the broken invariant

### Requirement: The application deploy builds and verifies the client

The `Deploy application` workflow MUST build a web image tagged for the commit,
write its tag to the generated `.env`, and verify that the client is running
after the deploy. It MUST still verify that the mail container's identity, start
time, and image are unchanged.

#### Scenario: A commit is deployed

- **WHEN** the application deploy workflow finishes
- **THEN** both the API and web images carry the commit tag and the run summary
  names the client's loopback address

#### Scenario: The mail stack changes during an application deploy

- **WHEN** the mail container's identity, start time, or image differs after the
  deploy
- **THEN** the workflow fails and reports the before and after values
