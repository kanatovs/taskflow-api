-- Read-only verification that user migration 1 is applied and the schema is clean.
-- Run with psql -v ON_ERROR_STOP=1 so a mismatch returns a nonzero exit code.
DO $$
BEGIN
    IF to_regclass('public.users') IS NULL
        OR to_regclass('public.schema_migrations') IS NULL THEN
        RAISE EXCEPTION 'Authentication schema is missing; run migrations first';
    END IF;

    IF (SELECT count(*) <> 1 OR bool_or(version < 1 OR dirty)
        FROM public.schema_migrations) THEN
        RAISE EXCEPTION 'Expected user migration to be applied with dirty=false';
    END IF;

    IF EXISTS (
        WITH expected (ordinal_position, column_name, data_type, is_nullable,
                       is_identity, identity_generation, column_default) AS (
            VALUES
                (1, 'id', 'bigint', 'NO', 'YES', 'ALWAYS', NULL::text),
                (2, 'email', 'text', 'NO', 'NO', NULL, NULL),
                (3, 'password_hash', 'text', 'NO', 'NO', NULL, NULL),
                (4, 'display_name', 'text', 'NO', 'NO', NULL, NULL),
                (5, 'created_at', 'timestamp with time zone', 'NO', 'NO', NULL, 'CURRENT_TIMESTAMP'),
                (6, 'updated_at', 'timestamp with time zone', 'NO', 'NO', NULL, 'CURRENT_TIMESTAMP')
        ), actual AS (
            SELECT ordinal_position, column_name::text, data_type::text,
                   is_nullable::text, is_identity::text, identity_generation::text,
                   column_default::text
            FROM information_schema.columns
            WHERE table_schema = 'public' AND table_name = 'users'
        )
        (SELECT * FROM expected EXCEPT SELECT * FROM actual)
        UNION ALL
        (SELECT * FROM actual EXCEPT SELECT * FROM expected)
    ) THEN
        RAISE EXCEPTION 'Users columns or defaults do not match migration 1';
    END IF;

    IF EXISTS (
        WITH expected (conname, contype, convalidated, definition) AS (
            VALUES
                ('users_pkey', 'p', true, 'PRIMARY KEY (id)'),
                ('users_email_key', 'u', true, 'UNIQUE (email)'),
                ('users_email_normalized', 'c', true,
                 'CHECK (((email = lower(btrim(email))) AND ((char_length(email) >= 3) AND (char_length(email) <= 254))))'),
                ('users_password_hash_not_empty', 'c', true,
                 'CHECK ((password_hash <> ''''::text))'),
                ('users_display_name_valid', 'c', true,
                 'CHECK (((display_name = btrim(display_name)) AND ((char_length(display_name) >= 1) AND (char_length(display_name) <= 100))))')
        ), actual AS (
            SELECT conname::text, contype::text, convalidated,
                   pg_get_constraintdef(oid) AS definition
            FROM pg_constraint
            WHERE conrelid = 'public.users'::regclass
        )
        (SELECT * FROM expected EXCEPT SELECT * FROM actual)
        UNION ALL
        (SELECT * FROM actual EXCEPT SELECT * FROM expected)
    ) THEN
        RAISE EXCEPTION 'Users constraints do not match migration 1';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM pg_index
        WHERE indrelid = 'public.users'::regclass AND NOT indisvalid
    ) THEN
        RAISE EXCEPTION 'Users has an invalid index';
    END IF;
END
$$;

SELECT 'Authentication schema matches migration 1; dirty=false' AS result;
