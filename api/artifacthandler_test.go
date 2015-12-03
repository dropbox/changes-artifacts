package api

import (
	"bytes"
	"fmt"
	"net/http"
	"testing"

	"golang.org/x/net/context"

	"gopkg.in/amz.v1/aws"
	"gopkg.in/amz.v1/s3"
	"gopkg.in/amz.v1/s3/s3test"

	"github.com/dropbox/changes-artifacts/common/sentry"
	"github.com/dropbox/changes-artifacts/database"
	"github.com/dropbox/changes-artifacts/model"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func createS3Bucket(t *testing.T) (*s3test.Server, *s3.Bucket) {
	s3Server, err := s3test.NewServer(&s3test.Config{Send409Conflict: true})
	if err != nil {
		t.Fatalf("Error bringing up fake s3 server: %s\n", err)
	}

	t.Logf("Fake S3 server up at %s\n", s3Server.URL())

	s3Client := s3.New(aws.Auth{AccessKey: "abc", SecretKey: "123"}, aws.Region{
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

	{
		// Create artifact with no name
		artifact, err := CreateArtifact(createArtifactReq{}, &model.Bucket{
			State: model.CLOSED,
		}, mockdb)
		require.Nil(t, artifact)
		require.Error(t, err)
	}

	{
		// Create artifact in closed bucket
		artifact, err := CreateArtifact(createArtifactReq{
			Name: "aName",
		}, &model.Bucket{
			State: model.CLOSED,
		}, mockdb)
		require.Nil(t, artifact)
		require.Error(t, err)
	}

	// ---------- BEGIN Streamed artifact creation --------------
	{
		artifact, err := CreateArtifact(createArtifactReq{
			Name:    "aName",
			Size:    0, // Invalid size
			Chunked: false,
		}, &model.Bucket{
			State: model.OPEN,
			Id:    "bName",
		}, mockdb)
		require.Nil(t, artifact)
		require.Error(t, err)
	}

	{
		artifact, err := CreateArtifact(createArtifactReq{
			Name:    "aName",
			Size:    MaxArtifactSizeBytes + 1, // Invalid size
			Chunked: false,
		}, &model.Bucket{
			State: model.OPEN,
			Id:    "bName",
		}, mockdb)
		require.Nil(t, artifact)
		require.Error(t, err)
	}

	{
		// Fail inserting into DB once and also fail getting artifact
		mockdb.On("InsertArtifact", mock.AnythingOfType("*model.Artifact")).Return(database.WrapInternalDatabaseError(fmt.Errorf("Err"))).Once()
		mockdb.On("GetArtifactByName", "bName", "aName").Return(nil, database.WrapInternalDatabaseError(fmt.Errorf("Err"))).Once()
		artifact, err := CreateArtifact(createArtifactReq{
			Name:    "aName",
			Size:    10,
			Chunked: false,
		}, &model.Bucket{
			State: model.OPEN,
			Id:    "bName",
		}, mockdb)
		require.Nil(t, artifact)
		require.Error(t, err)
		require.Equal(t, http.StatusInternalServerError, err.errCode)
		mockdb.AssertExpectations(t)
	}

	{
		// Successfully create artifact
		mockdb.On("InsertArtifact", mock.AnythingOfType("*model.Artifact")).Return(nil).Once()
		artifact, err := CreateArtifact(createArtifactReq{
			Name:    "aName",
			Size:    10,
			Chunked: false,
		}, &model.Bucket{
			State: model.OPEN,
			Id:    "bName",
		}, mockdb)
		require.NoError(t, err)
		require.NotNil(t, artifact)
		require.Equal(t, "aName", artifact.Name)
		require.Equal(t, "bName", artifact.BucketId)
		require.Equal(t, model.WAITING_FOR_UPLOAD, artifact.State)
		require.Equal(t, int64(10), artifact.Size)
		mockdb.AssertExpectations(t)
	}
	// ---------- END Streamed artifact creation --------------

	// ---------- BEGIN Chunked artifact creation --------------
	{
		// Fail on inserting into DB
		mockdb.On("InsertArtifact", mock.AnythingOfType("*model.Artifact")).Return(database.WrapInternalDatabaseError(fmt.Errorf("Err"))).Once()
		mockdb.On("GetArtifactByName", "bName", "aName").Return(nil, database.WrapInternalDatabaseError(fmt.Errorf("Err"))).Once()
		artifact, err := CreateArtifact(createArtifactReq{
			Name:    "aName",
			Chunked: true,
		}, &model.Bucket{
			State: model.OPEN,
			Id:    "bName",
		}, mockdb)
		require.Error(t, err)
		require.Nil(t, artifact)
		mockdb.AssertExpectations(t)
	}

	{
		// Successfully create artifact
		mockdb.On("InsertArtifact", mock.AnythingOfType("*model.Artifact")).Return(nil).Once()
		artifact, err := CreateArtifact(createArtifactReq{
			Name:         "aName",
			Chunked:      true,
			DeadlineMins: 20,
		}, &model.Bucket{
			State: model.OPEN,
			Id:    "bName",
		}, mockdb)
		require.NoError(t, err)
		require.NotNil(t, artifact)
		require.Equal(t, "aName", artifact.Name)
		require.Equal(t, "bName", artifact.BucketId)
		require.Equal(t, model.APPENDING, artifact.State)
		require.Equal(t, int64(0), artifact.Size)
		mockdb.AssertExpectations(t)
	}
	// ---------- END Streamed artifact creation --------------

	// ---------- BEGIN Duplicate artifact name ---------------
	{
		mockdb.On("InsertArtifact", mock.AnythingOfType("*model.Artifact")).Return(database.MockDatabaseError()).Once()
		mockdb.On("GetArtifactByName", "bName", "aName").Return(&model.Artifact{}, nil).Once()
		mockdb.On("InsertArtifact", mock.AnythingOfType("*model.Artifact")).Return(nil).Once()
		artifact, err := CreateArtifact(createArtifactReq{
			Name:         "aName",
			Chunked:      true,
			DeadlineMins: 20,
		}, &model.Bucket{
			State: model.OPEN,
			Id:    "bName",
		}, mockdb)
		require.NoError(t, err)
		require.NotNil(t, artifact)
		// "Random" string below is deterministically produced because we use a deterministic seed
		// during unit tests. (https://golang.org/pkg/math/rand/#Seed)
		require.Equal(t, "aName.dup.rfBd5", artifact.Name)
		require.Equal(t, "bName", artifact.BucketId)
		require.Equal(t, model.APPENDING, artifact.State)
		require.Equal(t, int64(0), artifact.Size)
		mockdb.AssertExpectations(t)
	}

	{
		mockdb.On("InsertArtifact", mock.AnythingOfType("*model.Artifact")).Return(database.MockDatabaseError()).Times(6)
		mockdb.On("GetArtifactByName", "bName", mock.Anything).Return(&model.Artifact{}, nil).Times(5)
		artifact, err := CreateArtifact(createArtifactReq{
			Name:         "aName",
			Chunked:      true,
			DeadlineMins: 20,
		}, &model.Bucket{
			State: model.OPEN,
			Id:    "bName",
		}, mockdb)
		require.Nil(t, artifact)
		require.Error(t, err)
		mockdb.AssertExpectations(t)
	}
	// ---------- END Duplicate artifact name -----------------
}

func getExpectedLogChunkToBeWritten() *model.LogChunk {
	return &model.LogChunk{
		Size:         2,
		ArtifactId:   10,
		ContentBytes: []byte("ab"),
	}
}

func getLogChunkToAppend() *createLogChunkReq {
	return &createLogChunkReq{
		Size:       2,
		Bytes:      []byte("ab"),
		ByteOffset: 0,
	}
}

func TestAppendLogChunk(t *testing.T) {
	mockdb := &database.MockDatabase{}

	// Already completed artifact
	require.Error(t, AppendLogChunk(context.Background(), mockdb, &model.Artifact{
		State: model.APPEND_COMPLETE,
	}, nil))

	// Size 0 chunk
	require.Error(t, AppendLogChunk(context.Background(), mockdb, &model.Artifact{
		State: model.APPENDING,
	}, &createLogChunkReq{
		Size: 0,
	}))

	// Blank string chunk
	require.Error(t, AppendLogChunk(context.Background(), mockdb, &model.Artifact{
		State: model.APPENDING,
	}, &createLogChunkReq{
		Size:    1,
		Content: "",
	}))

	// Mismatch between size and content chunk
	require.Error(t, AppendLogChunk(context.Background(), mockdb, &model.Artifact{
		State: model.APPENDING,
	}, &createLogChunkReq{
		Content: "ab",
		Size:    1,
	}))

	// Last logchunk was repeated.
	mockdb.On("GetLastLogChunkSeenForArtifact", int64(10)).Return(&model.LogChunk{Size: 2, ContentBytes: []byte("ab")}, nil).Once()
	require.NoError(t, AppendLogChunk(context.Background(), mockdb, &model.Artifact{
		State: model.APPENDING,
		Id:    10,
		Size:  2,
	}, getLogChunkToAppend()))

	// Unexpected logchunk
	mockdb.On("GetLastLogChunkSeenForArtifact", int64(10)).Return(&model.LogChunk{Size: 3}, nil).Once()
	require.Error(t, AppendLogChunk(context.Background(), mockdb, &model.Artifact{
		State: model.APPENDING,
		Id:    10,
		Size:  2,
	}, getLogChunkToAppend()))

	mockdb.On("UpdateArtifact", mock.AnythingOfType("*model.Artifact")).Return(database.MockDatabaseError()).Once()
	require.Error(t, AppendLogChunk(context.Background(), mockdb, &model.Artifact{
		State: model.APPENDING,
		Id:    10,
		Size:  0,
	}, getLogChunkToAppend()))

	mockdb.On("UpdateArtifact", mock.AnythingOfType("*model.Artifact")).Return(nil).Once()
	mockdb.On("InsertLogChunk", getExpectedLogChunkToBeWritten()).Return(database.MockDatabaseError()).Once()
	require.Error(t, AppendLogChunk(context.Background(), mockdb, &model.Artifact{
		State: model.APPENDING,
		Id:    10,
		Size:  0,
	}, getLogChunkToAppend()))

	mockdb.On("UpdateArtifact", mock.AnythingOfType("*model.Artifact")).Return(nil).Once()
	mockdb.On("InsertLogChunk", getExpectedLogChunkToBeWritten()).Return(nil).Once()
	require.NoError(t, AppendLogChunk(context.Background(), mockdb, &model.Artifact{
		State: model.APPENDING,
		Id:    10,
		Size:  0,
	}, getLogChunkToAppend()))

	mockdb.AssertExpectations(t)
}

func TestPutArtifactErrorChecks(t *testing.T) {
	// Chunked artifacts
	require.Error(t, PutArtifact(context.Background(), &model.Artifact{State: model.APPENDING}, nil, nil, PutArtifactReq{}))
	require.Error(t, PutArtifact(context.Background(), &model.Artifact{State: model.APPEND_COMPLETE}, nil, nil, PutArtifactReq{}))

	// Already being uploaded elsewhere
	require.Error(t, PutArtifact(context.Background(), &model.Artifact{State: model.UPLOADING}, nil, nil, PutArtifactReq{}))

	// Already completed upload
	require.Error(t, PutArtifact(context.Background(), &model.Artifact{State: model.UPLOADED}, nil, nil, PutArtifactReq{}))

	require.Error(t, PutArtifact(context.Background(), &model.Artifact{State: model.WAITING_FOR_UPLOAD}, nil, nil, PutArtifactReq{
		ContentLength: "",
	}))

	require.Error(t, PutArtifact(context.Background(), &model.Artifact{State: model.WAITING_FOR_UPLOAD}, nil, nil, PutArtifactReq{
		ContentLength: "foo",
	}))

	// Size mismatch
	require.Error(t, PutArtifact(context.Background(), &model.Artifact{State: model.WAITING_FOR_UPLOAD, Size: 10}, nil, nil, PutArtifactReq{
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
	require.NoError(t, PutArtifact(context.Background(), &model.Artifact{
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

	require.Error(t, PutArtifact(context.Background(), &model.Artifact{
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
	require.Error(t, MergeLogChunks(nil, &model.Artifact{State: model.WAITING_FOR_UPLOAD}, mockdb, nil))
	require.Error(t, MergeLogChunks(nil, &model.Artifact{State: model.APPENDING}, mockdb, nil))
	require.Error(t, MergeLogChunks(nil, &model.Artifact{State: model.UPLOADED}, mockdb, nil))
	require.Error(t, MergeLogChunks(nil, &model.Artifact{State: model.UPLOADING}, mockdb, nil))

	// Closing an empty artifact with no errors
	mockdb.On("UpdateArtifact", &model.Artifact{
		State: model.CLOSED_WITHOUT_DATA,
	}).Return(nil).Once()
	require.NoError(t, MergeLogChunks(nil, &model.Artifact{State: model.APPEND_COMPLETE, Size: 0}, mockdb, nil))

	// Closing an empty artifact with db errors
	mockdb.On("UpdateArtifact", &model.Artifact{
		State: model.CLOSED_WITHOUT_DATA,
	}).Return(database.MockDatabaseError()).Once()
	require.Error(t, MergeLogChunks(nil, &model.Artifact{State: model.APPEND_COMPLETE, Size: 0}, mockdb, nil))

	// ----- BEGIN Closing an artifact with some log chunks
	// DB Error while updating artifact
	mockdb.On("UpdateArtifact", &model.Artifact{
		State: model.UPLOADING,
		Size:  10,
	}).Return(database.MockDatabaseError()).Once()
	require.Error(t, MergeLogChunks(nil, &model.Artifact{State: model.APPEND_COMPLETE, Size: 10}, mockdb, nil))

	{
		// DB Error while fetching logchunks
		mockdb.On("UpdateArtifact", &model.Artifact{
			Id:    2,
			State: model.UPLOADING,
			Size:  10,
		}).Return(nil).Once()
		mockdb.On("ListLogChunksInArtifact", int64(2), int64(0), int64(10)).Return(nil, database.MockDatabaseError()).Once()
		s3Server, s3Bucket := createS3Bucket(t)
		require.Error(t, MergeLogChunks(nil, &model.Artifact{Id: 2, State: model.APPEND_COMPLETE, Size: 10}, mockdb, s3Bucket))
		s3Server.Quit()
	}

	{
		// Stitching chunks succeeds, but uploading to S3 fails
		mockdb.On("UpdateArtifact", &model.Artifact{
			Id:       2,
			State:    model.UPLOADING,
			Size:     10,
			Name:     "TestMergeLogChunks__artifactName",
			BucketId: "TestMergeLogChunks__bucketName",
		}).Return(nil).Once()
		mockdb.On("ListLogChunksInArtifact", int64(2), int64(0), int64(10)).Return([]model.LogChunk{
			model.LogChunk{ByteOffset: 0, Size: 5, ContentBytes: []byte("01234")},
			model.LogChunk{ByteOffset: 5, Size: 5, ContentBytes: []byte("56789")},
		}, nil).Once()
		s3Server, s3Bucket := createS3Bucket(t)
		s3Server.Quit()
		require.Error(t, MergeLogChunks(sentry.CreateAndInstallSentryClient(context.TODO(), "", ""),
			&model.Artifact{
				Id:       2,
				State:    model.APPEND_COMPLETE,
				Size:     10,
				Name:     "TestMergeLogChunks__artifactName",
				BucketId: "TestMergeLogChunks__bucketName",
			}, mockdb, s3Bucket))
	}

	// Stitching chunks and uploading to S3 successfully (but deleting logchunks fail)
	mockdb = &database.MockDatabase{}
	mockdb.On("UpdateArtifact", &model.Artifact{
		Id:       2,
		State:    model.UPLOADING,
		Size:     10,
		Name:     "TestMergeLogChunks__artifactName",
		BucketId: "TestMergeLogChunks__bucketName",
	}).Return(nil).Once()
	mockdb.On("ListLogChunksInArtifact", int64(2), int64(0), int64(10)).Return([]model.LogChunk{
		model.LogChunk{ByteOffset: 0, Size: 5, ContentBytes: []byte("01234")},
		model.LogChunk{ByteOffset: 5, Size: 5, ContentBytes: []byte("56789")},
	}, nil).Once()
	mockdb.On("UpdateArtifact", &model.Artifact{
		Id:       2,
		State:    model.UPLOADED,
		S3URL:    "/TestMergeLogChunks__bucketName/TestMergeLogChunks__artifactName",
		Name:     "TestMergeLogChunks__artifactName",
		BucketId: "TestMergeLogChunks__bucketName",
		Size:     10,
	}).Return(nil).Once()
	mockdb.On("DeleteLogChunksForArtifact", int64(2)).Return(int64(0), database.MockDatabaseError()).Once()
	s3Server, s3Bucket := createS3Bucket(t)
	require.NoError(t, MergeLogChunks(sentry.CreateAndInstallSentryClient(context.TODO(), "", ""),
		&model.Artifact{
			Id:       2,
			State:    model.APPEND_COMPLETE,
			Size:     10,
			Name:     "TestMergeLogChunks__artifactName",
			BucketId: "TestMergeLogChunks__bucketName",
		}, mockdb, s3Bucket))
	s3Server.Quit()

	// Stitching chunks and uploading to S3 successfully
	mockdb.On("UpdateArtifact", &model.Artifact{
		Id:       3,
		State:    model.UPLOADING,
		Size:     10,
		Name:     "TestMergeLogChunks__artifactName",
		BucketId: "TestMergeLogChunks__bucketName",
	}).Return(nil).Once()
	mockdb.On("ListLogChunksInArtifact", int64(3), int64(0), int64(10)).Return([]model.LogChunk{
		model.LogChunk{ByteOffset: 0, Size: 5, ContentBytes: []byte("01234")},
		model.LogChunk{ByteOffset: 5, Size: 5, ContentBytes: []byte("56789")},
	}, nil).Once()
	mockdb.On("DeleteLogChunksForArtifact", int64(3)).Return(int64(2), nil).Once()
	mockdb.On("UpdateArtifact", &model.Artifact{
		Id:       3,
		State:    model.UPLOADED,
		S3URL:    "/TestMergeLogChunks__bucketName/TestMergeLogChunks__artifactName",
		Name:     "TestMergeLogChunks__artifactName",
		BucketId: "TestMergeLogChunks__bucketName",
		Size:     10,
	}).Return(nil).Once()
	s3Server, s3Bucket = createS3Bucket(t)
	require.NoError(t, MergeLogChunks(nil, &model.Artifact{
		Id:       3,
		State:    model.APPEND_COMPLETE,
		Size:     10,
		Name:     "TestMergeLogChunks__artifactName",
		BucketId: "TestMergeLogChunks__bucketName",
	}, mockdb, s3Bucket))
	s3Server.Quit()
	// ----- END Closing an artifact with some log chunks
}
