# Product Requirements Document — m-Seismo v1

|                   |                                                        |
| ----------------- | ------------------------------------------------------ |
| **Status**        | Draft, under product review                            |
| **Version**       | 1.1                                                    |
| **Last updated**  | 2026-10-16                                             |
| **Product owner** | @wail                                                  |
| **Organisation**  | MicroClub USTHB                                        |
| **Source**        | m-Seismo cahier des charges (2026-10-14)               |
| **Platforms**     | iOS and Android apps; Flutter and React Native apps    |

### Revision history

| Version | Change                                                                                                                                                                       |
| ------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1.0     | First full product scope, derived from the cahier des charges. Technical architecture removed; only the features and the behaviour they must have are kept. |
| 1.1     | Amendments from building the iOS SDK's first release (FR-03, FR-08, FR-11, FR-20, FR-21, NFR-09): see the implementation notes under each affected requirement, added per the change-control process in section 22. |

### How to read this document

This document defines **what** m-Seismo must do and **for whom**. It does not
define how to build it: languages, architecture, storage, protocols and
infrastructure belong in the engineering documentation, not here.

Requirements are identified (`FR-nn`, `NFR-nn`) and written to be testable.
Each requirement names its release target: **v1** (must ship), **v1.x** (should
ship soon after v1) or **v2** (could ship later). Where a requirement comes
from the cahier des charges, its original ID is given in brackets, for example
`[SDK-02]`, so the two documents stay traceable.

Anything not in this document is out of scope until it is added here.

---

## 1. Overview

### 1.1 Problem

Bug reports from our mobile apps arrive incomplete: a screenshot, a one-line
description, and nothing else. A developer then has to chase the reporter, dig
through crash reports, search server logs and guess the device state and the
steps that led to the bug. Most of the time spent on a bug goes into collecting
context, not fixing it, and some bugs are never fixed because the context is
never found.

### 1.2 What this product is

m-Seismo is an open-source, self-hostable **bug-reporting tool** for our mobile
apps:

- a tester or user shakes the phone, as on Instagram, and a report starts;
- the app collects the full context of the moment by itself: device, app
  state, recent logs, the user's last actions, a screenshot;
- crashes are reported automatically, with no one having to shake anything;
- the report is linked to what happened on our servers at the same moment and
  to the matching crash data;
- a complete GitHub issue is opened, labelled, routed to the right repository
  and team, and never duplicated for the same bug.

### 1.3 What this product is not

It is **not** a crash reporter that replaces Crashlytics: it works alongside it
and reuses its data. It is **not** an analytics, monitoring or customer-support
product. It does **not** identify the people who report bugs: reporters are
anonymous. It is **not** a public SaaS: each team runs its own copy.

### 1.4 Goals

| Goal                                                                   | Measured by                                              | Target            |
| ---------------------------------------------------------------------- | -------------------------------------------------------- | ----------------- |
| A bug becomes a complete GitHub issue almost instantly                 | Time from trigger to issue created, on a normal connection | under 10 seconds  |
| Developers never chase reporters for missing context                   | Reports needing manual follow-up for missing context     | under 10%         |
| One bug, one issue                                                     | Duplicate issues created for the same root cause         | under 5%          |
| Adding m-Seismo never hurts the app                                    | Crashes caused by m-Seismo                               | 0                 |
| Adding m-Seismo barely changes the app's size                          | Size added to the app, per platform                      | under 300 KB      |

---

## 2. Users and roles

| Role                | Who                                                    | Core job                                                                       |
| ------------------- | ------------------------------------------------------ | ------------------------------------------------------------------------------ |
| **Reporter**        | A tester, QA member or end user of one of our apps     | Notices a bug and reports it in a few seconds without leaving the app          |
| **Developer**       | Member of the team that owns the app or the backend    | Receives the GitHub issue and fixes the bug without asking for more context    |
| **App integrator**  | Developer who adds m-Seismo to a mobile app            | Connects the app to m-Seismo and decides which triggers and captures are on    |
| **Project admin**   | Team lead or maintainer                                | Manages projects, keys, routing rules and retention; follows failed deliveries |
| **Operator**        | Person who runs m-Seismo and our servers               | Hosts m-Seismo and lets it collect server-side context next to our services    |

### 2.1 Reporter

