CREATE TABLE users (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    display_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT users_email_key UNIQUE (email),
    CONSTRAINT users_email_normalized CHECK (
        email = lower(btrim(email)) AND char_length(email) BETWEEN 3 AND 254
    ),
    CONSTRAINT users_password_hash_not_empty CHECK (password_hash <> ''),
    CONSTRAINT users_display_name_valid CHECK (
        display_name = btrim(display_name) AND char_length(display_name) BETWEEN 1 AND 100
    )
);
