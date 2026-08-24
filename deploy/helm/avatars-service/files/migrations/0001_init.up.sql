CREATE TYPE avatar_upload_status AS ENUM ('uploading', 'uploaded', 'failed');

CREATE TYPE avatar_processing_status AS ENUM ('pending', 'processing', 'ready', 'failed');

CREATE TABLE avatars (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id VARCHAR(255) NOT NULL,
    file_name VARCHAR(255) NOT NULL,
    mime_type VARCHAR(100) NOT NULL,
    size_bytes BIGINT NOT NULL,
    s3_key VARCHAR(500) NOT NULL,
    thumbnail_s3_keys JSONB NOT NULL DEFAULT '{}'::jsonb,
    upload_status avatar_upload_status NOT NULL DEFAULT 'uploading',
    processing_status avatar_processing_status NOT NULL DEFAULT 'pending',
    width INTEGER,
    height INTEGER,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX idx_avatars_user_id ON avatars(user_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_avatars_status ON avatars(upload_status, processing_status);
CREATE INDEX idx_avatars_user_created_at ON avatars(user_id, created_at DESC) WHERE deleted_at IS NULL;
