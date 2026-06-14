---
name: debug-sub2api-railway-timeouts
description: Use when debugging Sub2API production image/request failures involving Railway/Hikari 300s 502s, 499/client closed, context canceled, failed request body reads, OpenAI images generations/edits, base URL/Host attribution, bianxie upstream claims, or proving whether the user, Railway edge, Sub2API, or upstream disconnected.
---

# Debug Sub2API Railway Timeouts

## Overview

Build an evidence chain across Railway edge logs, Sub2API app logs, optional DB records, and upstream logs to identify which boundary disconnected first. Do not treat `context canceled` or Railway `499` alone as proof that the end user canceled.

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

4. Pull app logs for the local request id, Railway request id, or cancellation event:

```bash
railway logs --json --since 2h --filter 'openai.images.request_context_canceled' --lines 200
railway logs --json --since 2h --filter '<request_id> OR <railway_request_id>' --lines 100
```

5. Pull Railway edge logs for the same request id:

```bash
railway logs --http --json --since 2h --request-id '<railway_request_id>'
railway logs --http --json --since 8h --filter '@path:/v1/images/edits AND @totalDuration:299000..301000' --lines 200
```

6. Query DB records only when needed for aggregation or missing request ids. Inspect schema before assuming columns:

```bash
psql "$DATABASE_URL" -c "\dt"
psql "$DATABASE_URL" -c "\d+ usage_logs"
psql "$DATABASE_URL" -c "\d+ ops_error_logs"
```

7. Correlate by request ids, Railway ids, host, user/API key, model, account id, endpoint, and timestamp.
8. If upstream logs are provided, align by timestamp, source IP, path, and user-agent. Do not treat upstream `499` alone as proof the end user disconnected.

## Evidence Rules

Use these interpretations unless logs contradict them:

- `openai.images.request_context_canceled` with `termination_actor=railway_edge`, `termination_cause=edge_first_byte_timeout_300s`, and `termination_confidence=high`: classify as Railway/Hikari 300s first-byte timeout.
- `termination_cause=edge_body_read_timeout_300s` means Railway closed during request body upload/read, not while waiting for upstream response.
- If classification fields are absent, infer Railway 300s only when app logs show `phase=waiting_upstream`, `elapsed_ms≈300000`, `app_response_started=false`, `upstream_headers_received=false`, and a `railway_request_id`; confirm with Railway HTTP log `totalDuration≈300000`.
- `http.access status_code=502` is the app's intended/access status. It does not prove the final user received the app JSON response.
- Railway HTTP `httpStatus=499` means the edge saw its downstream side close before app response. It can still be caused by Railway edge timeout; do not label it "user canceled" without timing evidence.
- Client-visible `502 Bad Gateway` with `server: railway-hikari` and body like `upstream error` is edge-generated, not Sub2API application JSON.
- Client sees `502 Bad Gateway` with `server: railway-hikari` and app records `Failed to read request body` near `300000ms`: Railway edge closed during body read before the app could respond.
- Upstream Nginx `499` means its client closed the upstream connection. In this architecture that client is usually Sub2API, so prove why Sub2API closed it by checking local request context cancellation timing.
- Upstream disconnect is more likely when app logs show upstream EOF/timeout/status before `requestCtx.Err()` and before the 300s Railway boundary.
- User/client disconnect is more likely when Railway/app cancellation occurs well before 300s, durations vary by client/user-agent, and there is no 300s edge signature.

## Decision Table

| App log | Railway HTTP log | Classification |
|---|---|---|
| `termination_actor=railway_edge`, `edge_first_byte_timeout_300s`, `confidence=high` | optional confirmation | Railway edge 300s first-byte timeout |
| `termination_actor=railway_edge`, `edge_body_read_timeout_300s`, `confidence=high` | optional confirmation | Railway edge 300s body-read timeout |
| `phase=waiting_upstream`, `elapsed≈300s`, `app_response_started=false`, `upstream_headers_received=false` | `totalDuration≈300s`, `txBytes=0`, often `499/client has closed` | Railway edge 300s first-byte timeout |
| `phase=reading_body`, body not fully read, `elapsed≈300s` | `totalDuration≈300s` | Railway/body-read 300s boundary |
| `elapsed` well below 300s, no response started | Railway duration matches variable client timeout | downstream/client or intermediary closed early; do not over-claim final user |
| `upstream_headers_received=true`, upstream status/body exists | Railway duration matches app response | upstream returned an HTTP error |
| `app_response_started=true`, write/flush error | Railway txBytes may be non-zero | downstream closed during response write |

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
- `openai.images.request_context_canceled`: `phase`, `elapsed_ms`, `context_error`, `method`, `path`, `protocol`, `client_ip`, `content_length`, `host`, `user_agent`, `model`, `account_id`, `platform`, `stream`, `app_response_started`, `app_response_bytes_written`, `upstream_started`, `upstream_headers_received`, `upstream_latency_ms`, `termination_actor`, `termination_cause`, `termination_confidence`, `termination_evidence`

Use `termination_*` for the first-pass conclusion, then verify with Railway HTTP logs when the conclusion affects customer-facing attribution.

Use them to prove base URL attribution and cancellation phase without logging auth headers or API keys.
