-- +migrate Up
ALTER TABLE artifact ADD COLUMN relativepath VARCHAR(255);
UPDATE artifact SET relativepath = name WHERE relativepath IS NULL;

-- +migrate Down
ALTER TABLE artifact DROP COLUMN "relativepath";
