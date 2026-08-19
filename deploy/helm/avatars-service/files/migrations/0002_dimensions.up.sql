ALTER TABLE avatars
    ADD COLUMN width INTEGER,
    ADD COLUMN height INTEGER;

CREATE INDEX idx_avatars_user_created_at ON avatars(user_id, created_at DESC) WHERE deleted_at IS NULL;
