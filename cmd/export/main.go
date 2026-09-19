package main

import (
	"context"
	"github.com/reviz-tw/Episteme/internal/cloud"
	"github.com/reviz-tw/Episteme/internal/config"
	"github.com/reviz-tw/Episteme/internal/storage"
	"log"
	"os"
)

func main() {
	ctx := context.Background()
	s, e := storage.Open(ctx, config.Load().DatabaseURL)
	if e != nil {
		log.Fatal(e)
	}
	defer s.Pool.Close()
	if e = cloud.Export(ctx, s, os.Stdout); e != nil {
		log.Fatal(e)
	}
}
