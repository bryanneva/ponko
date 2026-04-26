# M2 Cutover Runbook: `personal-agent-bn` → `ponko-bn`

This runbook captures the operational steps to migrate Bryan's personal Slack
bot from the `personal-agent-bn` Fly app to the new `ponko-bn` Fly app as part
of **Consolidation M2: Deploy Ponko as Personal-Agent Replacement**. For the
overall consolidation strategy, scope, and milestone definitions, see
`~/Development/github.com/bryanneva/ponko-planning/projects/active/consolidation.md`.

The runbook is phase-ordered and runnable end-to-end. Phases 0-2 have been
executed; Phases 3-5 capture the remaining cutover work; Phase 6 (this doc) is
maintained until the source repo is archived in M4.

---

## Conventions

- All `fly` commands use the explicit `--app <name>` flag rather than relying
  on `fly.toml`. The OSS template ships with `app = '<your-app>'` as a
  placeholder on purpose — do not commit Bryan's app name into the repo.
- Never commit secret values. Pull from Fly with `fly ssh console`, redirect
  to `mktemp`, and shred the temp file afterward (`rm -P` on macOS — there is
  no `shred` in BSD coreutils).
- `personal-agent-bn` = the live Fly app being replaced. `ponko-bn` = the
  replacement. `personal-agent-bn-db` / `ponko-bn-db` = their attached Fly
  Postgres clusters.

---

## Phase 0 — Repo prep

Pre-cutover code work landed on `main` before standing up the new Fly app.

| PR  | Branch                                  | Purpose                                    |
| --- | --------------------------------------- | ------------------------------------------ |
| #29 | `fix/29-cmd-slack-db-newpool`           | `cmd/slack` switched to `db.NewPool`       |
| #30 | `fix/30-river-joblist`                  | `cmd/cli` uses typed `river.JobList`       |
| #34 | `feat/openai-compat-provider`           | OpenAI/OpenRouter provider abstraction; bundles the Dockerfile `./cmd/server` → `./cmd/slack` fix originally proposed in #33 (now closed as superseded) |

All three squash-merged into `main`. Verify `main` is at or past commit
`002f949` before deploying.
`fly deploy` — without it, the image builds the wrong binary path.

### Local push gotcha (Node version)

The repo's pre-push hook runs the frontend build, which needs Node ≥ 20.19.
System Node on this Mac is 20.5.1, which Vite rejects. Workaround:

```bash
mise exec node@22.22.2 -- git push -u origin <branch>
```

Do **not** use `--no-verify` to bypass the hook — the embed step needs the
built `web/dist/` to exist.

---

## Phase 1 — Stand up `ponko-bn`

Create the app, attach Postgres, deploy, and tune the machine for always-on.

### 1.1 Create the app and database

```bash
fly apps create ponko-bn --org personal

fly postgres create \
  --name ponko-bn-db \
  --org personal \
  --region iad \
  --flex \
  --initial-cluster-size 1 \
  --vm-size shared-cpu-1x \
  --vm-memory 256 \
  --volume-size 1 \
  --autostart
```

### 1.2 Attach Postgres

Pass both `--database-name` and `--database-user` to skip the interactive
prompt:

```bash
fly postgres attach ponko-bn-db \
  --app ponko-bn \
  --database-name ponko \
  --database-user ponko
```

This sets `DATABASE_URL` on `ponko-bn` automatically — do not also set it
manually in Phase 2.

### 1.3 First deploy

```bash
fly deploy --app ponko-bn --remote-only
```

### 1.4 Force always-on (Slack bot needs it)

The OSS Dockerfile ships with `auto_stop_machines = 'stop'`, which idle-stops
machines. A Slack bot must always have a machine to receive events:

```bash
fly scale count 1 --app ponko-bn --yes

# Find the machine ID:
fly machines list --app ponko-bn

fly machine update <machine-id> \
  --app ponko-bn \
  --autostop=off \
  --autostart=true \
  --yes
```

---

## Phase 2 — Port secrets and `channel_configs`

### 2.1 Pull PA secrets via SSH

PA's secrets are write-only on Fly after `fly secrets set`, so the only way
to retrieve them is from inside a running PA machine:

```bash
umask 077
PA_ENV=$(mktemp -t pa-env.XXXXXX)
fly ssh console --app personal-agent-bn -C "env" > "$PA_ENV"
```

### 2.2 Filter to the keys that move to ponko

Eighteen environment variables transfer directly:

| Category | Keys |
| --- | --- |
| LLM | `ANTHROPIC_API_KEY` |
| MCP | `MCP_ACCESS_KEY`, `MCP_SERVER_URLS` |
| Linear MCP | `LINEAR_MCP_URL`, `LINEAR_MCP_ACCESS_TOKEN`, `LINEAR_MCP_TOKEN_URL`, `LINEAR_MCP_CLIENT_ID`, `LINEAR_MCP_CLIENT_SECRET`, `LINEAR_MCP_REFRESH_TOKEN` |
| GitHub MCP | `GITHUB_MCP_URL`, `GITHUB_PAT` |
| Web session | `COOKIE_SIGNING_KEY` |
| Misc runtime | `TIMEZONE`, `WORKER_CONCURRENCY` |
| OTel | `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_HEADERS`, `OTEL_EXPORTER_OTLP_PROTOCOL`, `OTEL_SERVICE_NAME` |

Filter the env dump down to those keys, then stage on the new app:

```bash
grep -E '^(ANTHROPIC_API_KEY|MCP_ACCESS_KEY|MCP_SERVER_URLS|LINEAR_MCP_URL|LINEAR_MCP_ACCESS_TOKEN|LINEAR_MCP_TOKEN_URL|LINEAR_MCP_CLIENT_ID|LINEAR_MCP_CLIENT_SECRET|LINEAR_MCP_REFRESH_TOKEN|GITHUB_MCP_URL|GITHUB_PAT|COOKIE_SIGNING_KEY|TIMEZONE|WORKER_CONCURRENCY|OTEL_EXPORTER_OTLP_ENDPOINT|OTEL_EXPORTER_OTLP_HEADERS|OTEL_EXPORTER_OTLP_PROTOCOL|OTEL_SERVICE_NAME)=' "$PA_ENV" \
  | fly secrets import --app ponko-bn --stage
```

`--stage` defers the machine restart until you redeploy at the end, so
secret-setting and DB import only trigger one bounce.

### 2.3 Keys deliberately NOT copied

| Key                       | Reason                                                              |
| ------------------------- | ------------------------------------------------------------------- |
| `SLACK_*`                 | We use the **Ponyo** Slack app's tokens. Paste manually from the Slack app dashboard. |
| `PERSONAL_AGENT_API_KEY`  | Replaced with a fresh `PONKO_API_KEY = $(openssl rand -hex 32)`     |
| `DATABASE_URL`            | Set automatically by `fly postgres attach`                          |
| `APP_BASE_URL`, `DASHBOARD_URL` | Point at PA's hostname; set ponko's separately if needed       |

### 2.4 Shred the env dump

```bash
rm -P "$PA_ENV"
```

### 2.5 Port `channel_configs`

Schema is byte-identical between PA and ponko —
`internal/db/migrations/00003_create_channel_configs.sql` and
`00004_add_respond_mode.sql` were copied directly from PA — so a CSV copy
works without transformation.

`pg_dump` and `psql -c "COPY ... TO STDOUT"` both fail mysteriously through
`fly ssh console`'s output capture. The pattern that works is **server-side
COPY-to-file, then SFTP fetch**:

```bash
# 1. Dump on PA's DB VM (OPERATOR_PASSWORD lives in the VM's env — read it,
#    do not commit it):
fly ssh console --app personal-agent-bn-db -C \
  "bash -c 'PGPASSWORD=\$OPERATOR_PASSWORD psql -h localhost -U postgres -d personal_agent_bn -c \"COPY channel_configs TO '\\''/tmp/channels.csv'\\'' WITH CSV HEADER\"'"

# 2. Fetch locally:
fly ssh sftp shell --app personal-agent-bn-db <<< "get /tmp/channels.csv /tmp/channels-pa.csv"

# 3. Upload to ponko's DB VM:
fly ssh sftp shell --app ponko-bn-db <<< "put /tmp/channels-pa.csv /tmp/channels.csv"

# 4. Import on ponko:
fly ssh console --app ponko-bn-db -C \
  "bash -c 'PGPASSWORD=\$OPERATOR_PASSWORD psql -h localhost -U postgres -d ponko -c \"COPY channel_configs FROM '\\''/tmp/channels.csv'\\'' WITH CSV HEADER\"'"

# 5. Cleanup on both VMs and locally:
fly ssh console --app personal-agent-bn-db -C "rm /tmp/channels.csv"
fly ssh console --app ponko-bn-db -C "rm /tmp/channels.csv"
rm /tmp/channels-pa.csv
```

To find `OPERATOR_PASSWORD` on either DB VM:

```bash
fly ssh console --app <db-app> -C "env" | grep PASSWORD
```

### 2.6 Redeploy to apply staged secrets

```bash
fly deploy --app ponko-bn --remote-only
```

---

## Phase 3 — Configure the Ponyo Slack app

