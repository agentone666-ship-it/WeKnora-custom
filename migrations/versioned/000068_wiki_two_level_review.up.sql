ALTER TABLE wiki_change_sets
  ADD COLUMN IF NOT EXISTS change_category VARCHAR(32) NOT NULL DEFAULT 'update';
CREATE INDEX IF NOT EXISTS idx_wiki_change_sets_category
  ON wiki_change_sets (knowledge_base_id, status, change_category, created_at);

ALTER TABLE wiki_change_items
  ADD COLUMN IF NOT EXISTS change_category VARCHAR(32) NOT NULL DEFAULT 'update';
CREATE INDEX IF NOT EXISTS idx_wiki_change_items_category
  ON wiki_change_items (change_category);

ALTER TABLE wiki_reviews
  ADD COLUMN IF NOT EXISTS resolution VARCHAR(32) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS retained_claim TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS discarded_claims JSONB NOT NULL DEFAULT '[]'::JSONB;

-- L2 is a legacy representation of "human review required". Preserve all
-- reasons while normalizing existing queues and history to the L0/L1 model.
UPDATE wiki_change_sets SET review_level = 'L1' WHERE review_level = 'L2';

UPDATE wiki_change_sets
SET change_category = CASE
  WHEN reasons::text LIKE '%cross_page_claim_conflict%'
    OR reasons::text LIKE '%cross_page_claim_uncertain%'
    OR reasons::text LIKE '%cross_page_claim_supersedes_existing%' THEN 'conflict'
  WHEN reasons::text LIKE '%possible_duplicate%' THEN 'merge_duplicate'
  WHEN reasons::text LIKE '%rollback_requested%' THEN 'correction'
  ELSE change_category
END
WHERE change_category = 'update';

UPDATE wiki_change_items i
SET change_category = s.change_category
FROM wiki_change_sets s
WHERE i.change_set_id = s.id;

ALTER TABLE wiki_change_sets
  DROP CONSTRAINT IF EXISTS chk_wiki_change_sets_review_level;
ALTER TABLE wiki_change_sets
  ADD CONSTRAINT chk_wiki_change_sets_review_level CHECK (review_level IN ('L0', 'L1'));
