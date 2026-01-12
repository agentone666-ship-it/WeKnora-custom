-- Migration 000010: Remove seq_id from chunks and knowledge_tags tables
-- MySQL version

ALTER TABLE chunks DROP COLUMN seq_id;
ALTER TABLE knowledge_tags DROP COLUMN seq_id;
