CREATE TABLE projects (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    owner_id BIGINT NOT NULL REFERENCES users (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT projects_name_valid CHECK (
        name = btrim(name) AND char_length(name) BETWEEN 1 AND 100
    ),
    CONSTRAINT projects_description_valid CHECK (char_length(description) <= 2000)
);

CREATE TABLE project_members (
    project_id BIGINT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users (id),
    role TEXT NOT NULL,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (project_id, user_id),
    CONSTRAINT project_members_role_valid CHECK (role IN ('owner', 'admin', 'member'))
);

-- Supports listing the projects to which a user belongs.
CREATE INDEX project_members_user_id_idx ON project_members (user_id, project_id);
-- A project has at most one owner membership. Creation inserts it atomically.
CREATE UNIQUE INDEX project_members_owner_idx ON project_members (project_id) WHERE role = 'owner';
