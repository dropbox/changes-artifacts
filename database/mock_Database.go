package database

import "github.com/stretchr/testify/mock"

import "github.com/dropbox/changes-artifacts/model"
import _ "github.com/vektra/mockery"

type MockDatabase struct {
	mock.Mock
}

func (_m *MockDatabase) RegisterEntities() {
	_m.Called()
}
func (_m *MockDatabase) InsertBucket(_a0 *model.Bucket) *DatabaseError {
	ret := _m.Called(_a0)

	var r0 *DatabaseError
	if rf, ok := ret.Get(0).(func(*model.Bucket) *DatabaseError); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*DatabaseError)
		}
	}

	return r0
}
func (_m *MockDatabase) InsertArtifact(_a0 *model.Artifact) *DatabaseError {
	ret := _m.Called(_a0)

	var r0 *DatabaseError
	if rf, ok := ret.Get(0).(func(*model.Artifact) *DatabaseError); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*DatabaseError)
		}
	}

	return r0
}
func (_m *MockDatabase) InsertLogChunk(_a0 *model.LogChunk) *DatabaseError {
	ret := _m.Called(_a0)

	var r0 *DatabaseError
	if rf, ok := ret.Get(0).(func(*model.LogChunk) *DatabaseError); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*DatabaseError)
		}
	}

	return r0
}
func (_m *MockDatabase) UpdateBucket(_a0 *model.Bucket) *DatabaseError {
	ret := _m.Called(_a0)

	var r0 *DatabaseError
	if rf, ok := ret.Get(0).(func(*model.Bucket) *DatabaseError); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*DatabaseError)
		}
	}

	return r0
}
func (_m *MockDatabase) ListBuckets() ([]model.Bucket, *DatabaseError) {
	ret := _m.Called()

	var r0 []model.Bucket
	if rf, ok := ret.Get(0).(func() []model.Bucket); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]model.Bucket)
		}
	}

	var r1 *DatabaseError
	if rf, ok := ret.Get(1).(func() *DatabaseError); ok {
		r1 = rf()
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(*DatabaseError)
		}
	}

	return r0, r1
}
func (_m *MockDatabase) GetBucket(_a0 string) (*model.Bucket, *DatabaseError) {
	ret := _m.Called(_a0)

	var r0 *model.Bucket
	if rf, ok := ret.Get(0).(func(string) *model.Bucket); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*model.Bucket)
		}
	}

	var r1 *DatabaseError
	if rf, ok := ret.Get(1).(func(string) *DatabaseError); ok {
		r1 = rf(_a0)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(*DatabaseError)
		}
	}

	return r0, r1
}
func (_m *MockDatabase) ListArtifactsInBucket(_a0 string) ([]model.Artifact, *DatabaseError) {
	ret := _m.Called(_a0)

	var r0 []model.Artifact
	if rf, ok := ret.Get(0).(func(string) []model.Artifact); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]model.Artifact)
		}
	}

	var r1 *DatabaseError
	if rf, ok := ret.Get(1).(func(string) *DatabaseError); ok {
		r1 = rf(_a0)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(*DatabaseError)
		}
	}

	return r0, r1
}
func (_m *MockDatabase) UpdateArtifact(_a0 *model.Artifact) *DatabaseError {
	ret := _m.Called(_a0)

	var r0 *DatabaseError
	if rf, ok := ret.Get(0).(func(*model.Artifact) *DatabaseError); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*DatabaseError)
		}
	}

	return r0
}
func (_m *MockDatabase) ListLogChunksInArtifact(_a0 int64) ([]model.LogChunk, *DatabaseError) {
	ret := _m.Called(_a0)

	var r0 []model.LogChunk
	if rf, ok := ret.Get(0).(func(int64) []model.LogChunk); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]model.LogChunk)
		}
	}

	var r1 *DatabaseError
	if rf, ok := ret.Get(1).(func(int64) *DatabaseError); ok {
		r1 = rf(_a0)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(*DatabaseError)
		}
	}

	return r0, r1
}
func (_m *MockDatabase) DeleteLogChunksForArtifact(_a0 int64) (int64, *DatabaseError) {
	ret := _m.Called(_a0)

	var r0 int64
	if rf, ok := ret.Get(0).(func(int64) int64); ok {
		r0 = rf(_a0)
	} else {
		r0 = ret.Get(0).(int64)
	}

	var r1 *DatabaseError
	if rf, ok := ret.Get(1).(func(int64) *DatabaseError); ok {
		r1 = rf(_a0)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(*DatabaseError)
		}
	}

	return r0, r1
}
func (_m *MockDatabase) GetArtifactByName(bucket string, name string) (*model.Artifact, *DatabaseError) {
	ret := _m.Called(bucket, name)

	var r0 *model.Artifact
	if rf, ok := ret.Get(0).(func(string, string) *model.Artifact); ok {
		r0 = rf(bucket, name)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*model.Artifact)
		}
	}

	var r1 *DatabaseError
	if rf, ok := ret.Get(1).(func(string, string) *DatabaseError); ok {
		r1 = rf(bucket, name)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(*DatabaseError)
		}
	}

	return r0, r1
}
func (_m *MockDatabase) GetArtifactById(_a0 int64) (*model.Artifact, *DatabaseError) {
	ret := _m.Called(_a0)

	var r0 *model.Artifact
	if rf, ok := ret.Get(0).(func(int64) *model.Artifact); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*model.Artifact)
		}
	}

	var r1 *DatabaseError
	if rf, ok := ret.Get(1).(func(int64) *DatabaseError); ok {
		r1 = rf(_a0)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(*DatabaseError)
		}
	}

	return r0, r1
}
func (_m *MockDatabase) GetLastByteSeenForArtifact(_a0 int64) (int64, *DatabaseError) {
	ret := _m.Called(_a0)

	var r0 int64
	if rf, ok := ret.Get(0).(func(int64) int64); ok {
		r0 = rf(_a0)
	} else {
		r0 = ret.Get(0).(int64)
	}

	var r1 *DatabaseError
	if rf, ok := ret.Get(1).(func(int64) *DatabaseError); ok {
		r1 = rf(_a0)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(*DatabaseError)
		}
	}

	return r0, r1
}
func (_m *MockDatabase) GetLastLogChunkSeenForArtifact(_a0 int64) (*model.LogChunk, *DatabaseError) {
	ret := _m.Called(_a0)

	var r0 *model.LogChunk
	if rf, ok := ret.Get(0).(func(int64) *model.LogChunk); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*model.LogChunk)
		}
	}

	var r1 *DatabaseError
	if rf, ok := ret.Get(1).(func(int64) *DatabaseError); ok {
		r1 = rf(_a0)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(*DatabaseError)
		}
	}

	return r0, r1
}
