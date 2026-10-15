-- Read-only verification of project migration 2 and its required constraints.
DO $$
BEGIN
    IF to_regclass('public.projects') IS NULL OR to_regclass('public.project_members') IS NULL
        OR to_regclass('public.schema_migrations') IS NULL THEN
        RAISE EXCEPTION 'Project schema is missing';
    END IF;
    IF (SELECT count(*) <> 1 OR bool_or(version < 2 OR dirty) FROM public.schema_migrations) THEN
        RAISE EXCEPTION 'Expected project migration to be applied with dirty=false';
    END IF;
    IF EXISTS (
        WITH expected (table_name, column_name, data_type, is_nullable) AS (
            VALUES
                ('projects', 'id', 'bigint', 'NO'),
                ('projects', 'name', 'text', 'NO'),
                ('projects', 'description', 'text', 'NO'),
                ('projects', 'owner_id', 'bigint', 'NO'),
                ('projects', 'created_at', 'timestamp with time zone', 'NO'),
                ('projects', 'updated_at', 'timestamp with time zone', 'NO'),
                ('project_members', 'project_id', 'bigint', 'NO'),
                ('project_members', 'user_id', 'bigint', 'NO'),
                ('project_members', 'role', 'text', 'NO'),
                ('project_members', 'joined_at', 'timestamp with time zone', 'NO')
        )
        SELECT 1 FROM expected e LEFT JOIN information_schema.columns c
            ON c.table_schema = 'public' AND c.table_name = e.table_name AND c.column_name = e.column_name
        WHERE c.column_name IS NULL OR c.data_type <> e.data_type OR c.is_nullable <> e.is_nullable
    ) THEN
        RAISE EXCEPTION 'Project columns do not match migration 2';
    END IF;
    IF EXISTS (
        WITH expected (table_name, constraint_name, definition) AS (
            VALUES
                ('projects', 'projects_pkey', 'PRIMARY KEY (id)'),
                ('projects', 'projects_owner_id_fkey', 'FOREIGN KEY (owner_id) REFERENCES users(id)'),
                ('project_members', 'project_members_pkey', 'PRIMARY KEY (project_id, user_id)'),
                ('project_members', 'project_members_project_id_fkey', 'FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE'),
                ('project_members', 'project_members_user_id_fkey', 'FOREIGN KEY (user_id) REFERENCES users(id)'),
                ('project_members', 'project_members_role_valid', 'CHECK ((role = ANY (ARRAY[''owner''::text, ''admin''::text, ''member''::text])))')
        )
        SELECT 1 FROM expected e LEFT JOIN pg_constraint c
            ON c.conrelid = to_regclass('public.' || e.table_name) AND c.conname = e.constraint_name
        WHERE c.oid IS NULL OR NOT c.convalidated OR pg_get_constraintdef(c.oid) <> e.definition
    ) THEN
        RAISE EXCEPTION 'Project constraints do not match migration 2';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_index
        WHERE indexrelid = to_regclass('public.project_members_owner_idx')
            AND indrelid = 'public.project_members'::regclass AND indisunique AND indisvalid
            AND pg_get_expr(indpred, indrelid) = '(role = ''owner''::text)'
    ) THEN
        RAISE EXCEPTION 'Unique owner membership index is missing or invalid';
    END IF;
END
$$;

SELECT 'Project schema matches migration 2; dirty=false' AS result;
