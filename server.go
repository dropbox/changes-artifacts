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
	"net/http"
	"os"
	"os/signal"
	"runtime/pprof"
	"syscall"

	"gopkg.in/amz.v1/aws"
	"gopkg.in/amz.v1/s3"
	"gopkg.in/gorp.v1"

	"github.com/dropbox/changes-artifacts/api"
	"github.com/dropbox/changes-artifacts/common"
	"github.com/dropbox/changes-artifacts/database"
	"github.com/dropbox/changes-artifacts/model"
	"github.com/go-martini/martini"
	_ "github.com/lib/pq"
	"github.com/martini-contrib/render"
)

func HomeHandler(res http.ResponseWriter, req *http.Request) {
	res.Write([]byte("Hello, I am Artie Facts, the artifact store that stores artifacts."))
}

func VersionHandler(res http.ResponseWriter, req *http.Request) {
	res.Write([]byte(common.GetVersion()))
}

func bindBucket(w http.ResponseWriter, r render.Render, c martini.Context, params martini.Params, db database.Database) {
	bucket, err := db.GetBucket(params["bucket_id"])
	if bucket == nil {
		api.JsonErrorf(r, http.StatusBadRequest, err.Error())
		return
	}
	c.Map(bucket)

	if err != nil && err.EntityNotFound() {
		api.JsonErrorf(r, http.StatusBadRequest, "Bucket not found")
		return
	}

	if err != nil {
		api.JsonErrorf(r, http.StatusInternalServerError, "Database failure while trying to fetch bucket instance: %s", err.Error())
	}
}

func bindArtifact(w http.ResponseWriter, r render.Render, c martini.Context, params martini.Params, bucket *model.Bucket, db database.Database) {
	if bucket == nil {
		// XXX I don't know why we do this.
		var artifact *model.Artifact
		artifact = nil
		c.Map(artifact)
		return
	}

	artifact := api.GetArtifact(bucket, params["artifact_name"], db)

	if artifact == nil {
		api.JsonErrorf(r, http.StatusBadRequest, "Artifact not found")
		return
	}

	c.Map(artifact)
}

type config struct {
	DbConnstr   string
	S3Server    string
	S3Region    string
	S3Bucket    string
	S3AccessKey string
	S3SecretKey string
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

func main() {
	var flagConfigFile string
	flag.StringVar(&flagConfigFile, "config", "", "JSON Config file containing DB parameters and S3 information")

	var flagCPUProfile string
	flag.StringVar(&flagCPUProfile, "cpuprofile", "", "File to write CPU profile into")

	flagVerbose := flag.Bool("verbose", false, "Enable request logging")
	flagLogDBQueries := flag.Bool("log-db-queries", false, "Enable DB query logging (Use with care, will dump raw logchunk contents to logfile)")

	showVersion := flag.Bool("version", false, "Show version number and quit")

	flag.Parse()

	if *showVersion {
		fmt.Println(common.GetVersion())
		return
	}

	conf := getConfigFrom(flagConfigFile)

	// ----- BEGIN CPU profiling -----
	if flagCPUProfile != "" {
		sig := make(chan os.Signal, 1)

		f, err := os.Create(flagCPUProfile)
		if err != nil {
			log.Fatal(err)
		}

		go func() {
			<-sig
			fmt.Println("Handling SIGHUP")
			pprof.StopCPUProfile()
			os.Exit(0)
		}()

		pprof.StartCPUProfile(f)
		signal.Notify(sig, syscall.SIGHUP)
	}
	// ----- END CPU Profiling -----

	// ----- BEGIN DB Connections -----
	db, err := sql.Open("postgres", conf.DbConnstr)

	if err != nil {
		log.Fatalf("Could not connect to the database: %v\n", err)
	}

	dbmap := &gorp.DbMap{Db: db, Dialect: gorp.PostgresDialect{}}
	if *flagLogDBQueries {
		dbmap.TraceOn("[gorp]", log.New(os.Stdout, "artifacts:", log.Lmicroseconds))
	}
	gdb := database.NewGorpDatabase(dbmap)
	// ----- END DB Connections -----

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
		auth = aws.Auth{conf.S3AccessKey, conf.S3SecretKey}
	}

	s3Client := s3.New(auth, region)

	bucket := s3Client.Bucket(conf.S3Bucket)
	// ----- END AWS Connections -----

	// TODO: These should be extracted out into a versioned migration system like
	// https://bitbucket.org/liamstask/goose or https://github.com/mattes/migrate
	gdb.RegisterEntities()
	if err := gdb.CreateEntities(); err != nil {
		log.Fatalf("Error while creating/updating tables @ %s: %s\n", conf.DbConnstr, err)
	}

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
			ar.Post("/close", api.FinalizeArtifact)
			ar.Get("/content", api.GetArtifactContent)
		}, bindArtifact)
	}, bindBucket)
	m.Action(r.Handle)

	m.Run()
}