A **reporter is anonymous**. m-Seismo never asks for or collects a name, an
email address, an account or any other personal identifier. A reporter is known
only by a random identifier tied to one installation of the app. Reporters have
no account, no login and no dashboard.

---

## 3. Scope

### 3.1 In scope (v1)

Shake-to-report and the other triggers · the in-app report sheet · automatic
capture of device, app and user-action context · screenshot with masking tools ·
automatic crash and error reporting alongside Crashlytics · reliable delivery,
including offline · server-side logs and diagnostics collected for each report ·
crash correlation with Crashlytics · automatic GitHub issue creation with full
context · deduplication and grouping · per-project routing · privacy controls,
redaction and deletion · abuse protection · iOS, Android, Flutter and React
Native apps with identical behaviour.

### 3.2 Planned later

- **v1.x** — admin console, outgoing alerts to Slack and Teams, auto-triage,
  regression detection, network inspector, local debug bundle, spike alerts,
  readable crash traces for reports that do not come from Crashlytics, links to
  observability tools, release correlation, more server log sources.
- **v2** — reporter notification when a bug is fixed, lightweight session
  replay, short screen recording, web and command-line reporting, trackers
  other than GitHub (Jira, Linear, GitLab), reporting dashboard, performance
  snapshots, reports opened automatically by the server side.

### 3.3 Out of scope

Explicitly excluded from v1:

- reporting from web apps (planned v2);
- full video session replay;
- replacing Crashlytics as the crash reporter;
- issue trackers other than GitHub (planned v2);
- a public, hosted SaaS offering;
- customer-facing support chat or any reporter–developer messaging;
- reporter accounts, names, emails or any personal identity;
- continuous streaming of server logs: server context is collected only around
  an incident.

### 3.4 Assumptions and constraints

- Our apps already use Crashlytics. Crash correlation (section 11) depends on
  the Crashlytics data export being turned on for each target Firebase project,
  which is **not the case today** and requires the paid Firebase plan. Turning
  it on, and documenting how, is part of v1 (FR-44).
- Issues are filed on GitHub. m-Seismo is installed on the GitHub organisation
  with the smallest set of permissions it needs: write issues, read metadata.
- Where m-Seismo is hosted, where attachments are stored and which repository
  receives which issues are settings chosen per deployment and per project,
  never fixed in the product.
- iOS and Android come first, so the full loop (shake, capture, issue) is proven
  on native apps before Flutter and React Native support is built on top of it.
- Team sizes are not fixed yet, so the roadmap has phases and gates but no
  dates.

---

## 4. Project setup

A **project** is one app (or one app family) reporting into m-Seismo.

**FR-01** — A project admin creates a project and receives a project key. The
key can only send reports: it can never be used to read reports or any other
data. `[SDK-01]` — v1

**FR-02** — An app integrator connects an app to a project by giving it the
project's address and key, the environment (for example `production`,
`staging`) and the release (app version and build). Every report carries this
information. `[SDK-01]` — v1

**FR-03** — The app can run in one of three **modes**: `internal`, `QA` or
`production`. The mode decides which triggers are on, how the report sheet
looks and which fields are required. For example, `QA` can require a
description and severity, while `production` can disable shake entirely.
`[FE-04]` — v1

> **iOS v1 implementation note (2026-10-16):** the mode system is not built
> yet. In its place, the iOS SDK's `Configuration` exposes flat per-trigger
> switches (`shakeEnabled`, default on; `screenshotEnabled`, default off)
> set once at `configure()`, with no per-mode variation and no required-field
> logic. `internal`/`QA`/`production` modes remain planned, not yet
> implemented.

**FR-04** — A project admin can create, rotate and revoke project keys. A
revoked key stops being accepted immediately. `[BE-09]` — v1

---

## 5. Triggering a report

**FR-05** — **Shake to report.** Shaking the phone opens a report. The app
integrator can set the shake sensitivity, a cooldown between two triggers, and
turn shake on or off per mode. `[SDK-02]` — v1

**FR-06** — An accidental shake must not file a report on its own: shake always
opens the report sheet first (unless auto-submit is on, FR-12), and the cooldown
prevents a burst of reports from one shake. Shake must not break the system's
own shake gestures, such as undo on iOS. `[SDK-02]` — v1

