package api

import (
	"bytes"
	"fmt"
	"testing"

	"gopkg.in/amz.v1/aws"
	"gopkg.in/amz.v1/s3"
	"gopkg.in/amz.v1/s3/s3test"

	"github.com/dropbox/changes-artifacts/database"
	"github.com/dropbox/changes-artifacts/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func createS3Bucket(t *testing.T) (*s3test.Server, *s3.Bucket) {
	s3Server, err := s3test.NewServer(&s3test.Config{Send409Conflict: true})
	if err != nil {
		t.Fatalf("Error bringing up fake s3 server: %s\n", err)
	}

	t.Logf("Fake S3 server up at %s\n", s3Server.URL())

	s3Client := s3.New(aws.Auth{"abc", "123"}, aws.Region{
		Name:                 "fake-artifacts-test-region",
		S3Endpoint:           s3Server.URL(),
		S3LocationConstraint: true,
		Sign:                 aws.SignV2,
	})

	s3Bucket := s3Client.Bucket("fake-artifacts-store-bucket")
	if err := s3Bucket.PutBucket(s3.Private); err != nil {
		t.Fatalf("Error creating s3 bucket: %s\n", err)
	}

	return s3Server, s3Bucket
}

func TestCreateArtifact(t *testing.T) {
	mockdb := &database.MockDatabase{}

	var artifact *model.Artifact
	var err error

	// Create artifact with no name
	artifact, err = CreateArtifact(CreateArtifactReq{}, &model.Bucket{
		State: model.CLOSED,
	}, mockdb)
	assert.Error(t, err)
	assert.Nil(t, artifact)

	// Create artifact in closed bucket
	artifact, err = CreateArtifact(CreateArtifactReq{
		Name: "aName",
	}, &model.Bucket{
		State: model.CLOSED,
	}, mockdb)
	assert.Error(t, err)
	assert.Nil(t, artifact)

	// Duplicate artifact name
	mockdb.On("GetArtifactByName", "bName", "aName").Return(&model.Artifact{}, nil).Once()
	artifact, err = CreateArtifact(CreateArtifactReq{
		Name: "aName",
	}, &model.Bucket{
		State: model.OPEN,
		Id:    "bName",
	}, mockdb)
	assert.Error(t, err)
	assert.Nil(t, artifact)

	// ---------- BEGIN Streamed artifact creation --------------
	mockdb.On("GetArtifactByName", "bName", "aName").Return(nil, database.NewEntityNotFoundError("Notfound")).Once()
	artifact, err = CreateArtifact(CreateArtifactReq{
		Name:    "aName",
		Size:    0, // Invalid size
		Chunked: false,
	}, &model.Bucket{
		State: model.OPEN,
		Id:    "bName",
	}, mockdb)
	assert.Error(t, err)
	assert.Nil(t, artifact)

	// Fail on inserting into DB
	mockdb.On("GetArtifactByName", "bName", "aName").Return(nil, database.NewEntityNotFoundError("Notfound")).Once()
	mockdb.On("InsertArtifact", mock.AnythingOfType("*model.Artifact")).Return(database.WrapInternalDatabaseError(fmt.Errorf("Err"))).Once()
	artifact, err = CreateArtifact(CreateArtifactReq{
		Name:    "aName",
		Size:    10,
		Chunked: false,
	}, &model.Bucket{
		State: model.OPEN,
		Id:    "bName",
	}, mockdb)
	assert.Error(t, err)
	assert.Nil(t, artifact)

	// Successfully create artifact
	mockdb.On("GetArtifactByName", "bName", "aName").Return(nil, database.NewEntityNotFoundError("Notfound")).Once()
	mockdb.On("InsertArtifact", mock.AnythingOfType("*model.Artifact")).Return(nil).Once()
	artifact, err = CreateArtifact(CreateArtifactReq{
		Name:    "aName",
		Size:    10,
		Chunked: false,
	}, &model.Bucket{
		State: model.OPEN,
		Id:    "bName",
	}, mockdb)
	assert.NoError(t, err)
	assert.NotNil(t, artifact)
	assert.Equal(t, "aName", artifact.Name)
	assert.Equal(t, "bName", artifact.BucketId)
	assert.Equal(t, model.WAITING_FOR_UPLOAD, artifact.State)
	assert.Equal(t, int64(10), artifact.Size)
	// ---------- END Streamed artifact creation --------------

	// ---------- BEGIN Chunked artifact creation --------------
	// Fail on inserting into DB
	mockdb.On("GetArtifactByName", "bName", "aName").Return(nil, database.NewEntityNotFoundError("Notfound")).Once()
	mockdb.On("InsertArtifact", mock.AnythingOfType("*model.Artifact")).Return(database.WrapInternalDatabaseError(fmt.Errorf("Err"))).Once()
	artifact, err = CreateArtifact(CreateArtifactReq{
		Name:    "aName",
		Chunked: true,
	}, &model.Bucket{
		State: model.OPEN,
		Id:    "bName",
	}, mockdb)
	assert.Error(t, err)
	assert.Nil(t, artifact)

	// Successfully create artifact
	mockdb.On("GetArtifactByName", "bName", "aName").Return(nil, database.NewEntityNotFoundError("Notfound")).Once()
	mockdb.On("InsertArtifact", mock.AnythingOfType("*model.Artifact")).Return(nil).Once()
	artifact, err = CreateArtifact(CreateArtifactReq{
		Name:         "aName",
		Chunked:      true,
		DeadlineMins: 20,
	}, &model.Bucket{
		State: model.OPEN,
		Id:    "bName",
	}, mockdb)
	assert.NoError(t, err)
	assert.NotNil(t, artifact)
	assert.Equal(t, "aName", artifact.Name)
	assert.Equal(t, "bName", artifact.BucketId)
	assert.Equal(t, model.APPENDING, artifact.State)
	assert.Equal(t, int64(0), artifact.Size)
	// ---------- END Streamed artifact creation --------------

	mockdb.AssertExpectations(t)
}

