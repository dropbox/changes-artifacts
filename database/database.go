package database

import (
	"fmt"

	"github.com/dropbox/changes-artifacts/model"
	_ "github.com/vektra/mockery" // Required to generate MockDatabase
)

//go:generate stringer -type=DBErrorType
type DBErrorType int

const (
	INTERNAL DBErrorType = iota

	// Fields not set or invalid
	VALIDATION_FAILURE

	// Entity not found in the database
	ENTITY_NOT_FOUND
)

type DatabaseError struct {
	errStr  string
	errType DBErrorType
}

func (dbe *DatabaseError) Error() string {
	return fmt.Sprintf("DatabaseError[%s]: %s", dbe.errType, dbe.errStr)
}

func (dbe *DatabaseError) GetError() error {
	if dbe != nil {
		return fmt.Errorf(dbe.Error())
	}
	return nil
}

// We do this to ensure that DatabaseError implements Error.
var _ error = new(DatabaseError)

func MockDatabaseError() *DatabaseError {
	return &DatabaseError{errStr: "MOCK ERROR", errType: INTERNAL}
}

func WrapInternalDatabaseError(err error) *DatabaseError {
	if err == nil {
		return nil
	}

	return &DatabaseError{errStr: err.Error(), errType: INTERNAL}
}

func NewValidationError(format string, args ...interface{}) *DatabaseError {
	if format == "" {
		panic("Error formatting NewValidationError")
	}
	if len(args) > 0 {
		return &DatabaseError{errStr: fmt.Sprintf(format, args...), errType: VALIDATION_FAILURE}
	}
	return &DatabaseError{errStr: format, errType: VALIDATION_FAILURE}
}

func NewEntityNotFoundError(format string, args ...interface{}) *DatabaseError {
	if format == "" {
		panic("Error formatting NewEntityNotFoundError")
	}
	if len(args) > 0 {
		return &DatabaseError{errStr: fmt.Sprintf(format, args...), errType: ENTITY_NOT_FOUND}
	}
	return &DatabaseError{errStr: format, errType: ENTITY_NOT_FOUND}
}

func (dbe *DatabaseError) EntityNotFound() bool {
	return dbe != nil && dbe.errType == ENTITY_NOT_FOUND
}

//go:generate mockery -name=Database -inpkg
type Database interface {
	// Register all DB table<->object mappings in memory
	RegisterEntities()

	// Bucket instance is expected to have id, datecreated, state and owner field set.
	InsertBucket(*model.Bucket) *DatabaseError

	InsertArtifact(*model.Artifact) *DatabaseError

	InsertLogChunk(*model.LogChunk) *DatabaseError

	// Bucket instance is expected to have id, datecreated, state and owner field set.
	UpdateBucket(*model.Bucket) *DatabaseError

	// TODO: Pagination and/or other forms of filtering
	ListBuckets() ([]model.Bucket, *DatabaseError)

	GetBucket(string) (*model.Bucket, *DatabaseError)

	ListArtifactsInBucket(string) ([]model.Artifact, *DatabaseError)

	UpdateArtifact(*model.Artifact) *DatabaseError

	ListLogChunksInArtifact(int64) ([]model.LogChunk, *DatabaseError)

	// Delete list of log chunks, primarily used to clean up log chunks after merging and uploading
	DeleteLogChunksForArtifact(int64) (int64, *DatabaseError)

	GetArtifactByName(bucket string, name string) (*model.Artifact, *DatabaseError)

	// Get last logchunk seen for an artifact.
	GetLastLogChunkSeenForArtifact(int64) (*model.LogChunk, *DatabaseError)
}
