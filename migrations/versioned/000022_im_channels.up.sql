-- Migration: 000022_im_channels
-- Description: Create IM channels table for database-driven IM integration (replaces config.yaml IM settings)
DO $$ BEGIN RAISE NOTICE '[Migration 000022] Creating table: im_channels'; END $$;

CREATE TABLE IF NOT EXISTS im_channels (
    id VARCHAR(36) PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id BIGINT NOT NULL,
    agent_id VARCHAR(36) NOT NULL,
    platform VARCHAR(20) NOT NULL,
    name VARCHAR(255) NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT true,
    mode VARCHAR(20) NOT NULL DEFAULT 'websocket',
    output_mode VARCHAR(20) NOT NULL DEFAULT 'stream',
    credentials JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_im_channels_tenant ON im_channels (tenant_id);
CREATE INDEX IF NOT EXISTS idx_im_channels_agent ON im_channels (agent_id);
CREATE INDEX IF NOT EXISTS idx_im_channels_deleted ON im_channels (deleted_at) WHERE deleted_at IS NOT NULL;

COMMENT ON TABLE im_channels IS 'IM platform channel configurations bound to agents';
COMMENT ON COLUMN im_channels.agent_id IS 'Agent ID this channel is bound to';
COMMENT ON COLUMN im_channels.platform IS 'IM platform: wecom, feishu';
COMMENT ON COLUMN im_channels.name IS 'User-defined channel name for identification';
COMMENT ON COLUMN im_channels.mode IS 'Connection mode: webhook or websocket';
COMMENT ON COLUMN im_channels.output_mode IS 'Output mode: stream (real-time) or full (wait for complete answer)';
COMMENT ON COLUMN im_channels.credentials IS 'Platform credentials (JSONB): WeCom webhook={corp_id,agent_secret,token,encoding_aes_key,corp_agent_id}, WeCom ws={bot_id,bot_secret}, Feishu={app_id,app_secret,verification_token,encrypt_key}';

-- Add im_channel_id column to im_channel_sessions for linking
ALTER TABLE im_channel_sessions ADD COLUMN IF NOT EXISTS im_channel_id VARCHAR(36) DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_im_channel_sessions_channel ON im_channel_sessions (im_channel_id) WHERE im_channel_id != '';

DO $$ BEGIN RAISE NOTICE '[Migration 000022] im_channels setup completed successfully!'; END $$;
