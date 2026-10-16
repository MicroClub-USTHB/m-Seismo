# m-Seismo backend documentation

These documents explain how the backend is designed, how each part works, why
it was built that way, and what is still missing. They are written for
engineers who will work on the backend or build on top of it, for example the
team writing the future report-ingestion endpoint.

Each document says which parts are **implemented** and which are **planned**.
When the code and a document disagree, the code is right and the document is
a bug: fix it in the same pull request.

| # | Document | What it covers |
| - | -------- | -------------- |
| 1 | [Overview](01-overview.md) | What the backend is for, what it is not, how it relates to the PRD, current status |
| 2 | [Architecture](02-architecture.md) | Layers, package layout, dependency rules, request lifecycle, key design decisions |
| 3 | [Data model](03-data-model.md) | Every table and constraint, why it exists, migrations |
| 4 | [HTTP API](04-http-api.md) | Endpoints, authentication, errors, pagination, PATCH semantics, examples |
| 5 | [Internal services](05-services.md) | How each package works: config, db, audit, users, projects, apps, httpapi and the helpers |
| 6 | [Git connections](06-git-connections.md) | Planned design for linking GitHub (App), GitLab and Bitbucket, and linking repositories to projects |
| 7 | [Issues service](07-issues-service.md) | Planned design of the internal issues service: submit, dedup, idempotency, rate limiting, attachments, outbox, publishing |
| 8 | [Operations](08-operations.md) | Configuration, running, migrations, logging, health checks, shutdown, deployment notes |
| 9 | [Testing](09-testing.md) | Test strategy, the database harness, how to run and write tests |
| 10 | [Roadmap and gaps](10-roadmap-and-gaps.md) | Phase status, what is missing, known limitations, deviations from the plan, open decisions |

## Reading order

New to the code: read 1 → 2 → 3 → 5, then the rest as needed.

Building the report endpoint that calls the issues service: read 1, 2 and 7.

Running the service: read 8.
