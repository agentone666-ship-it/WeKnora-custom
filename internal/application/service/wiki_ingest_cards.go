package service

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Tencent/WeKnora/internal/agent"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"golang.org/x/sync/errgroup"
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

type crossPageAssessment struct {
	RelatedSlug          string  `json:"related_slug"`
	Relation             string  `json:"relation"`
	CandidateClaim       string  `json:"candidate_claim"`
	ExistingClaim        string  `json:"existing_claim"`
	Reason               string  `json:"reason"`
	Confidence           float64 `json:"confidence"`
	ApplicabilityOverlap bool    `json:"applicability_overlap"`
}

type crossPageAssessmentResult struct {
	Assessments []crossPageAssessment `json:"assessments"`
}

var validCrossPageRelations = map[string]bool{
	"consistent": true, "complementary": true, "conflicting": true,
	"supersedes": true, "unrelated": true, "uncertain": true,
}

var validSemanticCardRelations = map[string]bool{
	"supports": true, "contradicts": true, "tests": true, "answers": true,
	"causes": true, "depends_on": true, "applies_to": true, "measures": true,
	"example_of": true, "mitigates": true,
}

var crossPageRelationLabels = map[string]string{
	"consistent":    "内容一致",
	"complementary": "内容互补",
	"conflicting":   "说法冲突",
	"supersedes":    "新内容替代旧内容",
	"unrelated":     "暂无直接关联",
	"uncertain":     "关系待确认",
}

func crossPageRelationLabel(value any) string {
	relation := fmt.Sprint(value)
	if label := crossPageRelationLabels[relation]; label != "" {
		return label
	}
	return "关系待确认"
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
			fmt.Fprintf(&b, "- [[%s|%s]]（%s）\n", relation["target_slug"], relation["target_title"], crossPageRelationLabel(relation["relation_type"]))
		}
	}
	return b.String()
}

func crossPageQuerySignals(card scenarioCardCandidate) []string {
	statement := card.Statement
	if strings.TrimSpace(statement) == "" {
		statement = card.Summary
	}
	contextTerms := append([]string(nil), card.AffectedMetrics...)
	contextTerms = append(contextTerms, card.Scenarios...)
	values := []string{card.Title, statement, strings.Join(contextTerms, " ")}
	out := make([]string, 0, 3)
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		runes := []rune(value)
		if len(runes) > 500 {
			value = string(runes[:500])
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
		if len(out) == 3 {
			break
		}
	}
	return out
}

func renderRelatedPagesXML(pages []*types.WikiPage) string {
	var b strings.Builder
	for _, page := range pages {
		if page == nil {
			continue
		}
		content := strings.TrimSpace(page.Content)
		runes := []rune(content)
		if len(runes) > 3000 {
			content = string(runes[:3000])
		}
		fmt.Fprintf(&b, "<page slug=%q type=%q knowledge_type=%q review_status=%q maturity_status=%q>\n", html.EscapeString(page.Slug), html.EscapeString(page.PageType), html.EscapeString(page.KnowledgeType), html.EscapeString(page.ReviewStatus), html.EscapeString(page.MaturityStatus))
		fmt.Fprintf(&b, "<title>%s</title>\n<summary>%s</summary>\n<applicability>%s</applicability>\n<content>%s</content>\n</page>\n", html.EscapeString(page.Title), html.EscapeString(page.Summary), html.EscapeString(string(page.Applicability)), html.EscapeString(content))
	}
	return b.String()
}

