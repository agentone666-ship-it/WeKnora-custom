package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/agent"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

type scenarioCardContext struct {
	DocumentNature  string   `json:"document_nature"`
	BusinessLines   []string `json:"business_lines"`
	Scenarios       []string `json:"scenarios"`
	Audiences       []string `json:"audiences"`
	AffectedMetrics []string `json:"affected_metrics"`
	BusinessActions []string `json:"business_actions"`
}
type scenarioCardCandidate struct {
	KnowledgeType    string                     `json:"knowledge_type"`
	Title            string                     `json:"title"`
	Statement        string                     `json:"statement"`
	Summary          string                     `json:"summary"`
	BusinessLine     string                     `json:"business_line"`
	Scenarios        []string                   `json:"scenarios"`
	AudienceRoles    []string                   `json:"audience_roles"`
	AffectedMetrics  []string                   `json:"affected_metrics"`
	AnswerStrength   string                     `json:"answer_strength"`
	Applicability    map[string]any             `json:"applicability"`
	ProhibitedClaims []string                   `json:"prohibited_claims"`
	SourceChunks     []string                   `json:"source_chunks"`
	Relationships    []scenarioCardRelationship `json:"relationships"`
}
type scenarioCardRelationship struct {
	TargetTitle  string  `json:"target_title"`
	RelationType string  `json:"relation_type"`
	Reason       string  `json:"reason"`
	Confidence   float64 `json:"confidence"`
}
type scenarioCardExtraction struct {
	Context scenarioCardContext     `json:"context"`
	Cards   []scenarioCardCandidate `json:"cards"`
}

func mergeCardStrings(a, b types.StringArray) types.StringArray {
	out := make(types.StringArray, 0, len(a)+len(b))
	seen := map[string]bool{}
	for _, values := range []types.StringArray{a, b} {
		for _, value := range values {
			if value != "" && !seen[value] {
				seen[value] = true
				out = append(out, value)
			}
		}
	}
	return out
}

func renderCardChunksXML(chunks []*types.Chunk) string {
	const maxRunes = 32000
	ordered := append([]*types.Chunk(nil), chunks...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ChunkIndex < ordered[j].ChunkIndex })
	var b strings.Builder
	used := 0
	for _, ch := range ordered {
		if ch == nil || strings.TrimSpace(ch.Content) == "" {
			continue
		}
		content := []rune(ch.Content)
		remaining := maxRunes - used
		if remaining <= 0 {
			break
		}
		if len(content) > remaining {
			content = content[:remaining]
		}
		fmt.Fprintf(&b, "<c id=%q index=\"%d\">\n%s\n</c>\n", ch.ID, ch.ChunkIndex, string(content))
		used += len(content)
	}
	return b.String()
}

func cardMarkdown(c scenarioCardCandidate, relationships []map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n", c.Title, strings.TrimSpace(c.Statement))
	if len(c.Scenarios) > 0 {
		fmt.Fprintf(&b, "\n## 适用场景\n%s\n", strings.Join(c.Scenarios, "、"))
	}
	if len(c.AffectedMetrics) > 0 {
		fmt.Fprintf(&b, "\n## 影响指标\n%s\n", strings.Join(c.AffectedMetrics, "、"))
	}
	if len(c.ProhibitedClaims) > 0 {
		fmt.Fprintf(&b, "\n## 禁止边界\n- %s\n", strings.Join(c.ProhibitedClaims, "\n- "))
	}
	if len(relationships) > 0 {
		b.WriteString("\n## 关联知识\n")
		for _, relation := range relationships {
			fmt.Fprintf(&b, "- [[%s|%s]]（%s）\n", relation["target_slug"], relation["target_title"], relation["relation_type"])
		}
	}
	return b.String()
}

