package api

import (
	"fmt"
	"testing"

	"github.com/dropbox/changes-artifacts/common"
	"github.com/dropbox/changes-artifacts/database"
	"github.com/dropbox/changes-artifacts/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestCreateBucket(t *testing.T) {
	mockdb := &database.MockDatabase{}

	// Used to verify creation timestamp.
	mockClock := common.NewFrozenClock()

	// var bucket *model.Bucket
	var err error

	// Bad request
	_, err = CreateBucket(mockdb, mockClock, "", "owner")
	assert.Error(t, err)

	_, err = CreateBucket(mockdb, mockClock, "id", "")
	assert.Error(t, err)

	// DB error
	mockdb.On("GetBucket", "id").Return(nil, database.WrapInternalDatabaseError(fmt.Errorf("Internal Error"))).Once()
	_, err = CreateBucket(mockdb, mockClock, "id", "owner")
	assert.Error(t, err)

	// Entity exists
	mockdb.On("GetBucket", "id").Return(&model.Bucket{}, nil).Once()
	_, err = CreateBucket(mockdb, mockClock, "id", "owner")
	assert.Error(t, err)

	// DB error while creating bucket
	mockdb.On("GetBucket", "id").Return(nil, database.NewEntityNotFoundError("ENF")).Once()
	mockdb.On("InsertBucket", mock.AnythingOfType("*model.Bucket")).Return(database.WrapInternalDatabaseError(fmt.Errorf("INT"))).Once()
	_, err = CreateBucket(mockdb, mockClock, "id", "owner")
	assert.Error(t, err)

	// Successfully created bucket
	mockdb.On("GetBucket", "id").Return(nil, database.NewEntityNotFoundError("ENF")).Once()
	mockdb.On("InsertBucket", mock.AnythingOfType("*model.Bucket")).Return(nil).Once()
	bucket, err := CreateBucket(mockdb, mockClock, "id", "owner")
	assert.NoError(t, err)
	assert.NotNil(t, bucket)

	mockdb.AssertExpectations(t)
}

func TestCloseBucket(t *testing.T) {
	mockdb := &database.MockDatabase{}

	// We're using this to verify closing timestamp.
	mockClock := common.NewFrozenClock()

	// If bucket is not currently open, return failure
	bucket := &model.Bucket{State: model.CLOSED}
	assert.Error(t, CloseBucket(bucket, mockdb, nil, nil))

	bucket_id := "bucket_id_1"

	// If DB throws error in any step, return failure
	bucket = &model.Bucket{State: model.OPEN, Id: bucket_id}
	mockdb.On("UpdateBucket", bucket).Return(database.WrapInternalDatabaseError(fmt.Errorf("foo"))).Once()
	assert.Error(t, CloseBucket(bucket, mockdb, nil, mockClock))

	bucket = &model.Bucket{State: model.OPEN, Id: bucket_id}
	mockdb.On("UpdateBucket", bucket).Return(nil).Once()
	mockdb.On("ListArtifactsInBucket", bucket.Id).Return(nil, database.WrapInternalDatabaseError(fmt.Errorf("err"))).Once()
	assert.Error(t, CloseBucket(bucket, mockdb, nil, mockClock))

	// Closing bucket with no artifacts successfully. Verify bucket state and dateclosed.
	bucket = &model.Bucket{State: model.OPEN, Id: bucket_id}
	mockdb.On("UpdateBucket", bucket).Return(nil).Once()
	mockdb.On("ListArtifactsInBucket", bucket.Id).Return([]model.Artifact{}, nil).Once()
	assert.NoError(t, CloseBucket(bucket, mockdb, nil, mockClock))
	assert.Equal(t, model.CLOSED, bucket.State)
	assert.Equal(t, mockClock.Now(), bucket.DateClosed)

	// Closing bucket with no artifacts successfully. Verify bucket state and dateclosed.
	bucket = &model.Bucket{State: model.OPEN, Id: bucket_id}
	artifact := model.Artifact{Id: 20, State: model.UPLOADED}
	mockdb.On("UpdateBucket", bucket).Return(nil).Once()
	mockdb.On("ListArtifactsInBucket", bucket.Id).Return([]model.Artifact{artifact}, nil).Once()

	assert.NoError(t, CloseBucket(bucket, mockdb, nil, mockClock))
	assert.Equal(t, model.CLOSED, bucket.State)
	assert.Equal(t, mockClock.Now(), bucket.DateClosed)

	mockdb.AssertExpectations(t)
}
