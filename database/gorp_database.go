package database

import (
	"time"

	"github.com/dropbox/changes-artifacts/common/stats"
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

var insertBucketTimer = stats.NewTimingStat("insert_bucket")

func (db *GorpDatabase) InsertBucket(bucket *model.Bucket) *DatabaseError {
	defer insertBucketTimer.AddTimeSince(time.Now())

	if err := verifyBucketFields(bucket); err != nil {
		return err
	}

	return WrapInternalDatabaseError(db.dbmap.Insert(bucket))
}

var insertArtifactTimer = stats.NewTimingStat("insert_artifact")

func (db *GorpDatabase) InsertArtifact(artifact *model.Artifact) *DatabaseError {
	defer insertArtifactTimer.AddTimeSince(time.Now())
	return WrapInternalDatabaseError(db.dbmap.Insert(artifact))
}

var insertLogChunkTimer = stats.NewTimingStat("insert_logchunk")

func (db *GorpDatabase) InsertLogChunk(logChunk *model.LogChunk) *DatabaseError {
	defer insertLogChunkTimer.AddTimeSince(time.Now())
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
	// NOTE: Hardcoded limit of 25 buckets below.
	// Because of the large number of buckets (2.5M+ and increasing), its not feasible to list all
	// buckets at /buckets. Instead, we show only the latest 25 buckets. This endpoint is not
	// particularly useful and is not used by any client.
	if _, err := db.dbmap.Select(&buckets, "SELECT * FROM bucket ORDER BY datecreated LIMIT 25"); err != nil {
		return nil, WrapInternalDatabaseError(err)
	}

	return buckets, nil
}

func (db *GorpDatabase) GetBucket(id string) (*model.Bucket, *DatabaseError) {
	if bucket, err := db.dbmap.Get(model.Bucket{}, id); err != nil && !gorp.NonFatalError(err) {
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
		map[string]interface{}{"bucketid": bucketId}); err != nil && !gorp.NonFatalError(err) {
		return nil, WrapInternalDatabaseError(err)
	}

	return artifacts, nil
}

func (db *GorpDatabase) UpdateArtifact(artifact *model.Artifact) *DatabaseError {
	_, err := db.dbmap.Update(artifact)
	if !gorp.NonFatalError(err) {
		return WrapInternalDatabaseError(err)
	}

	return nil
}

func (db *GorpDatabase) ListLogChunksInArtifact(artifactId int64) ([]model.LogChunk, *DatabaseError) {
	logChunks := []model.LogChunk{}
	if _, err := db.dbmap.Select(&logChunks, "SELECT * FROM logchunk WHERE artifactid = :artifactid ORDER BY byteoffset ASC",
		map[string]interface{}{"artifactid": artifactId}); err != nil && !gorp.NonFatalError(err) {
		return nil, WrapInternalDatabaseError(err)
	}

	return logChunks, nil
}

// DeleteLogChunksForArtifact deletes all log chunks for an artifact.
// Returns (number of deleted rows, err)
func (db *GorpDatabase) DeleteLogChunksForArtifact(artifactID int64) (int64, *DatabaseError) {
	res, err := db.dbmap.Exec("DELETE FROM logchunk WHERE artifactid = $1", artifactID)
	if err != nil && !gorp.NonFatalError(err) {
		rows, _ := res.RowsAffected()
		return rows, WrapInternalDatabaseError(err)
	}

	rows, err := res.RowsAffected()
	if err != nil && !gorp.NonFatalError(err) {
		return rows, WrapInternalDatabaseError(err)
	}

	return rows, nil
}

func (db *GorpDatabase) GetArtifactByName(bucketId string, artifactName string) (*model.Artifact, *DatabaseError) {
	var artifact model.Artifact
	if err := db.dbmap.SelectOne(&artifact, "SELECT * FROM artifact WHERE bucketid = :bucketid AND name = :artifactname",
		map[string]string{"bucketid": bucketId, "artifactname": artifactName}); err != nil && !gorp.NonFatalError(err) {
		return nil, WrapInternalDatabaseError(err)
	}

	return &artifact, nil
}

func (db *GorpDatabase) GetArtifactById(artifactId int64) (*model.Artifact, *DatabaseError) {
	var artifact model.Artifact
	if err := db.dbmap.SelectOne(&artifact, "SELECT * FROM artifact WHERE artifactid = :artifactid",
		map[string]interface{}{"artifactid": artifactId}); err != nil && !gorp.NonFatalError(err) {
		return nil, WrapInternalDatabaseError(err)
	}

	return &artifact, nil
}

func (db *GorpDatabase) GetLastByteSeenForArtifact(artifactId int64) (int64, *DatabaseError) {
	if nextByteOffset, err := db.dbmap.SelectInt(
		"SELECT COALESCE(MAX(byteoffset + size), 0) as lastSeenByte FROM logchunk WHERE artifactid = :artifactid",
		map[string]interface{}{"artifactid": artifactId}); err != nil && !gorp.NonFatalError(err) {
		return 0, WrapInternalDatabaseError(err)
	} else {
		return nextByteOffset, nil
	}
}

// GetLastLogChunkSeenForArtifact returns the last full logchunk present in the database associated
// with artifact.
func (db *GorpDatabase) GetLastLogChunkSeenForArtifact(artifactID int64) (*model.LogChunk, *DatabaseError) {
	var logChunk model.LogChunk
	if err := db.dbmap.SelectOne(&logChunk, "SELECT * FROM logchunk WHERE artifactid = :artifactid ORDER BY byteoffset DESC LIMIT 1",
		map[string]interface{}{"artifactid": artifactID}); err != nil && !gorp.NonFatalError(err) {
		return nil, WrapInternalDatabaseError(err)
	}
	return &logChunk, nil
}

// Ensure GorpDatabase implements Database
var _ Database = new(GorpDatabase)
