package types

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// BuiltinAgentID constants for built-in agents
const (
	// BuiltinQuickAnswerID is the ID for the built-in quick answer (RAG) agent
	BuiltinQuickAnswerID = "builtin-quick-answer"
	// BuiltinSmartReasoningID is the ID for the built-in smart reasoning (ReAct) agent
	BuiltinSmartReasoningID = "builtin-smart-reasoning"
	// BuiltinDeepResearcherID is the ID for the built-in deep researcher agent
	BuiltinDeepResearcherID = "builtin-deep-researcher"
	// BuiltinDataAnalystID is the ID for the built-in data analyst agent
	BuiltinDataAnalystID = "builtin-data-analyst"
	// BuiltinKnowledgeGraphExpertID is the ID for the built-in knowledge graph expert agent
	BuiltinKnowledgeGraphExpertID = "builtin-knowledge-graph-expert"
	// BuiltinDocumentAssistantID is the ID for the built-in document assistant agent
	BuiltinDocumentAssistantID = "builtin-document-assistant"
)

// AgentMode constants for agent running mode
const (
	// AgentModeQuickAnswer is the RAG mode for quick Q&A
	AgentModeQuickAnswer = "quick-answer"
	// AgentModeSmartReasoning is the ReAct mode for multi-step reasoning
	AgentModeSmartReasoning = "smart-reasoning"
)

// CustomAgent represents a configurable AI agent (similar to GPTs)
type CustomAgent struct {
	// Unique identifier of the agent (composite primary key with TenantID)
	// For built-in agents, this is 'builtin-quick-answer' or 'builtin-smart-reasoning'
	// For custom agents, this is a UUID
	ID string `yaml:"id" json:"id" gorm:"type:varchar(36);primaryKey"`
	// Name of the agent
	Name string `yaml:"name" json:"name" gorm:"type:varchar(255);not null"`
	// Description of the agent
	Description string `yaml:"description" json:"description" gorm:"type:text"`
	// Avatar/Icon of the agent (emoji or icon name)
	Avatar string `yaml:"avatar" json:"avatar" gorm:"type:varchar(64)"`
	// Whether this is a built-in agent (normal mode / agent mode)
	IsBuiltin bool `yaml:"is_builtin" json:"is_builtin" gorm:"default:false"`
	// Tenant ID (composite primary key with ID)
	TenantID uint64 `yaml:"tenant_id" json:"tenant_id" gorm:"primaryKey"`
	// Created by user ID
	CreatedBy string `yaml:"created_by" json:"created_by" gorm:"type:varchar(36)"`

	// Agent configuration
	Config CustomAgentConfig `yaml:"config" json:"config" gorm:"type:json"`

	// Timestamps
	CreatedAt time.Time      `yaml:"created_at" json:"created_at"`
	UpdatedAt time.Time      `yaml:"updated_at" json:"updated_at"`
	DeletedAt gorm.DeletedAt `yaml:"deleted_at" json:"deleted_at" gorm:"index"`
}