func TestAppendLogChunk(t *testing.T) {
	mockdb := &database.MockDatabase{}

	// Already completed artifact
	assert.Error(t, AppendLogChunk(mockdb, &model.Artifact{
		State: model.APPEND_COMPLETE,
	}, nil))

	// Size 0 chunk
	assert.Error(t, AppendLogChunk(mockdb, &model.Artifact{
		State: model.APPENDING,
	}, &model.LogChunk{
		Size: 0,
	}))

	// Blank string chunk
	assert.Error(t, AppendLogChunk(mockdb, &model.Artifact{
		State: model.APPENDING,
	}, &model.LogChunk{
		Size:    1,
		Content: "",
	}))

	// Mismatch between size and content chunk
	assert.Error(t, AppendLogChunk(mockdb, &model.Artifact{
		State: model.APPENDING,
	}, &model.LogChunk{
		Size:    1,
		Content: "ab",
	}))

	logChunk := &model.LogChunk{
		Size:    2,
		Content: "ab",
	}

	// DB Error while trying to find where the next byte is expected
	mockdb.On("GetLastByteSeenForArtifact", int64(10)).Return(int64(0), database.MockDatabaseError()).Once()
	assert.Error(t, AppendLogChunk(mockdb, &model.Artifact{
		State: model.APPENDING,
		Id:    10,
	}, logChunk))

	mockdb.On("GetLastByteSeenForArtifact", int64(10)).Return(int64(2), nil).Once()
	assert.Error(t, AppendLogChunk(mockdb, &model.Artifact{
		State: model.APPENDING,
		Id:    10,
	}, logChunk))

	mockdb.On("GetLastByteSeenForArtifact", int64(10)).Return(int64(0), nil).Once()
	mockdb.On("UpdateArtifact", mock.AnythingOfType("*model.Artifact")).Return(database.MockDatabaseError()).Once()
	assert.Error(t, AppendLogChunk(mockdb, &model.Artifact{
		State: model.APPENDING,
		Id:    10,
	}, logChunk))

	mockdb.On("GetLastByteSeenForArtifact", int64(10)).Return(int64(0), nil).Once()
	mockdb.On("UpdateArtifact", mock.AnythingOfType("*model.Artifact")).Return(nil).Once()
	mockdb.On("InsertLogChunk", logChunk).Return(database.MockDatabaseError()).Once()
	assert.Error(t, AppendLogChunk(mockdb, &model.Artifact{
		State: model.APPENDING,
		Id:    10,
	}, logChunk))

	mockdb.On("GetLastByteSeenForArtifact", int64(10)).Return(int64(0), nil).Once()
	mockdb.On("UpdateArtifact", mock.AnythingOfType("*model.Artifact")).Return(nil).Once()
	mockdb.On("InsertLogChunk", logChunk).Return(nil).Once()
	assert.NoError(t, AppendLogChunk(mockdb, &model.Artifact{
		State: model.APPENDING,
		Id:    10,
	}, logChunk))

	mockdb.AssertExpectations(t)
}

