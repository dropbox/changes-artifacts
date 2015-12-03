// Main entrypoint for Artifact Server.
//
// This is a REST API (no html frontend) implemented using Martini.
//
// Database: Postgresql
// Storage: S3
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"golang.org/x/net/context"

	"gopkg.in/amz.v1/aws"
	"gopkg.in/amz.v1/s3"
	"gopkg.in/gorp.v1"
	"gopkg.in/tylerb/graceful.v1"

	"github.com/dropbox/changes-artifacts/api"
	"github.com/dropbox/changes-artifacts/common"
	"github.com/dropbox/changes-artifacts/common/sentry"
	"github.com/dropbox/changes-artifacts/common/stats"
	"github.com/dropbox/changes-artifacts/database"
	"github.com/dropbox/changes-artifacts/model"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/martini-contrib/render"
	"github.com/rubenv/sql-migrate"
)

type RenderOnGin struct {
	render.Render // This makes sure we don't have to create dummies for methods we don't use

	ginCtx *gin.Context
}

func (r RenderOnGin) JSON(statusCode int, obj interface{}) {
	r.ginCtx.JSON(statusCode, obj)
}

var _ render.Render = (*RenderOnGin)(nil)

func HomeHandler(c *gin.Context) {
	c.String(http.StatusOK, "Hello, I am Artie Facts, the artifact store that stores artifacts.")
}

func VersionHandler(c *gin.Context) {
	c.String(http.StatusOK, common.GetVersion())
}

func bindBucket(ctx context.Context, r render.Render, gc *gin.Context, db database.Database) {
	bucketId := gc.Param("bucket_id")
	bucket, err := db.GetBucket(bucketId)

	if err != nil && err.EntityNotFound() {
		// Don't log this error to Sentry
		// Changes will hit this endpoint for non-existant buckets very often.
		api.RespondWithErrorf(ctx, r, http.StatusNotFound, "Bucket not found")
		gc.Abort()
		return
	}

	if err != nil {
		api.LogAndRespondWithError(ctx, r, http.StatusInternalServerError, err)
		gc.Abort()
		return
	}

	if bucket == nil {
		api.LogAndRespondWithErrorf(ctx, r, http.StatusBadRequest, "Got nil bucket without error for bucket: %s", bucketId)
		gc.Abort()
		return
	}

	gc.Set("bucket", bucket)
}

func bindArtifact(ctx context.Context, r render.Render, gc *gin.Context, db database.Database, bucket *model.Bucket) {
	artifactName := gc.Param("artifact_name")
	artifact := api.GetArtifact(bucket, artifactName, db)

	if artifact == nil {
		api.LogAndRespondWithErrorf(ctx, r, http.StatusBadRequest, "Artifact not found")
		gc.Abort()
		return
	}

	gc.Set("artifact", artifact)
}

type config struct {
	DbConnstr    string
	CorsURLs     string
	Env          string
	S3Server     string
	S3Region     string
	S3Bucket     string
	S3AccessKey  string
	S3SecretKey  string
	SentryDSN    string
	StatsdPrefix string
	StatsdURL    string
}

var defaultConfig = config{
	CorsURLs:  "http://dropboxlocalhost.com:5000",
	DbConnstr: "postgres://artifacts:artifacts@artifactsdb/artifacts?sslmode=disable",
	S3Server:  "http://fakes3:4569",
	S3Region:  "fakes3",
	S3Bucket:  "artifacts",
}

func getConfigFrom(configFile string) config {
	var conf config

	if configFile == "" {
		return defaultConfig
	}

	if content, err := ioutil.ReadFile(configFile); err != nil {
		log.Fatalf("Unable to open config file %s\n", configFile)
	} else {
		if err := json.Unmarshal(content, &conf); err != nil {
			log.Fatalf("Unable to decode config file %s\n", configFile)
		} else {
			return conf
		}
	}

	return config{}
}

