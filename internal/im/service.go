package im

import (
	"context"
"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	// qaTimeout is the maximum time to wait for the QA pipeline to complete.
	qaTimeout = 120 * time.Second
	// dedupTTL is how long processed message IDs are retained.
	dedupTTL = 5 * time.Minute
	// dedupCleanupInterval is how often the dedup map is cleaned.
	dedupCleanupInterval = 1 * time.Minute
	// maxContentLength is the maximum allowed message content length.
	maxContentLength = 4096
	// streamFlushInterval is how often buffered stream content is flushed to the IM platform.
	// This prevents API rate-limiting while keeping perceived latency low.
	streamFlushInterval = 300 * time.Millisecond
)

// channelState holds runtime state for a running IM channel.
type channelState struct {
	Channel *IMChannel
	Adapter Adapter
	Cancel  context.CancelFunc // for stopping websocket goroutines
}

// AdapterFactory creates an Adapter from an IMChannel configuration.
// The second return value is an optional cleanup function (e.g., for stopping websocket connections).
type AdapterFactory func(ctx context.Context, channel *IMChannel, msgHandler func(ctx context.Context, msg *IncomingMessage) error) (Adapter, context.CancelFunc, error)

// Service orchestrates IM message handling:
// 1. Receives a unified IncomingMessage from an Adapter
// 2. Resolves or creates a WeKnora session for the IM channel
// 3. Calls the WeKnora QA pipeline
// 4. Collects the streaming answer and sends it back via the Adapter
type Service struct {
	db             *gorm.DB
	sessionService interfaces.SessionService
	messageService interfaces.MessageService
	tenantService  interfaces.TenantService
	agentService   interfaces.CustomAgentService

	// channels maps channel ID -> running channel state
	channels map[string]*channelState
	mu       sync.RWMutex

	// adapterFactories maps platform name -> factory function
	adapterFactories map[string]AdapterFactory

	// processedMsgs tracks recently processed message IDs to prevent duplicate handling.
	processedMsgs sync.Map

	stopCh chan struct{}
}

// NewService creates a new IM service.
func NewService(
	db *gorm.DB,
	sessionService interfaces.SessionService,
	messageService interfaces.MessageService,
	tenantService interfaces.TenantService,
	agentService interfaces.CustomAgentService,
) *Service {
	s := &Service{
		db:               db,
		sessionService:   sessionService,
		messageService:   messageService,
		tenantService:    tenantService,
		agentService:     agentService,
		channels:         make(map[string]*channelState),
		adapterFactories: make(map[string]AdapterFactory),
		stopCh:           make(chan struct{}),
	}

	// Start periodic dedup cleanup instead of per-message goroutines
	go s.dedupCleanupLoop()

	return s
}

// RegisterAdapterFactory registers a factory for creating adapters for a given platform.
func (s *Service) RegisterAdapterFactory(platform string, factory AdapterFactory) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adapterFactories[platform] = factory
}

// Stop gracefully shuts down the service, stopping all channels and background goroutines.
func (s *Service) Stop() {
	close(s.stopCh)
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, cs := range s.channels {
		if cs.Cancel != nil {
			cs.Cancel()
		}
		delete(s.channels, id)
	}
}

// dedupCleanupLoop periodically cleans up expired entries from the dedup map.
func (s *Service) dedupCleanupLoop() {
	ticker := time.NewTicker(dedupCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cutoff := time.Now().Add(-dedupTTL)
			s.processedMsgs.Range(func(key, value interface{}) bool {
				if t, ok := value.(time.Time); ok && t.Before(cutoff) {
					s.processedMsgs.Delete(key)
				}
				return true
			})
		case <-s.stopCh:
			return
		}
	}
}

// LoadAndStartChannels loads all enabled channels from the database and starts them.
func (s *Service) LoadAndStartChannels() error {
	ctx := context.Background()
	var channels []IMChannel
	if err := s.db.Where("enabled = ? AND deleted_at IS NULL", true).Find(&channels).Error; err != nil {
		return fmt.Errorf("load im channels: %w", err)
	}

	for i := range channels {
		ch := channels[i]
		if err := s.StartChannel(&ch); err != nil {
			logger.Warnf(ctx, "[IM] Failed to start channel %s (%s/%s): %v", ch.ID, ch.Platform, ch.Name, err)
		} else {
			logger.Infof(ctx, "[IM] Started channel: id=%s platform=%s name=%s mode=%s agent=%s",
				ch.ID, ch.Platform, ch.Name, ch.Mode, ch.AgentID)
		}
	}

	logger.Infof(ctx, "[IM] Loaded %d enabled channels", len(channels))
	return nil
}

