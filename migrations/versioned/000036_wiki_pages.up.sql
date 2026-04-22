-- Migration: 000036_wiki_pages
-- Description: Add wiki_pages table and wiki_config column to knowledge_bases.
-- Wiki pages are LLM-generated, interlinked markdown documents that form a persistent wiki.
DO $$ BEGIN RAISE NOTICE '[Migration 000036] Creating wiki_pages table and adding wiki_config column'; END $$;

-- Add wiki_config column to knowledge_bases
ALTER TABLE knowledge_bases ADD COLUMN IF NOT EXISTS wiki_config JSONB;

COMMENT ON COLUMN knowledge_bases.wiki_config IS 'Wiki configuration: {"auto_ingest": bool, "synthesis_model_id": string, "wiki_language": string, "max_pages_per_ingest": int}';

-- Create wiki_pages table
CREATE TABLE IF NOT EXISTS wiki_pages (
    id              VARCHAR(36) PRIMARY KEY,
    tenant_id       BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    slug            VARCHAR(255) NOT NULL,
    title           VARCHAR(512) NOT NULL DEFAULT '',
    page_type       VARCHAR(32) NOT NULL DEFAULT 'summary',
    status          VARCHAR(32) NOT NULL DEFAULT 'published',
    content         TEXT NOT NULL DEFAULT '',
    summary         TEXT NOT NULL DEFAULT '',
    source_refs     JSONB DEFAULT '[]'::JSONB,
    chunk_refs      JSONB DEFAULT '[]'::JSONB,
    in_links        JSONB DEFAULT '[]'::JSONB,
    out_links       JSONB DEFAULT '[]'::JSONB,
    page_metadata   JSONB DEFAULT '{}'::JSONB,
    aliases         JSONB DEFAULT '[]'::JSONB,
    version         INT NOT NULL DEFAULT 1,
    created_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMP WITH TIME ZONE
);

-- Unique constraint: slug must be unique within a knowledge base (for non-deleted pages)
CREATE UNIQUE INDEX IF NOT EXISTS idx_wiki_pages_kb_slug
    ON wiki_pages (knowledge_base_id, slug)
    WHERE deleted_at IS NULL;

-- Index for listing pages by knowledge base
CREATE INDEX IF NOT EXISTS idx_wiki_pages_kb_id
    ON wiki_pages (knowledge_base_id);

-- Index for filtering by page type
CREATE INDEX IF NOT EXISTS idx_wiki_pages_page_type
    ON wiki_pages (knowledge_base_id, page_type);

-- Index for tenant isolation
CREATE INDEX IF NOT EXISTS idx_wiki_pages_tenant_id
    ON wiki_pages (tenant_id);

-- Index for soft delete queries
CREATE INDEX IF NOT EXISTS idx_wiki_pages_deleted_at
    ON wiki_pages (deleted_at);

-- Full-text search index on title and content
CREATE INDEX IF NOT EXISTS idx_wiki_pages_fulltext
    ON wiki_pages USING GIN (to_tsvector('simple', coalesce(title, '') || ' ' || coalesce(content, '')));

DO $$ BEGIN RAISE NOTICE '[Migration 000036] wiki_pages table and wiki_config column created successfully'; END $$;
