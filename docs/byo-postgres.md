# Bring Your Own Postgres

Point the agentregistry chart at an external Postgres (RDS, Cloud SQL, etc.) instead of the bundled dev/eval pod. The connection string can be supplied inline in Helm values (`database.postgres.url`) or sourced from an existing Kubernetes Secret (`database.postgres.secretRef`).

## Chart values

| Value | Default | Notes |
|---|---|---|
| `database.postgres.bundled.enabled` | `true` | Set `false` to bring your own. |
| `database.postgres.url` | `""` | Inline connection string. |
| `database.postgres.secretRef.name` | `""` | Secret in the release namespace holding the connection string. |
| `database.postgres.secretRef.key` | `AGENT_REGISTRY_DATABASE_URL` | Key within that Secret. |

Exactly one path (bundled / inline `url` / `secretRef`) must be active; the chart fails fast otherwise.

## Connection-string formats

`AGENT_REGISTRY_DATABASE_URL` accepts either libpq form, auto-detected on the leading `postgres://` prefix:

- **URL** — `postgres://user:password@host:port/db?sslmode=require`. Reserved characters in user/password must be percent-encoded.
- **Keyword/value (DSN)** — `host=... port=... user=... password='...' dbname=... sslmode=require`. Single-quote the password; no encoding.

Use keyword/value form when credentials come from a rotating store (AWS Secrets Manager, Vault) — rotation flows in without re-encoding.

## Setup

1. Ensure the database referenced in your connection string exists on your Postgres instance. The chart does not run `CREATE DATABASE`; whatever `dbname=` you supply must already be present. Migrations run against the database the connection points at.

2. Point the chart at your Postgres. Pick one path:

   **Inline `url`** — connection string lives in Helm values:

   ```bash
   helm upgrade --install agentregistry oci://<registry>/agentregistry \
     --namespace <ns> --create-namespace \
     --set database.postgres.bundled.enabled=false \
     --set database.postgres.url="host=<host> port=5432 user=<user> password='<password>' dbname=<database> sslmode=require"
   ```

   **`secretRef`** — connection string lives in a Kubernetes Secret you create or have synced. Use this when credentials shouldn't be in values (rotation, GitOps, etc.):

   ```bash
   kubectl -n <ns> create secret generic db-creds \
     --from-literal=AGENT_REGISTRY_DATABASE_URL="host=<host> port=5432 user=<user> password='<password>' dbname=<database> sslmode=require"

   helm upgrade --install agentregistry oci://<registry>/agentregistry \
     --namespace <ns> --create-namespace \
     --set database.postgres.bundled.enabled=false \
     --set database.postgres.secretRef.name=db-creds
   ```
