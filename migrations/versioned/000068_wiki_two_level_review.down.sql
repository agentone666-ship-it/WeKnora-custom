ALTER TABLE wiki_change_sets DROP CONSTRAINT IF EXISTS chk_wiki_change_sets_review_level;

DROP INDEX IF EXISTS idx_wiki_change_items_category;
ALTER TABLE wiki_change_items DROP COLUMN IF EXISTS change_category;

DROP INDEX IF EXISTS idx_wiki_change_sets_category;
ALTER TABLE wiki_change_sets DROP COLUMN IF EXISTS change_category;

ALTER TABLE wiki_reviews
  DROP COLUMN IF EXISTS resolution,
  DROP COLUMN IF EXISTS retained_claim,
  DROP COLUMN IF EXISTS discarded_claims;