func normalizeCrossPageAssessments(raw string, related map[string]*types.WikiPage) ([]crossPageAssessment, int, error) {
	var parsed crossPageAssessmentResult
	if err := json.Unmarshal([]byte(cleanLLMJSON(raw)), &parsed); err != nil {
		return nil, len(related), fmt.Errorf("parse cross-page assessments: %w", err)
	}
	out := make([]crossPageAssessment, 0, len(parsed.Assessments))
	seen := map[string]bool{}
	for _, item := range parsed.Assessments {
		item.RelatedSlug = strings.TrimSpace(item.RelatedSlug)
		item.Relation = strings.ToLower(strings.TrimSpace(item.Relation))
		if related[item.RelatedSlug] == nil || seen[item.RelatedSlug] || !validCrossPageRelations[item.Relation] || item.Confidence < 0 || item.Confidence > 1 {
			continue
		}
		// Two claims with explicitly disjoint applicability cannot conflict.
		// Preserve the model's explanation but downgrade the relationship.
		if item.Relation == "conflicting" && !item.ApplicabilityOverlap {
			item.Relation = "complementary"
		}
		seen[item.RelatedSlug] = true
		out = append(out, item)
	}
	return out, len(related) - len(seen), nil
}

func applyCrossPageReviewMetadata(metadata map[string]any, assessments []crossPageAssessment, missing int, checkErr error) {
	metadata["cross_page_assessments"] = assessments
	if checkErr != nil {
		metadata["cross_page_check_failed"] = true
		metadata["cross_page_check_error"] = checkErr.Error()
		return
	}
	if missing > 0 {
		metadata["cross_page_check_incomplete"] = true
		metadata["cross_page_missing_assessments"] = missing
	}
	for _, item := range assessments {
		switch item.Relation {
		case "conflicting":
			if item.ApplicabilityOverlap && item.Confidence >= 0.75 {
				metadata["cross_page_conflict"] = true
			}
		case "uncertain":
			if item.ApplicabilityOverlap && item.Confidence >= 0.65 {
				metadata["cross_page_uncertain"] = true
			}
		case "supersedes":
			if item.Confidence >= 0.75 {
				metadata["cross_page_supersedes"] = true
			}
		}
	}
}

// crossPageCorrectionTarget locates the approved card an authoritative
// correction candidate should update, even when the LLM emits a different
// title/slug for the same rule. It returns the target page together with the
// method used to find it ("cross_page_authoritative_match" or
// "deterministic_content_match"), so the caller can record the method for the
// downstream safety gate.
func (s *wikiIngestService) crossPageCorrectionTarget(ctx context.Context, kbID, candidateSlug string, card scenarioCardCandidate, evidenceText string, assessments []crossPageAssessment) (*types.WikiPage, string) {
	if !containsAnyFold(evidenceText, explicitCorrectionSignals) || containsAnyFold(card.Title+" "+card.Statement+" "+card.Summary, uncertaintySignals) || containsAnyFold(card.Title+" "+card.Statement+" "+card.Summary, highRiskSignals) {
		return nil, ""
	}
	// Path 1: a high-confidence LLM cross-page assessment explicitly marks the
	// candidate as conflicting with or superseding an approved card.
	ordered := append([]crossPageAssessment(nil), assessments...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Confidence > ordered[j].Confidence })
	for _, assessment := range ordered {
		if (assessment.Relation != "conflicting" && assessment.Relation != "supersedes") || assessment.Confidence < 0.9 || !assessment.ApplicabilityOverlap {
			continue
		}
		page, err := s.wikiService.GetPageBySlug(ctx, kbID, assessment.RelatedSlug)
		if err != nil || page == nil || page.PageType != types.WikiPageTypeCard || page.ReviewStatus != types.WikiReviewApproved || page.KnowledgeType != card.KnowledgeType {
			continue
		}
		return page, "cross_page_authoritative_match"
	}
	// Path 2: deterministic identity match. When the source explicitly states a
	// correction but the LLM cross-page assessment did not reach the
	// high-confidence bar — common when the LLM produces a different title/slug
	// (e.g. a different language) for the same rule — locate the displaced
	// approved card by content-aware retrieval and accept it only when the
	// candidate and the existing card describe the same rule: same knowledge
	// type, overlapping applicability, and strong textual overlap of the
	// governing claim. Every other safety check is still enforced by
	// isSafeAutomaticCorrection downstream.
	candidateClaim := strings.TrimSpace(card.Statement)
	if candidateClaim == "" {
		candidateClaim = strings.TrimSpace(card.Summary)
	}
	if candidateClaim == "" {
		return nil, ""
	}
	pages, err := s.wikiService.FindRelatedPages(ctx, kbID, candidateSlug, candidateClaim, []string{types.WikiPageTypeCard}, 8)
	if err != nil {
		return nil, ""
	}
	candidateAppBytes, _ := json.Marshal(card.Applicability)
	candidateApp := types.JSON(candidateAppBytes)
	var best *types.WikiPage
	bestScore := 0.0
	for _, page := range pages {
		if page == nil || page.PageType != types.WikiPageTypeCard || page.ReviewStatus != types.WikiReviewApproved || page.KnowledgeType != card.KnowledgeType {
			continue
		}
		if !correctionApplicabilityOverlaps(page.Applicability, candidateApp) {
			continue
		}
		existingText := strings.TrimSpace(page.Title) + " " + strings.TrimSpace(page.Summary) + " " + strings.TrimSpace(page.Content)
		score := claimOverlapScore(candidateClaim, existingText)
		if score < correctionClaimOverlapThreshold || score <= bestScore {
			continue
		}
		best = page
		bestScore = score
	}
	if best != nil {
		return best, "deterministic_content_match"
	}
	return nil, ""
}

