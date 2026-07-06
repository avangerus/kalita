# Security Policy

## Reporting a vulnerability

Email **dantsiger@gmail.com** with subject `[kalita security]`.
Please include: affected version/commit, reproduction steps, impact assessment.
You will get an acknowledgement within 72 hours. Please do not open public
issues for security reports until a fix is released.

## Scope

The threat model — what the node defends against and what it explicitly does
not — is documented in [docs/SECURITY.md](docs/SECURITY.md) (T1–T6).
Highlights:

- every mutation passes one permission engine (default deny, deny > allow);
- the journal is append-only with a SHA-256 hash chain — tampering is
  detectable offline via `kalita verify`;
- agents act under least-privilege roles and cannot approve their own work;
- secrets live at node/worker level only, never in DSL packs.

## Supported versions

Pre-1.0: only the latest `main` commit is supported.