// StartChannel creates and registers an adapter for the given channel.
func (s *Service) StartChannel(channel *IMChannel) error {
	s.mu.Lock()
	factory, ok := s.adapterFactories[channel.Platform]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("no adapter factory for platform: %s", channel.Platform)
	}
	// Stop existing channel if running
	if existing, ok := s.channels[channel.ID]; ok {
		if existing.Cancel != nil {
			existing.Cancel()
		}
		delete(s.channels, channel.ID)
	}
	s.mu.Unlock()

	// Build the message handler that delegates to HandleMessage with this channel's config
	msgHandler := func(msgCtx context.Context, msg *IncomingMessage) error {
		return s.HandleMessage(msgCtx, msg, channel.ID)
	}

	ctx := context.Background()
	adapter, cancelFn, err := factory(ctx, channel, msgHandler)
	if err != nil {
		return fmt.Errorf("create adapter: %w", err)
	}

	s.mu.Lock()
	s.channels[channel.ID] = &channelState{
		Channel: channel,
		Adapter: adapter,
		Cancel:  cancelFn,
	}
	s.mu.Unlock()

	return nil
}

// StopChannel stops and removes a running channel.
func (s *Service) StopChannel(channelID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cs, ok := s.channels[channelID]; ok {
		if cs.Cancel != nil {
			cs.Cancel()
		}
		delete(s.channels, channelID)
		logger.Infof(context.Background(), "[IM] Stopped channel: id=%s", channelID)
	}
}

// GetChannelAdapter returns the adapter and channel config for a given channel ID.
func (s *Service) GetChannelAdapter(channelID string) (Adapter, *IMChannel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cs, ok := s.channels[channelID]
	if !ok {
		return nil, nil, false
	}
	return cs.Adapter, cs.Channel, true
}

// GetChannelByID loads a channel from the database.
func (s *Service) GetChannelByID(channelID string) (*IMChannel, error) {
	var ch IMChannel
	if err := s.db.Where("id = ? AND deleted_at IS NULL", channelID).First(&ch).Error; err != nil {
		return nil, err
	}
	return &ch, nil
}

// HandleMessage processes an incoming IM message end-to-end using channel config.
func (s *Service) HandleMessage(ctx context.Context, msg *IncomingMessage, channelID string) error {
	// Dedup: skip if this message was already processed (IM platforms may retry)
	if msg.MessageID != "" {
		if _, loaded := s.processedMsgs.LoadOrStore(msg.MessageID, time.Now()); loaded {
			logger.Infof(ctx, "[IM] Skipping duplicate message: %s", msg.MessageID)
			return nil
		}
	}

	// Reject overly long messages to protect the QA pipeline
	contentRunes := []rune(msg.Content)
	if len(contentRunes) > maxContentLength {
		logger.Warnf(ctx, "[IM] Message too long (%d runes), truncating to %d", len(contentRunes), maxContentLength)
		msg.Content = string(contentRunes[:maxContentLength])
	}

	// Get channel config
	adapter, channel, ok := s.GetChannelAdapter(channelID)
	if !ok {
		// Try loading from DB (channel might have been created after service start)
		ch, err := s.GetChannelByID(channelID)
		if err != nil {
			return fmt.Errorf("channel not found: %s", channelID)
		}
		// Start it dynamically
		if err := s.StartChannel(ch); err != nil {
			return fmt.Errorf("start channel %s: %w", channelID, err)
		}
		adapter, channel, ok = s.GetChannelAdapter(channelID)
		if !ok {
			return fmt.Errorf("channel adapter not available after start: %s", channelID)
		}
	}

	tenantID := channel.TenantID
	agentID := channel.AgentID

	logger.Infof(ctx, "[IM] HandleMessage: channel=%s platform=%s user=%s chat=%s content_len=%d",
		channelID, msg.Platform, msg.UserID, msg.ChatID, len(msg.Content))

	// 1. Get tenant
	tenant, err := s.tenantService.GetTenantByID(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("get tenant: %w", err)
	}
	sessionCtx := context.WithValue(ctx, types.TenantIDContextKey, tenantID)
	sessionCtx = context.WithValue(sessionCtx, types.TenantInfoContextKey, tenant)

	// 2. Resolve or create a WeKnora session
	channelSession, err := s.resolveSession(sessionCtx, msg, tenantID, agentID, channelID)
	if err != nil {
		return fmt.Errorf("resolve session: %w", err)
	}

	// 3. Get the WeKnora session
	session, err := s.sessionService.GetSession(sessionCtx, channelSession.SessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}

	// 4. Resolve custom agent (optional)
	var customAgent *types.CustomAgent
	if agentID != "" {
		agent, err := s.agentService.GetAgentByID(sessionCtx, agentID)
		if err != nil {
			logger.Warnf(ctx, "[IM] Failed to get agent %s: %v, using default", agentID, err)
		} else {
			customAgent = agent
		}
	}

	// 5. Resolve knowledge base IDs from agent config
	var kbIDs []string
	if customAgent != nil {
		kbIDs = customAgent.Config.KnowledgeBases
	}

	// 6. If the adapter supports streaming and output_mode is not "full", use streaming
	streamDisabled := channel.OutputMode == "full"
	if !streamDisabled {
		if streamer, ok := adapter.(StreamSender); ok {
			return s.handleMessageStream(sessionCtx, msg, session, customAgent, kbIDs, streamer, adapter)
		}
	}

	// Non-streaming fallback: collect full answer then send
	answer, err := s.runQA(sessionCtx, session, msg.Content, customAgent, kbIDs)
	if err != nil {
		logger.Errorf(ctx, "[IM] QA failed: %v, sending fallback reply", err)
		answer = "抱歉，处理您的问题时出现了异常，请稍后再试。"
	}

	reply := &ReplyMessage{
		Content: answer,
		IsFinal: true,
	}
	if err := adapter.SendReply(ctx, msg, reply); err != nil {
		return fmt.Errorf("send reply: %w", err)
	}

	logger.Infof(ctx, "[IM] Reply sent: channel=%s platform=%s user=%s answer_len=%d",
		channelID, msg.Platform, msg.UserID, len(answer))
	return nil
}

