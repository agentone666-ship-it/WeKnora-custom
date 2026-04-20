-- Migration: 000038_add_indexing_strategy (down)
-- Description: Remove indexing_strategy column from knowledge_bases.
DO $$ BEGIN RAISE NOTICE '[Migration 000038] Dropping indexing_strategy column from knowledge_bases'; END $$;

ALTER TABLE knowledge_bases DROP COLUMN IF EXISTS indexing_strategy;

DO $$ BEGIN RAISE NOTICE '[Migration 000038] indexing_strategy column dropped successfully'; END $$;
