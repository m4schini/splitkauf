-- Drops the attribution columns and members.user_id. The uuid-ossp extension is
-- deliberately left installed: it may pre-date this migration or be in use
-- elsewhere, and dropping it is not this migration's to undo.
ALTER TABLE items DROP COLUMN bought_by;
ALTER TABLE items DROP COLUMN added_by;
ALTER TABLE lists DROP COLUMN created_by;

DROP INDEX members_user_id_idx;
ALTER TABLE members DROP COLUMN user_id;
