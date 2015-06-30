package model

import (
	"fmt"
	"time"
)

//go:generate stringer -type=ArtifactState
type ArtifactState uint

// NOTE: Do not reorder. Always append new entries to the bottom. Any existing entries which
// are deprecated should be renamed from FOO to DEPRECATED_FOO and left in the same position.
//
// Please remember to update StateString
const (
	UNKNOWN_ARTIFACT_STATE ArtifactState = iota

	// Error during streamed upload.
	ERROR

	// Log file being streamed in chunks. We currently store them as LogChunks.
	APPENDING

	// Once the artifact has been finalized (or the bucket closed), the artifact which was being
	// appended will be marked for compaction and upload to S3.
	APPEND_COMPLETE

	// The artifact is waiting for a file upload request to stream through to S3.
	WAITING_FOR_UPLOAD

	// If the artifact is in LogChunks, it is now being merged and uploaded to S3.
	// Else, the file is being passed through to S3 directly from the client.
	UPLOADING

	// Terminal state: the artifact is in S3 in its entirety.
	UPLOADED

	// Deadline exceeded before APPEND_COMPLETE OR UPLOADED
	DEADLINE_EXCEEDED

	// Artifact was closed without any appends or upload operation.
	CLOSED_WITHOUT_DATA
)

type Artifact struct {
	BucketId     string
	DateCreated  time.Time
	// Auto-generated globally unique id.
	Id           int64
	// id that must be unique within a bucket (but not necessairly globally).
	// For streamed artifacts this is often the file nane.
	Name         string
	// This is deterministically generated as /<BucketId>/<Name> but in case we wish to
	// switch conventions later we store it.
	S3URL        string
	Size         int64
	State        ArtifactState
	DeadlineMins uint
}

func (a *Artifact) DefaultS3URL() string {
	return fmt.Sprintf("/%s/%s", a.BucketId, a.Name)
}
