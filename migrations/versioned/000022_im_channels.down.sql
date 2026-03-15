-- Rollback: 000022_im_channels
DO $$ BEGIN RAISE NOTICE '[Migration 000022] Rolling back: im_channels'; END $$;

ALTER TABLE im_channel_sessions DROP COLUMN IF EXISTS im_channel_id;
DROP TABLE IF EXISTS im_channels;

DO $$ BEGIN RAISE NOTICE '[Migration 000022] Rollback completed'; END $$;
