(function () {
  'use strict';

  function $(id) { return document.getElementById(id); }

  var messagesEl = $('messages');
  var inputEl = $('sp-input');
  var sendBtn = $('sp-send');
  var welcomeEl = $('welcome');
  var typingEl = $('typing');
  var sessionTitleEl = $('sp-session-title');

  // === 状态管理 ===
  var currentSessionId = null;
  var selectedKbId = 'all';
  var selectedAgentId = '';       // 当前选中的智能体 ID
  var selectedAgentEnabled = false; // 是否启用 agent 模式（smart-reasoning 用 agent-chat）
  var knowledgeBases = [];
  var agents = [];                // 从 API 加载的智能体列表
  var isSending = false;
  var cachedBaseUrl = '';          // 缓存服务地址，用于文件 URL 转换

  // 图片上传状态
  var pendingImages = [];          // { file: File, preview: string (blob URL) }
  var selectedAgentImageUpload = false;
  var MAX_IMAGES = 5;
  var ALLOWED_IMAGE_TYPES = ['image/jpeg', 'image/png', 'image/gif', 'image/webp'];
  var MAX_IMAGE_SIZE = 10 * 1024 * 1024; // 10MB

  // 流式状态
  var streamingText = '';
  var streamingBotDiv = null;
  var currentRequestId = '';
  var streamingReferences = [];
  var renderPending = false;

  // Agent 事件流状态（参考 AgentStreamDisplay.vue）
  var agentEventStream = [];
  var agentContainer = null;
  var agentExpandedIds = {};
  var agentActiveThinkingIds = {};
  var agentHasAnswer = false;
  var agentTreeExpanded = false;
  var agentRefsExpanded = false;
  var agentAnswerText = '';
  var agentAnswerDone = false;

  // === Session Title 展示 ===
  function updateSessionTitle(title) {
    if (!sessionTitleEl) return;
    if (title) {
      sessionTitleEl.textContent = title;
      sessionTitleEl.classList.add('show');
    } else {
      sessionTitleEl.textContent = '';
      sessionTitleEl.classList.remove('show');
    }
  }

  // === 加载智能体列表（优先加载，然后根据默认智能体过滤 KB）===
  function loadAgents() {
    chrome.runtime.sendMessage({ type: 'LIST_AGENTS' }, function (resp) {
      void chrome.runtime.lastError;
      if (resp && resp.success && resp.data) {
        agents = Array.isArray(resp.data) ? resp.data : (resp.data.data || []);
        // 恢复持久化的模式选择（仅当没有被 applyPendingPayload 预设时）
        chrome.storage.local.get('ka_selected_agent', function (stored) {
          if (!selectedAgentId && stored && stored.ka_selected_agent && stored.ka_selected_agent.agentId) {
            var saved = stored.ka_selected_agent;
            for (var i = 0; i < agents.length; i++) {
              if (agents[i].id === saved.agentId) {
                selectedAgentId = saved.agentId;
                selectedAgentEnabled = !!saved.agentEnabled;
                break;
              }
            }
          }
          // 兜底：默认选中第一个智能体
          if (agents.length > 0 && !selectedAgentId) {
            var defaultAgent = agents[0];
            selectedAgentId = defaultAgent.id;
            var isQuickAnswer = defaultAgent.id === 'builtin-quick-answer' || (defaultAgent.config && defaultAgent.config.agent_mode === 'quick-answer');
            selectedAgentEnabled = !isQuickAnswer;
          }
          renderAgentDropdown();
          // 更新图片上传按钮状态
          var selAgent = agents.find(function (a) { return a.id === selectedAgentId; });
          selectedAgentImageUpload = !!(selAgent && selAgent.config && selAgent.config.image_upload_enabled);
          updateImageUploadUI();
          // 加载全部知识库（然后在前端根据智能体配置过滤）
          loadAllKnowledgeBases();
        });
      } else {
        // 加载全部知识库（然后在前端根据智能体配置过滤）
        loadAllKnowledgeBases();
      }
    });
  }

  // === 全部知识库缓存 ===
  var allKnowledgeBases = [];

  // === 加载全部知识库 ===
  function loadAllKnowledgeBases() {
    chrome.runtime.sendMessage({ type: 'LIST_KNOWLEDGE_BASES' }, function (resp) {
      void chrome.runtime.lastError;
      if (resp && resp.success && resp.data) {
        allKnowledgeBases = Array.isArray(resp.data) ? resp.data : (resp.data.items || []);
      } else {
        allKnowledgeBases = [];
      }
      filterAndRenderKbs();
    });
  }

  // === 根据当前选中的智能体配置过滤知识库 ===
  function filterAndRenderKbs() {
    var agent = findAgentById(selectedAgentId);
    if (agent && agent.config) {
      var mode = agent.config.kb_selection_mode || 'all';
      if (mode === 'none') {
        knowledgeBases = [];
      } else if (mode === 'selected' && agent.config.knowledge_bases) {
        var allowedIds = agent.config.knowledge_bases;
        knowledgeBases = allKnowledgeBases.filter(function (kb) {
          return allowedIds.indexOf(kb.id) !== -1;
        });
      } else {
        // 'all' 或未设置：显示全部
        knowledgeBases = allKnowledgeBases.slice();
      }
    } else {
      knowledgeBases = allKnowledgeBases.slice();
    }
    renderKbDropdown();
  }

  function findAgentById(id) {
    for (var i = 0; i < agents.length; i++) {
      if (agents[i].id === id) return agents[i];
    }
    return null;
  }

  function renderKbDropdown() {
    var kbMenu = $('sp-kb-menu');
    if (!kbMenu) return;

    // 清除旧的知识库选项
    var items = kbMenu.querySelectorAll('.sp-dropdown-item');
    items.forEach(function (item) { item.remove(); });

    var firstDivider = kbMenu.querySelector('.sp-dropdown-divider');

    if (knowledgeBases.length === 0) {
      // 没有知识库时只显示一个"全部"选项
      var allItem = createKbItem('all', '全部知识库', true);
      kbMenu.insertBefore(allItem, firstDivider);
      selectedKbId = 'all';
      $('sp-kb-name').textContent = '全部';
      return;
    }

    // 插入 "全部知识库" 选项
    var allItem = createKbItem('all', '全部知识库', true);
    kbMenu.insertBefore(allItem, firstDivider);

    // 插入真实知识库
    knowledgeBases.forEach(function (kb) {
      var item = createKbItem(kb.id, kb.name, false);
      kbMenu.insertBefore(item, firstDivider);
    });

    // 重置选择
    selectedKbId = 'all';
    $('sp-kb-name').textContent = '全部';
  }

  function createKbItem(kbId, name, isSelected) {
    var div = document.createElement('div');
    div.className = 'sp-dropdown-item' + (isSelected ? ' selected' : '');
    div.setAttribute('data-kb', kbId);
    div.innerHTML = '<span class="sp-radio"></span> ' + escapeHtml(name);
    div.addEventListener('click', function (e) {
      e.stopPropagation();
      selectedKbId = kbId;
      $('sp-kb-name').textContent = name.length > 4 ? name.substring(0, 4) : name;
      var allItems = $('sp-kb-menu').querySelectorAll('.sp-dropdown-item');
      allItems.forEach(function (i) { i.classList.remove('selected'); });
      div.classList.add('selected');
      $('sp-kb-menu').classList.remove('show');
    });
    return div;
  }

  // === 渲染智能体下拉（替换静态模式选项）===
  function renderAgentDropdown() {
    var modeMenu = $('sp-mode-menu');
    if (!modeMenu || agents.length === 0) return;

    // 清除旧的模式选项
    var modeItems = modeMenu.querySelectorAll('.sp-mode-item');
    modeItems.forEach(function (item) { item.remove(); });

    agents.forEach(function (agent, idx) {
      var item = document.createElement('div');
      var isQuickAnswer = agent.id === 'builtin-quick-answer' || (agent.config && agent.config.agent_mode === 'quick-answer');
      var isSelected = selectedAgentId ? (agent.id === selectedAgentId) : (idx === 0);
      item.className = 'sp-mode-item' + (isSelected ? ' selected' : '');
      item.setAttribute('data-agent-id', agent.id);
      item.innerHTML = '<span class="sp-radio"></span> ' + escapeHtml(agent.name);
      if (isSelected) {
        $('sp-mode-name').textContent = agent.name;
      }
      item.addEventListener('click', function (e) {
        e.stopPropagation();
        selectedAgentId = agent.id;
        selectedAgentEnabled = !isQuickAnswer;
        selectedAgentImageUpload = !!(agent.config && agent.config.image_upload_enabled);
        updateImageUploadUI();
        modeMenu.querySelectorAll('.sp-mode-item').forEach(function (i) { i.classList.remove('selected'); });
        item.classList.add('selected');
        $('sp-mode-name').textContent = agent.name;
        modeMenu.classList.remove('show');
        // 持久化模式选择，同步给其他页面
        chrome.storage.local.set({ ka_selected_agent: { agentId: agent.id, agentEnabled: !isQuickAnswer } });
        // 切换智能体后根据配置过滤知识库
        filterAndRenderKbs();
      });
      modeMenu.appendChild(item);
    });
  }

  // === 消息发送 ===
  inputEl.addEventListener('input', function () {
    sendBtn.classList.toggle('active', inputEl.value.trim().length > 0);
    // 自动调整高度
    inputEl.style.height = 'auto';
    inputEl.style.height = Math.min(inputEl.scrollHeight, 120) + 'px';
  });

  inputEl.addEventListener('keydown', function (e) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      sendMessage();
    }
  });

  sendBtn.addEventListener('click', function () { sendMessage(); });

  function sendMessage(text, externalImages) {
    var query = (typeof text === 'string' && text) ? text : inputEl.value.trim();
    if (!query || isSending) return;

    if (welcomeEl) welcomeEl.style.display = 'none';

    // 图片来源：本地待上传 or 外部传入（popup base64）
    var msgImageUrls = pendingImages.map(function (img) { return img.preview; });
    var hasExternalImages = externalImages && externalImages.length > 0;
    if (hasExternalImages) {
      msgImageUrls = externalImages.map(function (img) { return img.data; });
    }

    appendMsg('user', query, false, msgImageUrls.length > 0 ? msgImageUrls : null);
    inputEl.value = '';
    inputEl.style.height = 'auto';
    sendBtn.classList.remove('active');

    showTyping(true);
    isSending = true;
    streamingText = '';
    streamingBotDiv = null;
    streamingReferences = [];
    renderPending = false;
    agentEventStream = [];
    agentContainer = null;
    agentExpandedIds = {};
    agentActiveThinkingIds = {};
    agentHasAnswer = false;
    agentTreeExpanded = false;
    agentRefsExpanded = false;
    agentAnswerText = '';
    agentAnswerDone = false;
    currentRequestId = Date.now().toString(36) + Math.random().toString(36).slice(2, 6);

    // 构建请求参数
    var payload = {
      query: query,
      sessionId: currentSessionId,
      agentId: selectedAgentId,
      agentEnabled: selectedAgentEnabled,
      _requestId: currentRequestId
    };
    if (selectedKbId && selectedKbId !== 'all') {
      payload.knowledgeBaseIds = [selectedKbId];
    }

    // 外部传入的图片直接使用
    if (hasExternalImages) {
      payload.images = externalImages;
      doSendPayload(payload);
    } else if (pendingImages.length > 0) {
      // 本地待上传图片，先转 base64 再发送
      var promises = pendingImages.map(function (img) { return fileToBase64(img.file); });
      Promise.all(promises).then(function (dataURIs) {
        payload.images = dataURIs.map(function (d) { return { data: d }; });
        clearImages(true);
        doSendPayload(payload);
      }).catch(function () {
        showToast('图片读取失败');
        isSending = false;
        showTyping(false);
      });
    } else {
      doSendPayload(payload);
    }
  }

  function doSendPayload(payload) {
    // 发送到 background 进行真实 API 调用
    chrome.runtime.sendMessage({ type: 'CHAT_QUERY', payload: payload }, function (resp) {
      void chrome.runtime.lastError;
      isSending = false;
      showTyping(false);

      if (resp && resp.success) {
        if (resp.sessionId) {
          currentSessionId = resp.sessionId;
        }
        if (agentContainer) {
          agentAnswerDone = true;
          renderAgentStream();
        } else if (streamingBotDiv && streamingText) {
          streamingBotDiv.innerHTML = renderMarkdown(streamingText);
          hydrateFileImages(streamingBotDiv);
          messagesEl.scrollTop = messagesEl.scrollHeight;
        } else if (resp.data && !streamingBotDiv && !agentContainer) {
          appendMsg('bot', resp.data, true);
        }
        streamingBotDiv = null;
        streamingText = '';
      } else {
        if (agentContainer) { agentContainer.remove(); agentContainer = null; }
        if (streamingBotDiv) { streamingBotDiv.remove(); streamingBotDiv = null; streamingText = ''; }
        var errMsg = (resp && resp.error) || '请求失败，请检查服务配置';
        appendMsg('bot', errMsg);
      }
    });
  }

  // === 处理来自 background 的流式推送（参考 AgentStreamDisplay.vue）===
  function cleanThinkingContent(text) {
    if (!text) return '';
    return text.replace(/Calling tool:\s*\w*/g, '').replace(/\n{3,}/g, '\n\n').trim();
  }

  function resolveAllPending() {
    for (var k = 0; k < agentEventStream.length; k++) {
      var e = agentEventStream[k];
      if (e._type === 'tool_call' && e.pending) { e.pending = false; e.success = true; e.output = e.output || ''; }
    }
  }

  function resolveOldPending() {
    var foundLatest = false;
    for (var k = agentEventStream.length - 1; k >= 0; k--) {
      var e = agentEventStream[k];
      if (e._type === 'tool_call' && e.pending) {
        if (!foundLatest) { foundLatest = true; continue; }
        e.pending = false; e.success = true; e.output = e.output || '';
      }
    }
  }

  function handleStreamChunk(payload) {
    if (!payload) return;
    if (payload.requestId && payload.requestId !== currentRequestId) return;
    var type = payload.responseType;

    if (payload.sessionId && !currentSessionId) {
      currentSessionId = payload.sessionId;
    }

    if (type === 'answer') {
      agentAnswerText += payload.content || '';
      if (payload.done) agentAnswerDone = true;
      if (!agentHasAnswer) {
        agentHasAnswer = true;
        showTyping(false);
        collapseActiveThinking();
        collapseAllExpanded();
        resolveAllPending();
        agentContainer && (agentContainer._stepsChanged = true);
      }
      scheduleAgentRender();

    } else if (type === 'thinking' || (type === 'tool_call' && payload.toolName === 'thinking')) {
      if (agentEventStream.length > 0) setTypingText('');
      var thinkContent = cleanThinkingContent(payload.content);
      if (!thinkContent || /^Thought process recorded/i.test(thinkContent)) return;
      var lastEv = agentEventStream.length > 0 ? agentEventStream[agentEventStream.length - 1] : null;
      if (lastEv && lastEv._type === 'thinking') {
        lastEv.content += thinkContent;
      } else {
        collapseAllExpanded();
        var tid = 'think-' + Date.now() + '-' + Math.random().toString(36).slice(2, 5);
        agentEventStream.push({ _type: 'thinking', id: tid, content: thinkContent });
        agentActiveThinkingIds[tid] = true;
        agentContainer && (agentContainer._stepsChanged = true);
      }
      scheduleAgentRender();

    } else if (type === 'tool_call') {
      if (agentEventStream.length > 0) setTypingText('');
      // 按 eventId(tool_call_id) 去重：同一 tool_call_id 的第二次事件是参数补充，合并而非新建
      var existingTc = null;
      if (payload.eventId) {
        for (var ei = agentEventStream.length - 1; ei >= 0; ei--) {
          if (agentEventStream[ei]._type === 'tool_call' && agentEventStream[ei].eventId === payload.eventId) {
            existingTc = agentEventStream[ei]; break;
          }
        }
      }
      if (existingTc) {
        if (payload.content) existingTc.content = payload.content;
      } else {
        collapseActiveThinking();
        collapseAllExpanded();
        resolveOldPending();
        var tcId = payload.eventId || ('tc-' + Date.now() + '-' + Math.random().toString(36).slice(2, 5));
        agentEventStream.push({
          _type: 'tool_call', id: tcId, eventId: payload.eventId || '', tool_name: payload.toolName || 'tool',
          pending: true, success: null, content: payload.content || '', output: ''
        });
        agentContainer && (agentContainer._stepsChanged = true);
      }
      scheduleAgentRender();

    } else if (type === 'tool_result') {
      if (payload.toolName === 'thinking') {
        var thinkRes = cleanThinkingContent(payload.content);
        if (thinkRes && !/^Thought process recorded/i.test(thinkRes)) {
          var lastThink = agentEventStream.length > 0 ? agentEventStream[agentEventStream.length - 1] : null;
          if (lastThink && lastThink._type === 'thinking') lastThink.content += thinkRes;
        }
        scheduleAgentRender();
      } else {
        var matched = false;
        for (var i = agentEventStream.length - 1; i >= 0; i--) {
          var ev = agentEventStream[i];
          if (ev._type !== 'tool_call' || !ev.pending) continue;
          var idMatch = payload.eventId && ev.eventId && ev.eventId === payload.eventId;
          var nameMatch = payload.toolName && ev.tool_name === payload.toolName;
          if (idMatch || nameMatch || !matched) {
            ev.pending = false; ev.success = payload.success !== false; ev.output = payload.content || '';
            matched = true; break;
          }
        }
        agentContainer && (agentContainer._stepsChanged = true);
        scheduleAgentRender();
      }

    } else if (type === 'references') {
      if (Array.isArray(payload.references) && payload.references.length > 0) {
        streamingReferences = payload.references;
        scheduleAgentRender();
      }

    } else if (type === 'complete') {
      showTyping(false);
      collapseActiveThinking();
      collapseAllExpanded();
      resolveAllPending();
      agentAnswerDone = true;
      agentContainer && (agentContainer._stepsChanged = true);
      scheduleAgentRender();

    } else if (type === 'error') {
      showTyping(false);
      if (agentContainer) { agentContainer.remove(); agentContainer = null; }
      if (streamingBotDiv) { streamingBotDiv.remove(); streamingBotDiv = null; streamingText = ''; }
      appendMsg('bot', payload.content || '请求出错');

    } else if (type === 'session_title') {
      var title = payload.content || '';
      if (title) {
        updateSessionTitle(title);
      }
    }
  }

  function collapseActiveThinking() {
    for (var key in agentActiveThinkingIds) {
      delete agentExpandedIds[key];
    }
    agentActiveThinkingIds = {};
    agentContainer && (agentContainer._stepsChanged = true);
  }

  function collapseAllExpanded() {
    agentExpandedIds = {};
    agentContainer && (agentContainer._stepsChanged = true);
  }

  var _agentRenderTimer = null;
  var _lastRenderTime = 0;
  var RENDER_THROTTLE = 60;
  function scheduleAgentRender() {
    var now = Date.now();
    var elapsed = now - _lastRenderTime;
    if (elapsed >= RENDER_THROTTLE) {
      if (_agentRenderTimer) { clearTimeout(_agentRenderTimer); _agentRenderTimer = null; }
      _lastRenderTime = now;
      renderAgentStream();
    } else if (!_agentRenderTimer) {
      _agentRenderTimer = setTimeout(function () {
        _agentRenderTimer = null;
        _lastRenderTime = Date.now();
        renderAgentStream();
      }, RENDER_THROTTLE - elapsed);
    }
  }

  // === 工具名称映射（参考 AgentStreamDisplay.vue）===
  var TOOL_NAME_DISPLAY = {
    knowledge_search: '语义搜索', search_knowledge: '语义搜索',
    grep_chunks: '文本搜索', list_knowledge_chunks: '阅读文档',
    get_document_info: '获取文档信息', get_document_content: '获取文档内容',
    get_related_documents: '查找关联文档',
    query_knowledge_graph: '知识图谱查询',
    web_search: '网络搜索', web_fetch: '网页抓取',
    todo_write: '制定计划', final_answer: '生成回答',
    thinking: '思考', read_skill: '读取技能',
    execute_skill_script: '执行技能脚本',
    data_analysis: '数据分析', data_schema: '数据结构',
    database_query: '数据库查询', image_analysis: '查看图片内容',
    knowledge_graph_extract: '知识图谱提取'
  };

  var TOOL_ICONS = {
    '语义搜索': '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>',
    '文本搜索': '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/></svg>',
    '阅读文档': '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/></svg>',
    '获取文档信息': '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>',
    '网络搜索': '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>',
    '网页抓取': '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>',
    '制定计划': '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 11l3 3L22 4"/><path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11"/></svg>',
    '思考': '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>',
    '数据分析': '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="20" x2="18" y2="10"/><line x1="12" y1="20" x2="12" y2="4"/><line x1="6" y1="20" x2="6" y2="14"/></svg>',
    '查看图片内容': '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="8.5" cy="8.5" r="1.5"/><polyline points="21 15 16 10 5 21"/></svg>'
  };
  var ICON_AGENT = '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg>';
  var ICON_CHEVRON = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"/></svg>';
  var DEFAULT_TOOL_ICON = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/></svg>';
  var ICON_COPY = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';

  function getToolDisplayName(rawName) {
    return TOOL_NAME_DISPLAY[rawName] || rawName;
  }

  function getToolIcon(displayName) {
    return TOOL_ICONS[displayName] || DEFAULT_TOOL_ICON;
  }

  function getThinkingSummary(content) {
    if (!content) return '';
    var cleaned = content.replace(/^#+\s+/gm, '').replace(/\*\*/g, '').replace(/\*/g, '')
      .replace(/`/g, '').replace(/\n+/g, ' ').trim();
    return cleaned.length <= 50 ? cleaned : cleaned.slice(0, 50) + '…';
  }

  function extractToolSummary(ev) {
    if (!ev.output) return '';
    try {
      var parsed = JSON.parse(ev.output);
      if (parsed.results && Array.isArray(parsed.results)) return '找到 ' + parsed.results.length + ' 条结果';
      if (parsed.total_matches !== undefined) return '匹配 ' + parsed.total_matches + ' 条';
      if (parsed.total !== undefined) return '找到 ' + parsed.total + ' 条结果';
    } catch (e) {}
    var summary = ev.output.replace(/\n/g, ' ').trim();
    return summary.length > 60 ? summary.substring(0, 60) + '…' : summary;
  }

  // === Agent 流式渲染 — 增量 DOM 更新（避免 innerHTML 全量重建导致图片重刷）===
  function renderAgentStream() {
    if (agentEventStream.length === 0 && !agentAnswerText) return;

    if (!agentContainer) {
      agentContainer = document.createElement('div');
      agentContainer.className = 'agent-stream';
      messagesEl.insertBefore(agentContainer, typingEl);
      agentContainer._stepsEl = document.createElement('div');
      agentContainer.appendChild(agentContainer._stepsEl);
    }

    var changed = false;

    // --- 1. 渲染中间步骤（仅当事件变化时更新）---
    if (!agentContainer._lastStepCount || agentContainer._lastStepCount !== agentEventStream.length ||
        agentContainer._stepsChanged || (agentHasAnswer && !agentContainer._treeMode)) {
      renderAgentSteps();
      if (agentHasAnswer) agentContainer._treeMode = true;
      changed = true;
    } else {
      var lastEv = agentEventStream.length > 0 ? agentEventStream[agentEventStream.length - 1] : null;
      if (lastEv && lastEv._type === 'thinking' && agentActiveThinkingIds[lastEv.id]) {
        renderAgentSteps();
        changed = true;
      }
    }

    // --- 2. 增量渲染回答内容（有图片，必须保持 DOM 稳定）---
    if (renderAgentAnswer()) changed = true;

    if (changed) messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  function renderAgentSteps() {
    var stepsEl = agentContainer._stepsEl;
    var events = agentEventStream;
    var lastEv = events.length > 0 ? events[events.length - 1] : null;

    // 如果只是活跃的思考内容在更新（事件数量没变、无状态切换），就地更新而非重建
    if (lastEv && lastEv._type === 'thinking' &&
        agentContainer._lastStepCount === events.length &&
        !agentContainer._stepsChanged && !agentHasAnswer) {
      var activeCard = stepsEl.querySelector('.agent-card[data-eid="' + lastEv.id + '"]');
      if (activeCard) {
        var detailEl = activeCard.querySelector('.agent-card-detail');
        if (detailEl) {
          detailEl.textContent = lastEv.content;
          detailEl.scrollTop = detailEl.scrollHeight;
        }
        return;
      }
    }

    // 需要全量重建
    agentContainer._lastStepCount = events.length;
    var wasCollapsed = agentContainer._stepsChanged;
    agentContainer._stepsChanged = false;

    if (!wasCollapsed) {
      stepsEl.querySelectorAll('.agent-card.expanded').forEach(function (el) {
        var eid = el.getAttribute('data-eid');
        if (eid) agentExpandedIds[eid] = true;
      });
      if (stepsEl.querySelector('.agent-tree-root.expanded')) agentTreeExpanded = true;
    }

    var intermediateEvents = events.filter(function (e) { return e._type !== 'error'; });
    var stepCount = intermediateEvents.length;
    var html = '';

    if (agentHasAnswer && stepCount > 0) {
      html += '<div class="agent-tree-root' + (agentTreeExpanded ? ' expanded' : '') + '">';
      html += '<span class="agent-tree-root-title">' + ICON_AGENT + '<span>智能体执行 ' + stepCount + ' 步</span></span>';
      html += '<span class="agent-tree-root-toggle">' + ICON_CHEVRON + '</span>';
      html += '</div>';
      html += '<div class="agent-tree-children">';
      for (var ti = 0; ti < intermediateEvents.length; ti++) {
        var isLast = ti === intermediateEvents.length - 1;
        html += '<div class="agent-tree-child' + (isLast ? ' last' : '') + '">';
        html += '<div class="agent-tree-branch"></div>';
        html += buildEventCardHTML(intermediateEvents[ti], true);
        html += '</div>';
      }
      html += '</div>';
    } else if (!agentHasAnswer) {
      for (var fi = 0; fi < events.length; fi++) {
        html += buildEventCardHTML(events[fi], false);
      }
    }

    stepsEl.innerHTML = html;
    bindStepEvents(stepsEl);

    // 活跃思考卡片自动滚动到底部
    for (var aid in agentActiveThinkingIds) {
      var thinkCard = stepsEl.querySelector('.agent-card[data-eid="' + aid + '"]');
      if (thinkCard) {
        var det = thinkCard.querySelector('.agent-card-detail');
        if (det) det.scrollTop = det.scrollHeight;
      }
    }
  }

  function bindStepEvents(stepsEl) {
    var treeRoot = stepsEl.querySelector('.agent-tree-root');
    if (treeRoot) {
      treeRoot.addEventListener('click', function () {
        agentTreeExpanded = !agentTreeExpanded;
        treeRoot.classList.toggle('expanded');
        var children = treeRoot.nextElementSibling;
        if (children && children.classList.contains('agent-tree-children')) {
          children.style.display = agentTreeExpanded ? 'block' : 'none';
        }
      });
    }
    stepsEl.querySelectorAll('.agent-card-header:not(.no-expand)').forEach(function (hdr) {
      hdr.addEventListener('click', function () {
        var card = hdr.closest('.agent-card');
        if (!card) return;
        var eid = card.getAttribute('data-eid');
        card.classList.toggle('expanded');
        if (eid) {
          if (card.classList.contains('expanded')) agentExpandedIds[eid] = true;
          else delete agentExpandedIds[eid];
        }
      });
    });
  }

  var ANSWER_RENDER_INTERVAL = 20;
  function renderAgentAnswer() {
    if (!agentAnswerText) return false;

    if (!agentContainer._answerWrapper) {
      var wrapper = document.createElement('div');
      wrapper.className = 'agent-answer';
      var content = document.createElement('div');
      content.className = 'agent-answer-content';
      wrapper.appendChild(content);
      agentContainer.appendChild(wrapper);
      agentContainer._answerWrapper = wrapper;
      agentContainer._answerContent = content;
      agentContainer._lastAnswerLen = 0;
      agentContainer._lastAnswerRenderTime = 0;
      agentContainer._answerTimer = null;
    }

    var newLen = agentAnswerText.length;
    if (newLen === (agentContainer._lastAnswerLen || 0)) return false;

    var now = Date.now();
    var timeSince = now - (agentContainer._lastAnswerRenderTime || 0);

    if (agentAnswerDone || timeSince >= ANSWER_RENDER_INTERVAL) {
      if (agentContainer._answerTimer) { clearTimeout(agentContainer._answerTimer); agentContainer._answerTimer = null; }
      agentContainer._lastAnswerLen = newLen;
      agentContainer._lastAnswerRenderTime = now;
      agentContainer._answerContent.innerHTML = renderMarkdown(agentAnswerText);
      hydrateFileImages(agentContainer._answerContent);

      // 回答完成后添加工具栏（只添加一次）
      if (agentAnswerDone && !agentContainer._toolbarEl) {
        var toolbar = document.createElement('div');
        toolbar.className = 'agent-answer-toolbar';
        toolbar.innerHTML = '<button class="btn-copy-answer" title="复制">' + ICON_COPY + '</button>';
        toolbar.querySelector('.btn-copy-answer').addEventListener('click', function () {
          copyToClipboard(agentAnswerText || '');
        });
        agentContainer._answerWrapper.appendChild(toolbar);
        agentContainer._toolbarEl = toolbar;
      }

      return true;
    }
    if (!agentContainer._answerTimer) {
      agentContainer._answerTimer = setTimeout(function () {
        agentContainer._answerTimer = null;
        if (renderAgentAnswer()) messagesEl.scrollTop = messagesEl.scrollHeight;
      }, ANSWER_RENDER_INTERVAL - timeSince);
    }
    return false;
  }

  function buildEventCardHTML(ev, inTree) {
    var isActive = !!agentActiveThinkingIds[ev.id];
    var isExpanded = isActive || !!agentExpandedIds[ev.id];
    var cls = 'agent-card';
    if (isExpanded) cls += ' expanded';
    if (ev.pending) cls += ' pending';
    if (ev.success === false) cls += ' error';

    var html = '<div class="' + cls + '" data-eid="' + ev.id + '">';

    if (ev._type === 'thinking') {
      var thinkIcon = getToolIcon('思考');
      html += '<div class="agent-card-header">';
      html += '<div class="agent-card-title">' + thinkIcon + '<span class="agent-card-name">思考</span>';
      if (isActive) html += '<span class="agent-spinner"></span>';
      html += '</div>';
      if (ev.content) html += '<span class="agent-card-toggle">' + ICON_CHEVRON + '</span>';
      html += '</div>';
      if (ev.content) {
        html += '<div class="agent-card-detail">' + escapeHtml(ev.content) + '</div>';
      }

    } else if (ev._type === 'tool_call') {
      var displayName = getToolDisplayName(ev.tool_name);
      var icon = getToolIcon(displayName);
      var hasDetail = false;

      if (ev.pending) {
        html += '<div class="agent-card-header no-expand">';
        html += '<div class="agent-card-title">' + icon + '<span class="agent-card-name">正在调用 ' + escapeHtml(displayName) + '…</span></div>';
        html += '<span class="agent-spinner"></span>';
        html += '</div>';
      } else {
        hasDetail = !!ev.output;
        html += '<div class="agent-card-header' + (hasDetail ? '' : ' no-expand') + '">';
        html += '<div class="agent-card-title">' + icon + '<span class="agent-card-name">' + escapeHtml(displayName) + '</span>';
        html += '</div>';
        if (hasDetail) html += '<span class="agent-card-toggle">' + ICON_CHEVRON + '</span>';
        html += '</div>';
        if (hasDetail) {
          html += '<div class="agent-card-detail">' + escapeHtml(ev.output).substring(0, 600) + '</div>';
        }
      }
    }

    html += '</div>';
    return html;
  }

  // buildReferencesHTML 和 bindAgentEvents 已拆解到 renderAgentSteps / renderAgentAnswer 中

  function copyToClipboard(text) {
    if (!text) return;
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(function () {
        showToast('已复制');
      }).catch(function () { fallbackCopy(text); });
    } else {
      fallbackCopy(text);
    }
  }

  function fallbackCopy(text) {
    var ta = document.createElement('textarea');
    ta.value = text;
    ta.style.cssText = 'position:fixed;left:-9999px;';
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand('copy'); showToast('已复制'); } catch (e) {}
    document.body.removeChild(ta);
  }

  function showToast(msg) {
    var toast = document.createElement('div');
    toast.textContent = msg;
    toast.style.cssText = 'position:fixed;top:60px;left:50%;transform:translateX(-50%);background:#07C160;color:#fff;padding:6px 18px;border-radius:20px;font-size:12px;z-index:9999;animation:agentFadeIn 0.2s;';
    document.body.appendChild(toast);
    setTimeout(function () { toast.remove(); }, 1500);
  }

  // === 图片上传 ===
  var imageInput = $('sp-image-input');
  var imageBtn = $('sp-image-btn');
  var imagePreviews = $('sp-image-previews');

  function addImageFiles(files) {
    if (!selectedAgentImageUpload) return;
    for (var i = 0; i < files.length; i++) {
      if (pendingImages.length >= MAX_IMAGES) {
        showToast('最多上传 ' + MAX_IMAGES + ' 张图片');
        break;
      }
      var f = files[i];
      if (ALLOWED_IMAGE_TYPES.indexOf(f.type) === -1) {
        showToast('仅支持 JPG/PNG/GIF/WebP 格式');
        continue;
      }
      if (f.size > MAX_IMAGE_SIZE) {
        showToast('图片大小不能超过 10MB');
        continue;
      }
      pendingImages.push({ file: f, preview: URL.createObjectURL(f) });
    }
    renderImagePreviews();
  }

  function removeImage(index) {
    if (index >= 0 && index < pendingImages.length) {
      URL.revokeObjectURL(pendingImages[index].preview);
      pendingImages.splice(index, 1);
    }
    renderImagePreviews();
  }

  function clearImages(skipRevoke) {
    if (!skipRevoke) {
      for (var i = 0; i < pendingImages.length; i++) {
        URL.revokeObjectURL(pendingImages[i].preview);
      }
    }
    pendingImages = [];
    renderImagePreviews();
  }

  function renderImagePreviews() {
    if (!imagePreviews) return;
    if (pendingImages.length === 0) {
      imagePreviews.style.display = 'none';
      imagePreviews.innerHTML = '';
      return;
    }
    imagePreviews.style.display = 'flex';
    var html = '';
    for (var i = 0; i < pendingImages.length; i++) {
      html += '<div class="sp-img-item" data-idx="' + i + '">'
        + '<img class="sp-img-thumb" src="' + pendingImages[i].preview + '">'
        + '<span class="sp-img-remove">&times;</span>'
        + '</div>';
    }
    imagePreviews.innerHTML = html;
    imagePreviews.querySelectorAll('.sp-img-remove').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var idx = parseInt(btn.parentElement.getAttribute('data-idx'));
        removeImage(idx);
      });
    });
  }

  function updateImageUploadUI() {
    if (imageBtn) imageBtn.style.display = selectedAgentImageUpload ? '' : 'none';
    if (!selectedAgentImageUpload && pendingImages.length > 0) clearImages();
  }

  function fileToBase64(file) {
    return new Promise(function (resolve, reject) {
      var reader = new FileReader();
      reader.onload = function () { resolve(reader.result); };
      reader.onerror = reject;
      reader.readAsDataURL(file);
    });
  }

  if (imageInput) {
    imageInput.addEventListener('change', function () {
      if (imageInput.files) addImageFiles(Array.from(imageInput.files));
      imageInput.value = '';
    });
  }
  if (imageBtn) {
    imageBtn.addEventListener('click', function () {
      if (imageInput) imageInput.click();
    });
  }

  // 粘贴图片
  inputEl.addEventListener('paste', function (e) {
    if (!selectedAgentImageUpload) return;
    var items = e.clipboardData && e.clipboardData.items;
    if (!items) return;
    var imageFiles = [];
    for (var i = 0; i < items.length; i++) {
      if (items[i].type.indexOf('image/') === 0) {
        var f = items[i].getAsFile();
        if (f) imageFiles.push(f);
      }
    }
    if (imageFiles.length > 0) {
      e.preventDefault();
      addImageFiles(imageFiles);
    }
  });

  function appendMsg(role, text, isMarkdown, imageUrls) {
    var div = document.createElement('div');
    div.className = 'msg msg-' + (role === 'user' ? 'user' : 'bot');
    if (role === 'bot' && isMarkdown) {
      div.innerHTML = renderMarkdown(text);
    } else {
      div.textContent = text;
    }
    // 在用户消息中显示上传的图片
    if (role === 'user' && imageUrls && imageUrls.length > 0) {
      var imgWrap = document.createElement('div');
      imgWrap.className = 'msg-user-images';
      imageUrls.forEach(function (url) {
        var img = document.createElement('img');
        img.src = url;
        img.className = 'msg-user-img';
        imgWrap.appendChild(img);
      });
      div.appendChild(imgWrap);
    }
    messagesEl.insertBefore(div, typingEl);
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  function appendClipCard(title, content) {
    if (welcomeEl) welcomeEl.style.display = 'none';

    var card = document.createElement('div');
    card.className = 'clip-card';
    card.innerHTML = '<div class="clip-card-title"><svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2"/><rect x="8" y="2" width="8" height="4" rx="1"/></svg> ' + escapeHtml(title) + '</div>'
      + '<div class="clip-card-content">' + escapeHtml(content).substring(0, 500) + '</div>'
      + '<div class="clip-actions"><button class="clip-action-btn save-clip-btn">保存到知识库</button></div>';

    card.querySelector('.save-clip-btn').addEventListener('click', function () {
      var btn = this;
      // 优先用对话中选中的 KB，其次用剪藏设置的 KB
      var targetKbId = (selectedKbId && selectedKbId !== 'all') ? selectedKbId : '';
      if (targetKbId) {
        doSaveClip(btn, targetKbId, title, content);
      } else {
        // 从 storage 读取剪藏知识库
        chrome.storage.local.get(['clipKbId'], function (data) {
          if (data.clipKbId) {
            doSaveClip(btn, data.clipKbId, title, content);
          } else {
            saveToLocal(btn, title, content);
          }
        });
      }
    });

    function doSaveClip(btn, kbId, clipTitle, clipContent) {
      // 获取当前页面 URL
      chrome.tabs.query({ active: true, currentWindow: true }, function (tabs) {
        void chrome.runtime.lastError;
        var pageUrl = (tabs && tabs[0]) ? tabs[0].url : '';
        chrome.runtime.sendMessage({
          type: 'SAVE_CLIP_TO_KB',
          payload: { kbId: kbId, title: clipTitle, content: clipContent, url: pageUrl }
        }, function (resp) {
          void chrome.runtime.lastError;
          if (resp && resp.success) {
            btn.textContent = '已保存到知识库';
            btn.disabled = true;
          } else {
            saveToLocal(btn, clipTitle, clipContent);
          }
        });
      });
    }

    messagesEl.insertBefore(card, typingEl);
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  function saveToLocal(btn, title, content) {
    chrome.runtime.sendMessage({
      type: 'SAVE_NOTE',
      payload: { type: 'clip', content: '## ' + title + '\n\n' + content }
    }, function (resp) {
      void chrome.runtime.lastError;
      if (resp && resp.success) {
        btn.textContent = '已保存';
        btn.disabled = true;
      }
    });
  }

  function showTyping(show) {
    typingEl.classList.toggle('show', show);
    if (!show) setTypingText(''); // 隐藏时重置文字
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  function setTypingText(text) {
    if (!typingEl) return;
    // 找到或创建文字元素
    var textEl = typingEl.querySelector('.typing-text');
    if (!textEl && text) {
      textEl = document.createElement('span');
      textEl.className = 'typing-text';
      textEl.style.cssText = 'font-size:12px;color:#999;margin-left:8px;';
      typingEl.appendChild(textEl);
    }
    if (textEl) {
      textEl.textContent = text;
    }
  }

  function escapeHtml(s) {
    var d = document.createElement('div');
    d.textContent = s;
    return d.innerHTML;
  }

  // === 文件 URL 转换：minio/cos/tos/local/s3 前缀走 /files 接口 ===
  var _fileBlobCache = {};
  var _fileFailedCache = {};

  function hydrateFileImages(container) {
    if (!container) return;
    var imgs = container.querySelectorAll('img[data-file-src]');
    if (!imgs.length) return;
    imgs.forEach(function (img) {
      var fileSrc = img.getAttribute('data-file-src');
      if (!fileSrc) return;
      if (_fileFailedCache[fileSrc]) {
        img.removeAttribute('src');
        img.alt = '[图片加载失败]';
        img.style.display = 'none';
        return;
      }
      if (_fileBlobCache[fileSrc]) {
        img.src = _fileBlobCache[fileSrc];
        return;
      }
      if (img.getAttribute('data-hydrated') === '1') return;
      img.setAttribute('data-hydrated', '1');
      chrome.runtime.sendMessage({ type: 'FETCH_FILE', payload: { filePath: fileSrc } }, function (resp) {
        void chrome.runtime.lastError;
        if (resp && resp.success && resp.dataUrl) {
          _fileBlobCache[fileSrc] = resp.dataUrl;
          img.src = resp.dataUrl;
          img.style.display = '';
        } else {
          _fileFailedCache[fileSrc] = true;
          img.alt = '[图片加载失败]';
          img.style.display = 'none';
        }
      });
    });
  }

  // === Markdown 渲染（参考 botmsg.vue / AgentStreamDisplay.vue）===
  var MD_MAX_LEN = 100000;
  function renderMarkdown(text) {
    if (!text) return '';
    try { return _renderMarkdownInner(text); } catch (e) {
      console.warn('renderMarkdown error:', e);
      var safe = text.length > 2000 ? text.substring(0, 2000) + '\n…（渲染出错，已截断）' : text;
      return '<p class="md-p">' + escapeHtml(safe) + '</p>';
    }
  }
  function _renderMarkdownInner(text) {
    if (text.length > MD_MAX_LEN) text = text.substring(0, MD_MAX_LEN) + '\n\n…（内容过长，已截断）';

    // 1. 提取代码块，替换为占位符（避免内部被解析）
    var codeBlocks = [];
    var processed = text.replace(/```(\w*)\n([\s\S]*?)```/g, function (_, lang, code) {
      var idx = codeBlocks.length;
      codeBlocks.push('<pre class="md-pre"><code>' + escapeHtml(code.replace(/\n$/, '')) + '</code></pre>');
      return '\x00CODEBLOCK' + idx + '\x00';
    });

    // 2. 提取行内代码
    var inlineCodes = [];
    processed = processed.replace(/`([^`]+)`/g, function (_, code) {
      var idx = inlineCodes.length;
      inlineCodes.push('<code class="md-code">' + escapeHtml(code) + '</code>');
      return '\x00INLINE' + idx + '\x00';
    });

    // 2.5 提取表格，替换为占位符
    var tables = [];
    processed = processed.replace(/((?:\|.+\|\n)+)/g, function (tableBlock) {
      var rows = tableBlock.trim().split('\n');
      if (rows.length < 2) return tableBlock;
      var out = '<table class="md-table">';
      rows.forEach(function (row, ri) {
        if (/^\|[\s\-:|]+\|$/.test(row)) return; // 跳过分隔行
        var tag = ri === 0 ? 'th' : 'td';
        var cells = row.split('|').filter(function (c, ci, arr) { return ci > 0 && ci < arr.length - 1; });
        out += '<tr>';
        cells.forEach(function (cell) { out += '<' + tag + '>' + inlineFormat(cell.trim()) + '</' + tag + '>'; });
        out += '</tr>';
      });
      out += '</table>';
      var idx = tables.length;
      tables.push(out);
      return '\n\x00TABLE' + idx + '\x00\n';
    });

    // 3. 逐行解析为块元素
    var lines = processed.split('\n');
    var html = '';
    var i = 0;

    while (i < lines.length) {
      var line = lines[i];

      // 代码块占位符
      var cbMatch = line.match(/^\x00CODEBLOCK(\d+)\x00$/);
      if (cbMatch) {
        html += codeBlocks[parseInt(cbMatch[1])];
        i++; continue;
      }

      // 表格占位符
      var tbMatch = line.match(/^\x00TABLE(\d+)\x00$/);
      if (tbMatch) {
        html += '<div class="md-table-wrap">' + tables[parseInt(tbMatch[1])] + '</div>';
        i++; continue;
      }

      // 标题
      if (/^(#{1,6})\s+(.+)/.test(line)) {
        var level = RegExp.$1.length;
        html += '<h' + level + ' class="md-h">' + inlineFormat(RegExp.$2) + '</h' + level + '>';
        i++; continue;
      }

      // 引用块（合并连续 > 行）
      if (/^>\s*(.*)/.test(line)) {
        var bqLines = [];
        while (i < lines.length && /^>\s*(.*)/.test(lines[i])) {
          bqLines.push(RegExp.$1);
          i++;
        }
        html += '<blockquote class="md-bq">' + inlineFormat(bqLines.join('<br>')) + '</blockquote>';
        continue;
      }

      // 无序列表（- 或 * 开头，需要和 *italic* 区分）
      if (/^(\s*)[*\-]\s+(.+)/.test(line)) {
        html += parseList(lines, i, 'ul');
        // 跳过已消费的行
        while (i < lines.length && /^(\s*)[*\-]\s+/.test(lines[i])) i++;
        continue;
      }

      // 有序列表
      if (/^(\s*)\d+\.\s+(.+)/.test(line)) {
        html += parseList(lines, i, 'ol');
        while (i < lines.length && /^(\s*)\d+\.\s+/.test(lines[i])) i++;
        continue;
      }

      // 水平线
      if (/^[-*_]{3,}\s*$/.test(line.trim())) {
        html += '<hr>';
        i++; continue;
      }

      // 空行
      if (line.trim() === '') {
        i++; continue;
      }

      // 普通段落（合并连续非空行，限制单段最大 500 行防溢出）
      var pLines = [];
      while (i < lines.length && pLines.length < 500) {
        var pl = lines[i];
        if (pl.trim() === '' || /^#{1,6}\s/.test(pl) || /^>\s/.test(pl) ||
            /^(\s*)[*\-]\s+/.test(pl) || /^(\s*)\d+\.\s+/.test(pl) ||
            /^\x00CODEBLOCK/.test(pl) || /^\x00TABLE/.test(pl) || /^[-*_]{3,}\s*$/.test(pl.trim())) break;
        pLines.push(pl);
        i++;
      }
      try {
        html += '<p class="md-p">' + inlineFormat(pLines.join('<br>')) + '</p>';
      } catch (e) {
        try { html += '<p class="md-p">' + escapeHtml(pLines.slice(0, 5).join('\n')) + '…</p>'; } catch (_) { /* skip */ }
      }
      if (html.length > 2000000) { html += '<p class="md-p">…（内容过长，已截断）</p>'; break; }
    }

    // 4. 恢复行内代码占位符
    html = html.replace(/\x00INLINE(\d+)\x00/g, function (_, idx) {
      return inlineCodes[parseInt(idx)];
    });

    return html;

    // --- 列表解析（支持嵌套）---
    function parseList(allLines, startIdx, defaultType) {
      var items = [];
      var j = startIdx;
      var baseIndent = -1;
      var listTag = defaultType;
      var re = listTag === 'ul' ? /^(\s*)[*\-]\s+(.+)/ : /^(\s*)\d+\.\s+(.+)/;

      while (j < allLines.length) {
        var m = allLines[j].match(re);
        if (!m) break;
        var indent = m[1].length;
        if (baseIndent < 0) baseIndent = indent;
        if (indent > baseIndent) {
          // 嵌套：找出子列表范围
          var subStart = j;
          while (j < allLines.length) {
            var sm = allLines[j].match(re);
            if (!sm || sm[1].length < indent) break;
            j++;
          }
          // 将子列表附加到上一个 li
          var subHtml = parseList(allLines, subStart, listTag);
          if (items.length > 0) {
            items[items.length - 1] += subHtml;
          }
          continue;
        }
        if (indent < baseIndent) break;
        items.push(inlineFormat(m[2]));
        j++;
      }

      var out = '<' + listTag + ' class="md-list">';
      for (var k = 0; k < items.length; k++) {
        out += '<li>' + items[k] + '</li>';
      }
      out += '</' + listTag + '>';
      return out;
    }

    // --- 行内格式化 ---
    function inlineFormat(s) {
      if (!s) return '';
      s = s.replace(/!\[([^\]]*)\]\(([^)]+)\)/g, function (_, alt, url) {
        if (/^(minio|cos|tos|local|s3):\/\//.test(url)) {
          if (_fileFailedCache[url]) return '';
          return '<img class="md-img" src="" data-file-src="' + url + '" alt="' + alt + '">';
        }
        return '<img class="md-img" src="' + url + '" alt="' + alt + '">';
      });
      // 链接
      s = s.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>');
      // 粗斜体
      s = s.replace(/\*\*\*(.+?)\*\*\*/g, '<strong><em>$1</em></strong>');
      // 粗体
      s = s.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
      // 斜体（避免匹配列表 * ）
      s = s.replace(/(?<!\*)\*([^\s*][^*]*?)\*(?!\*)/g, '<em>$1</em>');
      // 删除线
      s = s.replace(/~~(.+?)~~/g, '<del>$1</del>');
      // 恢复行内代码占位符
      s = s.replace(/\x00INLINE(\d+)\x00/g, function (_, idx) {
        return inlineCodes[parseInt(idx)];
      });
      return s;
    }
  }

  // === 清空对话 ===
  $('btn-clear').addEventListener('click', function () {
    // 先调用后端清空当前会话的消息
    if (currentSessionId) {
      chrome.runtime.sendMessage({
        type: 'CLEAR_SESSION_MESSAGES',
        payload: { sessionId: currentSessionId }
      }, function (resp) {
        if (resp && !resp.success) {
          console.warn('清空会话消息失败:', resp.error);
        }
      });
    }
    var msgs = messagesEl.querySelectorAll('.msg, .clip-card, .agent-stream');
    msgs.forEach(function (m) { m.remove(); });
    if (welcomeEl) welcomeEl.style.display = 'block';
    currentSessionId = null;
    updateSessionTitle('');
    streamingReferences = [];
    agentEventStream = [];
    agentContainer = null;
    agentExpandedIds = {};
    agentActiveThinkingIds = {};
    agentHasAnswer = false;
    agentTreeExpanded = false;
    agentRefsExpanded = false;
    agentAnswerText = '';
    agentAnswerDone = false;
    _fileFailedCache = {};
    chrome.storage.local.remove('ka_current_session');
  });

  // === 快捷问题 ===
  document.querySelectorAll('.quick-btn').forEach(function (btn) {
    btn.addEventListener('click', function () {
      sendMessage(btn.getAttribute('data-q'));
    });
  });

  // === 合并下拉菜单（知识库 + 模式 + 附件）===
  var kbBtn = $('sp-btn-kb');
  var kbMenu = $('sp-kb-menu');

  // 定位下拉菜单 — 使用 fixed 定位避免 overflow 裁剪
  function positionKbMenu() {
    var rect = kbBtn.getBoundingClientRect();
    requestAnimationFrame(function () {
      var menuH = kbMenu.offsetHeight;
      var top = rect.top - menuH - 6;
      if (top < 4) top = rect.bottom + 6;
      kbMenu.style.left = rect.left + 'px';
      kbMenu.style.top = top + 'px';
    });
  }

  kbBtn.addEventListener('click', function (e) {
    e.stopPropagation();
    modeMenu.classList.remove('show');
    if (kbMenu.classList.contains('show')) {
      kbMenu.classList.remove('show');
    } else {
      kbMenu.classList.add('show');
      positionKbMenu();
    }
  });

  // 知识库选项（静态选项的兜底，动态加载后会覆盖）
  kbMenu.querySelectorAll('.sp-dropdown-item').forEach(function (item) {
    item.addEventListener('click', function (e) {
      e.stopPropagation();
      var kb = item.getAttribute('data-kb');
      selectedKbId = kb;
      $('sp-kb-name').textContent = item.textContent.trim().substring(0, 4);
      kbMenu.querySelectorAll('.sp-dropdown-item').forEach(function (i) { i.classList.remove('selected'); });
      item.classList.add('selected');
      kbMenu.classList.remove('show');
    });
  });

  // 模式选项 — 静态选项的兜底，加载智能体后会被动态替换
  var modeBtn = $('sp-btn-mode');
  var modeMenu = $('sp-mode-menu');

  function positionModeMenu() {
    var rect = modeBtn.getBoundingClientRect();
    requestAnimationFrame(function () {
      var menuH = modeMenu.offsetHeight;
      var top = rect.top - menuH - 6;
      if (top < 4) top = rect.bottom + 6;
      modeMenu.style.left = rect.left + 'px';
      modeMenu.style.top = top + 'px';
    });
  }

  modeBtn.addEventListener('click', function (e) {
    e.stopPropagation();
    kbMenu.classList.remove('show');
    if (modeMenu.classList.contains('show')) {
      modeMenu.classList.remove('show');
    } else {
      modeMenu.classList.add('show');
      positionModeMenu();
    }
  });

  modeMenu.addEventListener('click', function (e) {
    e.stopPropagation();
  });

  // 点击其他地方关闭下拉
  document.addEventListener('click', function () {
    kbMenu.classList.remove('show');
    modeMenu.classList.remove('show');
  });

  // 菜单内部点击不冒泡关闭
  kbMenu.addEventListener('click', function (e) {
    e.stopPropagation();
  });

  // === 同步 popup 传来的智能体/知识库选择到 sidepanel 状态 ===
  function applyPendingPayload(payload) {
    if (payload.agentId) {
      selectedAgentId = payload.agentId;
      selectedAgentEnabled = !!payload.agentEnabled;
      selectedAgentImageUpload = !!payload.agentImageUpload;
      updateImageUploadUI();
      // 同步下拉 UI 选中状态
      var mItems = $('sp-mode-menu').querySelectorAll('.sp-mode-item');
      mItems.forEach(function (item) {
        var isMatch = item.getAttribute('data-agent-id') === payload.agentId;
        item.classList.toggle('selected', isMatch);
        if (isMatch) {
          $('sp-mode-name').textContent = item.textContent.trim();
        }
      });
    }
    if (payload.knowledgeBaseIds && payload.knowledgeBaseIds.length > 0) {
      selectedKbId = payload.knowledgeBaseIds[0];
    }
  }

  // === 监听来自 popup / content / background 的消息 ===
  chrome.runtime.onMessage.addListener(function (msg, sender, sendResponse) {
    if (msg.type === 'CHAT_QUERY' && msg.payload) {
      chrome.storage.local.remove('ka_pending_query');
      // 同步 popup 传来的智能体和知识库选择
      applyPendingPayload(msg.payload);
      sendMessage(msg.payload.query, msg.payload.images);
      sendResponse({ success: true });
    }
    if (msg.type === 'CLIP_RESULT' && msg.payload) {
      appendClipCard(msg.payload.title || '网页剪藏', msg.payload.content || '');
      sendResponse({ success: true });
    }
    if (msg.type === 'CHAT_STREAM_CHUNK' && msg.payload) {
      handleStreamChunk(msg.payload);
    }
    return true;
  });

  // === 初始化：检查是否有从 popup 传来的待处理问题 ===
  chrome.storage.local.get(['ka_pending_query', 'ka_current_session'], function (data) {
    // 恢复之前的会话 ID
    if (data.ka_current_session) {
      currentSessionId = data.ka_current_session;
    }

    if (data && data.ka_pending_query && data.ka_pending_query.query) {
      var pending = data.ka_pending_query;
      // 只处理 5 秒内的问题（防止旧数据）
      if (Date.now() - (pending.ts || 0) < 5000) {
        chrome.storage.local.remove('ka_pending_query');
        applyPendingPayload(pending);
        // 延迟一点让页面渲染完成
        setTimeout(function () {
          sendMessage(pending.query, pending.images);
        }, 200);
      } else {
        chrome.storage.local.remove('ka_pending_query');
      }
    }
  });

  // === 加载智能体列表（会自动链式加载知识库）===
  loadAgents();

  // 缓存 baseUrl 用于文件 URL 转换
  chrome.runtime.sendMessage({ type: 'GET_CONFIG' }, function (resp) {
    void chrome.runtime.lastError;
    if (resp && resp.success && resp.data && resp.data.baseUrl) {
      cachedBaseUrl = resp.data.baseUrl.replace(/\/+$/, '');
    }
  });

  // === 图片点击放大（灯箱）===
  messagesEl.addEventListener('click', function (e) {
    var img = e.target;
    if (img.tagName !== 'IMG') return;
    if (!img.classList.contains('md-img') && !img.classList.contains('msg-user-img')) return;
    var src = img.src;
    if (!src) return;
    var overlay = document.createElement('div');
    overlay.className = 'sp-lightbox';
    var bigImg = document.createElement('img');
    bigImg.src = src;
    overlay.appendChild(bigImg);
    document.body.appendChild(overlay);
    overlay.addEventListener('click', function () {
      overlay.remove();
    });
  });
})();
