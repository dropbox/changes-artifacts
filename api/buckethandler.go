package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dropbox/changes-artifacts/common"
	"github.com/dropbox/changes-artifacts/database"
	"github.com/dropbox/changes-artifacts/model"
	"github.com/martini-contrib/render"
	"gopkg.in/amz.v1/s3"
)

type HttpError struct {
	errCode int
	errStr  string
}

func (he *HttpError) Error() string {
	return he.errStr
}

func NewHttpError(code int, format string, args ...interface{}) *HttpError {
	return &HttpError{errCode: code, errStr: fmt.Sprintf(format, args...)}
}

func NewWrappedHttpError(code int, err error) *HttpError {
	return &HttpError{errCode: code, errStr: err.Error()}
}

// Ensure that HttpError implements error
var _ error = new(HttpError)

func ListBuckets(r render.Render, db database.Database) {
	if buckets, err := db.ListBuckets(); err != nil {
		JsonErrorf(r, http.StatusBadRequest, err.Error())
	} else {
		r.JSON(http.StatusOK, buckets)
	}
}

func CreateBucket(db database.Database, clk common.Clock, bucketId string, owner string) (*model.Bucket, *HttpError) {
	if bucketId == "" {
		return nil, NewHttpError(http.StatusBadRequest, "Bucket ID not provided")
	}

	if len(owner) == 0 {
		return nil, NewHttpError(http.StatusBadRequest, "Bucket Owner not provided")
	}

	_, err := db.GetBucket(bucketId)
	if err != nil && !err.EntityNotFound() {
		return nil, NewWrappedHttpError(http.StatusInternalServerError, err)
	}
	if err == nil {
		return nil, NewHttpError(http.StatusBadRequest, "Entity exists")
	}

	var bucket model.Bucket
	bucket.Id = bucketId
	bucket.DateCreated = clk.Now()
	bucket.State = model.OPEN
	bucket.Owner = owner
	if err := db.InsertBucket(&bucket); err != nil {
		return nil, NewWrappedHttpError(http.StatusBadRequest, err)
	}
	return &bucket, nil
}

func HandleCreateBucket(r render.Render, req *http.Request, db database.Database, clk common.Clock) {
	var createBucketReq struct {
		ID    string
		Owner string
	}

	if err := json.NewDecoder(req.Body).Decode(&createBucketReq); err != nil {
		JsonErrorf(r, http.StatusBadRequest, "Malformed JSON request")
		return
	}

	if bucket, err := CreateBucket(db, clk, createBucketReq.ID, createBucketReq.Owner); err != nil {
		r.JSON(err.errCode, map[string]string{"error": err.errStr})
	} else {
		r.JSON(http.StatusOK, bucket)
	}
}

func HandleGetBucket(r render.Render, bucket *model.Bucket) {
	if bucket == nil {
		JsonErrorf(r, http.StatusBadRequest, "Error: no bucket specified")
		return
	}

	r.JSON(http.StatusOK, bucket)
}

func HandleCloseBucket(r render.Render, db database.Database, bucket *model.Bucket, s3Bucket *s3.Bucket, clk common.Clock) {
	if bucket == nil {
		JsonErrorf(r, http.StatusBadRequest, "Error: no bucket specified")
		return
	}

	if err := CloseBucket(bucket, db, s3Bucket, clk); err != nil {
		r.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("Error while closing bucket: %s", err)})
	} else {
		r.JSON(http.StatusOK, bucket)
	}
	return
}

func CloseBucket(bucket *model.Bucket, db database.Database, s3Bucket *s3.Bucket, clk common.Clock) error {
	if bucket.State != model.OPEN {
		return fmt.Errorf("Bucket is already closed")
	}

	bucket.State = model.CLOSED
	bucket.DateClosed = clk.Now()
	if err := db.UpdateBucket(bucket); err != nil {
		return err
	}

	if artifacts, err := db.ListArtifactsInBucket(bucket.Id); err != nil {
		return err
	} else {
		for _, artifact := range artifacts {
			if err := CloseArtifact(&artifact, db, s3Bucket, false); err != nil {
				return err
			}
		}
	}

	return nil
}
