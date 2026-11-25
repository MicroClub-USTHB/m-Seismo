-- name: CreateRoutingRule :one
INSERT INTO routing_rules (project_id, position, app_id, severity, category, project_repository_id, labels, assignees, milestone, issue_template_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: ListRoutingRules :many
SELECT * FROM routing_rules
WHERE project_id = $1
ORDER BY position, created_at;

-- name: GetRoutingRule :one
SELECT * FROM routing_rules
WHERE id = $1 AND project_id = $2;

-- name: UpdateRoutingRule :one
UPDATE routing_rules
SET position = $3,
    app_id = $4,
    severity = $5,
    category = $6,
    project_repository_id = $7,
    labels = $8,
    assignees = $9,
    milestone = $10,
    issue_template_id = $11,
    updated_at = now()
WHERE id = $1 AND project_id = $2
RETURNING *;

-- name: DeleteRoutingRule :execrows
DELETE FROM routing_rules
WHERE id = $1 AND project_id = $2;

-- name: ListRoutingRulesForApp :many
-- The rules of every project the app reports into, in the order they are
-- tried, with the repository each one sends to and its template's content,
-- when it has one.
SELECT rr.*, pr.repository, it.title_template, it.body_template
FROM routing_rules rr
JOIN app_projects ap ON ap.project_id = rr.project_id
JOIN project_repositories pr ON pr.id = rr.project_repository_id
LEFT JOIN issue_templates it ON it.id = rr.issue_template_id
WHERE ap.app_id = $1
ORDER BY rr.project_id, rr.position, rr.created_at;

-- name: CountRoutingRulesForRepository :one
SELECT count(*) FROM routing_rules
WHERE project_repository_id = $1;
