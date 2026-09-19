package storage

import (
	"context"
	"fmt"
	"github.com/qdrant/go-client/qdrant"
	"github.com/reviz-tw/Episteme/internal/config"
)

type Hit struct {
	ID       string
	Score    float64
	Revision int
}
type VectorStore interface {
	Ensure(context.Context) error
	Upsert(context.Context, Chunk, []float32, []uint32, []float32) error
	Delete(context.Context, string, string) error
	Search(context.Context, string, []float32, []uint32, []float32, int) ([]Hit, error)
	Health(context.Context) (uint64, error)
}
type Qdrant struct {
	Client     *qdrant.Client
	Collection string
	Dimension  int
}

func NewQdrant(c config.Config) (*Qdrant, error) {
	client, e := qdrant.NewClient(&qdrant.Config{Host: c.QdrantHost, Port: c.QdrantPort, APIKey: c.QdrantKey, UseTLS: c.QdrantTLS, SkipCompatibilityCheck: true})
	return &Qdrant{client, c.CollectionName(), c.Dimension}, e
}
func (q *Qdrant) Ensure(ctx context.Context) error {
	exists, e := q.Client.CollectionExists(ctx, q.Collection)
	if e != nil {
		return e
	}
	if exists {
		info, e := q.Client.GetCollectionInfo(ctx, q.Collection)
		if e != nil {
			return e
		}
		v := info.GetConfig().GetParams().GetVectorsConfig().GetParamsMap().GetMap()["dense"]
		if v == nil || v.Size != uint64(q.Dimension) {
			return fmt.Errorf("Qdrant collection dimension mismatch")
		}
		return nil
	}
	return q.Client.CreateCollection(ctx, &qdrant.CreateCollection{CollectionName: q.Collection, VectorsConfig: qdrant.NewVectorsConfigMap(map[string]*qdrant.VectorParams{"dense": {Size: uint64(q.Dimension), Distance: qdrant.Distance_Cosine}}), SparseVectorsConfig: qdrant.NewSparseVectorsConfig(map[string]*qdrant.SparseVectorParams{"sparse": {Modifier: qdrant.PtrOf(qdrant.Modifier_Idf)}})})
}
func (q *Qdrant) Upsert(ctx context.Context, c Chunk, v []float32, ids []uint32, weights []float32) error {
	crumbs := make([]any, len(c.Breadcrumbs))
	for i, crumb := range c.Breadcrumbs {
		crumbs[i] = crumb
	}
	payload, e := qdrant.TryValueMap(map[string]any{"chunk_id": c.ID, "document_id": c.DocumentID, "breadcrumbs": crumbs, "content": c.Content, "token_count": c.Tokens, "revision": c.Revision, "document_metadata": c.DocumentMetadata.Payload()})
	if e != nil {
		return e
	}
	_, e = q.Client.Upsert(ctx, &qdrant.UpsertPoints{CollectionName: q.Collection, Wait: qdrant.PtrOf(true), Points: []*qdrant.PointStruct{{Id: qdrant.NewIDUUID(c.ID), Vectors: qdrant.NewVectorsMap(map[string]*qdrant.Vector{"dense": qdrant.NewVectorDense(v), "sparse": qdrant.NewVectorSparse(ids, weights)}), Payload: payload}}})
	return e
}
func (q *Qdrant) Delete(ctx context.Context, id, collection string) error {
	if collection == "" {
		collection = q.Collection
	}
	exists, e := q.Client.CollectionExists(ctx, collection)
	if e != nil || !exists {
		return e
	}
	_, e = q.Client.Delete(ctx, &qdrant.DeletePoints{CollectionName: collection, Wait: qdrant.PtrOf(true), Points: qdrant.NewPointsSelector(qdrant.NewIDUUID(id))})
	return e
}
func (q *Qdrant) Search(ctx context.Context, kind string, dense []float32, ids []uint32, weights []float32, limit int) ([]Hit, error) {
	query := qdrant.NewQueryDense(dense)
	if kind == "sparse" {
		if len(ids) == 0 {
			return []Hit{}, nil
		}
		query = qdrant.NewQuerySparse(ids, weights)
	}
	points, e := q.Client.Query(ctx, &qdrant.QueryPoints{CollectionName: q.Collection, Using: &kind, Query: query, Limit: qdrant.PtrOf(uint64(limit)), WithPayload: qdrant.NewWithPayload(true)})
	if e != nil {
		return nil, e
	}
	out := []Hit{}
	for _, p := range points {
		out = append(out, Hit{p.Id.GetUuid(), float64(p.Score), int(p.Payload["revision"].GetIntegerValue())})
	}
	return out, nil
}
func (q *Qdrant) Health(ctx context.Context) (uint64, error) {
	exists, e := q.Client.CollectionExists(ctx, q.Collection)
	if e != nil || !exists {
		return 0, e
	}
	i, e := q.Client.GetCollectionInfo(ctx, q.Collection)
	if e != nil {
		return 0, e
	}
	return i.GetPointsCount(), nil
}
