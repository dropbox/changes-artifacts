package model

import "time"

//go:generate stringer -type=BucketState
type BucketState uint

// Please remember to update the mapping to strings.
const (
	// A Bucket should never be in this state.
	UNKNOWN BucketState = iota

	// Accepting new artifacts and appends to existing artifacts
	OPEN

	// No further changes to this bucket. No new artifacts or appends to existing ones.
	CLOSED

	// Similar to `CLOSED`. Was forcibly closed because it was not explicitly closed before deadline.
	// TODO This isn't implemented yet. Implement it.
	TIMEDOUT
)

type Bucket struct {
	DateClosed  time.Time `json:"dateClosed"`
	DateCreated time.Time `json:"dateCreated"`
	// Must be globally unique even between different owners. Other than that, it can
	// be arbitrary.
	Id string `json:"id"`
	// A characteristic string signifying what service owns the bucket.
	Owner string      `json:"owner"`
	State BucketState `json:"state"`
}
