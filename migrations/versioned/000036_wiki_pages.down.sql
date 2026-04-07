-- Migration: 000032_wiki_pages (rollback)
-- Description: Remove wiki_pages table and wiki_config column from knowledge_bases.
DO $$ BEGIN RAISE NOTICE '[Migration 000032] Removing wiki_pages table and wiki_config column'; END $$;

DROP TABLE IF EXISTS wiki_pages;

ALTER TABLE knowledge_bases DROP COLUMN IF EXISTS wiki_config;

DO $$ BEGIN RAISE NOTICE '[Migration 000032] wiki_pages table and wiki_config column removed successfully'; END $$;
