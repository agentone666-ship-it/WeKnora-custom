DROP TABLE IF EXISTS wiki_governance_metric_events;
ALTER TABLE wiki_reviews DROP COLUMN IF EXISTS item_overrides;
ALTER TABLE wiki_change_items DROP COLUMN IF EXISTS evidence_excerpts;