// CustomAgentConfig represents the configuration of a custom agent
type CustomAgentConfig struct {
	// ===== Basic Settings =====
	// Agent mode: "quick-answer" for RAG mode, "smart-reasoning" for ReAct agent mode
	AgentMode string `yaml:"agent_mode" json:"agent_mode"`
	// System prompt for the agent (unified prompt, uses {{web_search_status}} placeholder for dynamic behavior)
	SystemPrompt string `yaml:"system_prompt" json:"system_prompt"`
	// Context template for normal mode (how to format retrieved chunks)
	ContextTemplate string `yaml:"context_template" json:"context_template"`

	// ===== Model Settings =====
	// Model ID to use for conversations
	ModelID string `yaml:"model_id" json:"model_id"`
	// ReRank model ID for retrieval
	RerankModelID string `yaml:"rerank_model_id" json:"rerank_model_id"`
	// Temperature for LLM (0-1)
	Temperature float64 `yaml:"temperature" json:"temperature"`
	// Maximum completion tokens (only for normal mode)
	MaxCompletionTokens int `yaml:"max_completion_tokens" json:"max_completion_tokens"`

	// ===== Agent Mode Settings =====
	// Maximum iterations for ReAct loop (only for agent type)
	MaxIterations int `yaml:"max_iterations" json:"max_iterations"`
	// Allowed tools (only for agent type)
	AllowedTools []string `yaml:"allowed_tools" json:"allowed_tools"`
	// Whether reflection is enabled (only for agent type)
	ReflectionEnabled bool `yaml:"reflection_enabled" json:"reflection_enabled"`
	// MCP service selection mode: "all" = all enabled MCP services, "selected" = specific services, "none" = no MCP
	MCPSelectionMode string `yaml:"mcp_selection_mode" json:"mcp_selection_mode"`
	// Selected MCP service IDs (only used when MCPSelectionMode is "selected")
	MCPServices []string `yaml:"mcp_services" json:"mcp_services"`

	// ===== Knowledge Base Settings =====
	// Knowledge base selection mode: "all" = all KBs, "selected" = specific KBs, "none" = no KB
	KBSelectionMode string `yaml:"kb_selection_mode" json:"kb_selection_mode"`
	// Associated knowledge base IDs (only used when KBSelectionMode is "selected")
	KnowledgeBases []string `yaml:"knowledge_bases" json:"knowledge_bases"`

	// ===== FAQ Strategy Settings =====
	// Whether FAQ priority strategy is enabled (FAQ answers prioritized over document chunks)
	FAQPriorityEnabled bool `yaml:"faq_priority_enabled" json:"faq_priority_enabled"`
	// FAQ direct answer threshold - if similarity > this value, use FAQ answer directly
	FAQDirectAnswerThreshold float64 `yaml:"faq_direct_answer_threshold" json:"faq_direct_answer_threshold"`
	// FAQ score boost multiplier - FAQ results score multiplied by this factor
	FAQScoreBoost float64 `yaml:"faq_score_boost" json:"faq_score_boost"`

	// ===== Web Search Settings =====
	// Whether web search is enabled
	WebSearchEnabled bool `yaml:"web_search_enabled" json:"web_search_enabled"`
	// Maximum web search results
	WebSearchMaxResults int `yaml:"web_search_max_results" json:"web_search_max_results"`

	// ===== Multi-turn Conversation Settings =====
	// Whether multi-turn conversation is enabled
	MultiTurnEnabled bool `yaml:"multi_turn_enabled" json:"multi_turn_enabled"`
	// Number of history turns to keep in context
	HistoryTurns int `yaml:"history_turns" json:"history_turns"`

	// ===== Retrieval Strategy Settings (for both modes) =====
	// Embedding/Vector retrieval top K
	EmbeddingTopK int `yaml:"embedding_top_k" json:"embedding_top_k"`
	// Keyword retrieval threshold
	KeywordThreshold float64 `yaml:"keyword_threshold" json:"keyword_threshold"`
	// Vector retrieval threshold
	VectorThreshold float64 `yaml:"vector_threshold" json:"vector_threshold"`
	// Rerank top K
	RerankTopK int `yaml:"rerank_top_k" json:"rerank_top_k"`
	// Rerank threshold
	RerankThreshold float64 `yaml:"rerank_threshold" json:"rerank_threshold"`

	// ===== Advanced Settings (mainly for normal mode) =====
	// Whether to enable query expansion
	EnableQueryExpansion bool `yaml:"enable_query_expansion" json:"enable_query_expansion"`
	// Whether to enable query rewrite for multi-turn conversations
	EnableRewrite bool `yaml:"enable_rewrite" json:"enable_rewrite"`
	// Rewrite prompt system message
	RewritePromptSystem string `yaml:"rewrite_prompt_system" json:"rewrite_prompt_system"`
	// Rewrite prompt user message template
	RewritePromptUser string `yaml:"rewrite_prompt_user" json:"rewrite_prompt_user"`
	// Fallback strategy: "fixed" for fixed response, "model" for model generation
	FallbackStrategy string `yaml:"fallback_strategy" json:"fallback_strategy"`
	// Fixed fallback response (when FallbackStrategy is "fixed")
	FallbackResponse string `yaml:"fallback_response" json:"fallback_response"`
	// Fallback prompt (when FallbackStrategy is "model")
	FallbackPrompt string `yaml:"fallback_prompt" json:"fallback_prompt"`
}

