# Security Notes (Draft)

## Credential handling

- Never log DSN credentials.
- Prefer secret managers or environment variables over plaintext files.
- Restrict DB credentials to least privileges required per operation.

## TLS

- `--tls-mode=required` is the default and should remain the production baseline.
- `--tls-mode=preferred` is downgrade-capable and should be used only as explicit operator opt-in for controlled environments.
- Runtime TLS settings are now applied to both SQL command paths and binlog streaming paths.
- Validate CA/cert/key paths before runtime operations.
- If a requested TLS requirement cannot be applied on a path, fail fast; do not rely on silent fallback.

## Sensitive artifacts

- Replication conflict report value samples are redacted by default.
- Plain-text conflict samples are available only via explicit opt-in: `--conflict-values=plain`.
- Treat plain conflict artifacts as sensitive data and limit retention/access.

## User and grant migration

- Keep user/grant migration optional and explicit.
- Produce a detailed compatibility report for auth plugin mismatches.
- Avoid unsafe automatic plugin downgrades by default.
- Current guardrail: surface auth-plugin drift during `plan` even when the baseline run is only schema/data.
