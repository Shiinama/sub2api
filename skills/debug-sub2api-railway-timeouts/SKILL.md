---
name: debug-sub2api-railway-timeouts
description: Use when debugging Sub2API production image/request failures involving Railway/Hikari 300s 502s, 499/client closed, context canceled, failed request body reads, OpenAI images generations/edits, base URL/Host attribution, bianxie upstream claims, or proving whether the user, Railway edge, Sub2API, or upstream disconnected.
---

# Debug Sub2API Railway Timeouts

## Overview

Build an evidence chain across Railway edge, Sub2API logs, DB records, and upstream logs to identify where a request disconnected.

## Safety

- Never print secrets, API keys, bearer tokens, DB URLs, or exported account credentials.
- Prefer env vars for `DATABASE_URL`, admin keys, and Railway credentials.
- Treat `base URL` as the inbound host/domain, not the application endpoint path.
- Normalize timestamps before comparing Railway, DB, app, and upstream logs.

## Workflow

1. Establish scope: user/API key, time window, timezone, endpoint family, upstream account, and whether the question is about inbound base URL or API path.
2. Confirm deploy context with `git rev-parse HEAD`, `railway status`, and `railway deployment list --limit 5 --json`.
3. Pull Railway evidence:

```bash
railway logs --http --since 6h --status ">=400" --json
railway logs --latest --build --lines 200
railway logs --latest --deployment --lines 200
```

4. Query DB/app records. Inspect schema before assuming columns:

```bash
psql "$DATABASE_URL" -c "\dt"
psql "$DATABASE_URL" -c "\d+ usage_logs"
psql "$DATABASE_URL" -c "\d+ ops_error_logs"
```

5. Correlate by request ids, Railway ids, host, user/API key, model, account id, endpoint, and timestamp.
6. If upstream logs are provided, align by timestamp, source IP, path, and user-agent. Do not treat upstream `499` alone as proof the end user disconnected.

## Evidence Rules

Use these interpretations unless logs contradict them:

- Client sees `502 Bad Gateway` with `server: railway-hikari` and app records `Failed to read request body` near `300000ms`: Railway edge closed before the app responded.
- `openai.images.request_context_canceled` with `phase=waiting_upstream` near 300s: downstream context was canceled while Sub2API waited on upstream.
- Upstream Nginx `499` means its client closed the upstream connection. In this architecture that client is usually Sub2API, so prove why Sub2API closed it by checking local request context cancellation timing.
- Upstream disconnect is more likely when app logs show upstream EOF/timeout/status before `requestCtx.Err()` and before the 300s Railway boundary.
- User/client disconnect is more likely when Railway/app cancellation occurs well before 300s and there is no Railway/Hikari 300s edge signature.

## Required Report Shape

Include:

- inbound base URL/host distribution, separate from endpoint path
- hourly error rate for the scoped user/account/window
- error counts by type, not split by endpoint unless requested
- duration distribution for failed requests, using explicit buckets:
  `<10s`, `10-30s`, `30-60s`, `60-120s`, `120-240s`, `240-270s`, `270-305s`, `305-360s`, `>360s`
- representative request IDs for each major class
- conclusion stating which boundary disconnected and what evidence supports it

## Current Log Fields

Expected fields:

- `http.access`: `host`, `forwarded_host`, `forwarded_proto`, `railway_request_id`, `railway_edge_request_id`
- `openai.images.request_context_canceled`: `phase`, `elapsed_ms`, `context_error`, `method`, `path`, `protocol`, `client_ip`, `content_length`, `host`, `user_agent`, `model`, `account_id`, `platform`, `stream`

Use them to prove base URL attribution and cancellation phase without logging auth headers or API keys.
