DROP INDEX IF EXISTS idx_avatars_user_created_at;

ALTER TABLE avatars
    DROP COLUMN IF EXISTS width,
    DROP COLUMN IF EXISTS height;
