CREATE TABLE comments (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    task_id BIGINT NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
    author_id BIGINT NOT NULL REFERENCES users (id),
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT comments_body_valid CHECK (body = btrim(body) AND char_length(body) BETWEEN 1 AND 2000)
);

-- Supports task-scoped listing and cascading deletion.
CREATE INDEX comments_task_id_idx ON comments (task_id, id);
