package mcp

import (
	"context"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTransports(t *testing.T) {
	for _, transport := range []string{"stdio-equivalent", "streamable-http", "sse"} {
		t.Run(transport, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			server := New(nil, nil)
			client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
			var target sdk.Transport
			switch transport {
			case "stdio-equivalent":
				a, b := sdk.NewInMemoryTransports()
				session, e := server.Connect(ctx, a, nil)
				if e != nil {
					t.Fatal(e)
				}
				defer session.Close()
				target = b
			case "streamable-http":
				httpServer := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, nil))
				defer httpServer.Close()
				target = &sdk.StreamableClientTransport{Endpoint: httpServer.URL}
			case "sse":
				httpServer := httptest.NewServer(sdk.NewSSEHandler(func(*http.Request) *sdk.Server { return server }, nil))
				defer httpServer.Close()
				target = &sdk.SSEClientTransport{Endpoint: httpServer.URL}
			}
			session, e := client.Connect(ctx, target, nil)
			if e != nil {
				t.Fatal(e)
			}
			defer session.Close()
			tools, e := session.ListTools(ctx, nil)
			if e != nil {
				t.Fatal(e)
			}
			if len(tools.Tools) != 2 {
				t.Fatalf("tools: %+v", tools)
			}
			resources, e := session.ListResources(ctx, nil)
			if e != nil {
				t.Fatal(e)
			}
			if len(resources.Resources) != 1 {
				t.Fatalf("resources: %+v", resources)
			}
		})
	}
}
