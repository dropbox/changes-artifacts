package client

import (
	"bytes"
	"database/sql"
	"fmt"
	"net/http"
	"testing"
	"time"

	"gopkg.in/gorp.v1"

	"github.com/dropbox/changes-artifacts/database"
	"github.com/dropbox/changes-artifacts/model"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
)

func setup(t *testing.T) *ArtifactStoreClient {
	url := "http://localhost:3000"

	// Always wait for server to be running first, before setting up the DB.
	// Otherwise, our surgery with the DB will interfere with any setup being done on the server
	// during startup.
	waitForServer(t, url)
	setupDB(t)

	return NewArtifactStoreClient(url)
}

func waitForServer(t *testing.T, url string) {
	retries := 1000

	for retries > 0 {
		if _, err := http.Get(url); err != nil {
			retries--
			time.Sleep(10 * time.Millisecond)
		} else {
			fmt.Println("*********** SERVER ONLINE ***********")
			return
		}
	}

	t.Fatalf("Artifacts server did not come up in time")
}

func setupDB(t *testing.T) {
	DB_STR := "postgres://artifacts:artifacts@artifactsdb/artifacts?sslmode=disable"

	db, err := sql.Open("postgres", DB_STR)
	if err != nil {
		t.Fatalf("Error connecting to Postgres: %s", err)
	}

	dbmap := &gorp.DbMap{Db: db, Dialect: gorp.PostgresDialect{}}
	gdb := database.NewGorpDatabase(dbmap)
	gdb.RegisterEntities()

	if err := gdb.RecreateTables(); err != nil {
		t.Fatalf("Error resetting Postgres DB: %s", err)
	}
	fmt.Println("************* DB RESET **************")
}

func TestCreateAndGetBucket(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping end-to-end test in short mode.")
	}

	client := setup(t)

	var bucket *Bucket
	var err error

	bucket, err = client.NewBucket("", "ownername", 31)
	assert.Nil(t, bucket)
	assert.Error(t, err)

	bucket, err = client.NewBucket("bucketname", "", 31)
	assert.Nil(t, bucket)
	assert.Error(t, err)

	bucket, err = client.NewBucket("bucketname", "ownername", 31)
	assert.NotNil(t, bucket)
	assert.Equal(t, "bucketname", bucket.bucket.Id)
	assert.Equal(t, "ownername", bucket.bucket.Owner)
	assert.Equal(t, model.OPEN, bucket.bucket.State)
	assert.NoError(t, err)

	// Duplicate
	bucket, err = client.NewBucket("bucketname", "ownername", 31)
	assert.Nil(t, bucket)
	assert.Error(t, err)

	bucket, err = client.GetBucket("bucketname")
	assert.NotNil(t, bucket)
	assert.Equal(t, "bucketname", bucket.bucket.Id)
	assert.Equal(t, "ownername", bucket.bucket.Owner)
	assert.Equal(t, model.OPEN, bucket.bucket.State)
	assert.NoError(t, err)

	bucket, err = client.GetBucket("bucketnotfound")
	assert.Nil(t, bucket)
	assert.Error(t, err)
}

func TestCreateAndGetChunkedArtifact(t *testing.T) {
	bucketName := "bucketName"
	ownerName := "ownerName"
	artifactName := "artifactName"

	if testing.Short() {
		t.Skip("Skipping end-to-end test in short mode.")
	}

	client := setup(t)

	var bucket *Bucket
	var artifact Artifact
	var err error

	bucket, err = client.NewBucket(bucketName, ownerName, 31)
	assert.NotNil(t, bucket)
	assert.NoError(t, err)

	artifact, err = bucket.NewChunkedArtifact(artifactName)
	assert.NotNil(t, artifact)
	assert.Equal(t, artifactName, artifact.GetArtifactModel().Name)
	assert.Equal(t, model.APPENDING, artifact.GetArtifactModel().State)
	// assert.Equal will crib if the types are not identical.
	assert.Equal(t, int64(0), artifact.GetArtifactModel().Size)
	assert.Equal(t, bucketName, artifact.GetArtifactModel().BucketId)
	assert.Empty(t, artifact.GetArtifactModel().S3URL)
	assert.NoError(t, err)

	// Duplicate
	artifact, err = bucket.NewChunkedArtifact(artifactName)
	assert.Nil(t, artifact)
	assert.Error(t, err)

	artifact, err = bucket.GetArtifact(artifactName)
	assert.NotNil(t, artifact)
	assert.Equal(t, artifactName, artifact.GetArtifactModel().Name)
	assert.Equal(t, model.APPENDING, artifact.GetArtifactModel().State)
	// assert.Equal will crib if the types are not identical.
	assert.Equal(t, int64(0), artifact.GetArtifactModel().Size)
	assert.Equal(t, bucketName, artifact.GetArtifactModel().BucketId)
	assert.Empty(t, artifact.GetArtifactModel().S3URL)
	assert.NoError(t, err)
}