func (s *wikiIngestService) assessCrossPageRelations(ctx context.Context, model chat.Chat, payload WikiIngestPayload, slug, lang string, card scenarioCardCandidate) ([]crossPageAssessment, map[string]any, int, bool, error) {
	pageBySlug := map[string]*types.WikiPage{}
	for _, signal := range crossPageQuerySignals(card) {
		pages, err := s.wikiService.FindRelatedPages(ctx, payload.KnowledgeBaseID, slug, signal, []string{types.WikiPageTypeCard, types.WikiPageTypeEntity, types.WikiPageTypeConcept}, 6)
		if err != nil {
			return nil, nil, 0, true, fmt.Errorf("find related wiki pages: %w", err)
		}
		for _, page := range pages {
			if page != nil && page.Slug != "" && page.Slug != slug {
				pageBySlug[page.Slug] = page
			}
		}
		if len(pageBySlug) >= 12 {
			break
		}
	}
	if len(pageBySlug) == 0 {
		return nil, nil, 0, false, nil
	}
	pages := make([]*types.WikiPage, 0, len(pageBySlug))
	for _, page := range pageBySlug {
		pages = append(pages, page)
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].Slug < pages[j].Slug })
	if len(pages) > 12 {
		pages = pages[:12]
		pageBySlug = map[string]*types.WikiPage{}
		for _, page := range pages {
			pageBySlug[page.Slug] = page
		}
	}
	return s.assessCrossPageRelationsAgainst(ctx, model, slug, lang, card, pages)
}

func (s *wikiIngestService) assessCrossPageRelationsAgainst(ctx context.Context, model chat.Chat, slug, lang string, card scenarioCardCandidate, pages []*types.WikiPage) ([]crossPageAssessment, map[string]any, int, bool, error) {
	pageBySlug := make(map[string]*types.WikiPage, len(pages))
	filtered := make([]*types.WikiPage, 0, len(pages))
	for _, page := range pages {
		if page == nil || page.Slug == "" || page.Slug == slug || pageBySlug[page.Slug] != nil {
			continue
		}
		pageBySlug[page.Slug] = page
		filtered = append(filtered, page)
	}
	if len(filtered) == 0 {
		return nil, nil, 0, false, nil
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].Slug < filtered[j].Slug })
	if len(filtered) > 12 {
		filtered = filtered[:12]
		pageBySlug = map[string]*types.WikiPage{}
		for _, page := range filtered {
			pageBySlug[page.Slug] = page
		}
	}
	evidence := make(map[string]any, len(filtered))
	for _, page := range filtered {
		excerpt := []rune(strings.TrimSpace(page.Content))
		if len(excerpt) > 1000 {
			excerpt = excerpt[:1000]
		}
		evidence[page.Slug] = map[string]any{"title": page.Title, "page_type": page.PageType, "knowledge_type": page.KnowledgeType, "source_refs": page.SourceRefs, "chunk_refs": page.ChunkRefs, "applicability": page.Applicability, "effective_from": page.EffectiveFrom, "effective_to": page.EffectiveTo, "excerpt": string(excerpt)}
	}
	candidateJSON, _ := json.Marshal(map[string]any{
		"slug": slug, "title": card.Title, "knowledge_type": card.KnowledgeType,
		"statement": card.Statement, "summary": card.Summary, "business_line": card.BusinessLine,
		"scenarios": card.Scenarios, "affected_metrics": card.AffectedMetrics,
		"applicability": card.Applicability, "prohibited_claims": card.ProhibitedClaims,
	})
	raw, err := s.generateWithTemplate(ctx, model, agent.WikiCrossPageConflictPrompt, map[string]string{"CandidateJSON": string(candidateJSON), "RelatedPagesXML": renderRelatedPagesXML(filtered), "Language": lang})
	if err != nil {
		return nil, evidence, 0, true, fmt.Errorf("assess cross-page conflicts: %w", err)
	}
	assessments, missing, err := normalizeCrossPageAssessments(raw, pageBySlug)
	if err != nil {
		return nil, evidence, missing, true, err
	}
	return assessments, evidence, missing, true, nil
}

