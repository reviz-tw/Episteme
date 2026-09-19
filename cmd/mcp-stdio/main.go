package main

import (
	"context"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/reviz-tw/Episteme/internal/app"
	"github.com/reviz-tw/Episteme/internal/mcp"
	"log"
)

func main() {
	ctx := context.Background()
	a, e := app.Open(ctx)
	if e != nil {
		log.Fatal(e)
	}
	defer a.Close()
	if e = mcp.New(a.Store, a.Search).Run(ctx, &sdk.StdioTransport{}); e != nil {
		log.Fatal(e)
	}
}