// resolveSession finds or creates a ChannelSession for the given IM message.
// ctx must already carry TenantIDContextKey and TenantInfoContextKey.
func (s *Service) resolveSession(ctx context.Context, msg *IncomingMessage, tenantID uint64, agentID string, imChannelID string) (*ChannelSession, error) {
	var cs ChannelSession
	result := s.db.Where("platform = ? AND user_id = ? AND chat_id = ? AND tenant_id = ? AND deleted_at IS NULL",
		string(msg.Platform), msg.UserID, msg.ChatID, tenantID).
		First(&cs)

	if result.Error == nil {
		return &cs, nil
	}

	if result.Error != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("query channel session: %w", result.Error)
	}

	// Create a new WeKnora session
	title := fmt.Sprintf("IM-%s", msg.Platform)
	if msg.UserName != "" {
		title = fmt.Sprintf("IM-%s-%s", msg.Platform, msg.UserName)
	}

	newSession := &types.Session{
		TenantID:    tenantID,
		Title:       title,
		Description: fmt.Sprintf("Auto-created from %s IM integration", msg.Platform),
	}

	createdSession, err := s.sessionService.CreateSession(ctx, newSession)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	// Create the channel-session mapping; use a unique constraint fallback
	// to handle concurrent creation attempts for the same channel.
	cs = ChannelSession{
		Platform:    string(msg.Platform),
		UserID:      msg.UserID,
		ChatID:      msg.ChatID,
		SessionID:   createdSession.ID,
		TenantID:    tenantID,
		AgentID:     agentID,
		IMChannelID: imChannelID,
	}
	if err := s.db.Create(&cs).Error; err != nil {
		// If the insert failed due to unique constraint (concurrent request),
		// fetch the existing record.
		var existing ChannelSession
		if findErr := s.db.Where("platform = ? AND user_id = ? AND chat_id = ? AND tenant_id = ? AND deleted_at IS NULL",
			string(msg.Platform), msg.UserID, msg.ChatID, tenantID).
			First(&existing).Error; findErr != nil {
			return nil, fmt.Errorf("create channel session: %w (lookup fallback: %v)", err, findErr)
		}
		return &existing, nil
	}

	logger.Infof(ctx, "[IM] Created new session mapping: channel=%s/%s/%s -> session=%s",
		msg.Platform, msg.UserID, msg.ChatID, createdSession.ID)

	return &cs, nil
}