func scenarioCardFromPage(page *types.WikiPage) scenarioCardCandidate {
	var applicability map[string]any
	_ = json.Unmarshal(page.Applicability, &applicability)
	meta, _ := page.PageMetadata.Map()
	scenarios := metadataStringSlice(meta["scenario_names"])
	statement := strings.TrimSpace(page.Summary)
	if statement == "" {
		body := strings.TrimSpace(strings.TrimPrefix(page.Content, "# "+page.Title))
		if i := strings.Index(body, "\n## "); i >= 0 {
			body = body[:i]
		}
		statement = strings.TrimSpace(body)
	}
	return scenarioCardCandidate{KnowledgeType: page.KnowledgeType, Title: page.Title, Statement: statement, Summary: page.Summary, BusinessLine: page.BusinessLine, Scenarios: scenarios, AudienceRoles: page.AudienceRoles, AffectedMetrics: page.AffectedMetrics, AnswerStrength: page.AnswerStrength, Applicability: applicability, ProhibitedClaims: page.ProhibitedClaims}
}

func replaceCardRelationshipSection(content string, relationships []map[string]any) string {
	const heading = "\n## 关联知识\n"
	if start := strings.Index(content, heading); start >= 0 {
		rest := content[start+len(heading):]
		if next := strings.Index(rest, "\n## "); next >= 0 {
			content = content[:start] + rest[next:]
		} else {
			content = strings.TrimRight(content[:start], "\n") + "\n"
		}
	}
	if len(relationships) == 0 {
		return strings.TrimRight(content, "\n") + "\n"
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(content, "\n"))
	b.WriteString(heading)
	for _, relation := range relationships {
		fmt.Fprintf(&b, "- [[%s|%s]]（%s）\n", relation["target_slug"], relation["target_title"], crossPageRelationLabel(relation["relation_type"]))
	}
	return b.String()
}

