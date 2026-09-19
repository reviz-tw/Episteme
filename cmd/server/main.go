package main

import (
	"context"
	"errors"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/reviz-tw/Episteme/internal/api"
	"github.com/reviz-tw/Episteme/internal/app"
	"github.com/reviz-tw/Episteme/internal/indexer"
	"github.com/reviz-tw/Episteme/internal/mcp"
	"github.com/reviz-tw/Episteme/internal/metadata"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	a, e := app.Open(ctx)
	if e != nil {
		slog.Error("startup", "error", e)
		os.Exit(1)
	}
	defer a.Close()
	ms := mcp.New(a.Store, a.Search)
	get := func(*http.Request) *sdk.Server { return ms }
	apiServer := &api.Server{Store: a.Store, Config: a.Config, Search: a.Search, TEI: a.TEI, Vectors: a.Vectors, MCP: sdk.NewStreamableHTTPHandler(get, nil), LegacyMCP: sdk.NewSSEHandler(get, nil)}
	worker := &indexer.Worker{Store: a.Store, Vectors: a.Vectors, Model: a.TEI, ModelKey: a.Config.ModelKey(), Collection: a.Config.CollectionName()}
	go worker.Run(ctx)
	if a.Config.MetadataEnabled {
		metadataWorker := &metadata.Worker{Store: a.Store, Extractor: metadata.New(a.Config.MetadataURL)}
		go metadataWorker.Run(ctx)
	}
	server := &http.Server{Addr: a.Config.Addr, Handler: apiServer.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20}
	go func() {
		<-ctx.Done()
		shutdown, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_ = server.Shutdown(shutdown)
	}()
	slog.Info("Episteme listening", "address", a.Config.Addr)
	if e = server.ListenAndServe(); e != nil && !errors.Is(e, http.ErrServerClosed) {
		slog.Error("server", "error", e)
		os.Exit(1)
	}
}
