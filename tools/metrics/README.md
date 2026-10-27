# Webnote metrics

Log-based counters in Cloud Monitoring, built from the App Engine request
log and the app's own log lines (`stdout`), and a dashboard that shows them.

| Metric (`logging.googleapis.com/user/...`) | Counts | Labels |
|---|---|---|
| `webnote_page_loads` | workspace pages served (GET 200) | none |
| `webnote_saves` | POSTs to `save.py` | `status` (200, 409, 500) |
| `webnote_failed_saves` | "Failed to save workspace" log lines | none |
| `webnote_5xx` | 5xx responses | `status` |
| `webnote_rss` | feed requests (`NAME.xml`, `rss.py`) | `status` (200, 304, 404) |
| `webnote_js_errors` | reports to `/webnote/jserror` | `version` (of `webnote.js`) |

Create or update them all, and the "Webnote" dashboard, with
`tools/metrics/apply.sh`. Counting starts when a metric is created; older
logs aren't counted.

There's no per-workspace label: each label value is a separate time series,
and workspace names are private. Count active workspaces on demand instead
(below).

## Cost

Checked 2026-10-26 (https://cloud.google.com/stackdriver/pricing):
user-defined log-based counters are chargeable Monitoring metrics, at 8
bytes per data point, and the first 150 MiB per billing account each month
is free. A time series gets at most one point a minute (43,200 a month, about
0.33 MiB). These metrics make roughly 10 series at a time (saves by status,
5xx by status, a few client versions), so a few MiB a month: free.
Error Reporting has no charge, and Log Analytics queries have no charge.

## Active workspaces (Log Analytics)

Log Analytics is free, but upgrading the `_Default` log bucket can't be
undone:

    gcloud logging buckets update _Default --location=global --enable-analytics --project webnote-184120

Then, in Logging → Log Analytics, count the workspaces whose page was opened
in the last 7 days by something other than a crawler. It prints only a
count; don't select the names. Saves can't be counted this way: the name is
in the POST body, which isn't logged.

```sql
SELECT COUNT(DISTINCT REGEXP_EXTRACT(proto_payload.request_log.resource, r'^/webnote/([^?]+)'))
  AS active_workspaces
FROM `webnote-184120.global._Default._AllLogs`
WHERE log_id = 'appengine.googleapis.com/request_log'
  AND timestamp > TIMESTAMP_SUB(CURRENT_TIMESTAMP(), INTERVAL 7 DAY)
  AND proto_payload.request_log.method = 'GET'
  AND proto_payload.request_log.status = 200
  AND REGEXP_CONTAINS(proto_payload.request_log.resource, r'^/webnote/[^?]+')
  AND NOT REGEXP_CONTAINS(proto_payload.request_log.resource,
    r'^/webnote/([^?]*\.(xml|py|js|css|ico|png|gif|svg|html|txt|webmanifest)(\?|$)|(images|icons)/)')
  AND NOT REGEXP_CONTAINS(proto_payload.request_log.user_agent, r'(?i)bot|spider|crawl')
  AND NOT STARTS_WITH(REGEXP_EXTRACT(proto_payload.request_log.resource, r'^/webnote/([^?]+)'),
    'webnote_smoketest')
```