**FR-07** — **Other triggers.** A report can also start when the user takes a
screenshot, when the user long-presses with a configurable gesture, or when the
app calls the report action itself (for example from a "Report a bug" button).
Each trigger can be turned on or off per mode. `[SDK-03]` — v1

**FR-08** — **Automatic reports.** An uncaught error or a crash files a report
automatically, without any action from the user. A crash is reported on the
next launch if it could not be sent at the time. `[SDK-08]` — v1

**FR-09** — Non-fatal errors that the app chooses to record are reported the
same way as crashes, flagged as non-fatal. `[SDK-08]` — v1

> **iOS v1 implementation note (2026-10-16):** FR-08's automatic capture
> currently covers uncaught Objective-C/Swift-bridged exceptions
> (`NSException`) and errors the app explicitly records via `recordError(_:)`
> (FR-09) — both reported on the next launch, as specified. It does **not**
> yet catch native/signal-level crashes (`fatalError()`, force-unwraps,
> array-index traps, `SIGSEGV`-class failures), which cover most of what
> actually crashes a Swift app day to day. Catching those safely needs an
> async-signal-safe handler (no memory allocation at crash time); getting
> that wrong risks violating NFR-02 ("must never crash the app"), so it was
> deliberately left out of this release. Crashlytics remains the only source
> of those crash traces until a dedicated native crash handler is built.

---

## 6. Report sheet

**FR-10** — The report sheet lets the reporter enter a title, a description, a
category and a severity, and shows the screenshot of the screen they were on.
`[SDK-04]` — v1

**FR-11** — The reporter can draw on the screenshot to point at the bug, and
blur or black out any part of it before sending. `[SDK-04, SDK-06]` — v1

> **iOS v1 implementation note (2026-10-16):** masking is implemented as a
> freehand solid-black stroke (draw over the area to hide it), not a true
> Gaussian blur. A blackout fully hides the content underneath, same
> practical effect as a blur, and was far simpler to implement correctly; a
> real blur region was not built for this release.

**FR-12** — Every field is optional by default. The sheet can be skipped
entirely: in **auto-submit** mode the report is sent as soon as it is triggered,
with whatever context was captured. Which fields are required depends on the
mode (FR-03). `[SDK-04]` — v1

> **iOS v1 implementation note (2026-10-16):** every field being optional is
> implemented. Auto-submit (skipping the sheet entirely) is not: every
> manual trigger (shake, screenshot, long-press, the manual call) currently
> always opens the report sheet. Automatic crash/error reports (FR-08/FR-09)
> are the one exception already in place — those are sent with no sheet
> shown, which is auto-submit behavior for that one path.

**FR-13** — The report sheet must never block the app: the reporter can
dismiss it at any time and return exactly where they were. `[SDK-04]` — v1

**FR-14** — Before the first report from an installation, the reporter is asked
for consent to send diagnostic data. Without consent, nothing is sent. Consent
can be required or skipped per mode (internal testers may consent once for the
team build). `[FE-05]` — v1

---

## 7. Automatic context

Everything in this section is captured with no action from the reporter.

**FR-15** — **Device and app.** Each report includes the operating system and
its version, the device model, language and region, battery level, available
memory, network type, screen orientation, and the app version and build.
`[SDK-05]` — v1

**FR-16** — **Feature flags and experiments.** Each report includes the feature
flags active at that moment and the experiment variants the user is in.
`[SDK-05, FE-03]` — v1

**FR-17** — **Recent logs.** Each report includes the app's most recent log
lines, limited to the last N lines or the last N seconds, as configured by the
app integrator. `[SDK-07]` — v1

**FR-18** — **User's last actions.** Each report includes the user's recent
actions: screens visited, taps, network calls made, and app lifecycle events
(opened, backgrounded, resumed). `[SDK-07]` — v1

**FR-19** — **Screenshot.** A screenshot of the current screen is taken at the
moment of the trigger, before the report sheet appears. `[SDK-06]` — v1

**FR-20** — **Crash details.** A crash report includes the full crash trace and
the state of the app at the time of the crash. `[SDK-08]` — v1 — see the
implementation note under FR-08/FR-09: this currently applies to uncaught
exceptions and recorded errors, not native/signal-level crashes.

