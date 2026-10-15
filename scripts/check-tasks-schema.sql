-- Read-only verification of task/comment migrations and required invariants.
DO $$
BEGIN
    IF to_regclass('public.tasks') IS NULL OR to_regclass('public.comments') IS NULL
        OR to_regclass('public.schema_migrations') IS NULL THEN
        RAISE EXCEPTION 'Task/comment schema is missing';
    END IF;
    IF (SELECT count(*) <> 1 OR bool_or(version < 4 OR dirty) FROM public.schema_migrations) THEN
        RAISE EXCEPTION 'Expected task/comment migrations with dirty=false';
    END IF;
    IF EXISTS (
        WITH expected (table_name, column_name, data_type, is_nullable) AS (
            VALUES
                ('tasks','id','bigint','NO'),
                ('tasks','project_id','bigint','NO'),
                ('tasks','title','text','NO'),
                ('tasks','description','text','NO'),
                ('tasks','status','text','NO'),
                ('tasks','priority','text','NO'),
                ('tasks','assignee_id','bigint','YES'),
                ('tasks','creator_id','bigint','NO'),
                ('tasks','due_date','timestamp with time zone','YES'),
                ('tasks','created_at','timestamp with time zone','NO'),
                ('tasks','updated_at','timestamp with time zone','NO'),
                ('comments','id','bigint','NO'),
                ('comments','task_id','bigint','NO'),
                ('comments','author_id','bigint','NO'),
                ('comments','body','text','NO'),
                ('comments','created_at','timestamp with time zone','NO'),
                ('comments','updated_at','timestamp with time zone','NO')
        )
        SELECT 1 FROM expected e LEFT JOIN information_schema.columns c
            ON c.table_schema='public' AND c.table_name=e.table_name AND c.column_name=e.column_name
        WHERE c.column_name IS NULL OR c.data_type<>e.data_type OR c.is_nullable<>e.is_nullable
    ) THEN
        RAISE EXCEPTION 'Task/comment columns do not match migrations';
    END IF;
    IF EXISTS (
        WITH expected (table_name, constraint_name, definition) AS (
            VALUES
                ('tasks','tasks_pkey','PRIMARY KEY (id)'),
                ('tasks','tasks_project_id_fkey','FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE'),
                ('tasks','tasks_creator_id_fkey','FOREIGN KEY (creator_id) REFERENCES users(id)'),
                ('tasks','tasks_assignee_id_fkey','FOREIGN KEY (assignee_id) REFERENCES users(id)'),
                ('comments','comments_pkey','PRIMARY KEY (id)'),
                ('comments','comments_task_id_fkey','FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE'),
                ('comments','comments_author_id_fkey','FOREIGN KEY (author_id) REFERENCES users(id)')
        )
        SELECT 1 FROM expected e LEFT JOIN pg_constraint c
            ON c.conrelid=to_regclass('public.' || e.table_name) AND c.conname=e.constraint_name
        WHERE c.oid IS NULL OR NOT c.convalidated OR pg_get_constraintdef(c.oid)<>e.definition
    ) THEN
        RAISE EXCEPTION 'Task/comment keys do not match migrations';
    END IF;
    IF EXISTS (
        WITH expected (table_name, constraint_name) AS (
            VALUES ('tasks','tasks_title_valid'),('tasks','tasks_description_valid'),
                ('tasks','tasks_status_valid'),('tasks','tasks_priority_valid'),('comments','comments_body_valid')
        )
        SELECT 1 FROM expected e LEFT JOIN pg_constraint c
            ON c.conrelid=to_regclass('public.' || e.table_name) AND c.conname=e.constraint_name
        WHERE c.oid IS NULL OR NOT c.convalidated OR c.contype<>'c'
    ) THEN
        RAISE EXCEPTION 'Task/comment check constraints are missing';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_index WHERE indexrelid=to_regclass('public.tasks_project_id_idx') AND indisvalid)
        OR NOT EXISTS (SELECT 1 FROM pg_index WHERE indexrelid=to_regclass('public.comments_task_id_idx') AND indisvalid) THEN
        RAISE EXCEPTION 'Task/comment listing indexes are missing';
    END IF;
END
$$;

SELECT 'Task/comment schema verified; dirty=false' AS result;
