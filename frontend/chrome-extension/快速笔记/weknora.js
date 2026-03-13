(function () {
  'use strict';

  // === DOM Helpers ===
  function $(sel) { return document.querySelector(sel); }
  function $$(sel) { return document.querySelectorAll(sel); }

  // === 状态 ===
  var knowledgeBases = [];
  var agents = [];
  var currentSessionId = null;
  var chatMessages = [];
  var wkSelectedAgentId = '';
  var wkSelectedAgentEnabled = false;
  var wkSelectedAgentImageUpload = false;

  // 图片上传状态
  var wkPendingImages = [];
  var WK_MAX_IMAGES = 5;
  var WK_ALLOWED_TYPES = ['image/jpeg', 'image/png', 'image/gif', 'image/webp'];
  var WK_MAX_IMAGE_SIZE = 10 * 1024 * 1024;

  // 流式消息状态
  var wkStreamingDiv = null;
  var wkStreamingText = '';
  var wkRequestId = '';
  var wkRenderPending = false;
  var wkStreamingReferences = [];
  var wkCachedBaseUrl = '';

  // Agent 事件流状态（参考 AgentStreamDisplay.vue）
  var wkAgentEvents = [];
  var wkAgentContainer = null;
  var wkAgentExpandedIds = {};
  var wkAgentActiveThinkingIds = {};
  var wkAgentHasAnswer = false;
  var wkAgentTreeExpanded = false;
  var wkAgentRefsExpanded = false;
  var wkAgentAnswerText = '';
  var wkAgentAnswerDone = false;

  // === Chrome 消息辅助 ===
  function sendMsg(data) {
    return new Promise(function (resolve) {
      if (chrome && chrome.runtime && chrome.runtime.sendMessage) {
        try {
          chrome.runtime.sendMessage(data, function (resp) {
            void chrome.runtime.lastError;
            resolve(resp || { success: false });
          });
        } catch (e) {
          resolve({ success: false, error: e.message });
        }
      } else {
        resolve({ success: false, error: 'no chrome api' });
      }
    });
  }

  // === 左侧导航切换 ===
  $$('.nav-item').forEach(function (item) {
    item.addEventListener('click', function () {
      $$('.nav-item').forEach(function (n) { n.classList.remove('active'); });
      item.classList.add('active');
    });
  });

  // === 内容区 tabs 切换 ===
  $$('.content-tab').forEach(function (tab) {
    tab.addEventListener('click', function () {
      $$('.content-tab').forEach(function (t) { t.classList.remove('active'); });
      tab.classList.add('active');
      loadKnowledgeBases();
    });
  });

  // === 加载智能体列表 ===
  function loadAgentList() {
    sendMsg({ type: 'LIST_AGENTS' }).then(function (resp) {
      if (resp && resp.success && resp.data) {
        agents = Array.isArray(resp.data) ? resp.data : (resp.data.data || []);
        // 恢复持久化的模式选择
        chrome.storage.local.get('ka_selected_agent', function (data) {
          if (data && data.ka_selected_agent && data.ka_selected_agent.agentId) {
            var saved = data.ka_selected_agent;
            // 验证保存的 agent 仍然存在
            for (var i = 0; i < agents.length; i++) {
              if (agents[i].id === saved.agentId) {
                wkSelectedAgentId = saved.agentId;
                wkSelectedAgentEnabled = !!saved.agentEnabled;
                break;
              }
            }
          }
          if (agents.length > 0 && !wkSelectedAgentId) {
            wkSelectedAgentId = agents[0].id;
            var isQA = agents[0].id === 'builtin-quick-answer' || (agents[0].config && agents[0].config.agent_mode === 'quick-answer');
            wkSelectedAgentEnabled = !isQA;
          }
          renderChatToolbar();
          // 更新图片上传按钮状态
          var selAgent = agents.find(function (a) { return a.id === wkSelectedAgentId; });
          wkSelectedAgentImageUpload = !!(selAgent && selAgent.config && selAgent.config.image_upload_enabled);
          wkUpdateImageUploadUI();
        });
      }
    });
  }

  // === 加载知识库列表 ===
  function loadKnowledgeBases() {
    sendMsg({ type: 'LIST_KNOWLEDGE_BASES' }).then(function (resp) {
      if (resp && resp.success && resp.data) {
        knowledgeBases = Array.isArray(resp.data) ? resp.data : (resp.data.items || []);
        renderKnowledgeBases();
      } else if (resp && resp.error) {
        showEmptyState('加载失败: ' + resp.error);
      }
    });
  }

  function renderKnowledgeBases() {
    var body = $('.content-body');
    if (!body) return;

    if (knowledgeBases.length === 0) {
      showEmptyState();
      return;
    }

    // 构建知识库卡片网格
    body.innerHTML = '<div class="kb-grid"></div>' +
      '<button class="fab-btn" id="fab-create"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="16"/><line x1="8" y1="12" x2="16" y2="12"/></svg></button>';
    body.style.alignItems = 'stretch';
    body.style.justifyContent = 'flex-start';
    body.style.padding = '20px 24px';

    var grid = body.querySelector('.kb-grid');
    grid.style.cssText = 'display:grid;grid-template-columns:repeat(auto-fill,minmax(200px,1fr));gap:16px;width:100%;';

    knowledgeBases.forEach(function (kb) {
      var card = document.createElement('div');
      card.style.cssText = 'background:#fff;border:1px solid #eaeaea;border-radius:12px;padding:16px;cursor:pointer;transition:all 0.2s;';
      card.innerHTML = '<div style="font-size:15px;font-weight:600;margin-bottom:6px;">' + escapeHtml(kb.name) + '</div>' +
        '<div style="font-size:12px;color:#999;margin-bottom:10px;">' + escapeHtml(kb.description || '暂无描述') + '</div>' +
        '<div style="display:flex;gap:12px;font-size:11px;color:#bbb;">' +
        '<span>' + (kb.document_count || 0) + ' 文档</span>' +
        '<span>' + (kb.chunk_count || 0) + ' 分段</span>' +
        '</div>';
      card.addEventListener('mouseenter', function () {
        card.style.borderColor = '#07C160';
        card.style.boxShadow = '0 4px 16px rgba(7,193,96,0.12)';
      });
      card.addEventListener('mouseleave', function () {
        card.style.borderColor = '#eaeaea';
        card.style.boxShadow = 'none';
      });
      grid.appendChild(card);
    });

    // FAB 按钮
    var fab = body.querySelector('#fab-create');
    if (fab) {
      fab.addEventListener('click', function () {
        showCreateKbDialog();
      });
    }
  }

  function showEmptyState(msg) {
    var body = $('.content-body');
    if (!body) return;
    body.style.alignItems = '';
    body.style.justifyContent = '';
    body.style.padding = '';
    body.innerHTML = '<div class="empty-illustration"><svg width="140" height="100" viewBox="0 0 140 100">' +
      '<circle cx="50" cy="8" r="5" fill="#c8e6c9"/><circle cx="70" cy="4" r="4" fill="#e0e0e0"/><circle cx="90" cy="10" r="6" fill="#a5d6a7"/>' +
      '<path d="M55 20 L50 30 M70 18 L70 30 M85 22 L82 30" stroke="#07C160" stroke-width="2" fill="none" stroke-linecap="round"/>' +
      '<polygon points="47,30 53,30 50,36" fill="#07C160"/><polygon points="67,30 73,30 70,36" fill="#07C160"/><polygon points="79,30 85,30 82,36" fill="#07C160"/>' +
      '<rect x="15" y="45" width="110" height="50" rx="8" fill="url(#boxGrad)"/>' +
      '<defs><linearGradient id="boxGrad" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stop-color="#e8f8ee"/><stop offset="100%" stop-color="#d0f0db"/></linearGradient></defs>' +
      '<circle cx="50" cy="72" r="5" fill="#2b6bff"/><circle cx="65" cy="72" r="5" fill="#2b6bff"/><circle cx="80" cy="72" r="5" fill="#e0e0e0"/></svg></div>' +
      '<div class="empty-title">' + (msg || '暂无知识库') + '</div>' +
      '<div class="empty-desc">点击下方按钮创建第一个知识库</div>' +
      '<button class="empty-btn" id="btn-create-kb"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 12v8a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-8"/><polyline points="16 6 12 2 8 6"/><line x1="12" y1="2" x2="12" y2="15"/></svg> 新建知识库</button>' +
      '<button class="fab-btn" id="fab-create2"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="16"/><line x1="8" y1="12" x2="16" y2="12"/></svg></button>';

    var createBtn = body.querySelector('#btn-create-kb');
    if (createBtn) createBtn.addEventListener('click', showCreateKbDialog);
    var fab = body.querySelector('#fab-create2');
    if (fab) fab.addEventListener('click', showCreateKbDialog);
  }

  function showCreateKbDialog() {
    var name = prompt('请输入知识库名称:');
    if (!name || !name.trim()) return;
    var desc = prompt('请输入知识库描述（可选）:') || '';

    sendMsg({
      type: 'CREATE_KNOWLEDGE_BASE',
      payload: { name: name.trim(), description: desc.trim() }
    }).then(function (resp) {
      if (resp && resp.success) {
        loadKnowledgeBases();
      } else {
        alert('创建失败: ' + (resp.error || '未知错误'));
      }
    });
  }

  function escapeHtml(s) {
    var d = document.createElement('div');
    d.textContent = s || '';
    return d.innerHTML;
  }

  // === 右侧对话面板 - 输入交互 ===
  var chatTextarea = $('.chat-input-card textarea');
  var chatBody = $('.chat-panel-body');
  var wkImageInput = $('#wk-image-input');
  var wkImageBtn = $('#wk-image-btn');
  var wkImagePreviews = $('#wk-image-previews');

  // === 图片上传 ===
  function wkShowToast(msg) {
    var toast = document.createElement('div');
    toast.textContent = msg;
    toast.style.cssText = 'position:fixed;top:60px;left:50%;transform:translateX(-50%);background:#07C160;color:#fff;padding:6px 18px;border-radius:20px;font-size:12px;z-index:9999;opacity:1;transition:opacity 0.3s;';
    document.body.appendChild(toast);
    setTimeout(function () { toast.style.opacity = '0'; setTimeout(function () { toast.remove(); }, 300); }, 1500);
  }

  function wkAddImageFiles(files) {
    if (!wkSelectedAgentImageUpload) return;
    for (var i = 0; i < files.length; i++) {
      if (wkPendingImages.length >= WK_MAX_IMAGES) {
        wkShowToast('最多上传 ' + WK_MAX_IMAGES + ' 张图片');
        break;
      }
      var f = files[i];
      if (WK_ALLOWED_TYPES.indexOf(f.type) === -1) {
        wkShowToast('仅支持 JPG/PNG/GIF/WebP 格式');
        continue;
      }
      if (f.size > WK_MAX_IMAGE_SIZE) {
        wkShowToast('图片大小不能超过 10MB');
        continue;
      }
      wkPendingImages.push({ file: f, preview: URL.createObjectURL(f) });
    }
    wkRenderImagePreviews();
  }

  function wkRemoveImage(index) {
    if (index >= 0 && index < wkPendingImages.length) {
      URL.revokeObjectURL(wkPendingImages[index].preview);
      wkPendingImages.splice(index, 1);
    }
    wkRenderImagePreviews();
  }

  function wkClearImages(skipRevoke) {
    if (!skipRevoke) {
      for (var i = 0; i < wkPendingImages.length; i++) {
        URL.revokeObjectURL(wkPendingImages[i].preview);
      }
    }
    wkPendingImages = [];
    wkRenderImagePreviews();
  }

  function wkRenderImagePreviews() {
    if (!wkImagePreviews) return;
    if (wkPendingImages.length === 0) {
      wkImagePreviews.style.display = 'none';
      wkImagePreviews.innerHTML = '';
      return;
    }
    wkImagePreviews.style.display = 'flex';
    var html = '';
    for (var i = 0; i < wkPendingImages.length; i++) {
      html += '<div class="wk-img-item" data-idx="' + i + '">'
        + '<img class="wk-img-thumb" src="' + wkPendingImages[i].preview + '">'
        + '<span class="wk-img-remove">&times;</span>'
        + '</div>';
    }
    wkImagePreviews.innerHTML = html;
    wkImagePreviews.querySelectorAll('.wk-img-remove').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var idx = parseInt(btn.parentElement.getAttribute('data-idx'));
        wkRemoveImage(idx);
      });
    });
  }

  function wkUpdateImageUploadUI() {
    if (wkImageBtn) wkImageBtn.style.display = wkSelectedAgentImageUpload ? '' : 'none';
    if (!wkSelectedAgentImageUpload && wkPendingImages.length > 0) wkClearImages();
  }

  function wkFileToBase64(file) {
    return new Promise(function (resolve, reject) {
      var reader = new FileReader();
      reader.onload = function () { resolve(reader.result); };
      reader.onerror = reject;
      reader.readAsDataURL(file);
    });
  }

  if (wkImageInput) {
    wkImageInput.addEventListener('change', function () {
      if (wkImageInput.files) wkAddImageFiles(Array.from(wkImageInput.files));
      wkImageInput.value = '';
    });
  }
  if (wkImageBtn) {
    wkImageBtn.addEventListener('click', function () {
      if (wkImageInput) wkImageInput.click();
    });
  }

  // 粘贴图片
  if (chatTextarea) {
    chatTextarea.addEventListener('paste', function (e) {
      if (!wkSelectedAgentImageUpload) return;
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
        wkAddImageFiles(imageFiles);
      }
    });
  }

  if (chatTextarea) {
    chatTextarea.addEventListener('input', function () {
      this.style.height = 'auto';
      this.style.height = Math.min(this.scrollHeight, 80) + 'px';
    });

    chatTextarea.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        var text = chatTextarea.value.trim();
        if (text) {
          sendChatMessage(text);
          chatTextarea.value = '';
          chatTextarea.style.height = 'auto';
        }
      }
    });
  }

  // === 流式消息状态 (weknora 页面) — 已移至顶部状态区 ===

  function sendChatMessage(text) {
    if (!chatBody) return;

    // 首次发送时替换欢迎内容
    if (chatBody.querySelector('h3')) {
      chatBody.innerHTML = '';
      chatBody.style.justifyContent = 'flex-start';
      chatBody.style.alignItems = 'stretch';
      chatBody.style.padding = '16px';
      chatBody.style.overflowY = 'auto';
    }

    // 捕获图片预览 URL（清除前）
    var msgImageUrls = wkPendingImages.map(function (img) { return img.preview; });

    // 显示用户消息（含图片）
    appendChatMsg('user', text, msgImageUrls.length > 0 ? msgImageUrls : null);

    // 显示 loading
    var loadingDiv = document.createElement('div');
    loadingDiv.className = 'chat-msg-bot wk-loading';
    loadingDiv.style.cssText = 'background:#f7f8fa;padding:10px 14px;border-radius:12px;margin-bottom:8px;font-size:13px;color:#999;';
    loadingDiv.textContent = '思考中…';
    chatBody.appendChild(loadingDiv);
    chatBody.scrollTop = chatBody.scrollHeight;

    wkStreamingDiv = null;
    wkStreamingText = '';
    wkStreamingReferences = [];
    wkRenderPending = false;
    wkAgentEvents = [];
    wkAgentContainer = null;
    wkAgentExpandedIds = {};
    wkAgentActiveThinkingIds = {};
    wkAgentHasAnswer = false;
    wkAgentTreeExpanded = false;
    wkAgentRefsExpanded = false;
    wkAgentAnswerText = '';
    wkAgentAnswerDone = false;
    _wkFileFailedCache = {};
    wkRequestId = Date.now().toString(36) + Math.random().toString(36).slice(2, 6);

    var chatPayload = {
      query: text,
      sessionId: currentSessionId,
      agentId: wkSelectedAgentId,
      agentEnabled: wkSelectedAgentEnabled,
      _requestId: wkRequestId
    };

    function doWkSend() {
      sendMsg({ type: 'CHAT_QUERY', payload: chatPayload }).then(function (resp) {
      loadingDiv.remove();
      if (resp && resp.success) {
        if (resp.sessionId) currentSessionId = resp.sessionId;
        if (wkAgentContainer) {
          wkAgentAnswerDone = true;
          wkRenderAgentStream();
        } else if (wkStreamingDiv && wkStreamingText) {
          wkStreamingDiv.innerHTML = renderMarkdown(wkStreamingText);
          wkHydrateFileImages(wkStreamingDiv);
          chatBody.scrollTop = chatBody.scrollHeight;
        } else if (resp.data && !wkStreamingDiv && !wkAgentContainer) {
          appendChatMsg('bot', resp.data);
        }
        wkStreamingDiv = null;
        wkStreamingText = '';
      } else {
        if (wkAgentContainer) { wkAgentContainer.remove(); wkAgentContainer = null; }
        appendChatMsg('bot', '请求失败: ' + (resp.error || '请检查服务配置'));
      }
    });
    }

    // 如果有待上传图片，先转 base64 再发送
    if (wkPendingImages.length > 0) {
      var promises = wkPendingImages.map(function (img) { return wkFileToBase64(img.file); });
      Promise.all(promises).then(function (dataURIs) {
        chatPayload.images = dataURIs.map(function (d) { return { data: d }; });
        wkClearImages(true);
        doWkSend();
      }).catch(function () {
        wkShowToast('图片读取失败');
        loadingDiv.remove();
      });
    } else {
      doWkSend();
    }
  }

  // 移除 loading 指示器
  function removeWkLoading() {
    if (!chatBody) return;
    var loadings = chatBody.querySelectorAll('.wk-loading');
    loadings.forEach(function (el) { el.remove(); });
  }

  // === 工具名称/图标映射 ===
  var WK_TOOL_NAMES = {
    knowledge_search: '语义搜索', search_knowledge: '语义搜索',
    grep_chunks: '文本搜索', list_knowledge_chunks: '阅读文档',
    get_document_info: '获取文档信息', get_document_content: '获取文档内容',
    web_search: '网络搜索', web_fetch: '网页抓取',
    todo_write: '制定计划', final_answer: '生成回答',
    thinking: '思考', image_analysis: '查看图片内容',
    data_analysis: '数据分析', database_query: '数据库查询'
  };
  var WK_TOOL_ICONS = {
    '语义搜索': '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>',
    '文本搜索': '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/></svg>',
    '阅读文档': '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/></svg>',
    '网络搜索': '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>',
    '网页抓取': '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>',
    '制定计划': '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 11l3 3L22 4"/><path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11"/></svg>',
    '思考': '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>'
  };
  var WK_ICON_AGENT = '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg>';
  var WK_ICON_CHEVRON = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"/></svg>';
  var WK_DEFAULT_ICON = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/></svg>';
  var WK_ICON_COPY = '<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';

  function wkGetToolName(raw) { return WK_TOOL_NAMES[raw] || raw; }
  function wkGetToolIcon(name) { return WK_TOOL_ICONS[name] || WK_DEFAULT_ICON; }
  function wkGetThinkingSummary(content) {
    if (!content) return '';
    var c = content.replace(/^#+\s+/gm, '').replace(/\*\*/g, '').replace(/`/g, '').replace(/\n+/g, ' ').trim();
    return c.length <= 50 ? c : c.slice(0, 50) + '…';
  }
  function wkExtractSummary(ev) {
    if (!ev.output) return '';
    try {
      var p = JSON.parse(ev.output);
      if (p.results && Array.isArray(p.results)) return '找到 ' + p.results.length + ' 条结果';
      if (p.total_matches !== undefined) return '匹配 ' + p.total_matches + ' 条';
    } catch (e) {}
    var s = ev.output.replace(/\n/g, ' ').trim();
    return s.length > 60 ? s.substring(0, 60) + '…' : s;
  }

  function wkCleanThinking(text) {
    if (!text) return '';
    return text.replace(/Calling tool:\s*\w*/g, '').replace(/\n{3,}/g, '\n\n').trim();
  }
  function wkResolveAllPending() {
    for (var k = 0; k < wkAgentEvents.length; k++) {
      var e = wkAgentEvents[k];
      if (e._type === 'tool_call' && e.pending) { e.pending = false; e.success = true; e.output = e.output || ''; }
    }
  }
  function wkResolveOldPending() {
    var found = false;
    for (var k = wkAgentEvents.length - 1; k >= 0; k--) {
      var e = wkAgentEvents[k];
      if (e._type === 'tool_call' && e.pending) {
        if (!found) { found = true; continue; }
        e.pending = false; e.success = true; e.output = e.output || '';
      }
    }
  }
  function wkCollapseThinking() {
    for (var k in wkAgentActiveThinkingIds) { delete wkAgentExpandedIds[k]; }
    wkAgentActiveThinkingIds = {};
    wkAgentContainer && (wkAgentContainer._stepsChanged = true);
  }

  function wkCollapseAllExpanded() {
    wkAgentExpandedIds = {};
    for (var k in wkAgentActiveThinkingIds) { delete wkAgentActiveThinkingIds[k]; }
    wkAgentContainer && (wkAgentContainer._stepsChanged = true);
  }

  // 监听流式推送
  if (chrome && chrome.runtime && chrome.runtime.onMessage) {
    chrome.runtime.onMessage.addListener(function (msg) {
      if (msg.type === 'CHAT_STREAM_CHUNK' && msg.payload) {
        var payload = msg.payload;
        if (payload.requestId && payload.requestId !== wkRequestId) return;

        if (payload.responseType === 'answer' && chatBody) {
          wkAgentAnswerText += payload.content || '';
          if (payload.done) wkAgentAnswerDone = true;
          if (!wkAgentHasAnswer) {
            wkAgentHasAnswer = true;
            removeWkLoading();
            wkCollapseAllExpanded();
            wkResolveAllPending();
            wkAgentContainer && (wkAgentContainer._stepsChanged = true);
          }
          wkScheduleRender();

        } else if ((payload.responseType === 'thinking' || (payload.responseType === 'tool_call' && payload.toolName === 'thinking')) && chatBody) {
          var thinkContent = wkCleanThinking(payload.content);
          if (!thinkContent || /^Thought process recorded/i.test(thinkContent)) return;
          var lastEv = wkAgentEvents.length > 0 ? wkAgentEvents[wkAgentEvents.length - 1] : null;
          if (lastEv && lastEv._type === 'thinking') {
            lastEv.content += thinkContent;
          } else {
            wkCollapseAllExpanded();
            var tid = 'wkt-' + Date.now();
            wkAgentEvents.push({ _type: 'thinking', id: tid, content: thinkContent });
            wkAgentActiveThinkingIds[tid] = true;
            wkAgentContainer && (wkAgentContainer._stepsChanged = true);
          }
          wkScheduleRender();

        } else if (payload.responseType === 'tool_call' && chatBody) {
          var existingWkTc = null;
          if (payload.eventId) {
            for (var wei = wkAgentEvents.length - 1; wei >= 0; wei--) {
              if (wkAgentEvents[wei]._type === 'tool_call' && wkAgentEvents[wei].eventId === payload.eventId) {
                existingWkTc = wkAgentEvents[wei]; break;
              }
            }
          }
          if (existingWkTc) {
            if (payload.content) existingWkTc.content = payload.content;
          } else {
            wkCollapseThinking();
            wkCollapseAllExpanded();
            wkResolveOldPending();
            var tcId = payload.eventId || ('wktc-' + Date.now() + '-' + Math.random().toString(36).slice(2, 5));
            wkAgentEvents.push({ _type: 'tool_call', id: tcId, eventId: payload.eventId || '', tool_name: payload.toolName || 'tool', pending: true, success: null, content: payload.content || '', output: '' });
            wkAgentContainer && (wkAgentContainer._stepsChanged = true);
          }
          wkScheduleRender();

        } else if (payload.responseType === 'tool_result' && chatBody) {
          if (payload.toolName === 'thinking') {
            var thinkRes = wkCleanThinking(payload.content);
            if (thinkRes && !/^Thought process recorded/i.test(thinkRes)) {
              var lastThink = wkAgentEvents.length > 0 ? wkAgentEvents[wkAgentEvents.length - 1] : null;
              if (lastThink && lastThink._type === 'thinking') lastThink.content += thinkRes;
            }
            wkScheduleRender();
          } else {
            var wkMatched = false;
            for (var ri = wkAgentEvents.length - 1; ri >= 0; ri--) {
              var rev = wkAgentEvents[ri];
              if (rev._type !== 'tool_call' || !rev.pending) continue;
              var idMatch = payload.eventId && rev.eventId && rev.eventId === payload.eventId;
              var nameMatch = payload.toolName && rev.tool_name === payload.toolName;
              if (idMatch || nameMatch || !wkMatched) {
                rev.pending = false; rev.success = payload.success !== false; rev.output = payload.content || '';
                wkMatched = true; break;
              }
            }
            wkAgentContainer && (wkAgentContainer._stepsChanged = true);
            wkScheduleRender();
          }

        } else if (payload.responseType === 'references' && chatBody) {
          var refs = payload.references;
          if (Array.isArray(refs) && refs.length > 0) {
            wkStreamingReferences = refs;
            wkScheduleRender();
          }

        } else if (payload.responseType === 'complete' && chatBody) {
          removeWkLoading();
          wkCollapseAllExpanded();
          wkResolveAllPending();
          wkAgentAnswerDone = true;
          wkAgentContainer && (wkAgentContainer._stepsChanged = true);
          wkScheduleRender();

        } else if (payload.responseType === 'error' && chatBody) {
          removeWkLoading();
          if (wkAgentContainer) { wkAgentContainer.remove(); wkAgentContainer = null; }
          if (wkStreamingDiv) { wkStreamingDiv.remove(); wkStreamingDiv = null; wkStreamingText = ''; }
          appendChatMsg('bot', payload.content || '请求出错');
        }
      }
    });
  }

  var _wkRenderTimer = null;
  var _wkLastRenderTime = 0;
  var WK_RENDER_THROTTLE = 60;
  function wkScheduleRender() {
    var now = Date.now();
    var elapsed = now - _wkLastRenderTime;
    if (elapsed >= WK_RENDER_THROTTLE) {
      if (_wkRenderTimer) { clearTimeout(_wkRenderTimer); _wkRenderTimer = null; }
      _wkLastRenderTime = now;
      wkRenderAgentStream();
    } else if (!_wkRenderTimer) {
      _wkRenderTimer = setTimeout(function () {
        _wkRenderTimer = null;
        _wkLastRenderTime = Date.now();
        wkRenderAgentStream();
      }, WK_RENDER_THROTTLE - elapsed);
    }
  }

  // === Agent 流式渲染 ===
  function wkRenderAgentStream() {
    if (wkAgentEvents.length === 0 && !wkAgentAnswerText) return;
    if (!chatBody) return;

    if (!wkAgentContainer) {
      wkAgentContainer = document.createElement('div');
      wkAgentContainer.className = 'agent-stream';
      chatBody.appendChild(wkAgentContainer);
      wkAgentContainer._stepsEl = document.createElement('div');
      wkAgentContainer.appendChild(wkAgentContainer._stepsEl);
    }

    var changed = false;

    // --- 1. 渲染中间步骤（仅当事件变化时更新）---
    if (!wkAgentContainer._lastStepCount || wkAgentContainer._lastStepCount !== wkAgentEvents.length ||
        wkAgentContainer._stepsChanged || (wkAgentHasAnswer && !wkAgentContainer._treeMode)) {
      wkRenderAgentSteps();
      if (wkAgentHasAnswer) wkAgentContainer._treeMode = true;
      changed = true;
    } else {
      var lastEvWk = wkAgentEvents.length > 0 ? wkAgentEvents[wkAgentEvents.length - 1] : null;
      if (lastEvWk && lastEvWk._type === 'thinking' && wkAgentActiveThinkingIds[lastEvWk.id]) {
        wkRenderAgentSteps();
        changed = true;
      }
    }
    // --- 2. 增量渲染回答内容（有图片，必须保持 DOM 稳定）---
    if (wkRenderAgentAnswer()) changed = true;

    if (changed) chatBody.scrollTop = chatBody.scrollHeight;
  }

  function wkRenderAgentSteps() {
    var stepsEl = wkAgentContainer._stepsEl;
    var events = wkAgentEvents;
    var lastEv = events.length > 0 ? events[events.length - 1] : null;

    if (lastEv && lastEv._type === 'thinking' &&
        wkAgentContainer._lastStepCount === events.length &&
        !wkAgentContainer._stepsChanged && !wkAgentHasAnswer) {
      var activeCard = stepsEl.querySelector('.agent-card[data-eid="' + lastEv.id + '"]');
      if (activeCard) {
        var summaryEl = activeCard.querySelector('.agent-card-summary');
        if (summaryEl) summaryEl.textContent = wkGetThinkingSummary(lastEv.content);
        var detailEl = activeCard.querySelector('.agent-card-detail');
        if (detailEl) {
          detailEl.textContent = lastEv.content;
          detailEl.scrollTop = detailEl.scrollHeight;
        }
        return;
      }
    }

    wkAgentContainer._lastStepCount = events.length;
    wkAgentContainer._stepsChanged = false;

    stepsEl.querySelectorAll('.agent-card.expanded').forEach(function (el) {
      var eid = el.getAttribute('data-eid');
      if (eid) wkAgentExpandedIds[eid] = true;
    });
    if (stepsEl.querySelector('.agent-tree-root.expanded')) wkAgentTreeExpanded = true;

    var intermediateEvents = events.filter(function (e) { return e._type !== 'error'; });
    var stepCount = intermediateEvents.length;
    var html = '';

    if (wkAgentHasAnswer && stepCount > 0) {
      html += '<div class="agent-tree-root' + (wkAgentTreeExpanded ? ' expanded' : '') + '">';
      html += '<span class="agent-tree-root-title">' + WK_ICON_AGENT + '<span>智能体执行 ' + stepCount + ' 步</span></span>';
      html += '<span class="agent-tree-root-toggle">' + WK_ICON_CHEVRON + '</span>';
      html += '</div>';
      html += '<div class="agent-tree-children">';
      for (var ti = 0; ti < intermediateEvents.length; ti++) {
        var isLast = ti === intermediateEvents.length - 1;
        html += '<div class="agent-tree-child' + (isLast ? ' last' : '') + '">';
        html += '<div class="agent-tree-branch"></div>';
        html += wkBuildCardHTML(intermediateEvents[ti]);
        html += '</div>';
      }
      html += '</div>';
    } else if (!wkAgentHasAnswer) {
      for (var fi = 0; fi < events.length; fi++) {
        html += wkBuildCardHTML(events[fi]);
      }
    }

    stepsEl.innerHTML = html;
    wkBindStepEvents(stepsEl);

    for (var aid in wkAgentActiveThinkingIds) {
      var thinkCard = stepsEl.querySelector('.agent-card[data-eid="' + aid + '"]');
      if (thinkCard) {
        var det = thinkCard.querySelector('.agent-card-detail');
        if (det) det.scrollTop = det.scrollHeight;
      }
    }
  }

  function wkBindStepEvents(stepsEl) {
    var treeRoot = stepsEl.querySelector('.agent-tree-root');
    if (treeRoot) {
      treeRoot.addEventListener('click', function () {
        wkAgentTreeExpanded = !wkAgentTreeExpanded;
        treeRoot.classList.toggle('expanded');
        var children = treeRoot.nextElementSibling;
        if (children && children.classList.contains('agent-tree-children')) {
          children.style.display = wkAgentTreeExpanded ? 'block' : 'none';
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
          if (card.classList.contains('expanded')) wkAgentExpandedIds[eid] = true;
          else delete wkAgentExpandedIds[eid];
        }
      });
    });
  }

  var WK_ANSWER_RENDER_INTERVAL = 20;
  function wkRenderAgentAnswer() {
    if (!wkAgentAnswerText) return false;

    if (!wkAgentContainer._answerWrapper) {
      var wrapper = document.createElement('div');
      wrapper.className = 'agent-answer';
      var content = document.createElement('div');
      content.className = 'agent-answer-content';
      wrapper.appendChild(content);
      wkAgentContainer.appendChild(wrapper);
      wkAgentContainer._answerWrapper = wrapper;
      wkAgentContainer._answerContent = content;
      wkAgentContainer._lastAnswerLen = 0;
      wkAgentContainer._lastAnswerRenderTime = 0;
      wkAgentContainer._answerTimer = null;
    }

    var newLen = wkAgentAnswerText.length;
    if (newLen === (wkAgentContainer._lastAnswerLen || 0)) return false;

    var now = Date.now();
    var timeSince = now - (wkAgentContainer._lastAnswerRenderTime || 0);

    if (wkAgentAnswerDone || timeSince >= WK_ANSWER_RENDER_INTERVAL) {
      if (wkAgentContainer._answerTimer) { clearTimeout(wkAgentContainer._answerTimer); wkAgentContainer._answerTimer = null; }
      wkAgentContainer._lastAnswerLen = newLen;
      wkAgentContainer._lastAnswerRenderTime = now;
      wkAgentContainer._answerContent.innerHTML = renderMarkdown(wkAgentAnswerText);
      wkHydrateFileImages(wkAgentContainer._answerContent);
      return true;
    }
    if (!wkAgentContainer._answerTimer) {
      wkAgentContainer._answerTimer = setTimeout(function () {
        wkAgentContainer._answerTimer = null;
        if (wkRenderAgentAnswer()) chatBody.scrollTop = chatBody.scrollHeight;
      }, WK_ANSWER_RENDER_INTERVAL - timeSince);
    }
    return false;

    if (wkAgentAnswerDone && !wkAgentContainer._toolbarEl) {
      var toolbar = document.createElement('div');
      toolbar.className = 'agent-answer-toolbar';
      toolbar.innerHTML = '<button class="wk-btn-copy" title="复制">' + WK_ICON_COPY + '</button>';
      toolbar.querySelector('.wk-btn-copy').addEventListener('click', function () {
        if (navigator.clipboard && navigator.clipboard.writeText) {
          navigator.clipboard.writeText(wkAgentAnswerText || '');
        } else {
          var ta = document.createElement('textarea'); ta.value = wkAgentAnswerText || '';
          ta.style.cssText = 'position:fixed;left:-9999px;'; document.body.appendChild(ta);
          ta.select(); try { document.execCommand('copy'); } catch (e) {} document.body.removeChild(ta);
        }
      });
      wkAgentContainer._answerWrapper.appendChild(toolbar);
      wkAgentContainer._toolbarEl = toolbar;
    }
  }

  function wkBuildCardHTML(ev) {
    var isActive = !!wkAgentActiveThinkingIds[ev.id];
    var isExpanded = isActive || !!wkAgentExpandedIds[ev.id];
    var cls = 'agent-card';
    if (isExpanded) cls += ' expanded';
    if (ev.pending) cls += ' pending';
    if (ev.success === false) cls += ' error';
    var html = '<div class="' + cls + '" data-eid="' + ev.id + '">';

    if (ev._type === 'thinking') {
      var summary = wkGetThinkingSummary(ev.content);
      html += '<div class="agent-card-header"><div class="agent-card-title">' + wkGetToolIcon('思考') + '<span class="agent-card-name">思考</span>';
      if (!isExpanded && summary) html += '<span class="agent-card-summary">' + escapeHtml(summary) + '</span>';
      if (isActive) html += '<span class="agent-spinner"></span>';
      html += '</div>';
      if (ev.content) html += '<span class="agent-card-toggle">' + WK_ICON_CHEVRON + '</span>';
      html += '</div>';
      if (ev.content) html += '<div class="agent-card-detail">' + escapeHtml(ev.content) + '</div>';

    } else if (ev._type === 'tool_call') {
      var displayName = wkGetToolName(ev.tool_name);
      var icon = wkGetToolIcon(displayName);
      if (ev.pending) {
        html += '<div class="agent-card-header no-expand"><div class="agent-card-title">' + icon + '<span class="agent-card-name">正在调用 ' + escapeHtml(displayName) + '…</span></div><span class="agent-spinner"></span></div>';
      } else {
        var toolSummary = wkExtractSummary(ev);
        var hasDetail = !!ev.output;
        html += '<div class="agent-card-header' + (hasDetail ? '' : ' no-expand') + '"><div class="agent-card-title">' + icon + '<span class="agent-card-name">' + escapeHtml(displayName) + '</span>';
        if (!isExpanded && toolSummary) html += '<span class="agent-card-summary">' + escapeHtml(toolSummary) + '</span>';
        html += '</div>';
        if (hasDetail) html += '<span class="agent-card-toggle">' + WK_ICON_CHEVRON + '</span>';
        html += '</div>';
        if (toolSummary && !isExpanded) html += '<div class="agent-card-info">' + escapeHtml(toolSummary) + '</div>';
        if (hasDetail) html += '<div class="agent-card-detail">' + escapeHtml(ev.output).substring(0, 600) + '</div>';
      }
    }
    html += '</div>';
    return html;
  }

  // wkBuildRefsHTML 和 wkBindAgentEvents 已拆解到 wkRenderAgentSteps / wkRenderAgentAnswer 中

  function appendChatMsg(role, text, imageUrls) {
    if (!chatBody) return;
    var div = document.createElement('div');
    if (role === 'user') {
      div.style.cssText = 'background:#07C160;color:#fff;padding:10px 14px;border-radius:12px;border-bottom-right-radius:4px;margin-bottom:8px;font-size:13px;align-self:flex-end;max-width:80%;word-break:break-word;';
    } else {
      div.style.cssText = 'background:#f7f8fa;padding:10px 14px;border-radius:12px;border-bottom-left-radius:4px;margin-bottom:8px;font-size:13px;color:#333;max-width:85%;word-break:break-word;line-height:1.6;';
    }
    if (role === 'bot') {
      div.innerHTML = renderMarkdown(text);
      wkHydrateFileImages(div);
    } else {
      div.textContent = text;
    }
    // 在用户消息中显示上传的图片
    if (role === 'user' && imageUrls && imageUrls.length > 0) {
      var imgWrap = document.createElement('div');
      imgWrap.style.cssText = 'display:flex;gap:6px;margin-top:6px;flex-wrap:wrap;';
      imageUrls.forEach(function (url) {
        var img = document.createElement('img');
        img.src = url;
        img.style.cssText = 'width:60px;height:60px;border-radius:8px;object-fit:cover;border:1px solid rgba(255,255,255,0.3);';
        imgWrap.appendChild(img);
      });
      div.appendChild(imgWrap);
    }
    chatBody.appendChild(div);
    chatBody.scrollTop = chatBody.scrollHeight;
  }

  // === 文件 URL 转换 ===
  var _wkFileBlobCache = {};
  var _wkFileFailedCache = {};

  function wkHydrateFileImages(container) {
    if (!container) return;
    var imgs = container.querySelectorAll('img[data-file-src]');
    if (!imgs.length) return;
    imgs.forEach(function (img) {
      var fileSrc = img.getAttribute('data-file-src');
      if (!fileSrc) return;
      if (_wkFileFailedCache[fileSrc]) {
        img.removeAttribute('src');
        img.alt = '[图片加载失败]';
        img.style.display = 'none';
        return;
      }
      if (_wkFileBlobCache[fileSrc]) {
        img.src = _wkFileBlobCache[fileSrc];
        return;
      }
      if (img.getAttribute('data-hydrated') === '1') return;
      img.setAttribute('data-hydrated', '1');
      chrome.runtime.sendMessage({ type: 'FETCH_FILE', payload: { filePath: fileSrc } }, function (resp) {
        void chrome.runtime.lastError;
        if (resp && resp.success && resp.dataUrl) {
          _wkFileBlobCache[fileSrc] = resp.dataUrl;
          img.src = resp.dataUrl;
          img.style.display = '';
        } else {
          _wkFileFailedCache[fileSrc] = true;
          img.alt = '[图片加载失败]';
          img.style.display = 'none';
        }
      });
    });
  }

  // === Markdown 渲染（参考 botmsg.vue / AgentStreamDisplay.vue）===
  var WK_MD_MAX_LEN = 200000;
  function renderMarkdown(text) {
    if (!text) return '';
    if (text.length > WK_MD_MAX_LEN) text = text.substring(0, WK_MD_MAX_LEN) + '\n\n…（内容过长，已截断）';

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
        if (/^\|[\s\-:|]+\|$/.test(row)) return;
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
        html += '<div style="overflow-x:auto;margin:6px 0;">' + tables[parseInt(tbMatch[1])] + '</div>';
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
        html += '<p class="md-p">' + escapeHtml(pLines.slice(0, 20).join('\n')) + '…</p>';
      }
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
          var subStart = j;
          while (j < allLines.length) {
            var sm = allLines[j].match(re);
            if (!sm || sm[1].length < indent) break;
            j++;
          }
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
          if (_wkFileFailedCache[url]) return '';
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

  // === 对话面板头部按钮 ===
  $$('.chat-panel-btn').forEach(function (btn) {
    btn.addEventListener('click', function () {
      // 预留：面板展开/收起等操作
    });
  });

  // === 品牌栏按钮 ===
  $$('.brand-bar-btn').forEach(function (btn) {
    btn.addEventListener('click', function () {
      // 预留：编辑、复制、历史等操作
    });
  });

  // === 对话面板工具栏 — 动态渲染智能体列表 ===
  function renderChatToolbar() {
    var toolbar = $('.chat-input-toolbar');
    if (!toolbar || agents.length === 0) return;

    // 清空现有工具栏按钮
    toolbar.innerHTML = '';

    agents.forEach(function (agent, idx) {
      var btn = document.createElement('button');
      var isSelected = wkSelectedAgentId ? (agent.id === wkSelectedAgentId) : (idx === 0);
      btn.className = 'chat-tool-btn' + (isSelected ? ' mode-active' : '');
      btn.textContent = agent.name;
      btn.setAttribute('data-agent-id', agent.id);
      var isQuickAnswer = agent.id === 'builtin-quick-answer' || (agent.config && agent.config.agent_mode === 'quick-answer');

      btn.addEventListener('click', function () {
        $$('.chat-tool-btn').forEach(function (b) { b.classList.remove('mode-active'); });
        btn.classList.add('mode-active');
        wkSelectedAgentId = agent.id;
        wkSelectedAgentEnabled = !isQuickAnswer;
        wkSelectedAgentImageUpload = !!(agent.config && agent.config.image_upload_enabled);
        wkUpdateImageUploadUI();
        // 持久化模式选择，同步给 sidepanel
        chrome.storage.local.set({ ka_selected_agent: { agentId: agent.id, agentEnabled: !isQuickAnswer } });
      });

      toolbar.appendChild(btn);
    });
  }

  // === 侧边栏折叠按钮 ===
  var logoBtn = $('.sidebar-logo-btn');
  if (logoBtn) {
    logoBtn.addEventListener('click', function () {
      var sidebar = $('.sidebar');
      sidebar.classList.toggle('collapsed');
    });
  }

  // === 网页内容收藏按钮 ===
  var navClips = $('#nav-clips');
  if (navClips) {
    navClips.addEventListener('click', function () {
      if (chrome && chrome.runtime && chrome.runtime.getURL) {
        window.location.href = chrome.runtime.getURL('clips.html');
      } else {
        window.location.href = 'clips.html';
      }
    });
  }

  // === 初始化：加载知识库和智能体 ===
  loadKnowledgeBases();
  loadAgentList();

  // 缓存 baseUrl 用于文件 URL 转换
  sendMsg({ type: 'GET_CONFIG' }).then(function (resp) {
    if (resp && resp.success && resp.data && resp.data.baseUrl) {
      wkCachedBaseUrl = resp.data.baseUrl.replace(/\/+$/, '');
    }
  });

  // 加载用户信息
  sendMsg({ type: 'GET_USER_INFO' }).then(function (resp) {
    if (resp && resp.success && resp.data) {
      var user = resp.data.user || resp.data;
      var nameEl = $('#wk-user-name');
      var emailEl = $('#wk-user-email');
      var letterEl = $('#wk-user-letter');
      if (nameEl) nameEl.textContent = user.username || user.name || '用户';
      if (emailEl) emailEl.textContent = user.email || '';
      if (letterEl) letterEl.textContent = (user.username || user.name || 'U').charAt(0).toUpperCase();
    }
  });

  // === 图片点击放大（灯箱）===
  if (chatBody) {
    chatBody.addEventListener('click', function (e) {
      var img = e.target;
      if (img.tagName !== 'IMG') return;
      var src = img.src;
      if (!src) return;
      var overlay = document.createElement('div');
      overlay.style.cssText = 'position:fixed;inset:0;background:rgba(0,0,0,0.8);z-index:9999;display:flex;align-items:center;justify-content:center;cursor:zoom-out;';
      var bigImg = document.createElement('img');
      bigImg.src = src;
      bigImg.style.cssText = 'max-width:95%;max-height:95%;object-fit:contain;border-radius:8px;box-shadow:0 4px 32px rgba(0,0,0,0.4);';
      overlay.appendChild(bigImg);
      document.body.appendChild(overlay);
      overlay.addEventListener('click', function () { overlay.remove(); });
    });
  }

})();
