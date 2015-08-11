-- +migrate Up
-- SQL in section 'Up' is executed when this migration is applied
CREATE TABLE IF NOT EXISTS bucket (
  dateclosed TIMESTAMP WITH TIME ZONE,
  datecreated TIMESTAMP WITH TIME ZONE,
  id TEXT NOT NULL PRIMARY KEY,
  owner TEXT,
  state TEXT
);
CREATE TABLE IF NOT EXISTS artifact (
  bucketid TEXT,
  datecreated TIMESTAMP WITH TIME ZONE,
  id BIGSERIAL NOT NULL PRIMARY KEY,
  name TEXT,
  s3url TEXT,
  size BIGINT,
  state TEXT,
  deadlinemins TEXT,
  UNIQUE (bucketid, name)
);
CREATE TABLE IF NOT EXISTS logchunk ("id" BIGSERIAL NOT NULL PRIMARY KEY , "artifactid" BIGINT, "byteoffset" BIGINT, "size" BIGINT, "content" TEXT);

-- +migrate Down
-- SQL section 'Down' is executed when this migration is rolled back
DROP TABLE bucket;
DROP TABLE artifact;
DROP TABLE logchunk;