// Value implements driver.Valuer interface for CustomAgentConfig
func (c CustomAgentConfig) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan implements sql.Scanner interface for CustomAgentConfig
func (c *CustomAgentConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(b, c)
}

// TableName returns the table name for CustomAgent
func (CustomAgent) TableName() string {
	return "custom_agents"
}

// EnsureDefaults sets default values for the agent
func (a *CustomAgent) EnsureDefaults() {
	if a == nil {
		return
	}
	if a.Config.Temperature == 0 {
		a.Config.Temperature = 0.7
	}
	if a.Config.MaxIterations == 0 {
		a.Config.MaxIterations = 10
	}
	if a.Config.WebSearchMaxResults == 0 {
		a.Config.WebSearchMaxResults = 5
	}
	if a.Config.HistoryTurns == 0 {
		a.Config.HistoryTurns = 5
	}
	// Retrieval strategy defaults
	if a.Config.EmbeddingTopK == 0 {
		a.Config.EmbeddingTopK = 10
	}
	if a.Config.KeywordThreshold == 0 {
		a.Config.KeywordThreshold = 0.3
	}
	if a.Config.VectorThreshold == 0 {
		a.Config.VectorThreshold = 0.5
	}
	if a.Config.RerankTopK == 0 {
		a.Config.RerankTopK = 5
	}
	if a.Config.RerankThreshold == 0 {
		a.Config.RerankThreshold = 0.5
	}
	// Advanced settings defaults
	if a.Config.FallbackStrategy == "" {
		a.Config.FallbackStrategy = "model"
	}
	if a.Config.MaxCompletionTokens == 0 {
		a.Config.MaxCompletionTokens = 2048
	}
	// Agent mode should always enable multi-turn conversation
	if a.Config.AgentMode == AgentModeSmartReasoning {
		a.Config.MultiTurnEnabled = true
	}
}

// IsAgentMode returns true if this agent uses ReAct agent mode
func (a *CustomAgent) IsAgentMode() bool {
	return a.Config.AgentMode == AgentModeSmartReasoning
}

// GetBuiltinQuickAnswerAgent returns the built-in quick answer (RAG) mode agent
func GetBuiltinQuickAnswerAgent(tenantID uint64) *CustomAgent {
	return &CustomAgent{
		ID:          BuiltinQuickAnswerID,
		Name:        "快速问答",
		Description: "基于知识库的 RAG 问答，快速准确地回答问题",
		IsBuiltin:   true,
		TenantID:    tenantID,
		Config: CustomAgentConfig{
			AgentMode:           AgentModeQuickAnswer,
			SystemPrompt:        "",
			Temperature:         0.7,
			MaxCompletionTokens: 2048,
			WebSearchEnabled:    true,
			WebSearchMaxResults: 5,
			MultiTurnEnabled:    true,
			HistoryTurns:        5,
			KBSelectionMode:     "all",
			// FAQ strategy
			FAQPriorityEnabled:       true,
			FAQDirectAnswerThreshold: 0.9,
			FAQScoreBoost:            1.2,
			// Retrieval strategy
			EmbeddingTopK:    10,
			KeywordThreshold: 0.3,
			VectorThreshold:  0.5,
			RerankTopK:       10,
			RerankThreshold:  0.3,
			// Advanced settings
			EnableQueryExpansion: true,
			EnableRewrite:        true,
			FallbackStrategy:     "model",
		},
	}
}

// GetBuiltinSmartReasoningAgent returns the built-in smart reasoning (ReAct) mode agent
func GetBuiltinSmartReasoningAgent(tenantID uint64) *CustomAgent {
	return &CustomAgent{
		ID:          BuiltinSmartReasoningID,
		Name:        "智能推理",
		Description: "ReAct 推理框架，支持多步思考和工具调用",
		IsBuiltin:   true,
		TenantID:    tenantID,
		Config: CustomAgentConfig{
			AgentMode:           AgentModeSmartReasoning,
			SystemPrompt:        "",
			Temperature:         0.7,
			MaxCompletionTokens: 2048,
			MaxIterations:       50,
			KBSelectionMode:     "all",
			AllowedTools:        []string{"thinking", "todo_write", "knowledge_search", "grep_chunks", "list_knowledge_chunks", "query_knowledge_graph", "get_document_info"},
			WebSearchEnabled:    true,
			WebSearchMaxResults: 5,
			ReflectionEnabled:   false,
			MultiTurnEnabled:    true,
			HistoryTurns:        5,
			// FAQ strategy
			FAQPriorityEnabled:       true,
			FAQDirectAnswerThreshold: 0.9,
			FAQScoreBoost:            1.2,
			// Retrieval strategy
			EmbeddingTopK:    10,
			KeywordThreshold: 0.3,
			VectorThreshold:  0.5,
			RerankTopK:       10,
			RerankThreshold:  0.3,
		},
	}
}

