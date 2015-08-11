-- +migrate Up
CREATE INDEX logchunk_artifactid_size_last ON logchunk (artifactid, size DESC NULLS LAST);

-- +migrate Down
DROP INDEX logchunk_artifactid_size_last;
