CREATE TABLE tasks (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id BIGINT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'todo',
    priority TEXT NOT NULL DEFAULT 'medium',
    assignee_id BIGINT REFERENCES users (id),
    creator_id BIGINT NOT NULL REFERENCES users (id),
    due_date TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT tasks_title_valid CHECK (title = btrim(title) AND char_length(title) BETWEEN 1 AND 200),
    CONSTRAINT tasks_description_valid CHECK (char_length(description) <= 2000),
    CONSTRAINT tasks_status_valid CHECK (status IN ('todo', 'in_progress', 'done')),
    CONSTRAINT tasks_priority_valid CHECK (priority IN ('low', 'medium', 'high'))
);

-- Supports project-scoped listing in stable ID order and cascading deletion.
CREATE INDEX tasks_project_id_idx ON tasks (project_id, id);