// handleMessageStream runs the QA pipeline and streams answer chunks to the IM platform
// in real-time via the StreamSender interface. Chunks are batched at streamFlushInterval
// to avoid API rate-limiting.
func (s *Service) handleMessageStream(ctx context.Context, msg *IncomingMessage, session *types.Session, customAgent *types.CustomAgent, kbIDs []string, streamer StreamSender, adapter Adapter) error {
	// Start the stream on the IM platform (e.g., create Feishu streaming card)
	streamID, err := streamer.StartStream(ctx, msg)
	if err != nil {
		logger.Warnf(ctx, "[IM] StartStream failed, falling back to non-streaming: %v", err)
		return s.fallbackNonStream(ctx, msg, session, customAgent, kbIDs, adapter)
	}

	// Prepare the QA pipeline
	qaCtx, qaCancel := context.WithTimeout(ctx, qaTimeout)
	defer qaCancel()

	eventBus := event.NewEventBus()

	var (
		bufMu         sync.Mutex
		buf           strings.Builder // buffered content awaiting flush (answer only)
		answerBuilder strings.Builder // full answer for DB persistence (includes <think>)
		qaErr         error
		done          = make(chan struct{})
		closeOnce     sync.Once
		inThinking    bool // tracks whether we're inside a <think> block
	)
	closeDone := func() { closeOnce.Do(func() { close(done) }) }

	// Subscribe to answer chunks
	eventBus.On(event.EventAgentFinalAnswer, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(event.AgentFinalAnswerData)
		if !ok {
			return nil
		}

		bufMu.Lock()
		// Always persist the full content (including thinking)
		answerBuilder.WriteString(data.Content)

		// Filter <think>...</think> blocks from the streamed output.
		// The pipeline emits: <think>..chunks..</think> then answer chunks.
		content := data.Content
		if !inThinking {
			if strings.HasPrefix(content, "<think>") {
				inThinking = true
				content = "" // skip this chunk for streaming
			}
		}
		if inThinking {
			if idx := strings.Index(content, "</think>"); idx >= 0 {
				inThinking = false
				// Keep anything after </think>
				content = strings.TrimSpace(content[idx+len("</think>"):])
			} else {
				content = "" // still inside thinking block
			}
		}

		if content != "" {
			buf.WriteString(content)
		}
		bufMu.Unlock()

		if data.Done {
			closeDone()
		}
		return nil
	})

	eventBus.On(event.EventError, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(event.ErrorData)
		if !ok {
			return nil
		}
		logger.Errorf(ctx, "[IM] QA stream error: %s", data.Error)
		bufMu.Lock()
		qaErr = fmt.Errorf("QA pipeline error: %s", data.Error)
		bufMu.Unlock()
		closeDone()
		return nil
	})

	// Determine whether to use agent mode
	useAgent := customAgent != nil && customAgent.IsAgentMode()
	requestID := uuid.New().String()

	// Create user message
	if _, err := s.messageService.CreateMessage(qaCtx, &types.Message{
		SessionID: session.ID, Role: "user", Content: msg.Content,
		RequestID: requestID, CreatedAt: time.Now(), IsCompleted: true,
	}); err != nil {
		return fmt.Errorf("create user message: %w", err)
	}

	// Create placeholder assistant message
	assistantMsg, err := s.messageService.CreateMessage(qaCtx, &types.Message{
		SessionID: session.ID, Role: "assistant",
		RequestID: requestID, CreatedAt: time.Now(), IsCompleted: false,
	})
	if err != nil {
		return fmt.Errorf("create assistant message: %w", err)
	}

	// Run QA async
	go func() {
		var err error
		if useAgent {
			err = s.sessionService.AgentQA(qaCtx, session, msg.Content, assistantMsg.ID, "", eventBus, customAgent, kbIDs, nil)
		} else {
			err = s.sessionService.KnowledgeQA(qaCtx, session, msg.Content, kbIDs, nil, assistantMsg.ID, "", false, eventBus, customAgent, false)
		}
		if err != nil {
			logger.Errorf(ctx, "[IM] QA stream execution error: %v", err)
			bufMu.Lock()
			qaErr = fmt.Errorf("QA execution error: %w", err)
			bufMu.Unlock()
			closeDone()
		}
	}()

	// Flush loop: periodically send buffered content to the IM platform
	ticker := time.NewTicker(streamFlushInterval)
	defer ticker.Stop()

	flush := func() {
		bufMu.Lock()
		chunk := buf.String()
		buf.Reset()
		bufMu.Unlock()

		if chunk != "" {
			if err := streamer.SendStreamChunk(ctx, msg, streamID, chunk); err != nil {
				logger.Warnf(ctx, "[IM] SendStreamChunk failed: %v", err)
			}
		}
	}