func TestCreateAndGetStreamedArtifact(t *testing.T) {
	bucketName := "bucketName"
	ownerName := "ownerName"
	artifactName := "artifactName"
	fileSize := int64(100)

	if testing.Short() {
		t.Skip("Skipping end-to-end test in short mode.")
	}

	client := setup(t)

	var bucket *Bucket
	var artifact Artifact
	var err error

	bucket, err = client.NewBucket(bucketName, ownerName, 31)
	assert.NotNil(t, bucket)
	assert.NoError(t, err)

	artifact, err = bucket.NewStreamedArtifact(artifactName, fileSize)
	assert.NotNil(t, artifact)
	assert.Equal(t, artifactName, artifact.GetArtifactModel().Name)
	assert.Equal(t, model.WAITING_FOR_UPLOAD, artifact.GetArtifactModel().State)
	assert.Equal(t, fileSize, artifact.GetArtifactModel().Size)
	assert.Equal(t, bucketName, artifact.GetArtifactModel().BucketId)
	assert.Empty(t, artifact.GetArtifactModel().S3URL)
	assert.NoError(t, err)

	// Duplicate
	artifact, err = bucket.NewChunkedArtifact(artifactName)
	assert.Nil(t, artifact)
	assert.Error(t, err)

	artifact, err = bucket.GetArtifact(artifactName)
	assert.NotNil(t, artifact)
	assert.Equal(t, artifactName, artifact.GetArtifactModel().Name)
	assert.Equal(t, model.WAITING_FOR_UPLOAD, artifact.GetArtifactModel().State)
	assert.Equal(t, fileSize, artifact.GetArtifactModel().Size)
	assert.Equal(t, bucketName, artifact.GetArtifactModel().BucketId)
	assert.Empty(t, artifact.GetArtifactModel().S3URL)
	assert.NoError(t, err)
}

func TestCloseBucket(t *testing.T) {
	bucketName := "bucketName"
	ownerName := "ownerName"
	chunkedArtifactName := "chunkedArt"
	streamedArtifactName := "streamedArt"
	fileSize := int64(100)

	if testing.Short() {
		t.Skip("Skipping end-to-end test in short mode.")
	}

	client := setup(t)

	var bucket *Bucket
	var artifact Artifact
	var err error

	bucket, err = client.NewBucket(bucketName, ownerName, 31)
	assert.NotNil(t, bucket)
	assert.NoError(t, err)

	artifact, err = bucket.NewChunkedArtifact(chunkedArtifactName)
	assert.NotNil(t, artifact)
	assert.NoError(t, err)

	artifact, err = bucket.NewStreamedArtifact(streamedArtifactName, fileSize)
	assert.NotNil(t, artifact)
	assert.NoError(t, err)

	assert.NoError(t, bucket.Close())

	// Verify bucket is closed
	bucket, err = client.GetBucket(bucketName)
	assert.NotNil(t, bucket)
	assert.Equal(t, bucketName, bucket.bucket.Id)
	assert.Equal(t, ownerName, bucket.bucket.Owner)
	assert.Equal(t, model.CLOSED, bucket.bucket.State)
	assert.NoError(t, err)

	// Verify chunked artifact is closed
	artifact, err = bucket.GetArtifact(chunkedArtifactName)
	assert.NotNil(t, artifact)
	assert.Equal(t, chunkedArtifactName, artifact.GetArtifactModel().Name)
	// Since we didn't write any content to the streamed artifact, it should be closed without data.
	assert.Equal(t, model.CLOSED_WITHOUT_DATA, artifact.GetArtifactModel().State)
	// assert.Equal will crib if the types are not identical.
	assert.Equal(t, int64(0), artifact.GetArtifactModel().Size)
	assert.Equal(t, bucketName, artifact.GetArtifactModel().BucketId)
	assert.Empty(t, artifact.GetArtifactModel().S3URL)
	assert.NoError(t, err)

	// Verify streamed artifact is closed
	artifact, err = bucket.GetArtifact(streamedArtifactName)
	assert.NotNil(t, artifact)
	assert.Equal(t, streamedArtifactName, artifact.GetArtifactModel().Name)
	assert.Equal(t, model.CLOSED_WITHOUT_DATA, artifact.GetArtifactModel().State)
	assert.Equal(t, fileSize, artifact.GetArtifactModel().Size)
	assert.Equal(t, bucketName, artifact.GetArtifactModel().BucketId)
	assert.Empty(t, artifact.GetArtifactModel().S3URL)
	assert.NoError(t, err)

	// Adding artifacts to a closed bucket
	artifact, err = bucket.NewChunkedArtifact("artifact")
	assert.Nil(t, artifact)
	assert.Error(t, err)

	// Adding artifacts to a closed bucket
	artifact, err = bucket.NewStreamedArtifact("artifact", 200)
	assert.Nil(t, artifact)
	assert.Error(t, err)
}

