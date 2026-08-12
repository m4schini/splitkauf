-- Attribution: who created a list, who added an item, who bought it.
--
-- Attribution is stored as the acting user's UUID (auth.User.ID) and the
-- display name is resolved at read time by joining members.user_id, so a
-- profile rename propagates to every past attribution.
--
-- members is keyed by the auth subject (text), which differs per auth mode:
-- dev/password subjects are the user UUID as a string, OIDC subjects are the
-- raw provider subject. user_id backfills accordingly — a subject that already
-- is a UUID casts directly, anything else is hashed with the same UUIDv5
-- namespace auth.subjectUUID uses (uuid.NewSHA1 == uuid_generate_v5).
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

ALTER TABLE members ADD COLUMN user_id uuid;
UPDATE members SET user_id = CASE
    WHEN subject ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
        THEN subject::uuid
    ELSE uuid_generate_v5('6f9619ff-8b86-d011-b42d-00c04fc964ff'::uuid, subject)
END;
ALTER TABLE members ALTER COLUMN user_id SET NOT NULL;
CREATE UNIQUE INDEX members_user_id_idx ON members (user_id);

-- No foreign key to members: the acting user is not guaranteed to have a
-- member row (the dev startup upsert is best-effort), and a FK would turn that
-- tolerable gap into a failed write. A missing member row simply resolves the
-- display name to NULL.
ALTER TABLE lists ADD COLUMN created_by uuid;
ALTER TABLE items ADD COLUMN added_by   uuid;
ALTER TABLE items ADD COLUMN bought_by  uuid;