loop:
	for {
		select {
		case <-ticker.C:
			flush()
		case <-done:
			break loop
		case <-qaCtx.Done():
			break loop
		}
	}

	// Final flush of any remaining content
	flush()

	// End the stream
	if err := streamer.EndStream(ctx, msg, streamID); err != nil {
		logger.Warnf(ctx, "[IM] EndStream failed: %v", err)
	}

	// Persist the full answer
	bufMu.Lock()
	answer := answerBuilder.String()
	finalErr := qaErr
	bufMu.Unlock()

	if answer == "" && finalErr != nil {
		answer = "抱歉，处理您的问题时出现了异常，请稍后再试。"
	}
	if answer == "" {
		answer = "抱歉，我暂时无法回答这个问题。"
	}

	assistantMsg.Content = answer
	assistantMsg.IsCompleted = true
	if err := s.messageService.UpdateMessage(ctx, assistantMsg); err != nil {
		logger.Warnf(ctx, "[IM] Failed to update assistant message: %v", err)
	}

	logger.Infof(ctx, "[IM] Stream reply sent: platform=%s user=%s answer_len=%d", msg.Platform, msg.UserID, len(answer))
	return nil
}

// fallbackNonStream is used when streaming initialization fails.
func (s *Service) fallbackNonStream(ctx context.Context, msg *IncomingMessage, session *types.Session, customAgent *types.CustomAgent, kbIDs []string, adapter Adapter) error {
	answer, err := s.runQA(ctx, session, msg.Content, customAgent, kbIDs)
	if err != nil {
		logger.Errorf(ctx, "[IM] QA fallback failed: %v", err)
		answer = "抱歉，处理您的问题时出现了异常，请稍后再试。"
	}

	return adapter.SendReply(ctx, msg, &ReplyMessage{Content: answer, IsFinal: true})
}

// runQA executes the WeKnora QA pipeline and returns the full answer text.
func (s *Service) runQA(ctx context.Context, session *types.Session, query string, customAgent *types.CustomAgent, kbIDs []string) (string, error) {
	// Add timeout to prevent indefinite blocking
	ctx, cancel := context.WithTimeout(ctx, qaTimeout)
	defer cancel()

	eventBus := event.NewEventBus()

	// Thread-safe answer collection
	var answerMu sync.Mutex
	var answerBuilder strings.Builder
	var qaErr error
	done := make(chan struct{})
	var closeOnce sync.Once
	closeDone := func() { closeOnce.Do(func() { close(done) }) }

	eventBus.On(event.EventAgentFinalAnswer, func(ctx context.Context, evt event.Event) error {
		data, ok := evt.Data.(event.AgentFinalAnswerData)
		if !ok {
			return nil
		}
		answerMu.Lock()
		answerBuilder.WriteString(data.Content)
		answerMu.Unlock()
		if data.Done {
			closeDone()
		}
		return nil
	})

	eventBus.On(event.EventError, func(ctx context.Context, evt event.Event) error {
		data, ok := evt.Data.(event.ErrorData)
		if !ok {
			return nil
		}
		logger.Errorf(ctx, "[IM] QA error: %s", data.Error)
		answerMu.Lock()
		qaErr = fmt.Errorf("QA pipeline error: %s", data.Error)
		answerMu.Unlock()
		closeDone()
		return nil
	})

	// Determine whether to use agent mode
	useAgent := customAgent != nil && customAgent.IsAgentMode()

	// Generate a shared RequestID to pair user and assistant messages for history
	requestID := uuid.New().String()

	// Create user message so it appears in conversation history
	userMsg, err := s.messageService.CreateMessage(ctx, &types.Message{
		SessionID:   session.ID,
		Role:        "user",
		Content:     query,
		RequestID:   requestID,
		CreatedAt:   time.Now(),
		IsCompleted: true,
	})
	if err != nil {
		return "", fmt.Errorf("create user message: %w", err)
	}
	_ = userMsg

	// Create a placeholder assistant message
	assistantMsg, err := s.messageService.CreateMessage(ctx, &types.Message{
		SessionID:   session.ID,
		Role:        "assistant",
		RequestID:   requestID,
		CreatedAt:   time.Now(),
		IsCompleted: false,
	})
	if err != nil {
		return "", fmt.Errorf("create assistant message: %w", err)
	}

	// Run QA async
	go func() {
		var err error
		if useAgent {
			err = s.sessionService.AgentQA(ctx, session, query, assistantMsg.ID, "", eventBus, customAgent, kbIDs, nil)
		} else {
			err = s.sessionService.KnowledgeQA(ctx, session, query, kbIDs, nil, assistantMsg.ID, "", false, eventBus, customAgent, false)
		}
		if err != nil {
			logger.Errorf(ctx, "[IM] QA execution error: %v", err)
			answerMu.Lock()
			qaErr = fmt.Errorf("QA execution error: %w", err)
			answerMu.Unlock()
			closeDone()
		}
	}()

	// Wait for completion or timeout
	select {
	case <-done:
	case <-ctx.Done():
		// Mark assistant message as completed to avoid dangling incomplete records
		assistantMsg.Content = "抱歉，回答超时，请稍后再试。"
		assistantMsg.IsCompleted = true
		// Use a fresh context since the original is cancelled
		if updateErr := s.messageService.UpdateMessage(context.WithoutCancel(ctx), assistantMsg); updateErr != nil {
			logger.Warnf(ctx, "[IM] Failed to update timed-out assistant message: %v", updateErr)
		}
		return "", fmt.Errorf("QA timed out after %v", qaTimeout)
	}

	answerMu.Lock()
	answer := answerBuilder.String()
	qaError := qaErr
	answerMu.Unlock()

	if answer == "" && qaError != nil {
		return "", qaError
	}
	if answer == "" {
		answer = "抱歉，我暂时无法回答这个问题。"
	}

	// Update assistant message with the final answer content
	assistantMsg.Content = answer
	assistantMsg.IsCompleted = true
	if err := s.messageService.UpdateMessage(ctx, assistantMsg); err != nil {
		logger.Warnf(ctx, "[IM] Failed to update assistant message: %v", err)
	}

	return answer, nil
}

