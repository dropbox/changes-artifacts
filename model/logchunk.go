package model

type LogChunk struct {
	// Automatically-generated unique id.
	Id           int64
	ArtifactId   int64
	ByteOffset   int64
	Size         int64
	ContentBytes []byte `db:"content_bytes"`
}
