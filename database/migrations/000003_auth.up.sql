CREATE TABLE sessions (
    token  text        NOT NULL PRIMARY KEY,
    data   bytea       NOT NULL,
    expiry timestamptz NOT NULL
);

CREATE INDEX sessions_expiry_idx ON sessions (expiry);

CREATE TABLE members (
    subject    text        NOT NULL PRIMARY KEY,
    email      text,
    name       text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