// ── CRUD operations for IM channels ──

// ListChannelsByAgent returns all channels for a given agent.
func (s *Service) ListChannelsByAgent(agentID string) ([]IMChannel, error) {
	var channels []IMChannel
	if err := s.db.Where("agent_id = ? AND deleted_at IS NULL", agentID).
		Order("created_at DESC").Find(&channels).Error; err != nil {
		return nil, err
	}
	return channels, nil
}

// CreateChannel creates a new IM channel and optionally starts it.
func (s *Service) CreateChannel(channel *IMChannel) error {
	if err := s.db.Create(channel).Error; err != nil {
		return err
	}
	if channel.Enabled {
		if err := s.StartChannel(channel); err != nil {
			logger.Warnf(context.Background(), "[IM] Created channel %s but failed to start: %v", channel.ID, err)
		}
	}
	return nil
}

// UpdateChannel updates a channel and restarts it if needed.
func (s *Service) UpdateChannel(channel *IMChannel) error {
	if err := s.db.Save(channel).Error; err != nil {
		return err
	}
	// Restart channel: stop old, start new if enabled
	s.StopChannel(channel.ID)
	if channel.Enabled {
		if err := s.StartChannel(channel); err != nil {
			logger.Warnf(context.Background(), "[IM] Updated channel %s but failed to restart: %v", channel.ID, err)
		}
	}
	return nil
}

// DeleteChannel soft-deletes a channel and stops it.
func (s *Service) DeleteChannel(channelID string) error {
	s.StopChannel(channelID)
	return s.db.Where("id = ?", channelID).Delete(&IMChannel{}).Error
}

// ToggleChannel enables or disables a channel.
func (s *Service) ToggleChannel(channelID string) (*IMChannel, error) {
	var ch IMChannel
	if err := s.db.Where("id = ? AND deleted_at IS NULL", channelID).First(&ch).Error; err != nil {
		return nil, err
	}
	ch.Enabled = !ch.Enabled
	if err := s.db.Save(&ch).Error; err != nil {
		return nil, err
	}
	if ch.Enabled {
		if err := s.StartChannel(&ch); err != nil {
			logger.Warnf(context.Background(), "[IM] Failed to start channel %s after enable: %v", ch.ID, err)
		}
	} else {
		s.StopChannel(channelID)
	}
	return &ch, nil
}
