CREATE TABLE IF NOT EXISTS users (
 id UUID PRIMARY KEY, username VARCHAR(50) UNIQUE NOT NULL, password_hash TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS sessions (
 token_hash TEXT PRIMARY KEY, user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 expires_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS documents (
 id UUID PRIMARY KEY, filename VARCHAR(255) NOT NULL, mime_type TEXT NOT NULL,
 storage_path TEXT NOT NULL, original BYTEA NOT NULL, source_markdown TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'draft', chunk_size INT NOT NULL DEFAULT 512,
 revision INT NOT NULL DEFAULT 1, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS document_chunks (
 id UUID PRIMARY KEY, document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
 chunk_index INT NOT NULL, content TEXT NOT NULL, raw_markdown TEXT NOT NULL,
 breadcrumbs JSONB NOT NULL DEFAULT '[]', token_count INT NOT NULL,
 is_reviewed BOOLEAN NOT NULL DEFAULT false, is_excluded BOOLEAN NOT NULL DEFAULT false,
 is_dirty BOOLEAN NOT NULL DEFAULT true, qdrant_point_id UUID,
 metadata JSONB NOT NULL DEFAULT '{}', source_start INT NOT NULL, source_end INT NOT NULL,
 revision INT NOT NULL DEFAULT 1, indexed_revision INT NOT NULL DEFAULT 0,
 indexed_content TEXT NOT NULL DEFAULT '', indexed_model TEXT NOT NULL DEFAULT '',
 indexed_breadcrumbs JSONB NOT NULL DEFAULT '[]',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(document_id, chunk_index) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX IF NOT EXISTS idx_chunks_doc_id ON document_chunks(document_id);
CREATE INDEX IF NOT EXISTS idx_chunks_dirty ON document_chunks(document_id,is_dirty);
CREATE TABLE IF NOT EXISTS indexing_jobs (
 id UUID PRIMARY KEY, kind TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'queued',
 total INT NOT NULL DEFAULT 0, completed INT NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Tasks deliberately survive document/chunk deletion: this is the durable outbox.
CREATE TABLE IF NOT EXISTS indexing_tasks (
 id BIGSERIAL PRIMARY KEY, job_id UUID NOT NULL REFERENCES indexing_jobs(id) ON DELETE CASCADE,
 document_id UUID NOT NULL, chunk_id UUID NOT NULL, kind TEXT NOT NULL,
 collection TEXT NOT NULL DEFAULT '', attempts INT NOT NULL DEFAULT 0,
 done BOOLEAN NOT NULL DEFAULT false, available_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_tasks_pending ON indexing_tasks(available_at,id) WHERE NOT done;

-- Additive upgrades also run against workspaces created before document metadata.
ALTER TABLE documents ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}';
ALTER TABLE document_chunks ADD COLUMN IF NOT EXISTS indexed_document_metadata JSONB NOT NULL DEFAULT '{}';
ALTER TABLE indexing_tasks ADD COLUMN IF NOT EXISTS document_metadata JSONB NOT NULL DEFAULT '{}';
ALTER TABLE documents ADD COLUMN IF NOT EXISTS metadata_revision INT NOT NULL DEFAULT 1;
CREATE TABLE IF NOT EXISTS metadata_jobs (
 document_id UUID PRIMARY KEY REFERENCES documents(id) ON DELETE CASCADE,
 status TEXT NOT NULL DEFAULT 'queued', model TEXT NOT NULL,
 base_revision INT NOT NULL, attempts INT NOT NULL DEFAULT 0,
 error TEXT NOT NULL DEFAULT '', sampled BOOLEAN NOT NULL DEFAULT false,
 result JSONB NOT NULL DEFAULT '{}', run_token UUID,
 available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_metadata_pending ON metadata_jobs(available_at) WHERE status IN ('queued','running');
