package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const wikiHistoryLoadAttempts = 5

var wikiLinkPattern = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]*)?\]\]`)

type agentHistoryEnvelope struct {
	Data []agentHistoryMessage `json:"data"`
}

type agentHistoryMessage struct {
	RequestID   string             `json:"request_id"`
	Content     string             `json:"content"`
	Role        string             `json:"role"`
	IsCompleted bool               `json:"is_completed"`
	AgentSteps  []agentHistoryStep `json:"agent_steps"`
}

type agentHistoryStep struct {
	ToolCalls []agentHistoryToolCall `json:"tool_calls"`
}

type agentHistoryToolCall struct {
	Name   string         `json:"name"`
	Args   map[string]any `json:"args"`
	Result *struct {
		Success bool `json:"success"`
	} `json:"result"`
}

type wikiPageSnapshot struct {
	ID              string   `json:"id"`
	KnowledgeBaseID string   `json:"knowledge_base_id"`
	Slug            string   `json:"slug"`
	Title           string   `json:"title"`
	PageType        string   `json:"page_type"`
	Content         string   `json:"content"`
	Summary         string   `json:"summary"`
	SourceRefs      []string `json:"source_refs"`
	ChunkRefs       []string `json:"chunk_refs"`
}

func (c *apiClient) enrichWikiAnswer(ctx context.Context, result knowledgeAnswer) (knowledgeAnswer, error) {
	message, err := c.loadCompletedAgentMessage(ctx, result.SessionID, result.RequestID)
	if err != nil {
		return knowledgeAnswer{}, fmt.Errorf("load completed Wiki Agent message: %w", err)
	}
	if answer := strings.TrimSpace(message.Content); answer != "" {
		// The persisted assistant content is the final answer only. Unlike the
		// SSE stream it never contains thinking/tool-call progress messages.
		result.Answer = answer
	}

	slugs := wikiSlugsUsedByMessage(message)
	if len(slugs) == 0 {
		return result, nil
	}

	seenNodeIDs := make(map[string]bool, len(result.RecalledNodes)+len(slugs))
	for _, node := range result.RecalledNodes {
		seenNodeIDs[node.NodeID] = true
	}
	for _, slug := range slugs {
		page, err := c.fetchWikiPage(ctx, result.KnowledgeBaseID, slug)
		if err != nil {
			return knowledgeAnswer{}, fmt.Errorf("resolve recalled Wiki page %q: %w", slug, err)
		}
		if page.ID == "" {
			return knowledgeAnswer{}, fmt.Errorf("resolve recalled Wiki page %q: response has no page id", slug)
		}
		if seenNodeIDs[page.ID] {
			continue
		}
		seenNodeIDs[page.ID] = true
		knowledgeIDs := sourceKnowledgeIDs(page.SourceRefs)
		knowledgeID := ""
		if len(knowledgeIDs) > 0 {
			knowledgeID = knowledgeIDs[0]
		}
		kbID := page.KnowledgeBaseID
		if kbID == "" {
			kbID = result.KnowledgeBaseID
		}
		content := page.Content
		if strings.TrimSpace(content) == "" {
			content = page.Summary
		}
		result.RecalledNodes = append(result.RecalledNodes, recalledNode{
			NodeID:          page.ID,
			SourceType:      "wiki_page",
			WikiSlug:        page.Slug,
			KnowledgeID:     knowledgeID,
			KnowledgeIDs:    knowledgeIDs,
			KnowledgeBaseID: kbID,
			SubNodeIDs:      normalizeIDs(page.ChunkRefs),
			KnowledgeTitle:  page.Title,
			Rank:            len(result.RecalledNodes) + 1,
			MatchType:       page.PageType,
			ContentExcerpt:  contentExcerpt(content, 500),
			ContentHash:     contentHash(content),
		})
	}
	if len(result.RecalledNodes) == 0 {
		return knowledgeAnswer{}, errors.New("Wiki Agent used pages but no traceable page snapshot was resolved")
	}
	return result, nil
}

func (c *apiClient) loadCompletedAgentMessage(ctx context.Context, sessionID, requestID string) (*agentHistoryMessage, error) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(requestID) == "" {
		return nil, errors.New("session_id and request_id are required")
	}
	path := "/messages/" + url.PathEscape(sessionID) + "/load?limit=20"
	var lastErr error
	for attempt := 0; attempt < wikiHistoryLoadAttempts; attempt++ {
		data, _, err := c.request(ctx, http.MethodGet, path, nil)
		if err != nil {
			lastErr = err
		} else {
			var envelope agentHistoryEnvelope
			if err := json.Unmarshal(data, &envelope); err != nil {
				lastErr = err
			} else {
				for i := range envelope.Data {
					message := &envelope.Data[i]
					if message.Role == "assistant" && message.RequestID == requestID && message.IsCompleted {
						return message, nil
					}
				}
				lastErr = errors.New("completed assistant message not visible yet")
			}
		}
		if attempt+1 < wikiHistoryLoadAttempts {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
			}
		}
	}
	return nil, lastErr
}

func wikiSlugsUsedByMessage(message *agentHistoryMessage) []string {
	if message == nil {
		return nil
	}
	values := make([]string, 0)
	for _, step := range message.AgentSteps {
		for _, call := range step.ToolCalls {
			if call.Name != "wiki_read_page" || call.Result == nil || !call.Result.Success {
				continue
			}
			values = append(values, stringValues(call.Args["slugs"])...)
			values = append(values, stringValues(call.Args["slug"])...)
		}
	}
	for _, match := range wikiLinkPattern.FindAllStringSubmatch(message.Content, -1) {
		if len(match) > 1 {
			values = append(values, match[1])
		}
	}
	return normalizeIDs(values)
}

func stringValues(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value, ok := item.(string); ok {
				out = append(out, value)
			}
		}
		return out
	case []string:
		return typed
	default:
		return nil
	}
}

func sourceKnowledgeIDs(refs []string) []string {
	values := make([]string, 0, len(refs))
	for _, ref := range refs {
		if index := strings.Index(ref, "|"); index >= 0 {
			ref = ref[:index]
		}
		values = append(values, ref)
	}
	return normalizeIDs(values)
}

func (c *apiClient) fetchWikiPage(ctx context.Context, knowledgeBaseID, slug string) (*wikiPageSnapshot, error) {
	segments := strings.Split(strings.Trim(slug, "/"), "/")
	for i := range segments {
		segments[i] = url.PathEscape(segments[i])
	}
	path := "/knowledgebase/" + url.PathEscape(knowledgeBaseID) + "/wiki/pages/" + strings.Join(segments, "/")
	data, _, err := c.request(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var page wikiPageSnapshot
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, err
	}
	return &page, nil
}
