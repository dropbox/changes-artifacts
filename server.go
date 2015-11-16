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
	"github.com/dropbox/changes-artifacts/common/reqcontext"
	"github.com/dropbox/changes-artifacts/common/sentry"
	"github.com/dropbox/changes-artifacts/common/stats"
	"github.com/dropbox/changes-artifacts/database"
	"github.com/dropbox/changes-artifacts/model"
	"github.com/go-martini/martini"
	_ "github.com/lib/pq"
	"github.com/martini-contrib/render"
	"github.com/rubenv/sql-migrate"
)

func HomeHandler(res http.ResponseWriter, req *http.Request) {
	res.Write([]byte("Hello, I am Artie Facts, the artifact store that stores artifacts."))
}

func VersionHandler(res http.ResponseWriter, req *http.Request) {
	res.Write([]byte(common.GetVersion()))
}

func bindBucket(ctx context.Context, w http.ResponseWriter, r render.Render, c martini.Context, params martini.Params, db database.Database) {
	bucket, err := db.GetBucket(params["bucket_id"])

	if err != nil && err.EntityNotFound() {
		// Don't log this error to Sentry
		// Changes will hit this endpoint for non-existant buckets very often.
		api.RespondWithErrorf(ctx, r, http.StatusNotFound, "Bucket not found")
		return
	}

	if err != nil {
		api.LogAndRespondWithError(ctx, r, http.StatusInternalServerError, err)
		return
	}

	if bucket == nil {
		api.LogAndRespondWithErrorf(ctx, r, http.StatusBadRequest, "Got nil bucket without error for bucket: %s", params["bucket_id"])
		return
	}

	c.Map(bucket)
}

func bindArtifact(ctx context.Context, w http.ResponseWriter, r render.Render, c martini.Context, params martini.Params, bucket *model.Bucket, db database.Database) {
	if bucket == nil {
		// Unfortunately, martini doesn't have a simple mechanism to stop method handling once an error
		// has been found. So, we have to perform error checking all the way down
		var artifact *model.Artifact
		artifact = nil
		c.Map(artifact)
		return
	}

	artifact := api.GetArtifact(bucket, params["artifact_name"], db)

	if artifact == nil {
		api.LogAndRespondWithErrorf(ctx, r, http.StatusBadRequest, "Artifact not found")
		return
	}

	c.Map(artifact)
}

type config struct {
	DbConnstr    string
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

	m := martini.New()
	m.Use(martini.Recovery())
	m.Map(dbmap)
	m.Map(s3Client)
	m.Map(bucket)
	m.Use(render.Renderer())

	if *flagVerbose {
		m.Use(martini.Logger())
	}

	// Bind the gdb instance to be returned every time a Database interface is required.
	m.MapTo(gdb, (*database.Database)(nil))
	// Bind real clock implementation
	m.MapTo(new(common.RealClock), (*common.Clock)(nil))

	// XXX It would be nice to use something like https://github.com/guregu/kami which does all this
	// in a very nice manner. It will also help us get rid of the crappy dependency injection magic
	// that is done by Martini. Also, kami/httprouter is supposed to be faster(TM).
	rootCtx := context.Background()
	rootCtx = sentry.CreateAndInstallSentryClient(rootCtx, conf.Env, conf.SentryDSN)
	m.Use(reqcontext.ContextHandler(rootCtx))
	m.Use(stats.Counter())
	m.Use(sentry.PanicHandler())

	r := martini.NewRouter()
	// '/' url is used to determine if the server is up. Do not remove.
	r.Get("/", HomeHandler)
	r.Get("/version", VersionHandler)
	r.Get("/buckets", api.ListBuckets)
	r.Post("/buckets", api.HandleCreateBucket)
	r.Group("/buckets/:bucket_id", func(br martini.Router) {
		br.Get("", api.HandleGetBucket)
		br.Post("/close", api.HandleCloseBucket)
		br.Get("/artifacts", api.ListArtifacts)
		br.Post("/artifacts", api.HandleCreateArtifact)
		br.Group("/artifacts/:artifact_name", func(ar martini.Router) {
			ar.Get("", api.HandleGetArtifact)
			ar.Post("", api.PostArtifact)
			ar.Post("/close", api.HandleCloseArtifact)
			ar.Get("/content", api.GetArtifactContent)
		}, bindArtifact)
	}, bindBucket)
	m.Action(r.Handle)
	http.Handle("/", m)

	// If the process gets a SIGTERM, it will close listening port allowing another server to bind and
	// begin listening immediately. Any ongoing connections will be given 15 seconds (by default) to
	// complete, after which they are forcibly terminated.
	graceful.Run(getListenAddr(), *shutdownTimeout, nil)
}
