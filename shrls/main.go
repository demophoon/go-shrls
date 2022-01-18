package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"log"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"goji.io/pat"

	"github.com/goji/httpauth"
	"goji.io"
)

type ShrlSettings struct {
	Port                  int
	DefaultRedirect       string
	UploadDirectory       string
	MongoConnectionString string
	AdminUsername         string
	AdminPassword         string
}

var collection *mongo.Collection
var ctx = context.TODO()

var Settings ShrlSettings

func init() {
	// Init Settings
	port, err := strconv.Atoi(os.Getenv("SHRLS_PORT"))
	if err != nil {
		log.Fatal(fmt.Sprintf("Invalid Port: %s", err))
		os.Exit(1)
	}

	Settings = ShrlSettings{
		Port:            port,
		DefaultRedirect: os.Getenv("DEFAULT_REDIRECT"),
		UploadDirectory: os.Getenv("UPLOAD_DIRECTORY"),
		// mongodb://mongo:example@localhost:27017
		MongoConnectionString: os.Getenv("MONGO_URI"),
		AdminUsername:         os.Getenv("SHRLS_USERNAME"),
		AdminPassword:         os.Getenv("SHRLS_PASSWORD"),
	}

	// Init Mongo
	clientOptions := options.Client().ApplyURI(Settings.MongoConnectionString)
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		log.Fatal(err)
	}

	err = client.Ping(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}

	collection = client.Database("shrls").Collection("urls")
}

func main() {
	workdir, err := os.Getwd()
	if err != nil {
		log.Fatal(fmt.Sprintf("Error %s", err))
		os.Exit(1)
	}

	mux := goji.NewMux()
	admin_mux := goji.SubMux()
	api_mux := goji.SubMux()

	auth_middleware := httpauth.SimpleBasicAuth("username", "password")

	api_mux.Use(auth_middleware)

	// Frontend
	mux.HandleFunc(pat.Get("/:shrl"), resolveShrl)

	// Admin
	mux.Handle(pat.New("/admin/*"), admin_mux)
	fs := http.FileServer(http.Dir(workdir + "/dist/"))
	admin_mux.Handle(pat.Get("/*"), http.StripPrefix("/admin/", fs))

	// Api
	mux.Handle(pat.New("/api/*"), api_mux)
	api_mux.HandleFunc(pat.Get("/shrl/:shrl"), urlPrintInfo)
	api_mux.HandleFunc(pat.Get("/shrl"), urlPrintAll)
	api_mux.HandleFunc(pat.Put("/shrl/:shrl_id"), urlModify)
	api_mux.HandleFunc(pat.Delete("/shrl/:shrl_id"), urlDelete)
	api_mux.HandleFunc(pat.Post("/shrl"), urlNew)

	// File Uploads
	api_mux.HandleFunc(pat.Post("/upload"), fileUpload)

	// Snippets
	api_mux.HandleFunc(pat.Post("/snippet"), snippetUpload)
	api_mux.HandleFunc(pat.Get("/snippet/:snippet_id"), snippetGet)

	http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", Settings.Port), mux)
}