**FR-21** — **Works with Crashlytics.** m-Seismo's crash handling runs alongside
Crashlytics and never replaces it: every crash still reaches Crashlytics
exactly as before. Each report carries the Crashlytics installation and user
identifiers and custom keys, so the two can be linked. `[SDK-08, SDK-11]` — v1

> **iOS v1 implementation note / conflict (2026-10-16):** decided during the
> iOS SDK build that the SDK will **not** integrate with the Crashlytics SDK
> on-device at all — it never reads Crashlytics' installation ID, user ID or
> custom keys, and does not attach them to the report. The first sentence of
> FR-21 (crash handling runs alongside Crashlytics, never replaces it) still
> holds. The second sentence (identifiers attached to the report) does not:
> that linking is intended to happen entirely on the backend instead, via the
> Crashlytics Data Export API, matching on installation ID and time proximity
> (FR-43/FR-44) — not by embedding Crashlytics' own identifiers into the
> report at capture time. SDK-11 ("attach Crashlytics user/installation ID
> and custom keys to the report") is accordingly out of scope for the SDK.

**FR-22** — **Session link.** Every report carries a session reference that is
also attached to every request the app makes to our servers, so a report can be
matched with exactly what our servers did for that user at that moment.
`[SDK-09]` — v1

**FR-23** — **Network requests.** A report can include the app's recent network
requests: address, method, result and duration. Request and response contents
are excluded unless the app integrator turns them on, and even then are
redacted (section 8). `[SDK-13, FE-08]` — v1.x

---

## 8. Privacy and reporter identity

**FR-24** — **Anonymous reporter.** A reporter is identified only by a random
identifier created on first use for that installation. No account, name, email
or other personal identifier is collected or required. `[SDK-16]` — v1

**FR-25** — **Redaction on the device.** Before anything leaves the phone,
personal data is removed from logs, user actions and network data, using
pattern rules (emails, phone numbers, card numbers, tokens) plus allow and deny
lists set by the app integrator. `[SDK-12]` — v1

**FR-26** — **Masked views.** The app integrator can tag any screen element as
sensitive (password field, card number, private message). Tagged elements are
always hidden in screenshots, whatever the reporter does. `[FE-05]` — v1

**FR-27** — **Per-field masking.** The app integrator can mask any individual
captured field (for example, hide the device locale or the battery level) for a
project. `[FE-05]` — v1

**FR-28** — **Default deny.** By default, m-Seismo captures no request or
response contents, no headers and no form values. Each must be turned on
explicitly. `[NFR Privacy]` — v1

**FR-29** — **Redaction on the server side.** Server logs and diagnostics added
to a report are stripped of secrets and personal data (tokens, emails, card
numbers, authorisation information) before they leave the server they come
from. `[AG-05]` — v1

**FR-30** — **Deletion.** A project admin can delete a single report, or every
report coming from one installation identifier. Deletion removes the report's
attachments too. `[BE-10, FE-05]` — v1

**FR-31** — **Retention.** A project admin sets how long reports and
attachments are kept. Older data is deleted automatically. `[BE-10]` — v1

---

## 9. Delivery

**FR-32** — **Instant acknowledgement.** The reporter sees that the report was
received within half a second of sending it; the rest of the work (collecting
server context, opening the issue) happens afterwards, without the reporter
waiting. `[BE-02]` — v1

**FR-33** — **Offline.** A report made without a connection, or that fails to
send, is kept on the phone and sent automatically when the connection returns,
retrying at growing intervals. A report still unsent when the app closes is
sent on the next launch. `[SDK-10]` — v1

**FR-34** — **Never lost, never doubled.** A report is delivered at least once,
and a report delivered twice (after a retry, for example) still produces only
one report and one issue. `[NFR Reliability]` — v1

**FR-35** — **Abuse protection.** m-Seismo limits how many reports a single
project key and a single installation can send in a period. Reports above the
limit are refused without affecting other projects or other users.
`[BE-08]` — v1

---

## 10. Server-side context

m-Seismo collects, next to our backend services, the server-side half of each
report. It collects only around an incident, never continuously.

**FR-36** — **Logs for the report.** For each report, m-Seismo retrieves the
server log lines that match the report's session reference (FR-22), or that
fall in the time window around the report, from every service involved.
`[AG-01, AG-02]` — v1

**FR-37** — **Log sources.** Server logs can come from log files, the system
journal, containers and Kubernetes pods. Recent logs are kept for a bounded
period on each server so they can be retrieved when a report arrives.
`[AG-01]` — v1

**FR-38** — **Speed.** Matching server logs are returned within 2 seconds of the
request, already redacted (FR-29). `[AG-02]` — v1

**FR-39** — **Service health at that moment.** For each report, m-Seismo adds a
snapshot of the services involved: recent error rate, response time, restarts,
resource usage and the version currently deployed. `[AG-03]` — v1

**FR-40** — **Light on our servers.** Server-side collection must stay
invisible to the services it runs next to: when idle it uses under 50 MB of
memory and under 2% of CPU on a standard server. `[AG-06]` — v1

**FR-41** — **No open doors.** Server-side collection only ever connects out to
m-Seismo; nothing needs to connect into our servers to reach it. `[AG-04]` — v1

**FR-42** — **More log sources.** Server context can also be pulled from
Prometheus, Loki and CloudWatch Logs. `[AG-07]` — v1.x

---

## 11. Crash correlation

**FR-43** — **Link to Crashlytics.** When a report matches a Crashlytics crash
(same installation, close in time), the issue links to that crash and includes
its crash trace, its breadcrumbs and its custom keys. `[INT-02]` — v1

**FR-44** — **Crashlytics data available.** The Crashlytics data export is
turned on for every target Firebase project, the steps are documented for the
team, and m-Seismo checks when it starts that it can read that data, warning the
project admin if it cannot. `[INT-05]` — v1

**FR-45** — **Readable crash traces without Crashlytics.** For crashes that did
not go through Crashlytics, crash traces are translated back into readable file
names and line numbers on both iOS and Android. `[BE-12]` — v1.x

---

## 12. GitHub issue

**FR-46** — **Issue opened automatically.** Every accepted report opens a
GitHub issue with no human involved, unless it is a duplicate (section 13).
`[BE-04]` — v1

**FR-47** — **Issue contents.** The issue contains:

- the reporter's title and description, or a generated title for automatic
  reports;
- category and severity;
- **steps to reproduce**, written as a numbered list from the user's last
  actions (FR-18) `[FE-02]`;
- device, app, release, environment, feature flags and experiment variants;
- the annotated screenshot;
- recent app logs and network requests;
- the matching server logs and service health snapshot (section 10);
- the linked Crashlytics crash, when there is one (section 11);
- the anonymous installation identifier.

Large items (screenshots, logs) are attached as links rather than pasted in
full. `[BE-04, BE-05]` — v1

**FR-48** — **Private attachments.** Attachment links are private and expire.
Anyone without access to the issue cannot open them. `[NFR Security]` — v1

**FR-49** — **Labels, assignees, milestone.** The issue gets labels, assignees
and a milestone according to the project's routing rules (section 14).
`[BE-04]` — v1

**FR-50** — **Issue templates.** Each project can choose the issue template
used for its issues, per app, severity or category. `[BE-07, INT-01]` — v1

**FR-51** — **Two-way status.** When the GitHub issue is closed, the report is
marked resolved in m-Seismo. `[INT-01]` — v1

**FR-52** — **Observability links.** The issue includes direct links to the
same session in Grafana, Loki, CloudWatch or Datadog, when the project uses
them. `[INT-03]` — v1.x

**FR-53** — **Release correlation.** The issue links to the GitHub release and
the range of commits that shipped the affected build. `[INT-04]` — v1.x

---

## 13. Deduplication and grouping

**FR-54** — **One issue per bug.** Each report gets a fingerprint from its crash
trace, error message, screen and app version. Reports with the same
fingerprint belong to the same bug. `[BE-06, FE-01]` — v1

**FR-55** — When a new report matches an existing open issue, m-Seismo does not
open a new issue. It adds a comment with the new report's context and updates
the issue's counters. `[BE-06]` — v1

**FR-56** — Each grouped issue shows the number of occurrences, the number of
distinct installations affected, and the list of affected app versions.
`[FE-01]` — v1

**FR-57** — **Regression detection.** When a bug whose issue was closed comes
back in a newer build, the issue is reopened and tagged with the release in
which it reappeared. `[FE-07]` — v1.x

---

## 14. Routing rules

**FR-58** — Each project has routing rules that decide, per app, severity or
category, which GitHub repository receives the issue, and which labels,
assignees and template it gets. A project can send everything to one central
triage repository or each app to its own repository. `[BE-07]` — v1

**FR-59** — A report that matches no rule goes to the project's default
repository. A project cannot be saved without one. — v1

**FR-60** — **Auto-triage.** m-Seismo suggests a severity, labels and an owner
from the crash trace, the screen and the repository's code-owners file, and can
add a short AI-written summary of the title and probable cause. Suggestions are
marked as such in the issue. `[FE-06]` — v1.x

---

## 15. Administration

**FR-61** — **Admin console.** A project admin has a web console to manage
projects, project keys, routing rules and retention, and to browse reports.
`[BE-09]` — v1.x

**FR-62** — **Failed deliveries.** When an issue cannot be opened on GitHub
(GitHub down, limits reached), m-Seismo keeps retrying, shows the failure in the
console, and lets the project admin retry it by hand. No report is dropped
because GitHub was unavailable. `[BE-09, NFR Reliability]` — v1.x

**FR-63** — **Local debug bundle.** A developer can export any report as a
single zip file, to share it with someone who has no access to m-Seismo.
`[FE-09]` — v1.x

---

## 16. Alerts

**FR-64** — **Outgoing alerts.** A project can send alerts to Slack, Microsoft
Teams or any web address on a new issue, a regression or a spike.
`[BE-11]` — v1.x

**FR-65** — **Spike alerts.** When the same bug is reported more than a set
number of times in a set period, an alert is sent to Slack or Teams.
`[FE-10]` — v1.x

---

## 17. Later releases (v2)

These features are recorded so the product does not close the door on them.
They are not built in v1.

**FR-66** — **Fixed-bug notice.** A reporter is told in the app (or by email,
if they chose to give one) when their bug is fixed and shipped in version X.
`[FE-11]`

**FR-67** — **Lightweight session replay.** A replay of the screens and
interactions leading to the bug, rebuilt from events rather than video, to stay
small and private. `[FE-12]`

**FR-68** — **Screen recording.** An opt-in recording of the last 10 to 30
seconds of the screen, attached to the report. `[SDK-14]`

**FR-69** — **Web and command line.** Reports can be filed from web apps, from
CI pipelines and from scripts. `[FE-13]`

**FR-70** — **Other trackers.** Issues can be filed in Jira, Linear or GitLab
instead of GitHub. `[FE-14]`

**FR-71** — **Dashboard.** Reports per app and version, time from report to
issue, most frequent bugs and reporter activity. `[FE-15]`

**FR-72** — **Performance snapshots.** Frame drops, cold-start time, and frozen
or unresponsive app detection, attached to reports. `[FE-16]`

**FR-73** — **Server-initiated reports.** m-Seismo opens a report by itself,
with no app involved, when a server's errors spike or a health check fails.
`[AG-08, FE-17]`

---

## 18. Non-functional requirements

**NFR-01 — Speed.** A report becomes a GitHub issue in under 10 seconds on a
normal connection. The reporter gets a confirmation within half a second.

**NFR-02 — No harm to the app.** m-Seismo must never crash the app, never slow
its start-up noticeably, and never freeze the screen while capturing or sending.
Shake detection uses under 1% of battery per hour. m-Seismo adds under 300 KB
to the app on each platform.

**NFR-03 — Privacy by default.** Nothing sensitive is captured unless turned on
(FR-28); personal data is redacted on the phone and on the server; screenshots
can be masked; consent is respected; any installation's data can be deleted.

**NFR-04 — Security.** A project key can only send reports, never read them.
Reports cannot be forged or replayed. GitHub access is limited to what is
strictly needed. Attachments are private and their links expire.

**NFR-05 — Same behaviour everywhere.** iOS, Android, Flutter and React Native
apps offer the same features, the same settings and the same behaviour. A
shared set of test cases proves it on every release. `[SDK-15]`

**NFR-06 — Reliability.** No report is lost: not offline, not when the app
closes, not when m-Seismo restarts, not when GitHub is down.

**NFR-07 — Scale.** m-Seismo handles 100 reports per second sustained, and can
grow by adding more capacity.

**NFR-08 — Self-hostable and open source.** Any team can run its own copy of
m-Seismo. Hosting and storage are choices made at installation, not built into
the product. The code is published under an open-source licence.

**NFR-09 — Supported platforms.** iOS 13 and later, Android 7.0 (API 24) and
later, Flutter 3, React Native 0.73 and later. The iOS minimum was decided
2026-10-16 building the iOS SDK's first release (was an iOS 15 placeholder in
v1.0); Android/Flutter/React Native minimums remain placeholders until the
open question in section 19 is answered.

