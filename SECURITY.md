# Security policy

Docveta stores people's most sensitive papers: IDs, bank statements, medical records.
Security reports are taken seriously and handled before feature work.

## Reporting a vulnerability

**Please don't open a public issue.** Report privately through GitHub:
*Security → Report a vulnerability* on <https://github.com/anand34577/docveta>
(private vulnerability reporting).

Include what you found, how to reproduce it, the affected version (`docveta version`), and
the impact you expect. You'll get an acknowledgement within 7 days and a fix or a plan
within 30 days for confirmed issues. Credit is given in the release notes unless you
prefer otherwise.

## Supported versions

Only the latest release receives security fixes while Docveta is pre-1.0.

## Scope

In scope: the server (`cmd/`, `internal/`), the web app (`web/`), the reference OCR
workers and the worker SDK (`workers/`), and the Docker images we publish.

Out of scope: vulnerabilities that need an administrator account or shell access to the
host, denial of service through very large uploads within the configured limit, and
issues in third-party services you connect (identity provider, SMTP, Gotify, ntfy).

## Hardening notes for operators

- Serve Docveta over HTTPS (`DOCVETA_BASE_URL=https://…`); cookies are only marked `Secure`
  then.
- Keep `DOCVETA_SECRET_KEY` secret and backed up; it encrypts stored credentials.
- Leave `DOCVETA_ALLOW_LOCAL_TARGETS` off unless you trust every user: it lets
  notification channels reach your private network.
- Expose `/metrics` only on a private network.
- Run `docveta doctor` after upgrades and restores.
