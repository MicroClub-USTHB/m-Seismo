-- NOTE: applied on every start. Swap for a real migration tool before the first schema change.

CREATE TABLE IF NOT EXISTS projects (
    id   uuid PRIMARY KEY,
    name text NOT NULL
);

-- NOTE: keys are stored as sha256 digests, not bcrypt. They are random and long, so a fast hash is enough.
CREATE TABLE IF NOT EXISTS project_keys (
    key_hash   bytea PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES projects (id),
    revoked_at timestamptz
);

CREATE TABLE IF NOT EXISTS reports (
    project_id      uuid NOT NULL REFERENCES projects (id),
    id              uuid NOT NULL,
    installation_id text NOT NULL,
    body            jsonb NOT NULL,
    received_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, id)
);