func TestPutArtifactErrorChecks(t *testing.T) {
	// Chunked artifacts
	assert.Error(t, PutArtifact(&model.Artifact{State: model.APPENDING}, nil, nil, PutArtifactReq{}))
	assert.Error(t, PutArtifact(&model.Artifact{State: model.APPEND_COMPLETE}, nil, nil, PutArtifactReq{}))

	// Already being uploaded elsewhere
	assert.Error(t, PutArtifact(&model.Artifact{State: model.UPLOADING}, nil, nil, PutArtifactReq{}))

	// Already completed upload
	assert.Error(t, PutArtifact(&model.Artifact{State: model.UPLOADED}, nil, nil, PutArtifactReq{}))

	assert.Error(t, PutArtifact(&model.Artifact{State: model.WAITING_FOR_UPLOAD}, nil, nil, PutArtifactReq{
		ContentLength: "",
	}))

	assert.Error(t, PutArtifact(&model.Artifact{State: model.WAITING_FOR_UPLOAD}, nil, nil, PutArtifactReq{
		ContentLength: "foo",
	}))

	// Size mismatch
	assert.Error(t, PutArtifact(&model.Artifact{State: model.WAITING_FOR_UPLOAD, Size: 10}, nil, nil, PutArtifactReq{
		ContentLength: "20",
	}))
}

func TestPutArtifactToS3Successfully(t *testing.T) {
	mockdb := &database.MockDatabase{}

	// First change to UPLOADING state...
	mockdb.On("UpdateArtifact", &model.Artifact{
		State:    model.UPLOADING,
		Size:     10,
		Name:     "TestPutArtifact__artifactName",
		BucketId: "TestPutArtifact__bucketName",
	}).Return(nil).Once()

	// Then change to UPLOADED state.
	mockdb.On("UpdateArtifact", &model.Artifact{
		State:    model.UPLOADED,
		Size:     10,
		S3URL:    "/TestPutArtifact__bucketName/TestPutArtifact__artifactName",
		Name:     "TestPutArtifact__artifactName",
		BucketId: "TestPutArtifact__bucketName",
	}).Return(nil).Once()

	s3Server, s3Bucket := createS3Bucket(t)
	assert.NoError(t, PutArtifact(&model.Artifact{
		State:    model.WAITING_FOR_UPLOAD,
		Size:     10,
		Name:     "TestPutArtifact__artifactName",
		BucketId: "TestPutArtifact__bucketName",
	}, mockdb, s3Bucket, PutArtifactReq{
		ContentLength: "10",
		Body:          bytes.NewBufferString("0123456789"),
	}))

	s3Server.Quit()

	// mockdb.AssertExpectations(t)
}

func TestPutArtifactToS3WithS3Errors(t *testing.T) {
	mockdb := &database.MockDatabase{}

	// First change to UPLOADING state...
	mockdb.On("UpdateArtifact", &model.Artifact{
		State:    model.UPLOADING,
		Size:     10,
		Name:     "TestPutArtifact__artifactName",
		BucketId: "TestPutArtifact__bucketName",
	}).Return(nil).Once()

	// Then change to UPLOADED state.
	mockdb.On("UpdateArtifact", &model.Artifact{
		State:    model.ERROR,
		Size:     10,
		Name:     "TestPutArtifact__artifactName",
		BucketId: "TestPutArtifact__bucketName",
	}).Return(nil).Once()

	s3Server, s3Bucket := createS3Bucket(t)
	// Terminate the s3 server to simulate s3 errors. We don't differentiate
	// between different s3 errors. So, this should handle all cases.
	s3Server.Quit()

	assert.Error(t, PutArtifact(&model.Artifact{
		State:    model.WAITING_FOR_UPLOAD,
		Size:     10,
		Name:     "TestPutArtifact__artifactName",
		BucketId: "TestPutArtifact__bucketName",
	}, mockdb, s3Bucket, PutArtifactReq{
		ContentLength: "10",
		Body:          bytes.NewBufferString("0123456789"),
	}))

	// mockdb.AssertExpectations(t)
}

