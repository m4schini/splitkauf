CREATE TABLE users (
    id            uuid        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    username      text        NOT NULL UNIQUE,
    password_hash text        NOT NULL,
    name          text        NOT NULL DEFAULT '',
    email         text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
