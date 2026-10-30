-- name: CreateIssueTemplate :one
INSERT INTO issue_templates (project_id, name, title_template, body_template)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListIssueTemplates :many
SELECT * FROM issue_templates
WHERE project_id = $1
ORDER BY created_at;

-- name: GetIssueTemplate :one
SELECT * FROM issue_templates
WHERE id = $1 AND project_id = $2;

-- name: UpdateIssueTemplate :one
UPDATE issue_templates
SET name = $3,
    title_template = $4,
    body_template = $5,
    updated_at = now()
WHERE id = $1 AND project_id = $2
RETURNING *;

-- name: DeleteIssueTemplate :execrows
DELETE FROM issue_templates
WHERE id = $1 AND project_id = $2;