func TestMergeLogChunks(t *testing.T) {
	mockdb := &database.MockDatabase{}

	// Merging log chunks not valid in following states
	assert.Error(t, MergeLogChunks(&model.Artifact{State: model.WAITING_FOR_UPLOAD}, mockdb, nil))
	assert.Error(t, MergeLogChunks(&model.Artifact{State: model.APPENDING}, mockdb, nil))
	assert.Error(t, MergeLogChunks(&model.Artifact{State: model.UPLOADED}, mockdb, nil))
	assert.Error(t, MergeLogChunks(&model.Artifact{State: model.UPLOADING}, mockdb, nil))

	// Closing an empty artifact with no errors
	mockdb.On("UpdateArtifact", &model.Artifact{
		State: model.CLOSED_WITHOUT_DATA,
	}).Return(nil).Once()
	assert.NoError(t, MergeLogChunks(&model.Artifact{State: model.APPEND_COMPLETE, Size: 0}, mockdb, nil))

	// Closing an empty artifact with db errors
	mockdb.On("UpdateArtifact", &model.Artifact{
		State: model.CLOSED_WITHOUT_DATA,
	}).Return(database.MockDatabaseError()).Once()
	assert.Error(t, MergeLogChunks(&model.Artifact{State: model.APPEND_COMPLETE, Size: 0}, mockdb, nil))

	// ----- BEGIN Closing an artifact with some log chunks
	// DB Error while updating artifact
	mockdb.On("UpdateArtifact", &model.Artifact{
		State: model.UPLOADING,
		Size:  10,
	}).Return(database.MockDatabaseError()).Once()
	assert.Error(t, MergeLogChunks(&model.Artifact{State: model.APPEND_COMPLETE, Size: 10}, mockdb, nil))

	// DB Error while fetching logchunks
	mockdb.On("UpdateArtifact", &model.Artifact{
		Id:    2,
		State: model.UPLOADING,
		Size:  10,
	}).Return(nil).Once()
	mockdb.On("ListLogChunksInArtifact", int64(2)).Return(nil, database.MockDatabaseError()).Once()
	assert.Error(t, MergeLogChunks(&model.Artifact{Id: 2, State: model.APPEND_COMPLETE, Size: 10}, mockdb, nil))

	// Stitching chunks and uploading to S3 successfully
	mockdb.On("UpdateArtifact", &model.Artifact{
		Id:       2,
		State:    model.UPLOADING,
		Size:     10,
		Name:     "TestMergeLogChunks__artifactName",
		BucketId: "TestMergeLogChunks__bucketName",
	}).Return(nil).Once()
	mockdb.On("ListLogChunksInArtifact", int64(2)).Return([]model.LogChunk{
		model.LogChunk{Content: "01234"},
		model.LogChunk{Content: "56789"},
	}, nil).Once()
	mockdb.On("UpdateArtifact", &model.Artifact{
		Id:       2,
		State:    model.UPLOADED,
		S3URL:    "/TestMergeLogChunks__bucketName/TestMergeLogChunks__artifactName",
		Name:     "TestMergeLogChunks__artifactName",
		BucketId: "TestMergeLogChunks__bucketName",
		Size:     10,
	}).Return(nil).Once()
	s3Server, s3Bucket := createS3Bucket(t)
	assert.NoError(t, MergeLogChunks(&model.Artifact{
		Id:       2,
		State:    model.APPEND_COMPLETE,
		Size:     10,
		Name:     "TestMergeLogChunks__artifactName",
		BucketId: "TestMergeLogChunks__bucketName",
	}, mockdb, s3Bucket))
	s3Server.Quit()
	// ----- END Closing an artifact with some log chunks
}
