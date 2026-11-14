# m-Seismo API — Bruno collection

A runnable reference for every `/api/v1` endpoint, replacing the old static
`docs/api.md`. Open this folder (`backend/bruno`) in
[Bruno](https://www.usebruno.com/), pick the **local** environment, and run
requests folder by folder — each one documents its own request/response
shape, status codes and failure modes in its **Docs** tab.

Start with:

1. `01 Auth and owners / Register` (or `Login`) — fills in `token`.
2. `04 Apps / Create App` — fills in `appId`.
3. `02 Projects / Create Project` — fills in `projectId`.

From there, `03 Project repositories`, `06 App project links`,
`07 App connections`, `08 Targets` and `09 Test issue` follow the same
flow as the old walkthrough: link a repository, link the app to the
project, connect a tracker, check targets, send a test issue.

For GitLab/Bitbucket, run `07 App connections / Set Tracker Credentials`
before `Connect` — each app registers its own OAuth application; see
`backend/docs/gitlab-bitbucket.md`.