**NFR-10 — m-Seismo watches itself.** m-Seismo reports its own health and
failures to the operator, and never tries to report its own failures through
itself.

---

## 19. Open questions

| # | Question                                                                                                    | Owner         |
| - | ----------------------------------------------------------------------------------------------------------- | ------------- |
| 1 | Which apps and platforms ship first, and which minimum OS versions are required? (NFR-09) — **iOS minimum decided: iOS 13** (2026-10-16); Android/Flutter/React Native minimums and ship order still open | Product owner |
| 2 | Which licence: Apache-2.0 or MIT?                                                                           | Product owner |
| 3 | Does the name m-Seismo clash with an existing project or trademark? To be checked before the public release | Product owner |

---

## 20. Acceptance scenarios

### 20.1 Shake to issue

v1 is not complete until this works reliably:

1. a project admin creates a project and gets a project key;
2. an app integrator connects an iOS app in `QA` mode;
3. a tester opens the app, gives consent and navigates through a few screens;
4. the tester shakes the phone; the report sheet opens with a screenshot of the
   current screen;
5. the tester circles the bug, blurs a private area, adds a title and a
   severity, and sends;
6. the tester sees a confirmation within half a second and goes back to the app
   where they left it;
7. within 10 seconds, a GitHub issue appears in the repository chosen by the
   routing rules, with the right labels and assignee;