// GetBuiltinDeepResearcherAgent returns the built-in deep researcher agent
// This agent is optimized for in-depth research and comprehensive analysis
func GetBuiltinDeepResearcherAgent(tenantID uint64) *CustomAgent {
	return &CustomAgent{
		ID:          BuiltinDeepResearcherID,
		Name:        "深度研究员",
		Description: "专注于深度研究和综合分析，能够制定研究计划、多维度检索信息、深入思考并给出全面的分析报告",
		Avatar:      "🔬",
		IsBuiltin:   true,
		TenantID:    tenantID,
		Config: CustomAgentConfig{
			AgentMode: AgentModeSmartReasoning,
			SystemPrompt: `你是一位专业的深度研究员，擅长进行系统性的研究和综合分析。

## 核心能力
- 制定结构化的研究计划
- 多维度信息检索和交叉验证
- 深入思考和逻辑推理
- 综合分析和报告撰写

## 工作流程
1. **理解问题**：深入分析用户的研究需求，明确研究目标和范围
2. **制定计划**：使用 todo_write 工具制定详细的研究计划
3. **信息收集**：
   - 使用 knowledge_search 进行语义搜索获取相关文档
   - 使用 grep_chunks 进行关键词精确搜索
   - 使用 query_knowledge_graph 探索实体关系
   - 必要时使用网络搜索获取最新信息
4. **深度分析**：使用 thinking 工具进行深入思考和推理
5. **综合报告**：整合所有信息，给出结构化的研究报告

## 输出要求
- 研究报告应包含：背景、方法、发现、分析、结论
- 引用具体的信息来源
- 指出信息的可靠性和局限性
- 提供进一步研究的建议`,
			Temperature:         0.5,
			MaxCompletionTokens: 4096,
			MaxIterations:       30,
			KBSelectionMode:     "all",
			AllowedTools:        []string{"thinking", "todo_write", "knowledge_search", "grep_chunks", "list_knowledge_chunks", "query_knowledge_graph", "get_document_info"},
			WebSearchEnabled:    true,
			WebSearchMaxResults: 10,
			ReflectionEnabled:   true,
			MultiTurnEnabled:    true,
			HistoryTurns:        10,
			// FAQ strategy
			FAQPriorityEnabled:       true,
			FAQDirectAnswerThreshold: 0.9,
			FAQScoreBoost:            1.2,
			// Retrieval strategy - more comprehensive
			EmbeddingTopK:    20,
			KeywordThreshold: 0.2,
			VectorThreshold:  0.4,
			RerankTopK:       15,
			RerankThreshold:  0.25,
		},
	}
}

