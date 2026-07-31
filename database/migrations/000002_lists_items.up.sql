CREATE TABLE lists (
    id         uuid        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    name       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE items (
    id         uuid        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    list_id    uuid        NOT NULL REFERENCES lists (id) ON DELETE CASCADE,
    name       text        NOT NULL,
    quantity   int         NOT NULL DEFAULT 1 CHECK (quantity >= 1),
    note       text,
    checked    boolean     NOT NULL DEFAULT false,
    checked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX items_list_id_idx ON items (list_id);
