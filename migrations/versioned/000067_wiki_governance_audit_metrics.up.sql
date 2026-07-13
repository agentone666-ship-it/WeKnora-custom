-- Forward-compatible additions for installations that already applied the
-- initial wiki governance migration before audit excerpts/metrics landed.
ALTER TABLE wiki_change_items
  ADD COLUMN IF NOT EXISTS evidence_excerpts JSONB NOT NULL DEFAULT '{}'::JSONB;

ALTER TABLE wiki_reviews
  ADD COLUMN IF NOT EXISTS item_overrides JSONB NOT NULL DEFAULT '{}'::JSONB;

CREATE TABLE IF NOT EXISTS wiki_governance_metric_events (
  id VARCHAR(36) PRIMARY KEY,
  knowledge_base_id VARCHAR(36) NOT NULL,
  metric_name VARCHAR(64) NOT NULL,
  value BIGINT NOT NULL DEFAULT 1,
  metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_wiki_governance_metric_events
  ON wiki_governance_metric_events (knowledge_base_id, metric_name, created_at);