// GetBuiltinDataAnalystAgent returns the built-in data analyst agent
// This agent is optimized for database queries and data analysis
func GetBuiltinDataAnalystAgent(tenantID uint64) *CustomAgent {
	return &CustomAgent{
		ID:          BuiltinDataAnalystID,
		Name:        "数据分析师",
		Description: "专注于数据库查询和数据分析，能够理解业务需求、构建SQL查询、分析数据并提供洞察",
		Avatar:      "📊",
		IsBuiltin:   true,
		TenantID:    tenantID,
		Config: CustomAgentConfig{
			AgentMode: AgentModeSmartReasoning,
			SystemPrompt: `你是一位专业的数据分析师，擅长数据库查询和数据分析。

## 核心能力
- 理解业务需求并转化为数据查询
- 构建高效的 SQL 查询语句
- 数据分析和可视化建议
- 提供数据驱动的业务洞察

## 工作流程
1. **需求理解**：
   - 明确用户的数据分析目标
   - 确定需要查询的数据范围和维度
2. **数据探索**：
   - 使用 database_query 查询相关数据
   - 使用 get_document_info 了解数据结构和元数据
3. **数据分析**：
   - 使用 thinking 工具进行数据分析和推理
   - 识别数据模式和趋势
4. **结果呈现**：
   - 清晰展示查询结果
   - 提供数据解读和业务建议

## 输出要求
- 解释 SQL 查询的逻辑
- 以表格或结构化方式展示数据
- 提供数据的业务含义解读
- 指出数据的局限性和注意事项
- 建议后续分析方向`,
			Temperature:         0.3,
			MaxCompletionTokens: 2048,
			MaxIterations:       20,
			KBSelectionMode:     "all",
			AllowedTools:        []string{"thinking", "todo_write", "database_query", "knowledge_search", "get_document_info"},
			WebSearchEnabled:    false,
			WebSearchMaxResults: 5,
			ReflectionEnabled:   false,
			MultiTurnEnabled:    true,
			HistoryTurns:        5,
			// FAQ strategy
			FAQPriorityEnabled:       true,
			FAQDirectAnswerThreshold: 0.9,
			FAQScoreBoost:            1.2,
			// Retrieval strategy
			EmbeddingTopK:    10,
			KeywordThreshold: 0.3,
			VectorThreshold:  0.5,
			RerankTopK:       5,
			RerankThreshold:  0.3,
		},
	}
}

// GetBuiltinKnowledgeGraphExpertAgent returns the built-in knowledge graph expert agent
// This agent is optimized for knowledge graph exploration and relationship analysis
func GetBuiltinKnowledgeGraphExpertAgent(tenantID uint64) *CustomAgent {
	return &CustomAgent{
		ID:          BuiltinKnowledgeGraphExpertID,
		Name:        "知识图谱专家",
		Description: "专注于知识图谱查询和关系分析，能够探索实体关系、发现隐藏联系并构建知识网络",
		Avatar:      "🕸️",
		IsBuiltin:   true,
		TenantID:    tenantID,
		Config: CustomAgentConfig{
			AgentMode: AgentModeSmartReasoning,
			SystemPrompt: `你是一位知识图谱专家，擅长探索实体关系和构建知识网络。

## 核心能力
- 实体识别和关系发现
- 知识图谱查询和遍历
- 关系链分析和推理
- 知识网络可视化建议

## 工作流程
1. **实体识别**：
   - 从用户问题中识别关键实体
   - 确定需要探索的关系类型
2. **图谱查询**：
   - 使用 query_knowledge_graph 查询实体关系
   - 探索多跳关系和间接联系
3. **关系分析**：
   - 使用 thinking 工具分析关系模式
   - 发现隐藏的关联和规律
4. **知识整合**：
   - 结合 knowledge_search 获取更多上下文
   - 构建完整的知识图景

## 输出要求
- 清晰展示实体和关系
- 使用图形化描述（如 A -> 关系 -> B）
- 解释关系的含义和重要性
- 指出可能的推理和假设
- 建议进一步探索的方向`,
			Temperature:         0.5,
			MaxCompletionTokens: 2048,
			MaxIterations:       25,
			KBSelectionMode:     "all",
			AllowedTools:        []string{"thinking", "todo_write", "query_knowledge_graph", "knowledge_search", "grep_chunks", "get_document_info"},
			WebSearchEnabled:    false,
			WebSearchMaxResults: 5,
			ReflectionEnabled:   true,
			MultiTurnEnabled:    true,
			HistoryTurns:        5,
			// FAQ strategy
			FAQPriorityEnabled:       true,
			FAQDirectAnswerThreshold: 0.9,
			FAQScoreBoost:            1.2,
			// Retrieval strategy
			EmbeddingTopK:    15,
			KeywordThreshold: 0.3,
			VectorThreshold:  0.4,
			RerankTopK:       10,
			RerankThreshold:  0.3,
		},
	}
}

