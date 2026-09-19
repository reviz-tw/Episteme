package storage

import (
	"context"
	"github.com/google/uuid"
	q "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
	"net"
	"testing"
	"time"
)

type collectionsRPC struct {
	q.UnimplementedCollectionsServer
	created *q.CreateCollection
}

func (s *collectionsRPC) CollectionExists(context.Context, *q.CollectionExistsRequest) (*q.CollectionExistsResponse, error) {
	return &q.CollectionExistsResponse{Result: &q.CollectionExists{Exists: s.created != nil}}, nil
}
func (s *collectionsRPC) Create(_ context.Context, r *q.CreateCollection) (*q.CollectionOperationResponse, error) {
	s.created = r
	return &q.CollectionOperationResponse{Result: true}, nil
}
func (s *collectionsRPC) Get(context.Context, *q.GetCollectionInfoRequest) (*q.GetCollectionInfoResponse, error) {
	return &q.GetCollectionInfoResponse{Result: &q.CollectionInfo{Config: &q.CollectionConfig{Params: &q.CollectionParams{VectorsConfig: s.created.VectorsConfig}}, PointsCount: q.PtrOf(uint64(1))}}, nil
}

type pointsRPC struct {
	q.UnimplementedPointsServer
	last    *q.UpsertPoints
	deleted *q.DeletePoints
}

func (s *pointsRPC) Upsert(_ context.Context, r *q.UpsertPoints) (*q.PointsOperationResponse, error) {
	s.last = r
	return &q.PointsOperationResponse{Result: &q.UpdateResult{Status: q.UpdateStatus_Completed}}, nil
}
func (s *pointsRPC) Delete(_ context.Context, r *q.DeletePoints) (*q.PointsOperationResponse, error) {
	s.deleted = r
	return &q.PointsOperationResponse{Result: &q.UpdateResult{Status: q.UpdateStatus_Completed}}, nil
}
func (s *pointsRPC) Query(context.Context, *q.QueryPoints) (*q.QueryResponse, error) {
	p := s.last.Points[0]
	return &q.QueryResponse{Result: []*q.ScoredPoint{{Id: p.Id, Payload: p.Payload, Score: .9}}}, nil
}
func TestGRPCContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	collections := &collectionsRPC{}
	points := &pointsRPC{}
	q.RegisterCollectionsServer(server, collections)
	q.RegisterPointsServer(server, points)
	go server.Serve(listener)
	defer server.Stop()
	client, e := q.NewClient(&q.Config{Host: "localhost", Port: 6334, PoolSize: 1, SkipCompatibilityCheck: true, GrpcOptions: []grpc.DialOption{grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() })}})
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	adapter := &Qdrant{Client: client, Collection: "test", Dimension: 3}
	if e = adapter.Ensure(ctx); e != nil {
		t.Fatal(e)
	}
	if e = adapter.Ensure(ctx); e != nil {
		t.Fatal(e)
	}
	c := Chunk{ID: uuid.NewString(), DocumentID: uuid.NewString(), Content: "中文內容", Breadcrumbs: []string{"章節", "子章節"}, Tokens: 4, Revision: 7}
	c.DocumentMetadata = DocumentMetadata{Title: "測試文件", Tags: []string{"中文", "查核"}, Custom: map[string]string{"document_id": "user-defined"}}
	if e = adapter.Upsert(ctx, c, []float32{1, 0, 0}, []uint32{1}, []float32{1}); e != nil {
		t.Fatal(e)
	}
	p := points.last.Points[0]
	if !points.last.GetWait() || len(p.Payload["breadcrumbs"].GetListValue().GetValues()) != 2 {
		t.Fatal("invalid payload / wait semantics")
	}
	metadata := p.Payload["document_metadata"].GetStructValue().GetFields()
	if metadata["title"].GetStringValue() != "測試文件" || len(metadata["tags"].GetListValue().GetValues()) != 2 || p.Payload["document_id"].GetStringValue() != c.DocumentID || metadata["custom"].GetStructValue().GetFields()["document_id"].GetStringValue() != "user-defined" {
		t.Fatal("document metadata payload lost fields or overwrote reserved IDs")
	}
	hits, e := adapter.Search(ctx, "dense", []float32{1, 0, 0}, nil, nil, 20)
	if e != nil || len(hits) != 1 || hits[0].Revision != 7 {
		t.Fatalf("query: %+v %v", hits, e)
	}
	if e = adapter.Delete(ctx, c.ID, "test"); e != nil {
		t.Fatal(e)
	}
	if !points.deleted.GetWait() {
		t.Fatal("delete must wait for completion")
	}
}
