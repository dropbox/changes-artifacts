package database

import "github.com/stretchr/testify/mock"

import "github.com/dropbox/changes-artifacts/model"

type MockDatabase struct {
	mock.Mock
}

func (m *MockDatabase) RegisterEntities() {
	m.Called()
}
func (m *MockDatabase) CreateEntities() *DatabaseError {
	ret := m.Called()

	var r0 *DatabaseError
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*DatabaseError)
	}

	return r0
}
func (m *MockDatabase) RecreateTables() *DatabaseError {
	ret := m.Called()

	var r0 *DatabaseError
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*DatabaseError)
	}

	return r0
}
func (m *MockDatabase) InsertBucket(_a0 *model.Bucket) *DatabaseError {
	ret := m.Called(_a0)

	var r0 *DatabaseError
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*DatabaseError)
	}

	return r0
}
func (m *MockDatabase) InsertArtifact(_a0 *model.Artifact) *DatabaseError {
	ret := m.Called(_a0)

	var r0 *DatabaseError
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*DatabaseError)
	}

	return r0
}
func (m *MockDatabase) InsertLogChunk(_a0 *model.LogChunk) *DatabaseError {
	ret := m.Called(_a0)

	var r0 *DatabaseError
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*DatabaseError)
	}

	return r0
}
func (m *MockDatabase) UpdateBucket(_a0 *model.Bucket) *DatabaseError {
	ret := m.Called(_a0)

	var r0 *DatabaseError
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*DatabaseError)
	}

	return r0
}
func (m *MockDatabase) ListBuckets() ([]model.Bucket, *DatabaseError) {
	ret := m.Called()

	var r0 []model.Bucket
	if ret.Get(0) != nil {
		r0 = ret.Get(0).([]model.Bucket)
	}
	var r1 *DatabaseError
	if ret.Get(1) != nil {
		r1 = ret.Get(1).(*DatabaseError)
	}

	return r0, r1
}
func (m *MockDatabase) GetBucket(_a0 string) (*model.Bucket, *DatabaseError) {
	ret := m.Called(_a0)

	var r0 *model.Bucket
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*model.Bucket)
	}
	var r1 *DatabaseError
	if ret.Get(1) != nil {
		r1 = ret.Get(1).(*DatabaseError)
	}

	return r0, r1
}
func (m *MockDatabase) ListArtifactsInBucket(_a0 string) ([]model.Artifact, *DatabaseError) {
	ret := m.Called(_a0)

	var r0 []model.Artifact
	if ret.Get(0) != nil {
		r0 = ret.Get(0).([]model.Artifact)
	}
	var r1 *DatabaseError
	if ret.Get(1) != nil {
		r1 = ret.Get(1).(*DatabaseError)
	}

	return r0, r1
}
func (m *MockDatabase) UpdateArtifact(_a0 *model.Artifact) *DatabaseError {
	ret := m.Called(_a0)

	var r0 *DatabaseError
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*DatabaseError)
	}

	return r0
}
func (m *MockDatabase) ListLogChunksInArtifact(_a0 int64) ([]model.LogChunk, *DatabaseError) {
	ret := m.Called(_a0)

	var r0 []model.LogChunk
	if ret.Get(0) != nil {
		r0 = ret.Get(0).([]model.LogChunk)
	}
	var r1 *DatabaseError
	if ret.Get(1) != nil {
		r1 = ret.Get(1).(*DatabaseError)
	}

	return r0, r1
}
func (m *MockDatabase) GetArtifactByName(_a0 string, _a1 string) (*model.Artifact, *DatabaseError) {
	ret := m.Called(_a0, _a1)

	var r0 *model.Artifact
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*model.Artifact)
	}
	var r1 *DatabaseError
	if ret.Get(1) != nil {
		r1 = ret.Get(1).(*DatabaseError)
	}

	return r0, r1
}
func (m *MockDatabase) GetArtifactById(_a0 int64) (*model.Artifact, *DatabaseError) {
	ret := m.Called(_a0)

	var r0 *model.Artifact
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*model.Artifact)
	}
	var r1 *DatabaseError
	if ret.Get(1) != nil {
		r1 = ret.Get(1).(*DatabaseError)
	}

	return r0, r1
}
func (m *MockDatabase) GetLastByteSeenForArtifact(_a0 int64) (int64, *DatabaseError) {
	ret := m.Called(_a0)

	r0 := ret.Get(0).(int64)
	var r1 *DatabaseError
	if ret.Get(1) != nil {
		r1 = ret.Get(1).(*DatabaseError)
	}

	return r0, r1
}
