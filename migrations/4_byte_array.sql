-- +migrate Up
ALTER TABLE logchunk ADD COLUMN content_bytes BYTEA;

-- +migrate Down
ALTER TABLE logchunk DROP COLUMN content_bytes;
