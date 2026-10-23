# Worker

The worker turns accepted reports into issues. The ingestion API stores each
report and announces it on NATS JetStream (`reports.accepted` in the
`REPORTS` stream); the worker reads those messages, opens the issues and only
then acknowledges them. The code lives in `internal/worker`.

```
POST /reports ──▶ Postgres (reports) ──▶ JetStream REPORTS ──▶ worker ──▶ issue.Service ──▶ GitHub
                                          reports.accepted       │
                                                                 └──▶ report_deliveries, reports.processed_at
```

It runs inside the backend process by default. Several processes can run it
at once: they share one durable consumer, `report-worker`, and JetStream
hands each message to one of them.

## What it does with a report

1. Loads the report by the ID in the message. A report that no longer exists
   (deleted with its app, for example) is dropped.
2. Skips it if `processed_at` is already set.
3. Routes it (see [routing.md](routing.md)): for every project the app is
   linked to, the first routing rule that matches picks the repository,
   labels, assignees and milestone; with no match, the project's default
   repository. An app with no target is dropped with a warning.
4. Renders the issue from the report (`render.go`): the reporter's title (or
   one made from the crash or the platform and release), description,
   category and severity, the breadcrumbs as numbered steps to reproduce,
   the crash and its stack trace, the environment, recent logs and network
   requests, and the server logs around the report when the app's
   coordinator is attached. Network request bodies are never included, and an `@` in
   reporter-written text can't notify anyone on GitHub.
5. Opens the issue in each project that doesn't have it yet, through
   `issue.Service.OpenInRepository(ctx, appID, repositoryID, draft)`, or
   adds the report to the open issue of the same bug (see below), and
   records each one in `report_deliveries`. Each issue goes through the
   reporting **app's own connection** for that repository's tracker (see
   [issue-service.md](issue-service.md#which-connection-opens-it)).
6. Sets `processed_at` once every target has its issue, then acknowledges
   the message.

The issue format is provisional: the report schema and the issue template
will be settled once enrichment (crash, server context) is in. Everything
about the format is in `render.go`.

## One issue per bug

A report with a crash gets a fingerprint (`internal/dedup`): its platform,
the crash type and the top 5 frames of the stack trace, without line
numbers, memory addresses and offsets, hashed. The app version is not part
of it, so one issue covers every version a bug shows up in. A report
without a crash has no fingerprint and always opens its own issue: what a
reporter writes is too loose to group on without merging different bugs.

For each route, when the repository has an open issue with the same
fingerprint (`issues.fingerprint`, set when m-Seismo opens an issue):

1. the worker counts the reports grouped under it, this one included: how
   many, from how many installations, and in which versions;
2. it checks on the tracker that the issue is still open, then adds a
   comment with the counts and what is new in this report: the reporter's
   description, the crash reason, the steps, the environment and the
   server logs (`render_occurrence.go`). The stack trace is the issue's,
   so it is not repeated;
3. the delivery is recorded against that issue.

An issue closed on the tracker since is recorded as closed and the report
opens a new issue. Telling that a bug came back (a regression) is not done
yet.

Two reports of a new bug can come in together, on two workers. So the
worker takes a Postgres advisory lock on the repository and fingerprint
while it looks for the issue and opens it: the second report waits, then
finds the issue the first one opened. The lock holds one database
connection per report in progress, within `database.max_conns`.

```
crash report ──▶ fingerprint ──▶ lock (repository, fingerprint)
                                   │
                    open issue with that fingerprint?
                     ├─ no ─────────────────────────▶ open a new issue
                     └─ yes ─▶ still open on the tracker?
                                ├─ yes ─▶ comment: "Seen again: 12 reports,
                                │         5 installations · versions 2.4.0, 2.5.0"
                                └─ no ──▶ record it closed, open a new issue
```

## Never lost, never doubled

JetStream delivers a message **at least once**. A worker that stops before
acknowledging, or doesn't answer within the ack wait, gets the message
redelivered, possibly to another worker. So processing is idempotent:

- each project that got the issue has a `report_deliveries` row and is
  skipped on the next attempt, so a report opens at most one issue per
  project, even if a routing rule changed in between;
- when some targets fail, the others are recorded and the next attempt only
  retries the failed ones;
- a processed report is left alone.

One gap remains: if the issue is opened but recording the delivery fails
(the database goes away at that moment), the next attempt opens it again.
The worker logs it as an error.

While working on a message the worker tells JetStream every 20 seconds that
it is still on it, so a slow tracker doesn't cause a redelivery; only a
worker that stopped loses its messages to another one.

## Failures and retries

| Outcome | What the worker does |
|---|---|
| Done | acknowledges |
| Permanent: report gone, unreadable, app with no target | logs a warning and terminates the message |
| Tracker rate limit (`issue.RateLimitError`) | hands the message back to come again after the tracker's wait |
| Other error (tracker down, network, database) | hands it back after 5 s, 30 s, 2 min, 10 min, then every 30 min |
| The app has no connection for a target (`issue.ErrNoConnection`) | treated like any other error: retried on the same schedule, so the issue still opens if the owner connects the app in time, then terminated at `worker.max_attempts` |
| `worker.max_attempts` reached | logs an error and terminates the message; the report keeps `processed_at` empty |
| Shutdown | waits for the reports in progress; past the shutdown timeout their work is cancelled and their messages come back at once |

A terminated report can be found with `processed_at IS NULL`. Retrying it
from the admin console comes with failed deliveries.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `SEISMO_WORKER_ENABLED` | `true` | Run the worker in this process. Set `false` to run the API alone and the workers elsewhere. |
| `SEISMO_WORKER_CONCURRENCY` | `4` | Reports one process works on at a time. Also caps the messages it holds. |
| `SEISMO_WORKER_MAX_ATTEMPTS` | `10` | Attempts before giving up on a report. Waiting out a rate limit counts as one. |

## Data

`report_deliveries` has one row per report and repository it was delivered
to:

| Column | Meaning |
|---|---|
| `report_id` | the report (`reports.id`); cascade on delete |
| `project_repository_id` | the repository the issue went to; cascade on delete |
| `issue_id` | the issue row, or null if the issue was opened but could not be recorded; set null when the issue row is deleted |
| `url` | the issue's URL on the tracker |
| `created_at` | when it was delivered |

`reports.processed_at` is set when every target has its issue.

## Not done yet

- **Regressions**: a closed issue's bug coming back opens a new issue
  without saying it is back.
- **Counts in the issue itself**: they are in each comment, as the
  trackers' issue bodies are not updated.
- **Crash enrichment**: no Crashlytics crash yet. Server logs come from the
  app's coordinator when one is attached (see [coordinator.md](coordinator.md)).
- **Attachments**: the issue says a screenshot was sent but doesn't link it;
  that needs private, expiring links.
- **Sweeper**: a report stored but never published (`queued_at IS NULL`,
  when NATS was down and the SDK never retried) is not picked up.

## Tests

`go test ./internal/worker/` covers rendering, idempotent processing with
fakes, and how each outcome settles the message. The end-to-end flow (report
in, issue on the fake GitHub, resend, partial failure, worker stopped while
reports arrive) was run against the dev stack.
