package chatpipeline

import (
	"context"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// wikiBoostFactor is the score multiplier applied to wiki page chunks.
// Wiki pages contain LLM-synthesized, cross-referenced knowledge and should
// be preferred over raw document chunks when both are available.
const wikiBoostFactor = 1.3

// PluginWikiBoost boosts the relevance score of wiki page chunks in search results.
// Wiki pages contain pre-synthesized knowledge that is more coherent and
// cross-referenced than raw document chunks, so they should rank higher.
//
// This plugin runs in the CHUNK_RERANK phase, after initial retrieval and reranking.
// It identifies chunks with ChunkType == "wiki_page" and multiplies their score.
type PluginWikiBoost struct {
	kbService interfaces.KnowledgeBaseService
}

// NewPluginWikiBoost creates and registers the wiki boost plugin
func NewPluginWikiBoost(eventManager *EventManager, kbService interfaces.KnowledgeBaseService) *PluginWikiBoost {
	p := &PluginWikiBoost{
		kbService: kbService,
	}
	eventManager.Register(p)
	return p
}

// ActivationEvents returns the event types this plugin handles
func (p *PluginWikiBoost) ActivationEvents() []types.EventType {
	return []types.EventType{types.CHUNK_RERANK}
}

// OnEvent boosts wiki page chunk scores after reranking
func (p *PluginWikiBoost) OnEvent(
	ctx context.Context,
	eventType types.EventType,
	chatManage *types.ChatManage,
	next func() *PluginError,
) *PluginError {
	// Run the normal reranking first
	if err := next(); err != nil {
		return err
	}

	// Check if any search target is a wiki KB
	hasWikiKB := false
	for _, target := range chatManage.SearchTargets {
		kb, err := p.kbService.GetKnowledgeBaseByIDOnly(ctx, target.KnowledgeBaseID)
		if err == nil && kb.Type == types.KnowledgeBaseTypeWiki {
			hasWikiKB = true
			break
		}
	}

	if !hasWikiKB {
		return nil
	}

	// Boost wiki page chunks in RerankResult
	boostedCount := 0
	for i := range chatManage.RerankResult {
		if chatManage.RerankResult[i].ChunkType == types.ChunkTypeWikiPage {
			chatManage.RerankResult[i].Score *= wikiBoostFactor
			boostedCount++
		}
	}

	if boostedCount > 0 {
		logger.Infof(ctx, "WikiBoost: boosted %d wiki page chunks by %.1fx", boostedCount, wikiBoostFactor)

		// Re-sort by score after boosting
		for i := 0; i < len(chatManage.RerankResult); i++ {
			for j := i + 1; j < len(chatManage.RerankResult); j++ {
				if chatManage.RerankResult[j].Score > chatManage.RerankResult[i].Score {
					chatManage.RerankResult[i], chatManage.RerankResult[j] = chatManage.RerankResult[j], chatManage.RerankResult[i]
				}
			}
		}
	}

	return nil
}