func performMigrations(db *sql.DB) error {
	migrations := &migrate.AssetMigrationSource{
		Asset:    database.Asset,
		AssetDir: database.AssetDir,
		Dir:      "migrations",
	}

	n, err := migrate.Exec(db, "postgres", migrations, migrate.Up)
	log.Printf("Completed %d migrations\n", n)
	if err != nil {
		log.Println("Error completing DB migration:", err)
	}

	return err
}

func getListenAddr() string {
	port := os.Getenv("PORT")

	if len(port) == 0 {
		// By default, we use port 3000
		port = "3000"
	}

	// Host is "" => any address (IPv4/IPv6)
	hostPort := ":" + port
	log.Printf("About to listen on %s", hostPort)

	return hostPort
}

func main() {
	var flagConfigFile string
	flag.StringVar(&flagConfigFile, "config", "", "JSON Config file containing DB parameters and S3 information")

	flagVerbose := flag.Bool("verbose", false, "Enable request logging")
	flagLogDBQueries := flag.Bool("log-db-queries", false, "Enable DB query logging (Use with care, will dump raw logchunk contents to logfile)")

	showVersion := flag.Bool("version", false, "Show version number and quit")

	onlyPerformMigrations := flag.Bool("migrations-only", false, "Only perform database migrations and quit")

	dbMaxIdleConns := flag.Int("db-max-idle-conns", 20, "Maximum number of idle connections to the DB")

	dbMaxOpenConns := flag.Int("db-max-open-conns", 50, "Maximum number of open connections to the DB")

	shutdownTimeout := flag.Duration("shutdown-timeout", 15*time.Second, "Time to wait before closing active connections after SIGTERM signal has been recieved")

	flag.Parse()
	log.SetFlags(log.Lmicroseconds | log.Lshortfile)

	// Required for artifact name deduplication (not strictly necessary, but good to have)
	rand.Seed(time.Now().UTC().UnixNano())

	if *showVersion {
		fmt.Println(common.GetVersion())
		return
	}

	conf := getConfigFrom(flagConfigFile)

	// ----- BEGIN DB Connections Setup -----
	db, err := sql.Open("postgres", conf.DbConnstr)

	if err != nil {
		log.Fatalf("Could not connect to the database: %v\n", err)
	}

	if *onlyPerformMigrations {
		err := performMigrations(db)
		db.Close()
		if err != nil {
			os.Exit(1)
		}

		return
	}

	db.SetMaxIdleConns(*dbMaxIdleConns)
	db.SetMaxOpenConns(*dbMaxOpenConns)
	defer db.Close()
	dbmap := &gorp.DbMap{Db: db, Dialect: gorp.PostgresDialect{}}
	if *flagLogDBQueries {
		dbmap.TraceOn("[gorp]", log.New(os.Stdout, "artifacts:", log.Lmicroseconds))
	}
	gdb := database.NewGorpDatabase(dbmap)
	// ----- END DB Connections Setup -----

	// ----- BEGIN AWS Connections -----
	var region aws.Region
	var auth aws.Auth
	if conf.S3Region == "fakes3" {
		region = aws.Region{
			Name:       conf.S3Region,
			S3Endpoint: conf.S3Server,
			Sign:       aws.SignV2,
		}

		auth = aws.Auth{}
	} else {
		region = aws.Regions[conf.S3Region]
		auth = aws.Auth{AccessKey: conf.S3AccessKey, SecretKey: conf.S3SecretKey}
	}

	s3Client := s3.New(auth, region)

	bucket := s3Client.Bucket(conf.S3Bucket)
	// ----- END AWS Connections -----

	gdb.RegisterEntities()

	stats.CreateStatsdClient(conf.StatsdURL, conf.StatsdPrefix)
	defer stats.ShutdownStatsdClient()

	g := gin.New()
	g.Use(gin.Recovery())

	if *flagVerbose {
		g.Use(gin.Logger())
	}

	realClock := new(common.RealClock)

	rootCtx := context.Background()
	rootCtx = sentry.CreateAndInstallSentryClient(rootCtx, conf.Env, conf.SentryDSN)
	g.Use(stats.Counter())

	g.GET("/", HomeHandler)
	g.GET("/version", VersionHandler)
	g.GET("/buckets", func(gc *gin.Context) {
		api.ListBuckets(rootCtx, &RenderOnGin{ginCtx: gc}, gdb)
	})
	g.POST("/buckets/", func(gc *gin.Context) {
		api.HandleCreateBucket(rootCtx, &RenderOnGin{ginCtx: gc}, gc.Request, gdb, realClock)
	})

	br := g.Group("/buckets/:bucket_id", func(gc *gin.Context) {
		bindBucket(rootCtx, &RenderOnGin{ginCtx: gc}, gc, gdb)
	})
	{
		br.GET("", func(gc *gin.Context) {
			bkt := gc.MustGet("bucket").(*model.Bucket)
			api.HandleGetBucket(rootCtx, &RenderOnGin{ginCtx: gc}, bkt)
		})
		br.POST("/close", func(gc *gin.Context) {
			bkt := gc.MustGet("bucket").(*model.Bucket)
			api.HandleCloseBucket(rootCtx, &RenderOnGin{ginCtx: gc}, gdb, bkt, bucket, realClock)
		})
		br.GET("/artifacts/", func(gc *gin.Context) {
			bkt := gc.MustGet("bucket").(*model.Bucket)
			api.ListArtifacts(rootCtx, &RenderOnGin{ginCtx: gc}, gc.Request, gdb, bkt)
		})
		br.POST("/artifacts", func(gc *gin.Context) {
			bkt := gc.MustGet("bucket").(*model.Bucket)
			api.HandleCreateArtifact(rootCtx, &RenderOnGin{ginCtx: gc}, gc.Request, gdb, bkt)
		})

		ar := br.Group("/artifacts/:artifact_name", func(gc *gin.Context) {
			bkt := gc.MustGet("bucket").(*model.Bucket)
			bindArtifact(rootCtx, &RenderOnGin{ginCtx: gc}, gc, gdb, bkt)
		})
		{
			ar.GET("", func(gc *gin.Context) {
				afct := gc.MustGet("artifact").(*model.Artifact)
				api.HandleGetArtifact(rootCtx, &RenderOnGin{ginCtx: gc}, afct)
			})
			ar.POST("", func(gc *gin.Context) {
				afct := gc.MustGet("artifact").(*model.Artifact)
				api.PostArtifact(rootCtx, &RenderOnGin{ginCtx: gc}, gc.Request, gdb, bucket, afct)
			})
			ar.POST("/close", func(gc *gin.Context) {
				afct := gc.MustGet("artifact").(*model.Artifact)
				api.HandleCloseArtifact(rootCtx, &RenderOnGin{ginCtx: gc}, gdb, bucket, afct)
			})
			ar.GET("/content", func(gc *gin.Context) {
				afct := gc.MustGet("artifact").(*model.Artifact)
				api.GetArtifactContent(rootCtx, &RenderOnGin{ginCtx: gc}, gc.Request, gc.Writer, gdb, bucket, afct)
			})
			ar.GET("/chunked", func(gc *gin.Context) {
				if conf.CorsURLs != "" {
					gc.Writer.Header().Add("Access-Control-Allow-Origin", conf.CorsURLs)
				}
				afct := gc.MustGet("artifact").(*model.Artifact)
				api.GetArtifactContentChunks(rootCtx, &RenderOnGin{ginCtx: gc}, gc.Request, gc.Writer, gdb, bucket, afct)
			})
		}
	}

	http.Handle("/", g)

	// If the process gets a SIGTERM, it will close listening port allowing another server to bind and
	// begin listening immediately. Any ongoing connections will be given 15 seconds (by default) to
	// complete, after which they are forcibly terminated.
	graceful.Run(getListenAddr(), *shutdownTimeout, nil)
}