func TestAppendToChunkedArtifact(t *testing.T) {
	bucketName := "bucketName"
	ownerName := "ownerName"
	artifactName := "artifactName"

	if testing.Short() {
		t.Skip("Skipping end-to-end test in short mode.")
	}

	client := setup(t)

	var bucket *Bucket
	var artifact Artifact
	var cartifact *ChunkedArtifact
	var err error

	bucket, err = client.NewBucket(bucketName, ownerName, 31)
	assert.NotNil(t, bucket)
	assert.NoError(t, err)

	cartifact, err = bucket.NewChunkedArtifact(artifactName)
	assert.NotNil(t, cartifact)
	assert.NoError(t, err)

	// Append chunk to artifact.
	assert.NoError(t, cartifact.AppendLog("0123456789"))

	// Append another chunk to artifact.
	assert.NoError(t, cartifact.AppendLog("9876543210"))

	// Flush and verify contents.
	assert.NoError(t, cartifact.Flush())

	artifact, err = bucket.GetArtifact(artifactName)
	assert.NotNil(t, artifact)
	assert.Equal(t, model.APPENDING, artifact.GetArtifactModel().State)
	// assert.Equal will crib if the types are not identical.
	assert.Equal(t, int64(20), artifact.GetArtifactModel().Size)
	assert.Equal(t, bucketName, artifact.GetArtifactModel().BucketId)
	assert.Empty(t, artifact.GetArtifactModel().S3URL)
	assert.NoError(t, err)

	// Append yet another chunk to artifact.
	assert.NoError(t, cartifact.AppendLog("9876543210"))
	// Close the artifact
	assert.NoError(t, cartifact.Close())

	// Verify it exists on S3
	artifact, err = bucket.GetArtifact(artifactName)
	assert.NotNil(t, artifact)
	assert.Equal(t, model.UPLOADED, artifact.GetArtifactModel().State)
	// assert.Equal will crib if the types are not identical.
	assert.Equal(t, int64(30), artifact.GetArtifactModel().Size)
	assert.Equal(t, bucketName, artifact.GetArtifactModel().BucketId)
	assert.Equal(t, fmt.Sprintf("/%s/%s", bucketName, artifactName), artifact.GetArtifactModel().S3URL)
	assert.NoError(t, err)

	content, err := cartifact.GetContent()
	assert.NoError(t, err)
	var buf bytes.Buffer
	buf.ReadFrom(content)
	assert.Equal(t, 30, buf.Len())
}

func TestPostStreamedArtifact(t *testing.T) {
	bucketName := "bucketName"
	ownerName := "ownerName"
	artifactName := "postedStreamedArtifactName"

	if testing.Short() {
		t.Skip("Skipping end-to-end test in short mode.")
	}

	client := setup(t)

	var bucket *Bucket
	var artifact Artifact
	var streamedArtifact *StreamedArtifact
	var err error

	bucket, err = client.NewBucket(bucketName, ownerName, 31)
	assert.NotNil(t, bucket)
	assert.NoError(t, err)

	streamedArtifact, err = bucket.NewStreamedArtifact(artifactName, 30)
	assert.NotNil(t, streamedArtifact)
	assert.NoError(t, err)

	rd := bytes.NewReader([]byte("012345678998765432100123456789"))
	// Append chunk to artifact.
	assert.NoError(t, streamedArtifact.UploadArtifact(rd))

	artifact, err = bucket.GetArtifact(artifactName)
	assert.NotNil(t, artifact)
	assert.Equal(t, model.UPLOADED, artifact.GetArtifactModel().State)
	// assert.Equal will crib if the types are not identical.
	assert.Equal(t, int64(30), artifact.GetArtifactModel().Size)
	assert.Equal(t, bucketName, artifact.GetArtifactModel().BucketId)
	assert.Equal(t, fmt.Sprintf("/%s/%s", bucketName, artifactName), artifact.GetArtifactModel().S3URL)
	assert.NoError(t, err)

	// Verify it exists on S3
	content, err := streamedArtifact.GetContent()
	assert.NoError(t, err)
	var buf bytes.Buffer
	buf.ReadFrom(content)
	assert.Equal(t, 30, buf.Len())
}