8. the issue contains numbered steps to reproduce, device and app details,
   feature flags, recent logs, the annotated screenshot and the matching server
   logs;
9. no personal data appears anywhere in the issue or its attachments;
10. the same scenario passes, with the same result, on Android, Flutter and
    React Native.

### 20.2 Crash

1. the app crashes without anyone shaking the phone;
2. Crashlytics still receives the crash exactly as before;
3. on the next launch, m-Seismo sends the crash report;
4. the issue links to the Crashlytics crash and includes its trace and custom
   keys.

### 20.3 Offline

1. the tester reports a bug in airplane mode;
2. the tester closes the app;
3. on the next launch with a connection, the report is sent and one issue is
   opened, not two.

### 20.4 Duplicate

1. ten installations hit the same bug;
2. exactly one issue exists;
3. it shows 10 occurrences, the number of distinct installations and the
   affected versions.

### 20.5 Privacy and deletion

1. a password field tagged as sensitive is hidden in every screenshot;
2. an email address written in a log line is redacted before leaving the phone;
3. a token in a server log line is redacted before leaving the server;
4. a project admin deletes everything from one installation identifier, and none
   of its reports or attachments can be found afterwards.

### 20.6 Protection

1. a project key cannot be used to read any report;
2. a flood of reports from one installation is refused above the limit, while
   other users' reports still go through;
3. a report sent twice produces one issue.

---

## 21. Launch criteria

v1 is ready for launch consideration when:

- shake, screenshot, long-press, manual and automatic crash triggers all work on
  iOS and Android, then on Flutter and React Native;
- every v1 requirement in this document is met;
- the shared test cases pass identically on all four platforms;
- the Crashlytics data export is turned on for every target Firebase project and
  the setup is documented;
- crash correlation works on real crashes from at least one real app;
- server-side context is collected for at least one real backend;
- the success targets of section 1.4 are met on a pilot app;
- no crash caused by m-Seismo has been seen during the pilot;
- no personal data has been found in any issue produced during the pilot;
- the licence is chosen and the name has been cleared (section 19);
- no known critical security issue remains.

---

## 22. Change control

This document is the authoritative product scope for m-Seismo v1. Technical
decisions live in the engineering documentation of the repository.

When an implementation conflicts with this document:

1. name the conflict explicitly;
2. do not invent a product rule to resolve it;
3. preserve correct existing behaviour where possible;
4. obtain an explicit decision from the product owner;
5. update this document only after that decision.

Anything outside this document is out of scope until it is added here.
