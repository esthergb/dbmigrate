# Security Notes (Draft)

## Credential handling

- Never log DSN credentials.
- Prefer secret managers or environment variables over plaintext files.
- Restrict DB credentials to least privileges required per operation.

## TLS

- Use `--tls-mode=required` when possible.
- Validate CA/cert/key paths before runtime operations.

## User and grant migration

- Keep user/grant migration optional and explicit.
- Produce a detailed compatibility report for auth plugin mismatches.
- Avoid unsafe automatic plugin downgrades by default.
