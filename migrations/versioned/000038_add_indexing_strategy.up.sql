-- Migration: 000038_add_indexing_strategy
-- Description: Add indexing_strategy column to knowledge_bases table.
-- Controls which indexing pipelines are active (vector, keyword, wiki, graph).
DO $$ BEGIN RAISE NOTICE '[Migration 000038] Adding indexing_strategy column to knowledge_bases'; END $$;

ALTER TABLE knowledge_bases ADD COLUMN IF NOT EXISTS indexing_strategy JSONB;

COMMENT ON COLUMN knowledge_bases.indexing_strategy IS 'Indexing pipelines strategy: {"vector_enabled": bool, "keyword_enabled": bool, "wiki_enabled": bool, "graph_enabled": bool}';

-- Backfill: existing rows get vector+keyword=true (legacy default behavior),
-- wiki_enabled synced from wiki_config.enabled, graph_enabled synced from extract_config.enabled.
UPDATE knowledge_bases
SET indexing_strategy = jsonb_build_object(
    'vector_enabled',  TRUE,
    'keyword_enabled', TRUE,
    'wiki_enabled',    FALSE,
    'graph_enabled',   FALSE
)
WHERE indexing_strategy IS NULL;

DO $$ BEGIN RAISE NOTICE '[Migration 000038] indexing_strategy column added and backfilled successfully'; END $$;
