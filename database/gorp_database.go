package database

import (
	"github.com/dropbox/changes-artifacts/model"
	"gopkg.in/gorp.v1"
)

type GorpDatabase struct {
	dbmap *gorp.DbMap
}

func NewGorpDatabase(dbmap *gorp.DbMap) *GorpDatabase {
	return &GorpDatabase{dbmap: dbmap}
}

func verifyBucketFields(bucket *model.Bucket) *DatabaseError {
	if len(bucket.Id) == 0 {
		return NewValidationError("Bucket.ID not set")
	}

	if bucket.State != model.OPEN && bucket.State != model.CLOSED && bucket.State != model.TIMEDOUT {
		return NewValidationError("Bucket in unknown state")
	}

	if len(bucket.Owner) == 0 {
		return NewValidationError("Bucket owner not set")
	}

	return nil
}

func (db *GorpDatabase) RegisterEntities() {
	// Add bucket non-autoincrementing ID field.
	db.dbmap.AddTableWithName(model.Bucket{}, "bucket").SetKeys(false, "Id")

	// Add artifact autoincrementing ID field.
	db.dbmap.AddTableWithName(model.Artifact{}, "artifact").
		SetKeys(true, "Id").
		SetUniqueTogether("BucketID", "Name")

	// Add logchunk autoincrementing ID field.
	db.dbmap.AddTableWithName(model.LogChunk{}, "logchunk").SetKeys(true, "Id")
}

func (db *GorpDatabase) CreateEntities() *DatabaseError {
	return WrapInternalDatabaseError(db.dbmap.CreateTablesIfNotExists())
}

func (db *GorpDatabase) RecreateTables() *DatabaseError {
	if err := db.dbmap.DropTables(); err != nil {
		return WrapInternalDatabaseError(err)
	}

	return db.CreateEntities()
}

func (db *GorpDatabase) InsertBucket(bucket *model.Bucket) *DatabaseError {
	if err := verifyBucketFields(bucket); err != nil {
		return err
	}

	return WrapInternalDatabaseError(db.dbmap.Insert(bucket))
}

func (db *GorpDatabase) InsertArtifact(artifact *model.Artifact) *DatabaseError {
	return WrapInternalDatabaseError(db.dbmap.Insert(artifact))
}

func (db *GorpDatabase) InsertLogChunk(logChunk *model.LogChunk) *DatabaseError {
	return WrapInternalDatabaseError(db.dbmap.Insert(logChunk))
}

func (db *GorpDatabase) UpdateBucket(bucket *model.Bucket) *DatabaseError {
	if err := verifyBucketFields(bucket); err != nil {
		return err
	}

	_, err := db.dbmap.Update(bucket)
	return WrapInternalDatabaseError(err)
}

func (db *GorpDatabase) ListBuckets() ([]model.Bucket, *DatabaseError) {
	buckets := []model.Bucket{}
	if _, err := db.dbmap.Select(&buckets, "SELECT * FROM bucket"); err != nil {
		return nil, WrapInternalDatabaseError(err)
	}

	return buckets, nil
}

func (db *GorpDatabase) GetBucket(id string) (*model.Bucket, *DatabaseError) {
	if bucket, err := db.dbmap.Get(model.Bucket{}, id); err != nil {
		return nil, WrapInternalDatabaseError(err)
	} else if bucket == nil {
		return nil, NewEntityNotFoundError("Entity %s not found", id)
	} else {
		return bucket.(*model.Bucket), nil
	}
}

func (db *GorpDatabase) ListArtifactsInBucket(bucketId string) ([]model.Artifact, *DatabaseError) {
	artifacts := []model.Artifact{}
	if _, err := db.dbmap.Select(&artifacts, "SELECT * FROM artifact WHERE bucketid = :bucketid",
		map[string]interface{}{"bucketid": bucketId}); err != nil {
		return nil, WrapInternalDatabaseError(err)
	}

	return artifacts, nil
}

func (db *GorpDatabase) UpdateArtifact(artifact *model.Artifact) *DatabaseError {
	_, err := db.dbmap.Update(artifact)
	return WrapInternalDatabaseError(err)
}

func (db *GorpDatabase) ListLogChunksInArtifact(artifactId int64) ([]model.LogChunk, *DatabaseError) {
	logChunks := []model.LogChunk{}
	if _, err := db.dbmap.Select(&logChunks, "SELECT * FROM logchunk WHERE artifactid = :artifactid ORDER BY byteoffset ASC",
		map[string]interface{}{"artifactid": artifactId}); err != nil {
		return nil, WrapInternalDatabaseError(err)
	}

	return logChunks, nil
}

func (db *GorpDatabase) GetArtifactByName(bucketId string, artifactName string) (*model.Artifact, *DatabaseError) {
	var artifact model.Artifact
	if err := db.dbmap.SelectOne(&artifact, "SELECT * FROM artifact WHERE bucketid = :bucketid AND name = :artifactname",
		map[string]string{"bucketid": bucketId, "artifactname": artifactName}); err != nil {
		return nil, WrapInternalDatabaseError(err)
	}

	return &artifact, nil
}

func (db *GorpDatabase) GetArtifactById(artifactId int64) (*model.Artifact, *DatabaseError) {
	var artifact model.Artifact
	if err := db.dbmap.SelectOne(&artifact, "SELECT * FROM artifact WHERE artifactid = :artifactid",
		map[string]interface{}{"artifactid": artifactId}); err != nil {
		return nil, WrapInternalDatabaseError(err)
	}

	return &artifact, nil
}

func (db *GorpDatabase) GetLastByteSeenForArtifact(artifactId int64) (int64, *DatabaseError) {
	if nextByteOffset, err := db.dbmap.SelectInt(
		"SELECT COALESCE(MAX(byteoffset + size), 0) as lastSeenByte FROM logchunk WHERE artifactid = :artifactid",
		map[string]interface{}{"artifactid": artifactId}); err != nil {
		return 0, WrapInternalDatabaseError(err)
	} else {
		return nextByteOffset, nil
	}
}

// Ensure GorpDatabase implements Database
var _ Database = new(GorpDatabase)
