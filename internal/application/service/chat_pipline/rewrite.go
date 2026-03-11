// Package chatpipline provides chat pipeline processing capabilities
// Including query rewriting, history processing, model invocation and other features
package chatpipline

import (
	"context"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

// PluginRewrite is a plugin for rewriting user queries
// It uses historical dialog context and large language models to optimize the user's original query
type PluginRewrite struct {
	modelService   interfaces.ModelService   // Model service for calling large language models
	messageService interfaces.MessageService // Message service for retrieving historical messages
	config         *config.Config            // System configuration
}

// reg is a regular expression used to match and remove content between <think></think> tags
var reg = regexp.MustCompile(`(?s)<think>.*?</think>`)
var rewriteImageSepPattern = regexp.MustCompile(`(?s)^(.*?)\s*\n?---\n(.*)$`)

const (
	noSearchPrefix = "[NO_SEARCH]"
)

// NewPluginRewrite creates a new query rewriting plugin instance
// Also registers the plugin with the event manager
func NewPluginRewrite(eventManager *EventManager,
	modelService interfaces.ModelService, messageService interfaces.MessageService,
	config *config.Config,
) *PluginRewrite {
	res := &PluginRewrite{
		modelService:   modelService,
		messageService: messageService,
		config:         config,
	}
	eventManager.Register(res)
	return res
}

// ActivationEvents returns the list of event types this plugin responds to
// This plugin only responds to REWRITE_QUERY events
func (p *PluginRewrite) ActivationEvents() []types.EventType {
	return []types.EventType{types.REWRITE_QUERY}
}

// OnEvent processes triggered events.
// Handles three input combinations:
//   - Text only: standard rewrite + intent classification (uses chat model)
//   - Text + images: multimodal rewrite + intent + image description (uses VLM/vision model)
//   - Images only: multimodal analysis + intent + image description (uses VLM/vision model)
func (p *PluginRewrite) OnEvent(ctx context.Context,
	eventType types.EventType, chatManage *types.ChatManage, next func() *PluginError,
) *PluginError {
	chatManage.RewriteQuery = chatManage.Query

	hasImages := len(chatManage.Images) > 0
	needRewrite := chatManage.EnableRewrite
	// When images are present we always run the step for image analysis + intent,
	// even without history or rewrite enabled.
	if !needRewrite && !hasImages {
		pipelineInfo(ctx, "Rewrite", "skip", map[string]interface{}{
			"session_id": chatManage.SessionID,
			"reason":     "rewrite_disabled_no_images",
		})
		return next()
	}

	pipelineInfo(ctx, "Rewrite", "input", map[string]interface{}{
		"session_id":     chatManage.SessionID,
		"tenant_id":      chatManage.TenantID,
		"user_query":     chatManage.Query,
		"has_images":     hasImages,
		"enable_rewrite": chatManage.EnableRewrite,
	})

	// --- Load and prepare conversation history ---
	historyList := p.loadHistory(ctx, chatManage)

	// Skip if there's nothing to do: no history to rewrite AND no images to analyse
	if len(historyList) == 0 && !hasImages {
		pipelineInfo(ctx, "Rewrite", "skip", map[string]interface{}{
			"session_id": chatManage.SessionID,
			"reason":     "empty_history_no_images",
		})
		return next()
	}

	// --- Select the appropriate model ---
	rewriteModel, useImages := p.selectModel(ctx, chatManage, hasImages)
	if rewriteModel == nil {
		pipelineError(ctx, "Rewrite", "get_model", map[string]interface{}{
			"session_id": chatManage.SessionID,
		})
		return next()
	}

	// --- Build prompts ---
	systemContent, userContent := p.buildPrompts(chatManage, historyList)

	// Build user message (with images when using a vision-capable model)
	userMsg := chat.Message{Role: "user", Content: userContent}
	if useImages {
		userMsg.Images = chatManage.Images
	}

	maxTokens := 60
	if useImages {
		maxTokens = 500
	}

	// --- Emit progress event for image analysis ---
	var toolCallID string
	if useImages && chatManage.EventBus != nil {
		toolCallID = uuid.New().String()
		chatManage.EventBus.Emit(ctx, types.Event{
			Type:      types.EventType(event.EventAgentToolCall),
			SessionID: chatManage.SessionID,
			Data: event.AgentToolCallData{
				ToolCallID: toolCallID,
				ToolName:   "image_analysis",
			},
		})
	}

	// --- Call model ---
	thinking := false
	vlmStart := time.Now()
	response, err := rewriteModel.Chat(ctx, []chat.Message{
		{Role: "system", Content: systemContent},
		userMsg,
	}, &chat.ChatOptions{
		Temperature:         0.3,
		MaxCompletionTokens: maxTokens,
		Thinking:            &thinking,
	})
	if err != nil {
		if toolCallID != "" && chatManage.EventBus != nil {
			chatManage.EventBus.Emit(ctx, types.Event{
				Type:      types.EventType(event.EventAgentToolResult),
				SessionID: chatManage.SessionID,
				Data: event.AgentToolResultData{
					ToolCallID: toolCallID,
					ToolName:   "image_analysis",
					Output:     "图片分析失败",
					Success:    false,
					Duration:   time.Since(vlmStart).Milliseconds(),
				},
			})
		}
		pipelineError(ctx, "Rewrite", "model_call", map[string]interface{}{
			"session_id": chatManage.SessionID,
			"error":      err.Error(),
		})
		return next()
	}

	// --- Emit completion event for image analysis ---
	if toolCallID != "" && chatManage.EventBus != nil {
		chatManage.EventBus.Emit(ctx, types.Event{
			Type:      types.EventType(event.EventAgentToolResult),
			SessionID: chatManage.SessionID,
			Data: event.AgentToolResultData{
				ToolCallID: toolCallID,
				ToolName:   "image_analysis",
				Output:     "已分析图片内容",
				Success:    true,
				Duration:   time.Since(vlmStart).Milliseconds(),
			},
		})
	}

	// --- Parse structured output ---
	p.parseRewriteOutput(chatManage, response.Content)
	pipelineInfo(ctx, "Rewrite", "output", map[string]interface{}{
		"session_id":      chatManage.SessionID,
		"rewrite_query":   chatManage.RewriteQuery,
		"skip_kb_search":  chatManage.SkipKBSearch,
		"has_image_desc":  chatManage.ImageOCRText != "",
		"original_output": response.Content,
	})
	return next()
}

// loadHistory fetches and processes conversation history for rewrite context.
func (p *PluginRewrite) loadHistory(ctx context.Context, chatManage *types.ChatManage) []*types.History {
	history, err := p.messageService.GetRecentMessagesBySession(ctx, chatManage.SessionID, 20)
	if err != nil {
		pipelineWarn(ctx, "Rewrite", "history_fetch", map[string]interface{}{
			"session_id": chatManage.SessionID,
			"error":      err.Error(),
		})
	}

	historyMap := make(map[string]*types.History)
	for _, message := range history {
		h, ok := historyMap[message.RequestID]
		if !ok {
			h = &types.History{}
		}
		if message.Role == "user" {
			h.Query = message.Content
			h.CreateAt = message.CreatedAt
		} else {
			h.Answer = reg.ReplaceAllString(message.Content, "")
			h.KnowledgeReferences = message.KnowledgeReferences
		}
		historyMap[message.RequestID] = h
	}

	historyList := make([]*types.History, 0)
	for _, h := range historyMap {
		if h.Answer != "" && h.Query != "" {
			historyList = append(historyList, h)
		}
	}

	sort.Slice(historyList, func(i, j int) bool {
		return historyList[i].CreateAt.After(historyList[j].CreateAt)
	})

	maxRounds := p.config.Conversation.MaxRounds
	if chatManage.MaxRounds > 0 {
		maxRounds = chatManage.MaxRounds
	}
	if len(historyList) > maxRounds {
		historyList = historyList[:maxRounds]
	}

	slices.Reverse(historyList)
	chatManage.History = historyList

	if len(historyList) > 0 {
		pipelineInfo(ctx, "Rewrite", "history_ready", map[string]interface{}{
			"session_id":     chatManage.SessionID,
			"history_rounds": len(historyList),
		})
	}

	return historyList
}

// selectModel picks the model for rewrite. When images are present it prefers
// a vision-capable model (either the chat model itself, or the agent's VLM).
// Returns (model, useImages).
func (p *PluginRewrite) selectModel(ctx context.Context, chatManage *types.ChatManage, hasImages bool) (chat.Chat, bool) {
	if hasImages {
		if chatManage.ChatModelSupportsVision {
			m, err := p.modelService.GetChatModel(ctx, chatManage.ChatModelID)
			if err == nil {
				return m, true
			}
			pipelineWarn(ctx, "Rewrite", "vision_model_fallback", map[string]interface{}{
				"session_id": chatManage.SessionID,
				"error":      err.Error(),
			})
		}
		if chatManage.VLMModelID != "" {
			m, err := p.modelService.GetChatModel(ctx, chatManage.VLMModelID)
			if err == nil {
				return m, true
			}
			pipelineWarn(ctx, "Rewrite", "vlm_model_fallback", map[string]interface{}{
				"session_id":   chatManage.SessionID,
				"vlm_model_id": chatManage.VLMModelID,
				"error":        err.Error(),
			})
		}
		pipelineWarn(ctx, "Rewrite", "no_vision_model", map[string]interface{}{
			"session_id": chatManage.SessionID,
		})
	}

	// Fallback: text-only rewrite with chat model
	m, err := p.modelService.GetChatModel(ctx, chatManage.ChatModelID)
	if err != nil {
		pipelineError(ctx, "Rewrite", "get_model", map[string]interface{}{
			"session_id":    chatManage.SessionID,
			"chat_model_id": chatManage.ChatModelID,
			"error":         err.Error(),
		})
		return nil, false
	}
	return m, false
}

// buildPrompts constructs system and user prompts with placeholder replacement.
func (p *PluginRewrite) buildPrompts(chatManage *types.ChatManage, historyList []*types.History) (string, string) {
	userPrompt := p.config.Conversation.RewritePromptUser
	if chatManage.RewritePromptUser != "" {
		userPrompt = chatManage.RewritePromptUser
	}
	systemPrompt := p.config.Conversation.RewritePromptSystem
	if chatManage.RewritePromptSystem != "" {
		systemPrompt = chatManage.RewritePromptSystem
	}
	// Strengthen context inheritance in multi-turn conversation:
	// for follow-up questions that clearly refer to previous turns (especially
	// uploaded-image understanding), prefer NO_SEARCH over KB retrieval.
	systemPrompt += "\n\n## Additional Context Inheritance Guidance\n" +
		"- If the current question is a follow-up to previous conversation content (especially previously uploaded images) and can be answered by that context, you MUST classify it as NO_SEARCH.\n" +
		"- Examples: “第一张图再详细描述一下”, “第二张门上的字是什么意思”, “这个再展开讲讲”.\n" +
		"- In these follow-up cases, output with [NO_SEARCH] prefix."

	conversationText := formatConversationHistory(historyList)
	currentTime := time.Now().Format("2006-01-02 15:04:05")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	replacePlaceholders := func(s string) string {
		s = strings.ReplaceAll(s, "{{conversation}}", conversationText)
		s = strings.ReplaceAll(s, "{{query}}", chatManage.Query)
		s = strings.ReplaceAll(s, "{{current_time}}", currentTime)
		s = strings.ReplaceAll(s, "{{yesterday}}", yesterday)
		return s
	}

	return replacePlaceholders(systemPrompt), replacePlaceholders(userPrompt)
}

// parseRewriteOutput extracts intent classification, rewritten query, and
// optional image description from the model's structured output.
//
// Expected formats:
//
//	Text only:  "[NO_SEARCH] rewritten question"  or  "rewritten question"
//	With images: "[NO_SEARCH]\nrewritten question\n---\nimage description"
func (p *PluginRewrite) parseRewriteOutput(chatManage *types.ChatManage, raw string) {
	content := strings.TrimSpace(raw)
	if content == "" {
		return
	}

	// 1. Parse intent marker
	if strings.HasPrefix(content, noSearchPrefix) {
		chatManage.SkipKBSearch = true
		content = strings.TrimSpace(strings.TrimPrefix(content, noSearchPrefix))
	}

	// 2. Split rewritten query and image description.
	// Be tolerant to model output variants like:
	// - "query\n---\nimage_desc" (expected)
	// - "query---\nimage_desc"   (missing leading newline before separator)
	if m := rewriteImageSepPattern.FindStringSubmatch(content); len(m) == 3 {
		chatManage.RewriteQuery = strings.TrimSpace(m[1])
		chatManage.ImageOCRText = strings.TrimSpace(m[2])
		return
	}
	if content != "" {
		chatManage.RewriteQuery = content
	}
}

// formatConversationHistory formats conversation history for prompt template
func formatConversationHistory(historyList []*types.History) string {
	if len(historyList) == 0 {
		return ""
	}

	var builder strings.Builder
	for _, h := range historyList {
		builder.WriteString("------BEGIN------\n")
		builder.WriteString("User question: ")
		builder.WriteString(h.Query)
		builder.WriteString("\nAssistant answer: ")
		builder.WriteString(h.Answer)
		builder.WriteString("\n------END------\n")
	}
	return builder.String()
}
