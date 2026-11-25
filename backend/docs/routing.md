# Routing rules

Routing rules decide where a report's issue goes and what it carries. Each
project has its own rules; the code lives in `internal/routing`.

## How a report is routed

The worker reads three facts from the report (`routing.FactsOf`):

| Fact | From |
|---|---|
| app | the app whose key sent the report |
| severity | `user_input.severity`, empty when the reporter set none |
| category | `user_input.category`; a report the SDK sent on its own after a crash (`trigger` `crash` or `non_fatal`) with no category counts as `crash` |

Then, for every project the app is linked to (`Router.Routes`):

1. The project's rules are tried in `position` order (ties by creation).
2. A rule matches when every condition it sets holds. A condition left
   empty matches any report.
3. The first rule that matches sends the issue to its repository, with its
   labels, assignees and milestone.
4. When no rule matches, the issue goes to the project's default repository
   with nothing added.

A project with no default repository still gets the issues its rules route.
One report opens one issue per project, never more: once a project has the
report's issue, a later attempt skips it even if a rule changed since.

```
facts {app, severity, category}
  │
  ├─ project A: rule 0 ✗ → rule 1 ✓ → A/mobile + labels, assignees, milestone
  └─ project B: no rule matches      → B's default repository
```

Rules are read for every report, so a change applies to the next report.

## API

All under `/api/v1`, as the project's owner.

| Method | Path | |
|---|---|---|
| `GET` | `/projects/{projectID}/routing-rules` | the rules, in the order they are tried |
| `POST` | `/projects/{projectID}/routing-rules` | add a rule, 201 |
| `PUT` | `/projects/{projectID}/routing-rules/{ruleID}` | replace a rule, 200 |
| `DELETE` | `/projects/{projectID}/routing-rules/{ruleID}` | delete a rule, 204 |

Body of `POST` and `PUT`:

```json
{
  "position": 0,
  "app_id": "…",
  "severity": "critical",
  "category": "crash",
  "repository_id": "…",
  "labels": ["crash", "ios"],
  "assignees": ["octocat"],
  "milestone": "v2.4"
}
```

| Field | Rules |
|---|---|
| `repository_id` | required; one of the project's repositories (`GET /projects/{id}/repositories`) |
| `position` | 0 or more. Optional: a new rule goes after the last one, a replaced rule keeps its place |
| `app_id` | optional; an app of the same owner. The rule goes when the app is deleted |
| `severity` | optional; `low`, `medium`, `high` or `critical` |
| `category` | optional; `bug`, `crash`, `ui` or `other` |
| `labels` | up to 20, each up to 50 characters; trimmed, empty and duplicate entries dropped |
| `assignees` | up to 10 tracker usernames, without `@` or spaces |
| `milestone` | up to 255 characters; GitHub resolves it by title |

`PUT` replaces the whole rule: a field left out is cleared. Errors: 422 for
an invalid body, 404 for an unknown project, rule, repository or app.

## Repositories and rules

- A repository can't be unlinked while rules send to it (409): change or
  delete those rules first.
- The default repository can't be unlinked while other repositories are
  linked (409): make another one the default first. The last repository can
  be unlinked.

## Not done yet

- **Issue templates per rule**: rules set labels, assignees and milestone;
  the issue body is the same for every rule until issue templates exist.
