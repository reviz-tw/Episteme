package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL, Addr, Origin, EmbedURL, RerankURL, Model, Collection, QdrantHost, QdrantKey, MCPToken string
	Dimension, QdrantPort                                                                              int
	SecureCookie, QdrantTLS                                                                            bool
	MetadataURL, MetadataModel                                                                         string
	MetadataEnabled                                                                                    bool
}

func env(k, v string) string {
	if s := os.Getenv(k); s != "" {
		return s
	}
	return v
}
func Load() Config {
	dim, _ := strconv.Atoi(env("EMBEDDING_DIMENSION", "1024"))
	port, _ := strconv.Atoi(env("QDRANT_GRPC_PORT", "6334"))
	return Config{DatabaseURL: env("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/episteme?sslmode=disable"),
		Addr: env("LISTEN_ADDR", "127.0.0.1:8080"), Origin: env("APP_ORIGIN", "http://localhost:3000"),
		EmbedURL: env("TEI_EMBED_URL", "http://localhost:8081"), RerankURL: env("TEI_RERANK_URL", "http://localhost:8082"),
		Model: env("EMBEDDING_MODEL", "BAAI/bge-m3"), Dimension: dim, Collection: env("QDRANT_COLLECTION", "knowledge_base"),
		QdrantHost: env("QDRANT_HOST", "localhost"), QdrantPort: port, QdrantKey: os.Getenv("QDRANT_API_KEY"), QdrantTLS: os.Getenv("QDRANT_TLS") == "true",
		SecureCookie: os.Getenv("COOKIE_SECURE") == "true", MCPToken: os.Getenv("MCP_TOKEN"),
		MetadataURL: env("METADATA_OLLAMA_URL", "http://127.0.0.1:11434"), MetadataModel: env("METADATA_MODEL", "gemma2:2b"), MetadataEnabled: env("METADATA_ENABLED", "true") == "true"}
}
func (c Config) ModelKey() string { return fmt.Sprintf("%s:%d", c.Model, c.Dimension) }
func (c Config) CollectionName() string {
	h := sha256.Sum256([]byte(c.ModelKey()))
	return fmt.Sprintf("%s_%x", c.Collection, h[:6])
}
