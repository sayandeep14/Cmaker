// Command api is the cmaker packs API server's entrypoint: load config,
// connect to Postgres, run migrations, and serve HTTP.
package main

import (
	"context"
	"log"
	"net/http"

	"cmaker-packs-server/internal/api"
	"cmaker-packs-server/internal/config"
	"cmaker-packs-server/internal/db"
	"cmaker-packs-server/internal/githubapi"
	"cmaker-packs-server/internal/objectstore"
	"cmaker-packs-server/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("migration failed: %v", err)
	}
	log.Println("migrations up to date")

	objects, err := objectstore.New(ctx, cfg.R2AccountID, cfg.R2AccessKeyID, cfg.R2SecretAccessKey, cfg.R2Bucket)
	if err != nil {
		log.Fatalf("object store setup failed: %v", err)
	}

	srv := api.New(store.New(pool), githubapi.NewClient(), objects)

	addr := ":" + cfg.Port
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, srv); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