func (s *wikiIngestService) extractAndSubmitScenarioCards(ctx context.Context, model chat.Chat, payload WikiIngestPayload, knowledgeID, sourceTitle, sourceUpdatedAt, lang string, chunks []*types.Chunk, forceReview bool) ([]types.WikiLogPageRef, int, error) {
	if s.governanceSvc == nil || len(chunks) == 0 {
		return nil, 0, nil
	}
	raw, err := s.generateWithTemplate(ctx, model, agent.WikiScenarioCardPrompt, map[string]string{"ChunksXML": renderCardChunksXML(chunks), "Language": lang})
	if err != nil {
		return nil, 0, err
	}
	raw = cleanLLMJSON(raw)
	var parsed scenarioCardExtraction
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, 0, fmt.Errorf("parse scenario cards: %w", err)
	}
	validChunk := map[string]bool{}
	chunkContent := map[string]string{}
	for _, ch := range chunks {
		if ch != nil {
			validChunk[ch.ID] = true
			chunkContent[ch.ID] = ch.Content
		}
	}
	scenarioByKey := map[string]string{}
	if scenarios, listErr := s.governanceSvc.ListScenarios(ctx, payload.KnowledgeBaseID); listErr == nil {
		for _, scenario := range scenarios {
			scenarioByKey[strings.ToLower(strings.TrimSpace(scenario.BusinessLine))+"\x00"+strings.ToLower(strings.TrimSpace(scenario.Name))] = scenario.ID
		}
	}
	refs := make([]types.WikiLogPageRef, 0, len(parsed.Cards))
	submitted := 0
	seenSlugs := map[string]bool{}
	titleToSlug := map[string]string{}
	for _, card := range parsed.Cards {
		if validKnowledgeTypes[card.KnowledgeType] && strings.TrimSpace(card.Title) != "" {
			titleToSlug[strings.ToLower(strings.TrimSpace(card.Title))] = fmt.Sprintf("card/%s-%s", card.KnowledgeType, slugify(card.Title))
		}
	}
	validRelationTypes := map[string]bool{"supports": true, "contradicts": true, "tests": true, "answers": true, "causes": true, "depends_on": true, "applies_to": true, "measures": true, "example_of": true, "mitigates": true}
	for _, card := range parsed.Cards {
		card.Title = strings.TrimSpace(card.Title)
		card.Statement = strings.TrimSpace(card.Statement)
		if card.Title == "" || card.Statement == "" || !validKnowledgeTypes[card.KnowledgeType] {
			continue
		}
		var evidence types.StringArray
		evidenceExcerpts := map[string]string{}
		for _, id := range card.SourceChunks {
			if validChunk[id] {
				evidence = append(evidence, id)
				runes := []rune(chunkContent[id])
				if len(runes) > 1000 {
					runes = runes[:1000]
				}
				evidenceExcerpts[id] = string(runes)
			}
		}
		slug := fmt.Sprintf("card/%s-%s", card.KnowledgeType, slugify(card.Title))
		if seenSlugs[slug] {
			continue
		}
		seenSlugs[slug] = true
		var relationships []map[string]any
		var outLinks types.StringArray
		for _, relation := range card.Relationships {
			targetTitle := strings.TrimSpace(relation.TargetTitle)
			targetSlug := titleToSlug[strings.ToLower(targetTitle)]
			if targetSlug == "" || targetSlug == slug || !validRelationTypes[relation.RelationType] || relation.Confidence < 0.7 || relation.Confidence > 1 {
				continue
			}
			outLinks = mergeCardStrings(outLinks, types.StringArray{targetSlug})
			relationships = append(relationships, map[string]any{"target_title": targetTitle, "target_slug": targetSlug, "relation_type": relation.RelationType, "reason": relation.Reason, "confidence": relation.Confidence})
		}
		metadata := map[string]any{"document_nature": parsed.Context.DocumentNature, "source_title": sourceTitle, "source_updated_at": sourceUpdatedAt, "business_actions": parsed.Context.BusinessActions, "scenario_names": card.Scenarios, "relationships": relationships, "evidence_excerpts": evidenceExcerpts, "force_review": forceReview}
		appBytes, _ := json.Marshal(card.Applicability)
		var scenarioIDs types.StringArray
		for _, name := range card.Scenarios {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(card.BusinessLine)) + "\x00" + strings.ToLower(name)
			id := scenarioByKey[key]
			if id != "" {
				scenarioIDs = append(scenarioIDs, id)
			}
		}
		existing, _ := s.wikiService.GetPageBySlug(ctx, payload.KnowledgeBaseID, slug)
		if existing == nil {
			if similar, similarErr := s.wikiService.FindSimilarPages(ctx, payload.KnowledgeBaseID, card.Title, []string{types.WikiPageTypeCard}, 3); similarErr == nil {
				for _, match := range similar {
					if match != nil && match.Slug != slug {
						metadata["possible_duplicate_slug"] = match.Slug
						metadata["possible_duplicate_title"] = match.Title
						break
					}
				}
			}
		}
		metaBytes, _ := json.Marshal(metadata)
		page := &types.WikiPage{TenantID: payload.TenantID, KnowledgeBaseID: payload.KnowledgeBaseID, Slug: slug, Title: card.Title, PageType: types.WikiPageTypeCard, KnowledgeType: card.KnowledgeType, MaturityStatus: types.WikiMaturityPendingReview, AnswerStrength: card.AnswerStrength, ReviewStatus: types.WikiReviewPending, BusinessLine: card.BusinessLine, ScenarioIDs: scenarioIDs, AudienceRoles: card.AudienceRoles, AffectedMetrics: card.AffectedMetrics, Applicability: types.JSON(appBytes), ProhibitedClaims: card.ProhibitedClaims, Content: cardMarkdown(card, relationships), Summary: card.Summary, SourceRefs: types.StringArray{knowledgeID}, ChunkRefs: evidence, OutLinks: outLinks, PageMetadata: types.JSON(metaBytes), Status: types.WikiPageStatusDraft}
		if existing != nil {
			page.Status = existing.Status
			page.MaturityStatus = existing.MaturityStatus
			page.EffectiveFrom = existing.EffectiveFrom
			page.EffectiveTo = existing.EffectiveTo
			page.SourceRefs = mergeCardStrings(existing.SourceRefs, page.SourceRefs)
			page.ChunkRefs = mergeCardStrings(existing.ChunkRefs, page.ChunkRefs)
			page.ScenarioIDs = mergeCardStrings(existing.ScenarioIDs, page.ScenarioIDs)
			page.AudienceRoles = mergeCardStrings(existing.AudienceRoles, page.AudienceRoles)
			page.AffectedMetrics = mergeCardStrings(existing.AffectedMetrics, page.AffectedMetrics)
		}
		set, err := s.governanceSvc.SubmitCandidate(ctx, page, existing, knowledgeID, model.GetModelID())
		if err != nil {
			logger.Warnf(ctx, "wiki cards: submit %s failed: %v", slug, err)
			continue
		}
		submitted++
		if set.Status == types.WikiChangeSetApplied {
			refs = append(refs, types.WikiLogPageRef{Slug: slug, Title: card.Title})
		}
	}
	return refs, submitted, nil
}