// GetBuiltinDocumentAssistantAgent returns the built-in document assistant agent
// This agent is optimized for document retrieval, organization and summarization
func GetBuiltinDocumentAssistantAgent(tenantID uint64) *CustomAgent {
	return &CustomAgent{
		ID:          BuiltinDocumentAssistantID,
		Name:        "文档助手",
		Description: "专注于文档检索和内容整理，能够快速定位文档、提取关键信息并生成摘要",
		Avatar:      "📚",
		IsBuiltin:   true,
		TenantID:    tenantID,
		Config: CustomAgentConfig{
			AgentMode: AgentModeSmartReasoning,
			SystemPrompt: `你是一位专业的文档助手，擅长文档检索、信息提取和内容整理。

## 核心能力
- 快速定位相关文档和内容
- 提取文档关键信息
- 生成结构化摘要
- 文档对比和整合

## 工作流程
1. **需求分析**：
   - 理解用户的文档检索需求
   - 确定搜索关键词和范围
2. **文档检索**：
   - 使用 knowledge_search 语义搜索相关内容
   - 使用 grep_chunks 精确匹配关键词
   - 使用 get_document_info 获取文档元信息
3. **内容处理**：
   - 使用 list_knowledge_chunks 查看完整内容
   - 使用 thinking 工具整理和分析信息
4. **结果输出**：
   - 提供结构化的信息摘要
   - 标注信息来源和位置

## 输出要求
- 清晰标注信息来源（文档名、位置）
- 使用结构化格式展示信息
- 区分直接引用和总结内容
- 指出信息的完整性和可能遗漏
- 提供相关文档的导航建议`,
			Temperature:         0.3,
			MaxCompletionTokens: 2048,
			MaxIterations:       20,
			KBSelectionMode:     "all",
			AllowedTools:        []string{"thinking", "knowledge_search", "grep_chunks", "list_knowledge_chunks", "get_document_info"},
			WebSearchEnabled:    false,
			WebSearchMaxResults: 5,
			ReflectionEnabled:   false,
			MultiTurnEnabled:    true,
			HistoryTurns:        5,
			// FAQ strategy
			FAQPriorityEnabled:       true,
			FAQDirectAnswerThreshold: 0.9,
			FAQScoreBoost:            1.2,
			// Retrieval strategy - focused on precision
			EmbeddingTopK:    15,
			KeywordThreshold: 0.25,
			VectorThreshold:  0.45,
			RerankTopK:       10,
			RerankThreshold:  0.3,
		},
	}
}

// Deprecated: Use GetBuiltinQuickAnswerAgent instead
func GetBuiltinNormalAgent(tenantID uint64) *CustomAgent {
	return GetBuiltinQuickAnswerAgent(tenantID)
}

// Deprecated: Use GetBuiltinSmartReasoningAgent instead
func GetBuiltinAgentAgent(tenantID uint64) *CustomAgent {
	return GetBuiltinSmartReasoningAgent(tenantID)
}

// BuiltinAgentRegistry provides a registry of all built-in agents for easy extension
var BuiltinAgentRegistry = map[string]func(uint64) *CustomAgent{
	BuiltinQuickAnswerID:          GetBuiltinQuickAnswerAgent,
	BuiltinSmartReasoningID:       GetBuiltinSmartReasoningAgent,
	BuiltinDeepResearcherID:       GetBuiltinDeepResearcherAgent,
	BuiltinDataAnalystID:          GetBuiltinDataAnalystAgent,
	BuiltinKnowledgeGraphExpertID: GetBuiltinKnowledgeGraphExpertAgent,
	BuiltinDocumentAssistantID:    GetBuiltinDocumentAssistantAgent,
}

// builtinAgentIDsOrdered defines the fixed display order of built-in agents
var builtinAgentIDsOrdered = []string{
	BuiltinQuickAnswerID,
	BuiltinSmartReasoningID,
	BuiltinDeepResearcherID,
	BuiltinDataAnalystID,
	BuiltinKnowledgeGraphExpertID,
	BuiltinDocumentAssistantID,
}

// GetBuiltinAgentIDs returns all built-in agent IDs in fixed order
func GetBuiltinAgentIDs() []string {
	return builtinAgentIDsOrdered
}

// IsBuiltinAgentID checks if the given ID is a built-in agent ID
func IsBuiltinAgentID(id string) bool {
	_, exists := BuiltinAgentRegistry[id]
	return exists
}

// GetBuiltinAgent returns a built-in agent by ID, or nil if not found
func GetBuiltinAgent(id string, tenantID uint64) *CustomAgent {
	if factory, exists := BuiltinAgentRegistry[id]; exists {
		return factory(tenantID)
	}
	return nil
}
