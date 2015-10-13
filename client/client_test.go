package client

import (
	"bytes"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/gorp.v1"

	"github.com/dropbox/changes-artifacts/database"
	"github.com/dropbox/changes-artifacts/model"
	_ "github.com/lib/pq"
	"github.com/rubenv/sql-migrate"
	"github.com/stretchr/testify/require"
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

	migrations := &migrate.AssetMigrationSource{
		Asset:    database.Asset,
		AssetDir: database.AssetDir,
		Dir:      "migrations",
	}

	if n, err := migrate.Exec(db, "postgres", migrations, migrate.Down); err != nil {
		t.Fatalf("Error resetting Postgres DB: %s", err)
	} else {
		fmt.Printf("Completed %d DOWN migrations\n", n)
	}

	// `maxMigrations` below is the maximum number of migration levels to be performed.
	// This is left here to make it easy to verify that database upgrades are backwards compatible.
	// After a new migration is added, this number should be bumped up.
	const maxMigrations = 4
	if n, err := migrate.ExecMax(db, "postgres", migrations, migrate.Up, maxMigrations); err != nil {
		t.Fatalf("Error recreating Postgres DB: %s", err)
	} else {
		fmt.Printf("Completed %d UP migrations\n", n)
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
	require.Nil(t, bucket)
	require.Error(t, err)

	bucket, err = client.NewBucket("bucketname", "", 31)
	require.Nil(t, bucket)
	require.Error(t, err)

	bucket, err = client.NewBucket("bucketname", "ownername", 31)
	require.NotNil(t, bucket)
	require.Equal(t, "bucketname", bucket.bucket.Id)
	require.Equal(t, "ownername", bucket.bucket.Owner)
	require.Equal(t, model.OPEN, bucket.bucket.State)
	require.NoError(t, err)

	// Duplicate
	bucket, err = client.NewBucket("bucketname", "ownername", 31)
	require.Nil(t, bucket)
	require.Error(t, err)

	bucket, err = client.GetBucket("bucketname")
	require.NotNil(t, bucket)
	require.Equal(t, "bucketname", bucket.bucket.Id)
	require.Equal(t, "ownername", bucket.bucket.Owner)
	require.Equal(t, model.OPEN, bucket.bucket.State)
	require.NoError(t, err)

	bucket, err = client.GetBucket("bucketnotfound")
	require.Nil(t, bucket)
	require.Error(t, err)
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
	require.NotNil(t, bucket)
	require.NoError(t, err)

	artifact, err = bucket.NewChunkedArtifact(artifactName)
	require.NotNil(t, artifact)
	require.Equal(t, artifactName, artifact.GetArtifactModel().Name)
	require.Equal(t, model.APPENDING, artifact.GetArtifactModel().State)
	// require.Equal will crib if the types are not identical.
	require.Equal(t, int64(0), artifact.GetArtifactModel().Size)
	require.Equal(t, bucketName, artifact.GetArtifactModel().BucketId)
	require.Empty(t, artifact.GetArtifactModel().S3URL)
	require.NoError(t, err)

	artifact, err = bucket.GetArtifact(artifactName)
	require.NotNil(t, artifact)
	require.Equal(t, artifactName, artifact.GetArtifactModel().Name)
	require.Equal(t, model.APPENDING, artifact.GetArtifactModel().State)
	// require.Equal will crib if the types are not identical.
	require.Equal(t, int64(0), artifact.GetArtifactModel().Size)
	require.Equal(t, bucketName, artifact.GetArtifactModel().BucketId)
	require.Empty(t, artifact.GetArtifactModel().S3URL)
	require.NoError(t, err)
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
	require.NotNil(t, bucket)
	require.NoError(t, err)

	artifact, err = bucket.NewStreamedArtifact(artifactName, fileSize)
	require.NotNil(t, artifact)
	require.Equal(t, artifactName, artifact.GetArtifactModel().Name)
	require.Equal(t, model.WAITING_FOR_UPLOAD, artifact.GetArtifactModel().State)
	require.Equal(t, fileSize, artifact.GetArtifactModel().Size)
	require.Equal(t, bucketName, artifact.GetArtifactModel().BucketId)
	require.Empty(t, artifact.GetArtifactModel().S3URL)
	require.NoError(t, err)

	artifact, err = bucket.GetArtifact(artifactName)
	require.NotNil(t, artifact)
	require.Equal(t, artifactName, artifact.GetArtifactModel().Name)
	require.Equal(t, model.WAITING_FOR_UPLOAD, artifact.GetArtifactModel().State)
	require.Equal(t, fileSize, artifact.GetArtifactModel().Size)
	require.Equal(t, bucketName, artifact.GetArtifactModel().BucketId)
	require.Empty(t, artifact.GetArtifactModel().S3URL)
	require.NoError(t, err)
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
	require.NotNil(t, bucket)
	require.NoError(t, err)

	artifact, err = bucket.NewChunkedArtifact(chunkedArtifactName)
	require.NotNil(t, artifact)
	require.NoError(t, err)

	artifact, err = bucket.NewStreamedArtifact(streamedArtifactName, fileSize)
	require.NotNil(t, artifact)
	require.NoError(t, err)

	require.NoError(t, bucket.Close())

	// Verify bucket is closed
	bucket, err = client.GetBucket(bucketName)
	require.NotNil(t, bucket)
	require.Equal(t, bucketName, bucket.bucket.Id)
	require.Equal(t, ownerName, bucket.bucket.Owner)
	require.Equal(t, model.CLOSED, bucket.bucket.State)
	require.NoError(t, err)

	// Verify chunked artifact is closed
	artifact, err = bucket.GetArtifact(chunkedArtifactName)
	require.NotNil(t, artifact)
	require.Equal(t, chunkedArtifactName, artifact.GetArtifactModel().Name)
	// Since we didn't write any content to the streamed artifact, it should be closed without data.
	require.Equal(t, model.CLOSED_WITHOUT_DATA, artifact.GetArtifactModel().State)
	// require.Equal will crib if the types are not identical.
	require.Equal(t, int64(0), artifact.GetArtifactModel().Size)
	require.Equal(t, bucketName, artifact.GetArtifactModel().BucketId)
	require.Empty(t, artifact.GetArtifactModel().S3URL)
	require.NoError(t, err)

	// Verify streamed artifact is closed
	artifact, err = bucket.GetArtifact(streamedArtifactName)
	require.NotNil(t, artifact)
	require.Equal(t, streamedArtifactName, artifact.GetArtifactModel().Name)
	require.Equal(t, model.CLOSED_WITHOUT_DATA, artifact.GetArtifactModel().State)
	require.Equal(t, fileSize, artifact.GetArtifactModel().Size)
	require.Equal(t, bucketName, artifact.GetArtifactModel().BucketId)
	require.Empty(t, artifact.GetArtifactModel().S3URL)
	require.NoError(t, err)

	// Adding artifacts to a closed bucket
	artifact, err = bucket.NewChunkedArtifact("artifact")
	require.Nil(t, artifact)
	require.Error(t, err)

	// Adding artifacts to a closed bucket
	artifact, err = bucket.NewStreamedArtifact("artifact", 200)
	require.Nil(t, artifact)
	require.Error(t, err)
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
	require.NotNil(t, bucket)
	require.NoError(t, err)

	cartifact, err = bucket.NewChunkedArtifact(artifactName)
	require.NotNil(t, cartifact)
	require.NoError(t, err)

	// Append chunk to artifact.
	require.NoError(t, cartifact.AppendLog("0123456789"))

	// Append another chunk to artifact.
	require.NoError(t, cartifact.AppendLog("9876543210"))

	// Flush and verify contents.
	require.NoError(t, cartifact.Flush())

	artifact, err = bucket.GetArtifact(artifactName)
	require.NotNil(t, artifact)
	require.Equal(t, model.APPENDING, artifact.GetArtifactModel().State)
	// require.Equal will crib if the types are not identical.
	require.Equal(t, int64(20), artifact.GetArtifactModel().Size)
	require.Equal(t, bucketName, artifact.GetArtifactModel().BucketId)
	require.Empty(t, artifact.GetArtifactModel().S3URL)
	require.NoError(t, err)

	// Append yet another chunk to artifact.
	require.NoError(t, cartifact.AppendLog("9876543210"))
	// Close the artifact
	require.NoError(t, cartifact.Close())

	// Verify it exists on S3
	artifact, err = bucket.GetArtifact(artifactName)
	require.NotNil(t, artifact)
	require.Equal(t, model.UPLOADED, artifact.GetArtifactModel().State)
	// require.Equal will crib if the types are not identical.
	require.Equal(t, int64(30), artifact.GetArtifactModel().Size)
	require.Equal(t, bucketName, artifact.GetArtifactModel().BucketId)
	require.Equal(t, fmt.Sprintf("/%s/%s", bucketName, artifactName), artifact.GetArtifactModel().S3URL)
	require.NoError(t, err)

	content, err := cartifact.GetContent()
	require.NoError(t, err)
	var buf bytes.Buffer
	buf.ReadFrom(content)
	require.Equal(t, 30, buf.Len())
}

func TestChunkedArtifactBytes(t *testing.T) {
	const bucketName = "bucketName"
	const ownerName = "ownerName"
	const artifactName = "artifactName"

	if testing.Short() {
		t.Skip("Skipping end-to-end test in short mode.")
	}

	client := setup(t)

	bucket, err := client.NewBucket(bucketName, ownerName, 31)
	require.NotNil(t, bucket)
	require.NoError(t, err)

	// Append bytes to artifacts.
	for i := 0; i <= 255; i++ {
		cartifact, err := bucket.NewChunkedArtifact(artifactName + strconv.Itoa(i))
		require.NotNil(t, cartifact)
		require.NoError(t, err)
		require.NoError(t, cartifact.AppendLog(string(byte(i))))
		require.NoError(t, cartifact.Flush())
	}
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
	require.NotNil(t, bucket)
	require.NoError(t, err)

	streamedArtifact, err = bucket.NewStreamedArtifact(artifactName, 30)
	require.NotNil(t, streamedArtifact)
	require.NoError(t, err)

	rd := bytes.NewReader([]byte("012345678998765432100123456789"))
	// Append chunk to artifact.
	require.NoError(t, streamedArtifact.UploadArtifact(rd))

	artifact, err = bucket.GetArtifact(artifactName)
	require.NotNil(t, artifact)
	require.Equal(t, model.UPLOADED, artifact.GetArtifactModel().State)
	// require.Equal will crib if the types are not identical.
	require.Equal(t, int64(30), artifact.GetArtifactModel().Size)
	require.Equal(t, bucketName, artifact.GetArtifactModel().BucketId)
	require.Equal(t, fmt.Sprintf("/%s/%s", bucketName, artifactName), artifact.GetArtifactModel().S3URL)
	require.NoError(t, err)

	// Verify it exists on S3
	content, err := streamedArtifact.GetContent()
	require.NoError(t, err)
	var buf bytes.Buffer
	buf.ReadFrom(content)
	require.Equal(t, 30, buf.Len())
}

func TestCreateAndListArtifacts(t *testing.T) {
	bucketName := "bucketName"
	ownerName := "ownerName"
	artifactName1 := "artifactName1"
	artifactName2 := "artifactName2"

	if testing.Short() {
		t.Skip("Skipping end-to-end test in short mode.")
	}

	client := setup(t)

	var bucket *Bucket
	var streamedArtifact *StreamedArtifact
	var chunkedArtifact *ChunkedArtifact
	var err error
	var artifacts []Artifact

	bucket, err = client.NewBucket(bucketName, ownerName, 31)
	require.NotNil(t, bucket)
	require.NoError(t, err)

	streamedArtifact, err = bucket.NewStreamedArtifact(artifactName1, 30)
	require.NotNil(t, streamedArtifact)
	require.NoError(t, err)

	chunkedArtifact, err = bucket.NewChunkedArtifact(artifactName2)
	require.NotNil(t, chunkedArtifact)
	require.NoError(t, err)

	artifacts, err = bucket.ListArtifacts()
	require.NotNil(t, artifacts)
	require.Nil(t, err)
	require.Len(t, artifacts, 2)

	// We really shouldn't worry about order here.
	require.Equal(t, artifactName1, artifacts[0].GetArtifactModel().Name)
	require.Equal(t, artifactName2, artifacts[1].GetArtifactModel().Name)
}

func TestCreateDuplicateArtifactRace(t *testing.T) {
	bucketName := "bucketName"
	ownerName := "ownerName"
	artifactName := "artifactName"

	if testing.Short() {
		t.Skip("Skipping end-to-end test in short mode.")
	}

	client := setup(t)

	bucket, err := client.NewBucket(bucketName, ownerName, 31)
	require.NotNil(t, bucket)
	require.NoError(t, err)

	// More parallelism will hit postgres connection limit.
	// http://www.postgresql.org/docs/current/static/runtime-config-connection.html#GUC-MAX-CONNECTIONS
	createdArtifactNames := make([]string, 20)

	// Create many duplicates
	var wg sync.WaitGroup

	for i := range createdArtifactNames {
		wg.Add(1)
		go func(counter int) {
			defer wg.Done()
			artifact, e := bucket.NewStreamedArtifact(artifactName, 1)
			require.NoError(t, e)
			require.NotNil(t, artifact)
			createdArtifactNames[counter] = artifact.GetArtifactModel().Name
		}(i)
	}

	wg.Wait()

	seen := make(map[string]bool)

	for _, created := range createdArtifactNames {
		if !strings.HasPrefix(created, artifactName) {
			t.Fatalf("Expected created artifact name to have prefix '%s', got '%s'", artifactName, created)
		}

		if seen[created] {
			t.Fatalf("Duplicate artifact name: %s", created)
		} else {
			seen[created] = true
		}
	}
}
