package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/reviz-tw/Episteme/internal/retrieval"
	"github.com/reviz-tw/Episteme/internal/storage"
	"strings"
)

func New(store *storage.Store, search *retrieval.Service) *sdk.Server {
	server := sdk.NewServer(&sdk.Implementation{Name: "Episteme", Version: "0.1.0"}, nil)
	sdk.AddTool(server, &sdk.Tool{Name: "search_knowledge", Description: "Search reviewed and published knowledge using weighted dense/BM25 retrieval. alpha=0 is lexical; alpha=1 is dense. Filters weak matches and duplicate passages by default. min_score (0–1) defaults to 0.1 with reranking, 0.5 without; complete query-term matches are retained. include_low_relevance=true returns unfiltered candidates for debugging."}, func(ctx context.Context, req *sdk.CallToolRequest, in retrieval.Input) (*sdk.CallToolResult, any, error) {
		out, e := search.Search(ctx, in)
		return nil, out, e
	})
	sdk.AddTool(server, &sdk.Tool{Name: "list_documents", Description: "List workspace documents and review/indexing status."}, func(ctx context.Context, req *sdk.CallToolRequest, in struct{}) (*sdk.CallToolResult, any, error) {
		out, e := store.Documents(ctx)
		return nil, out, e
	})
	server.AddResource(&sdk.Resource{URI: "episteme://documents", Name: "Document library", MIMEType: "application/json"}, func(ctx context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		out, e := store.Documents(ctx)
		if e != nil {
			return nil, e
		}
		b, e := json.Marshal(out)
		return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: req.Params.URI, MIMEType: "application/json", Text: string(b)}}}, e
	})
	server.AddResourceTemplate(&sdk.ResourceTemplate{URITemplate: "episteme://documents/{id}", Name: "Document source", MIMEType: "text/markdown"}, func(ctx context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		id := strings.TrimPrefix(req.Params.URI, "episteme://documents/")
		if id == req.Params.URI {
			return nil, fmt.Errorf("invalid resource")
		}
		d, e := store.Document(ctx, id)
		if e != nil {
			return nil, e
		}
		return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: req.Params.URI, MIMEType: "text/markdown", Text: d.Source}}}, nil
	})
	return server
}