func applyCardGraph(page *types.WikiPage, assessments []crossPageAssessment, evidence map[string]any) error {
	metadata, _ := page.PageMetadata.Map()
	if metadata == nil {
		metadata = map[string]any{}
	}
	for _, key := range []string{"cross_page_assessments", "cross_page_check_failed", "cross_page_check_error", "cross_page_check_incomplete", "cross_page_missing_assessments", "cross_page_conflict", "cross_page_uncertain", "cross_page_supersedes", "related_page_evidence"} {
		delete(metadata, key)
	}
	applyCrossPageReviewMetadata(metadata, assessments, 0, nil)
	if len(evidence) > 0 {
		metadata["related_page_evidence"] = evidence
	}
	var relationships []map[string]any
	if raw, ok := metadata["relationships"].([]any); ok {
		for _, value := range raw {
			relation, ok := value.(map[string]any)
			typeName, _ := relation["relation_type"].(string)
			if ok && validSemanticCardRelations[typeName] {
				relationships = append(relationships, relation)
			}
		}
	}
	seen := map[string]bool{}
	for _, relation := range relationships {
		if slug, _ := relation["target_slug"].(string); slug != "" {
			seen[slug] = true
		}
	}
	for _, assessment := range assessments {
		if assessment.RelatedSlug == page.Slug || assessment.Confidence < 0.7 || assessment.Relation == "unrelated" || assessment.Relation == "uncertain" || seen[assessment.RelatedSlug] {
			continue
		}
		title := assessment.RelatedSlug
		if related, ok := evidence[assessment.RelatedSlug].(map[string]any); ok {
			if value, ok := related["title"].(string); ok && value != "" {
				title = value
			}
		}
		seen[assessment.RelatedSlug] = true
		relationships = append(relationships, map[string]any{"target_title": title, "target_slug": assessment.RelatedSlug, "relation_type": assessment.Relation, "reason": assessment.Reason, "confidence": assessment.Confidence, "applicability_overlap": assessment.ApplicabilityOverlap, "graph_source": "convergence"})
	}
	metadata["relationships"] = relationships
	metadata["graph_rebuilt_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	page.PageMetadata = types.JSON(metadataBytes)
	page.Content = replaceCardRelationshipSection(page.Content, relationships)
	links := make(types.StringArray, 0, len(relationships))
	for _, relation := range relationships {
		if slug, _ := relation["target_slug"].(string); slug != "" && slug != page.Slug {
			links = mergeCardStrings(links, types.StringArray{slug})
		}
	}
	page.OutLinks = links
	return nil
}

type scoredGraphPage struct {
	page  *types.WikiPage
	score float64
}

func (s *wikiIngestService) relatedGraphPages(ctx context.Context, kbID string, page *types.WikiPage, pending []*types.WikiPendingGraphCard, includePending bool) ([]*types.WikiPage, error) {
	card := scenarioCardFromPage(page)
	bySlug := map[string]*types.WikiPage{}
	for _, signal := range crossPageQuerySignals(card) {
		pages, err := s.wikiService.FindRelatedPages(ctx, kbID, page.Slug, signal, []string{types.WikiPageTypeCard}, 6)
		if err != nil {
			return nil, err
		}
		for _, related := range pages {
			if related != nil && related.Slug != "" && related.Slug != page.Slug && related.ReviewStatus == types.WikiReviewApproved {
				bySlug[related.Slug] = related
			}
		}
	}
	if includePending {
		candidateText := strings.TrimSpace(page.Title + " " + page.Summary + " " + page.Content)
		scored := make([]scoredGraphPage, 0, len(pending))
		latest := map[string]*types.WikiPage{}
		for _, candidate := range pending {
			if candidate != nil && candidate.Page != nil {
				latest[candidate.Page.Slug] = candidate.Page
			}
		}
		for slug, related := range latest {
			if slug == "" || slug == page.Slug {
				continue
			}
			relatedText := strings.TrimSpace(related.Title + " " + related.Summary + " " + related.Content)
			score := claimOverlapScore(candidateText, relatedText)
			if page.BusinessLine != "" && page.BusinessLine == related.BusinessLine {
				score += 0.08
			}
			if score >= 0.12 {
				scored = append(scored, scoredGraphPage{page: related, score: score})
			}
		}
		sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
		for i, item := range scored {
			if i >= 12 {
				break
			}
			bySlug[item.page.Slug] = item.page
		}
	}
	out := make([]*types.WikiPage, 0, len(bySlug))
	for _, related := range bySlug {
		out = append(out, related)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	if len(out) > 12 {
		out = out[:12]
	}
	return out, nil
}

// rebuildGovernedCardGraphs converges the two graph layers after ingest or a
// review decision. Pending snapshots may see pending + approved cards, while
// published pages may only see approved cards. This separation prevents
// unreviewed claims from leaking into the graph used by ordinary RAG answers.
func (s *wikiIngestService) rebuildGovernedCardGraphs(ctx context.Context, model chat.Chat, payload WikiIngestPayload, affectedSlugs []string, lang string) (int, int, error) {
	if s.governanceSvc == nil {
		return 0, 0, nil
	}
	pending, err := s.governanceSvc.ListPendingGraphCards(ctx, payload.KnowledgeBaseID, 2000)
	if err != nil {
		return 0, 0, err
	}
	// Keep an immutable candidate pool for relation retrieval. Workers mutate
	// their own page copy, never the page another worker is reading.
	pendingPool := make([]*types.WikiPendingGraphCard, 0, len(pending))
	for _, candidate := range pending {
		if candidate == nil || candidate.Page == nil {
			continue
		}
		pageCopy := *candidate.Page
		pendingPool = append(pendingPool, &types.WikiPendingGraphCard{ChangeSetID: candidate.ChangeSetID, ChangeItemID: candidate.ChangeItemID, Page: &pageCopy})
	}
	var pendingUpdated atomic.Int64
	// Rebuild all pending snapshots. A rejection can invalidate incoming
	// candidate edges on cards other than the rejected card itself.
	var pendingGroup errgroup.Group
	pendingGroup.SetLimit(4)
	for _, candidate := range pendingPool {
		if candidate == nil || candidate.Page == nil {
			continue
		}
		candidate := candidate
		pendingGroup.Go(func() error {
			pageCopy := *candidate.Page
			related, relErr := s.relatedGraphPages(ctx, payload.KnowledgeBaseID, &pageCopy, pendingPool, true)
			if relErr != nil {
				logger.Warnf(ctx, "wiki graph: find pending relations for %s failed: %v", pageCopy.Slug, relErr)
				return nil
			}
			assessments, evidence, _, attempted, assessErr := s.assessCrossPageRelationsAgainst(ctx, model, pageCopy.Slug, lang, scenarioCardFromPage(&pageCopy), related)
			if assessErr != nil {
				logger.Warnf(ctx, "wiki graph: assess pending relations for %s failed: %v", pageCopy.Slug, assessErr)
				return nil
			}
			if !attempted {
				assessments, evidence = nil, nil
			}
			if err := applyCardGraph(&pageCopy, assessments, evidence); err != nil {
				return nil
			}
			if updated, updateErr := s.governanceSvc.UpdatePendingGraphCard(ctx, payload.KnowledgeBaseID, candidate.ChangeSetID, candidate.ChangeItemID, &pageCopy); updateErr != nil {
				logger.Warnf(ctx, "wiki graph: update pending snapshot %s failed: %v", pageCopy.Slug, updateErr)
			} else if updated {
				pendingUpdated.Add(1)
			}
			return nil
		})
	}
	_ = pendingGroup.Wait()

	var publishedUpdated atomic.Int64
	seen := map[string]bool{}
	var publishedGroup errgroup.Group
	publishedGroup.SetLimit(4)
	for _, slug := range affectedSlugs {
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		slug := slug
		publishedGroup.Go(func() error {
			page, pageErr := s.wikiService.GetPageBySlug(ctx, payload.KnowledgeBaseID, slug)
			if pageErr != nil || page == nil || page.PageType != types.WikiPageTypeCard || page.ReviewStatus != types.WikiReviewApproved || page.Status == types.WikiPageStatusArchived {
				return nil
			}
			related, relErr := s.relatedGraphPages(ctx, payload.KnowledgeBaseID, page, nil, false)
			if relErr != nil {
				logger.Warnf(ctx, "wiki graph: find published relations for %s failed: %v", slug, relErr)
				return nil
			}
			assessments, evidence, _, attempted, assessErr := s.assessCrossPageRelationsAgainst(ctx, model, slug, lang, scenarioCardFromPage(page), related)
			if assessErr != nil {
				logger.Warnf(ctx, "wiki graph: assess published relations for %s failed: %v", slug, assessErr)
				return nil
			}
			if !attempted {
				assessments, evidence = nil, nil
			}
			if err := applyCardGraph(page, assessments, evidence); err != nil {
				return nil
			}
			metadata := append([]byte(nil), page.PageMetadata...)
			if err := s.wikiService.UpdateAutoLinkedContent(ctx, page); err != nil {
				logger.Warnf(ctx, "wiki graph: update published links for %s failed: %v", slug, err)
				return nil
			}
			fresh, freshErr := s.wikiService.GetPageBySlug(ctx, payload.KnowledgeBaseID, slug)
			if freshErr == nil && fresh != nil {
				fresh.PageMetadata = types.JSON(metadata)
				if metaErr := s.wikiService.UpdatePageMeta(ctx, fresh); metaErr != nil {
					logger.Warnf(ctx, "wiki graph: update published metadata for %s failed: %v", slug, metaErr)
				}
			}
			publishedUpdated.Add(1)
			return nil
		})
	}
	_ = publishedGroup.Wait()
	return int(pendingUpdated.Load()), int(publishedUpdated.Load()), nil
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
	// documentEvidenceText is the whole source document's chunk text. It is used
	// to detect explicit correction intent even when the LLM splits the
	// correction instruction into a sibling card whose own evidence excerpt does
	// not contain the "废止/supersedes" signal. The per-card evidence_excerpts
	// still gate the actual L0 auto-apply downstream, so this only widens the
	// target-discovery step.
	var documentEvidenceText strings.Builder
	for _, ch := range chunks {
		if ch == nil {
			continue
		}
		content := strings.TrimSpace(ch.Content)
		if content == "" {
			continue
		}
		if documentEvidenceText.Len() > 32000 {
			break
		}
		documentEvidenceText.WriteString(content)
		documentEvidenceText.WriteByte('\n')
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
		assessments, relatedEvidence, missingAssessments, attemptedCrossCheck, crossCheckErr := s.assessCrossPageRelations(ctx, model, payload, slug, lang, card)
		if attemptedCrossCheck {
			applyCrossPageReviewMetadata(metadata, assessments, missingAssessments, crossCheckErr)
			if len(relatedEvidence) > 0 {
				metadata["related_page_evidence"] = relatedEvidence
			}
			if existing == nil && (parsed.Context.DocumentNature == "official_rule" || parsed.Context.DocumentNature == "business_manual") {
				if target, method := s.crossPageCorrectionTarget(ctx, payload.KnowledgeBaseID, slug, card, documentEvidenceText.String(), assessments); target != nil {
					metadata["correction_target_method"] = method
					metadata["correction_original_slug"] = slug
					delete(metadata, "possible_duplicate_slug")
					delete(metadata, "possible_duplicate_title")
					existing = target
					slug = target.Slug
					// Preserve the reviewed card identity and scope. The automatic
					// operation is allowed to replace the conclusion and evidence,
					// not silently rename or broaden the published card.
					card.Title = target.Title
					card.BusinessLine = target.BusinessLine
					card.AnswerStrength = target.AnswerStrength
					card.ProhibitedClaims = append([]string(nil), target.ProhibitedClaims...)
					var reviewedApplicability map[string]any
					if json.Unmarshal(target.Applicability, &reviewedApplicability) == nil {
						card.Applicability = reviewedApplicability
					}
				}
			}
			for _, assessment := range assessments {
				if assessment.RelatedSlug == slug || assessment.Relation == "unrelated" || assessment.Confidence < 0.7 {
					continue
				}
				related := relatedEvidence[assessment.RelatedSlug]
				relatedMap, _ := related.(map[string]any)
				title, _ := relatedMap["title"].(string)
				outLinks = mergeCardStrings(outLinks, types.StringArray{assessment.RelatedSlug})
				relationships = append(relationships, map[string]any{"target_title": title, "target_slug": assessment.RelatedSlug, "relation_type": assessment.Relation, "reason": assessment.Reason, "confidence": assessment.Confidence, "applicability_overlap": assessment.ApplicabilityOverlap})
			}
			metadata["relationships"] = relationships
		}
		appBytes, _ := json.Marshal(card.Applicability)
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