In `api.slack.com → Your Apps → Ponyo`, set the following request URLs:

| Setting                | URL                                                       |
| ---------------------- | --------------------------------------------------------- |
| Event Subscriptions    | `https://ponko-bn.fly.dev/slack/events`                   |
| Interactivity          | `https://ponko-bn.fly.dev/slack/interactions`             |
| Slash Commands         | `https://ponko-bn.fly.dev/slack/commands`                 |
| OAuth redirect URI     | `https://ponko-bn.fly.dev/api/auth/slack/callback`        |

Bot event subscriptions (match PA exactly):

- `app_mention`
- `message.channels`
- `message.im`

Then:

1. Reinstall the Ponyo app to the workspace to pick up the new URLs.
2. Invite the Ponyo bot to a single test channel for shadow validation.

---

## Phase 4 — Shadow validation

Drive Ponyo (on `ponko-bn`) and the existing PA bot side-by-side in different
channels and compare behavior. Categories to exercise:

1. App-mention message in a public channel.
2. Direct message to the bot.
3. Slash command invocation.
4. Multi-turn conversation thread (replies in-thread).
5. Long-running tool use (Linear MCP / GitHub MCP / web fetch).

For each defect, file a GitHub issue in `bryanneva/ponko` labeled
`consolidation-m2`. Block the production cutover until all P0/P1 defects in
that label are resolved.

---

## Phase 5 — Production cutover

Once shadow validation is clean:

1. In `api.slack.com → Your Apps → Personal Agent` (the original PA Slack
   app), re-point all four request URLs from `personal-agent-bn.fly.dev` to
   `ponko-bn.fly.dev` (same paths as Phase 3).
2. Reinstall the PA Slack app to the workspace so the new URLs take effect.
3. Drain PA:
   ```bash
   fly scale count 0 --app personal-agent-bn
   ```
4. Watch ponko logs for 24 hours:
   ```bash
   fly logs --app ponko-bn --no-tail
   ```
   Repeat periodically; do not stream — `fly logs` without `--no-tail` hangs
   in non-interactive shells.

`personal-agent-bn` stays scaled to zero (not destroyed) for the duration of
M3 in case rollback is needed.

---

## Reverse path (rollback)

### Soft rollback — return traffic to PA

If ponko misbehaves after Phase 5:

1. Re-point the original PA Slack app's request URLs back to
   `https://personal-agent-bn.fly.dev/slack/{events,interactions,commands}`
   and `https://personal-agent-bn.fly.dev/api/auth/slack/callback`.
2. Scale PA back up:
   ```bash
   fly scale count 1 --app personal-agent-bn
   ```
3. Reinstall the PA Slack app to the workspace.
4. Validate in the test channel before announcing.

`ponko-bn` can stay running idle on Fly while you debug — it costs only the
single shared-cpu-1x machine.

### Hard teardown — delete `ponko-bn`

Only after confirming no important state has accumulated in `ponko-bn-db`
(check `channel_configs`, `river_job`, `workflows`):

```bash
fly apps destroy ponko-bn --yes
fly apps destroy ponko-bn-db --yes
```

This is destructive and irreversible — the volumes go with the apps.

---

## Verification

Run after each phase that touches `ponko-bn` or its DB.

### Health endpoint

```bash
curl -fsS https://ponko-bn.fly.dev/health
```

### Postgres row counts

```bash
fly ssh console --app ponko-bn-db -C \
  "bash -c 'PGPASSWORD=\$OPERATOR_PASSWORD psql -h localhost -U postgres -d ponko -c \"SELECT count(*) FROM channel_configs;\"'"
```

After Phase 2.5 the count should match what PA had before the export.

### Log tail

```bash
fly logs --app ponko-bn --no-tail | tail -100
```

Look for: successful Slack event handshakes, no panics, no `DATABASE_URL`
errors, River workers starting cleanly.

### API auth smoke test

```bash
curl -fsS -X POST https://ponko-bn.fly.dev/api/workflows/start \
  -H "Authorization: Bearer $PONKO_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"prompt":"runbook smoke test"}'
```

Expect a workflow ID in the response. Confirm it shows up in
`GET /api/workflows/{id}` with the same bearer token.

---

## When to remove this runbook

Keep this runbook in place until **M4 archives the `personal-agent` repo**.
Until then, the reverse path above is the documented rollback procedure and
must remain accessible. Once M4 is complete:

1. Confirm `personal-agent-bn` is destroyed (`fly apps list --org personal`
   should not show it).
2. Confirm the `personal-agent` repo is archived on GitHub.
3. Delete this file and any links to it from `docs/` indices.
