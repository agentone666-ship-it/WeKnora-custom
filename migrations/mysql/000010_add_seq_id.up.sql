-- Migration 000010: Add seq_id (auto-increment integer ID) to chunks and knowledge_tags tables
-- This provides integer IDs for FAQ entries and tags for external API usage
-- MySQL version

-- ============================================================================
-- Section 1: Add seq_id to chunks table
-- ============================================================================

-- Add seq_id column with AUTO_INCREMENT (historical data starts from 1)
ALTER TABLE chunks ADD COLUMN seq_id BIGINT NOT NULL AUTO_INCREMENT UNIQUE KEY;

-- Update historical data to start from 100000000
UPDATE chunks SET seq_id = seq_id + 99999999;

-- Set AUTO_INCREMENT for future inserts (must be greater than max seq_id)
ALTER TABLE chunks AUTO_INCREMENT = 200000000;

-- ============================================================================
-- Section 2: Add seq_id to knowledge_tags table
-- ============================================================================

-- Add seq_id column with AUTO_INCREMENT (historical data starts from 1)
ALTER TABLE knowledge_tags ADD COLUMN seq_id BIGINT NOT NULL AUTO_INCREMENT UNIQUE KEY;

-- Update historical data to start from 10000000
UPDATE knowledge_tags SET seq_id = seq_id + 9999999;

-- Set AUTO_INCREMENT for future inserts (must be greater than max seq_id)
ALTER TABLE knowledge_tags AUTO_INCREMENT = 20000000;
