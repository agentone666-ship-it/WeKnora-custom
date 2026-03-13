// background.js — Service Worker
// 存储管理 + 消息路由 + 右键菜单 + API 通信

// === WeKnora API Helper ===
// 构建请求头：API Key 使用 X-API-Key header，Bearer token 使用 Authorization header
async function buildHeaders(config) {
  var headers = {
    'Content-Type': 'application/json',
    'X-Request-ID': Date.now().toString(36) + Math.random().toString(36).slice(2, 8)
  };
  if (config.apiKey) {
    // API Key (sk- 开头) 使用 X-API-Key header 进行租户级认证
    headers['X-API-Key'] = config.apiKey;
  }
  if (config.bearerToken) {
    // Bearer token (通过用户名密码登录获取) 使用 Authorization header
    headers['Authorization'] = 'Bearer ' + config.bearerToken;
  }
  return headers;
}

async function apiRequest(method, path, body, options) {
  var config = await getConfigData();
  if (!config || !config.baseUrl) {
    return { success: false, error: '未配置服务地址，请先在设置中配置' };
  }
  var baseUrl = config.baseUrl.replace(/\/+$/, '');
  var url = baseUrl + path;
  var headers = await buildHeaders(config);
  try {
    var fetchOpts = { method: method, headers: headers };
    if (body && method !== 'GET') {
      fetchOpts.body = JSON.stringify(body);
    }
    if (options && options.signal) {
      fetchOpts.signal = options.signal;
    }
    var resp = await fetch(url, fetchOpts);
    if (!resp.ok) {
      var errText = '';
      try { var errJson = await resp.json(); errText = errJson.error?.message || errJson.message || resp.statusText; } catch (e) { errText = resp.statusText; }
      return { success: false, error: errText, status: resp.status };
    }
    var data = await resp.json();
    return data;
  } catch (err) {
    return { success: false, error: err.message || '网络请求失败' };
  }
}

// SSE streaming chat request — returns ReadableStream
async function apiChatStream(path, body) {
  var config = await getConfigData();
  if (!config || !config.baseUrl) {
    return { success: false, error: '未配置服务地址' };
  }
  var baseUrl = config.baseUrl.replace(/\/+$/, '');
  var url = baseUrl + path;
  var headers = await buildHeaders(config);
  headers['Accept'] = 'text/event-stream';
  try {
    var resp = await fetch(url, {
      method: 'POST',
      headers: headers,
      body: JSON.stringify(body),
      cache: 'no-store'  // 避免浏览器缓存导致 SSE 流被缓冲
    });
    if (!resp.ok) {
      var errText = '';
      try { var errJson = await resp.json(); errText = errJson.error?.message || errJson.message || resp.statusText; } catch (e) { errText = resp.statusText; }
      return { success: false, error: errText };
    }
    return { success: true, response: resp };
  } catch (err) {
    return { success: false, error: err.message || '网络请求失败' };
  }
}

// 流式推送消息到 sidepanel / popup 等前端页面
function notifyStream(msg) {
  chrome.runtime.sendMessage(msg).catch(function () {});
}

// Helper to get raw config data
async function getConfigData() {
  var data = await chrome.storage.local.get('ka_config');
  return data.ka_config || null;
}

// Session cache per tab for sidepanel chat
var chatSessions = {};

// === 右键菜单 ===
// 防止并发注册
var _menuSetupInProgress = false;

function setupContextMenus() {
  if (_menuSetupInProgress) return;
  _menuSetupInProgress = true;

  chrome.contextMenus.removeAll(function () {
    void chrome.runtime.lastError; // 清除可能的 lastError

    // 保存选中文字
    chrome.contextMenus.create({
      id: 'ka-save-selection',
      title: '保存到快速笔记 5.0',
      contexts: ['selection']
    }, function () { void chrome.runtime.lastError; });

    // 用选中文字提问
    chrome.contextMenus.create({
      id: 'ka-ask-selection',
      title: '使用知识助理提问',
      contexts: ['selection']
    }, function () { void chrome.runtime.lastError; });

    // 保存图片到快速笔记
    chrome.contextMenus.create({
      id: 'ka-save-image',
      title: '保存图片到快速笔记',
      contexts: ['image']
    }, function () { void chrome.runtime.lastError; });

    _menuSetupInProgress = false;

    // 根据登录状态更新菜单文案
    updateContextMenuTitle();
  });
}

// 插件安装/更新时注册
chrome.runtime.onInstalled.addListener(function () {
  setupContextMenus();
});

// Service Worker 每次启动时也注册（确保重载后菜单存在）
setupContextMenus();

// 根据登录类型动态更新右键菜单中"提问"的文案
async function updateContextMenuTitle() {
  var data = await chrome.storage.local.get('ka_auth');
  var auth = data.ka_auth;
  var askTitle = '使用知识助理提问';
  if (auth && auth.type === 'wk') {
    askTitle = '使用 WeKnora 提问';
  } else if (auth && auth.type === 'ka') {
    askTitle = '使用知识助理提问';
  }
  chrome.contextMenus.update('ka-ask-selection', { title: askTitle }, function () {
    void chrome.runtime.lastError; // 菜单还未创建时静默忽略
  });
}

chrome.contextMenus.onClicked.addListener(function (info, tab) {
  // 处理图片保存（不需要 selectionText）
  if (info.menuItemId === 'ka-save-image') {
    var imgUrl = info.srcUrl;
    if (!imgUrl) return;
    var title = '图片收藏 - ' + (tab.title || '未知页面');
    var clip = {
      type: 'image-clip',
      content: '![图片](' + imgUrl + ')',
      title: title,
      meta: { url: tab.url || '', title: tab.title || '', imageUrl: imgUrl }
    };
    saveClip(clip).then(function () {
      if (tab && tab.id) {
        chrome.tabs.sendMessage(tab.id, {
          type: 'SHOW_NOTIFICATION',
          payload: { msg: '图片已保存到快速笔记', status: 'success' }
        }).catch(function () {});
      }
    });
    return;
  }

  if (!info.selectionText) return;

  if (info.menuItemId === 'ka-save-selection') {
    var title = '选中文本 - ' + (tab.title || '未知页面');
    var clip = {
      type: 'select-clip',
      content: info.selectionText,
      title: title,
      meta: { url: tab.url || '', title: tab.title || '' }
    };
    saveClip(clip).then(function () {
      if (tab && tab.id) {
        chrome.tabs.sendMessage(tab.id, {
          type: 'SHOW_NOTIFICATION',
          payload: { msg: '已保存到快速笔记', status: 'success' }
        }).catch(function () {});
      }
    });
  }

  if (info.menuItemId === 'ka-ask-selection') {
    // 打开侧边栏并将选中文字作为问题
    if (tab && tab.id) {
      chrome.storage.local.set({
        ka_pending_query: { query: info.selectionText, ts: Date.now() }
      });
      chrome.sidePanel.open({ tabId: tab.id }).catch(function () {});
    }
  }
});

chrome.runtime.onMessage.addListener(function (msg, sender, sendResponse) {
  handleMessage(msg, sender).then(function (result) {
    sendResponse(result);
  }).catch(function (err) {
    sendResponse({ success: false, error: err.message || '未知错误' });
  });
  return true;
});

async function handleMessage(msg, sender) {
  switch (msg.type) {
    case 'GET_AUTH':
      return getAuth();
    case 'SET_AUTH':
      return setAuth(msg.payload);
    case 'CLEAR_AUTH':
      return clearAuth();
    case 'GET_CONFIG':
      return getConfig();
    case 'SET_CONFIG':
      return setConfig(msg.payload);
    case 'SAVE_NOTE':
      return saveNote(msg.payload);
    case 'GET_NOTES':
      return getNotes();
    case 'SAVE_CLIP':
      return saveClip(msg.payload);
    case 'GET_CLIPS':
      return getClips();
    case 'DELETE_CLIP':
      return deleteClip(msg.payload);
    case 'DELETE_NOTE':
      return deleteNote(msg.payload);
    case 'UPDATE_CLIP':
      return updateClip(msg.payload);
    case 'UPDATE_NOTE':
      return updateNote(msg.payload);
    case 'INJECT_SCRIPT':
      return injectScript(msg.payload.tabId);
    case 'ASK_WEKNORA':
      // 打开侧边栏并传递选中的文字作为问题
      if (sender && sender.tab && sender.tab.id) {
        await chrome.sidePanel.open({ tabId: sender.tab.id });
        // 存储待处理的问题，sidepanel 加载后会读取
        await chrome.storage.local.set({
          ka_pending_query: { query: msg.payload.text, ts: Date.now() }
        });
      }
      return { success: true };
    case 'SAVE_SELECTION':
      // 从气泡保存选中文字
      return saveClip(msg.payload);
    case 'SAVE_IMAGE':
      // 从气泡保存图片
      return saveClip(msg.payload);
    case 'CAPTURE_SCREENSHOT':
      // 截取当前标签页可见区域
      try {
        var tabId = sender && sender.tab && sender.tab.id;
        if (!tabId) return { success: false, error: '无法获取标签页' };
        var dataUrl = await chrome.tabs.captureVisibleTab(null, { format: 'jpeg', quality: 90 });
        return { success: true, dataUrl: dataUrl };
      } catch (err) {
        return { success: false, error: err.message || '截图失败' };
      }

    // === WeKnora API 相关 ===
    case 'VALIDATE_CONFIG':
      // API Key 没有专门的验证端点，通过调用知识库列表接口来验证连通性和认证
      return apiRequest('GET', '/knowledge-bases');

    case 'LOGIN': {
      // 用户名密码登录，获取 Bearer token
      var loginPayload = msg.payload || {};
      var loginResp = await apiRequest('POST', '/auth/login', {
        email: loginPayload.email,
        password: loginPayload.password
      });
      // 如果登录成功，将 token 存入 config
      if (loginResp && loginResp.success && loginResp.token) {
        var curConfig = await getConfigData() || {};
        curConfig.bearerToken = loginResp.token;
        curConfig.refreshToken = loginResp.refresh_token;
        await chrome.storage.local.set({ ka_config: curConfig });
      }
      return loginResp;
    }

    case 'LIST_KNOWLEDGE_BASES':
      // agent_id 参数仅用于共享智能体（跨租户），本地/内置智能体不传
      var agentFilter = (msg.payload && msg.payload.sharedAgentId) ? '?agent_id=' + msg.payload.sharedAgentId : '';
      return apiRequest('GET', '/knowledge-bases' + agentFilter);

    case 'LIST_AGENTS':
      return apiRequest('GET', '/agents');

    case 'CREATE_KNOWLEDGE_BASE':
      return apiRequest('POST', '/knowledge-bases', msg.payload);

    case 'CREATE_SESSION':
      return apiRequest('POST', '/sessions', msg.payload || {});

    case 'LIST_SESSIONS':
      var p = msg.payload || {};
      return apiRequest('GET', '/sessions?page=' + (p.page || 1) + '&page_size=' + (p.page_size || 20));

    case 'CLEAR_SESSION_MESSAGES': {
      var sid = (msg.payload || {}).sessionId;
      if (!sid) return { success: false, error: '缺少 sessionId' };
      return apiRequest('DELETE', '/sessions/' + sid + '/messages');
    }

    case 'CHAT_QUERY': {
      // 真正的知识库问答 — 使用 SSE 流式输出
      var payload = msg.payload || {};
      var query = payload.query;
      if (!query) return { success: false, error: '请输入问题' };

      // 获取或创建会话
      var sessionId = payload.sessionId;
      if (!sessionId) {
        var sessionResp = await apiRequest('POST', '/sessions', {});
        if (sessionResp && sessionResp.success && sessionResp.data) {
          sessionId = sessionResp.data.id;
        } else if (sessionResp && sessionResp.id) {
          sessionId = sessionResp.id;
        }
        if (!sessionId) {
          return { success: false, error: '创建会话失败: ' + (sessionResp.error || '未知错误') };
        }
        await chrome.storage.local.set({ ka_current_session: sessionId });
      }

      // 确定使用知识库问答还是智能体问答
      var kbIds = payload.knowledgeBaseIds || [];
      var agentId = payload.agentId || '';
      var useAgent = payload.agentEnabled || false;
      var chatPath = useAgent
        ? '/agent-chat/' + sessionId
        : '/knowledge-chat/' + sessionId;

      // 构建完整请求体，参考 CreateKnowledgeQARequest
      var chatBody = { query: query };
      if (kbIds.length > 0) {
        chatBody.knowledge_base_ids = kbIds;
      }
      if (agentId) {
        chatBody.agent_id = agentId;
      }
      if (useAgent) {
        chatBody.agent_enabled = true;
      }
      if (payload.webSearchEnabled) {
        chatBody.web_search_enabled = true;
      }
      if (payload.mentionedItems) {
        chatBody.mentioned_items = payload.mentionedItems;
      }
      if (payload.images && payload.images.length > 0) {
        chatBody.images = payload.images;
      }

      // 使用请求 ID 区分不同来源的流式推送
      var chatRequestId = payload._requestId || (Date.now().toString(36) + Math.random().toString(36).slice(2, 6));

      // SSE 流式请求
      var streamResult = await apiChatStream(chatPath, chatBody);
      if (!streamResult.success) {
        return { success: false, error: streamResult.error };
      }

      // 读取 SSE 流，逐块推送到前端
      try {
        var reader = streamResult.response.body.getReader();
        var decoder = new TextDecoder();
        var fullText = '';
        var buffer = '';

        while (true) {
          var readResult = await reader.read();
          if (readResult.done) break;
          buffer += decoder.decode(readResult.value, { stream: true });

          // SSE 格式: "event: message\ndata: {json}\n\n"
          // 按双换行分割完整事件块
          var eventBlocks = buffer.split('\n\n');
          buffer = eventBlocks.pop() || '';

          for (var bi = 0; bi < eventBlocks.length; bi++) {
            var block = eventBlocks[bi].trim();
            if (!block) continue;

            // 从事件块中提取 data 行
            var dataLine = '';
            var blockLines = block.split('\n');
            for (var li = 0; li < blockLines.length; li++) {
              var bline = blockLines[li];
              if (bline.startsWith('data:')) {
                dataLine = bline.substring(5).trim();
              }
            }
            if (!dataLine || dataLine === '[DONE]') continue;

            try {
              var evt = JSON.parse(dataLine);
              var responseType = evt.response_type || '';

              // 根据 response_type 处理不同事件
              if (responseType === 'answer') {
                var chunk = evt.content || '';
                if (chunk) {
                  fullText += chunk;
                  notifyStream({
                    type: 'CHAT_STREAM_CHUNK',
                    payload: { requestId: chatRequestId, sessionId: sessionId, responseType: 'answer', content: chunk, done: !!evt.done }
                  });
                }
              } else if (responseType === 'thinking') {
                notifyStream({
                  type: 'CHAT_STREAM_CHUNK',
                  payload: { requestId: chatRequestId, sessionId: sessionId, responseType: 'thinking', content: evt.content || '', eventId: evt.data && evt.data.event_id }
                });
              } else if (responseType === 'tool_call') {
                notifyStream({
                  type: 'CHAT_STREAM_CHUNK',
                  payload: { requestId: chatRequestId, sessionId: sessionId, responseType: 'tool_call', content: evt.content || '', toolName: evt.data && evt.data.tool_name, eventId: evt.data && (evt.data.event_id || evt.data.tool_call_id) }
                });
              } else if (responseType === 'tool_result') {
                notifyStream({
                  type: 'CHAT_STREAM_CHUNK',
                  payload: { requestId: chatRequestId, sessionId: sessionId, responseType: 'tool_result', content: evt.content || '', toolName: evt.data && evt.data.tool_name, eventId: evt.data && (evt.data.event_id || evt.data.tool_call_id), success: evt.data && evt.data.success }
                });
              } else if (responseType === 'references') {
                var kRefs = evt.knowledge_references;
                if (Array.isArray(kRefs) && kRefs.length > 0) {
                  notifyStream({
                    type: 'CHAT_STREAM_CHUNK',
                    payload: { requestId: chatRequestId, sessionId: sessionId, responseType: 'references', references: kRefs }
                  });
                }
              } else if (responseType === 'error') {
                notifyStream({
                  type: 'CHAT_STREAM_CHUNK',
                  payload: { requestId: chatRequestId, sessionId: sessionId, responseType: 'error', content: evt.content || '请求出错' }
                });
              } else if (responseType === 'complete') {
                notifyStream({
                  type: 'CHAT_STREAM_CHUNK',
                  payload: { requestId: chatRequestId, sessionId: sessionId, responseType: 'complete', done: true }
                });
              }
              if (responseType === 'session_title') {
                notifyStream({
                  type: 'CHAT_STREAM_CHUNK',
                  payload: { requestId: chatRequestId, sessionId: sessionId, responseType: 'session_title', content: evt.content || '' }
                });
              }
              // agent_query 等事件静默忽略
            } catch (e) {
              // 非 JSON data 行，忽略
            }
          }
        }

        return { success: true, data: fullText || '未获取到回复内容', sessionId: sessionId, requestId: chatRequestId };
      } catch (streamErr) {
        return { success: false, error: '读取回复流失败: ' + streamErr.message };
      }
    }

    case 'SAVE_CLIP_TO_KB': {
      // 保存剪藏内容到知识库（作为手动知识条目）
      var pl = msg.payload || {};
      if (!pl.kbId || !pl.content) return { success: false, error: '缺少知识库 ID 或内容' };
      var contentWithMeta = pl.content;
      if (pl.url) {
        contentWithMeta = '> 来源: ' + pl.url + '\n\n' + pl.content;
      }
      return apiRequest('POST', '/knowledge-bases/' + pl.kbId + '/knowledge/manual', {
        title: pl.title || '快速笔记剪藏',
        content: contentWithMeta,
        status: 'publish'
      });
    }

    case 'FETCH_FILE': {
      // 通过 background 代理带认证头请求文件（图片等），返回 data URL
      var filePath = (msg.payload || {}).filePath;
      if (!filePath) return { success: false, error: '缺少 filePath' };
      var cfg = await getConfigData();
      if (!cfg || !cfg.baseUrl) return { success: false, error: '未配置服务地址' };
      var fileBaseUrl = cfg.baseUrl.replace(/\/+$/, '').replace(/\/api\/v\d+$/, '');
      var fileUrl = fileBaseUrl + '/files?file_path=' + encodeURIComponent(filePath);
      var fileHeaders = await buildHeaders(cfg);
      try {
        var fileResp = await fetch(fileUrl, { method: 'GET', headers: fileHeaders });
        if (!fileResp.ok) return { success: false, error: 'HTTP ' + fileResp.status };
        var blob = await fileResp.blob();
        // 转为 data URL 以便跨上下文传递
        var reader2 = new FileReader();
        var dataUrl = await new Promise(function (resolve, reject) {
          reader2.onload = function () { resolve(reader2.result); };
          reader2.onerror = function () { reject(new Error('FileReader error')); };
          reader2.readAsDataURL(blob);
        });
        return { success: true, dataUrl: dataUrl };
      } catch (err) {
        return { success: false, error: err.message || '文件请求失败' };
      }
    }

    case 'GET_USER_INFO':
      return apiRequest('GET', '/auth/me');

    default:
      return { success: false, error: '未知消息类型' };
  }
}

// === Auth ===
async function getAuth() {
  var data = await chrome.storage.local.get('ka_auth');
  return { success: true, data: data.ka_auth || null };
}

async function setAuth(auth) {
  await chrome.storage.local.set({ ka_auth: auth });
  updateContextMenuTitle();
  return { success: true };
}

async function clearAuth() {
  await chrome.storage.local.remove('ka_auth');
  updateContextMenuTitle();
  return { success: true };
}

// === Config (WeKnora) ===
async function getConfig() {
  var data = await chrome.storage.local.get('ka_config');
  return { success: true, data: data.ka_config || { baseUrl: '', apiKey: '' } };
}

async function setConfig(config) {
  await chrome.storage.local.set({ ka_config: config });
  return { success: true };
}

// === Notes (Markdown) ===
async function saveNote(note) {
  var data = await chrome.storage.local.get('ka_notes');
  var notes = data.ka_notes || [];
  note.id = Date.now().toString();
  note.createdAt = new Date().toISOString();
  notes.unshift(note);
  if (notes.length > 100) notes = notes.slice(0, 100);
  await chrome.storage.local.set({ ka_notes: notes });
  return { success: true, data: note };
}

async function getNotes() {
  var data = await chrome.storage.local.get('ka_notes');
  return { success: true, data: data.ka_notes || [] };
}

// === Clips (网页截取收藏) ===
async function saveClip(clip) {
  try {
    var data = await chrome.storage.local.get('ka_clips');
    var clips = data.ka_clips || [];

    // 如果传入了已有 id，说明是编辑已有笔记，执行更新而非新增
    if (clip.id) {
      var found = false;
      for (var i = 0; i < clips.length; i++) {
        if (clips[i].id === clip.id) {
          // 保留原始创建时间和其他元数据，只更新内容相关字段
          clips[i].content = clip.content;
          if (clip.title) clips[i].title = clip.title;
          if (clip.type) clips[i].type = clip.type;
          clips[i].updatedAt = new Date().toISOString();
          clip = clips[i]; // 返回完整的更新后记录
          found = true;
          break;
        }
      }
      // 如果在 ka_clips 中没找到，再到 ka_notes 中查找并更新
      if (!found) {
        var notesData = await chrome.storage.local.get('ka_notes');
        var notes = notesData.ka_notes || [];
        for (var j = 0; j < notes.length; j++) {
          if (notes[j].id === clip.id) {
            notes[j].content = clip.content;
            if (clip.title) notes[j].title = clip.title;
            if (clip.type) notes[j].type = clip.type;
            notes[j].updatedAt = new Date().toISOString();
            clip = notes[j];
            found = true;
            await chrome.storage.local.set({ ka_notes: notes });
            break;
          }
        }
      }
      if (found) {
        await chrome.storage.local.set({ ka_clips: clips });
        return { success: true, data: clip };
      }
      // 没找到原记录，当作新建处理（fallthrough）
    }

    // 新建记录
    clip.id = Date.now().toString();
    clip.createdAt = new Date().toISOString();
    clips.unshift(clip);
    if (clips.length > 200) clips = clips.slice(0, 200);
    await chrome.storage.local.set({ ka_clips: clips });
    return { success: true, data: clip };
  } catch (err) {
    // 如果保存失败（可能是截图太大），尝试去掉截图再保存
    if (clip.screenshot) {
      try {
        delete clip.screenshot;
        var data2 = await chrome.storage.local.get('ka_clips');
        var clips2 = data2.ka_clips || [];
        clips2.unshift(clip);
        if (clips2.length > 200) clips2 = clips2.slice(0, 200);
        await chrome.storage.local.set({ ka_clips: clips2 });
        return { success: true, data: clip, warning: '截图过大已省略，仅保存文字' };
      } catch (err2) {
        return { success: false, error: '保存失败: ' + (err2.message || '存储空间不足') };
      }
    }
    return { success: false, error: '保存失败: ' + (err.message || '未知错误') };
  }
}

async function getClips() {
  var data = await chrome.storage.local.get('ka_clips');
  return { success: true, data: data.ka_clips || [] };
}

async function deleteClip(payload) {
  var data = await chrome.storage.local.get('ka_clips');
  var clips = data.ka_clips || [];
  clips = clips.filter(function (c) { return c.id !== payload.id; });
  await chrome.storage.local.set({ ka_clips: clips });
  return { success: true };
}

async function deleteNote(payload) {
  var data = await chrome.storage.local.get('ka_notes');
  var notes = data.ka_notes || [];
  notes = notes.filter(function (n) { return n.id !== payload.id; });
  await chrome.storage.local.set({ ka_notes: notes });
  return { success: true };
}

async function updateClip(payload) {
  var data = await chrome.storage.local.get('ka_clips');
  var clips = data.ka_clips || [];
  var found = false;
  for (var i = 0; i < clips.length; i++) {
    if (clips[i].id === payload.id) {
      clips[i].content = payload.content;
      if (payload.title) clips[i].title = payload.title;
      clips[i].updatedAt = new Date().toISOString();
      found = true;
      break;
    }
  }
  if (!found) return { success: false, error: '未找到对应记录' };
  await chrome.storage.local.set({ ka_clips: clips });
  return { success: true };
}

async function updateNote(payload) {
  var data = await chrome.storage.local.get('ka_notes');
  var notes = data.ka_notes || [];
  var found = false;
  for (var i = 0; i < notes.length; i++) {
    if (notes[i].id === payload.id) {
      notes[i].content = payload.content;
      if (payload.title) notes[i].title = payload.title;
      notes[i].updatedAt = new Date().toISOString();
      found = true;
      break;
    }
  }
  if (!found) return { success: false, error: '未找到对应记录' };
  await chrome.storage.local.set({ ka_notes: notes });
  return { success: true };
}

// === Inject content script ===
async function injectScript(tabId) {
  try {
    await chrome.scripting.executeScript({ target: { tabId: tabId }, files: ['content.js'] });
    await chrome.scripting.insertCSS({ target: { tabId: tabId }, files: ['content.css'] });
    return { success: true };
  } catch (e) {
    return { success: false, error: e.message };
  }
}

// === Commands ===
chrome.commands.onCommand.addListener(async function (cmd, tab) {
  if (!tab || !tab.id) return;
  if (cmd === 'open-sidepanel') {
    await chrome.sidePanel.open({ tabId: tab.id });
  }
  if (cmd === 'quick-ask') {
    await chrome.sidePanel.open({ tabId: tab.id });
  }
  if (cmd === 'select-clip') {
    // 快捷键触发选择剪藏：先确保 content script 已注入，再发消息
    try {
      await chrome.tabs.sendMessage(tab.id, { type: 'SELECT_CLIP' });
    } catch (e) {
      // content script 未注入，先注入再发送
      try {
        await chrome.scripting.executeScript({ target: { tabId: tab.id }, files: ['content.js'] });
        await chrome.scripting.insertCSS({ target: { tabId: tab.id }, files: ['content.css'] });
        setTimeout(function () {
          chrome.tabs.sendMessage(tab.id, { type: 'SELECT_CLIP' }).catch(function () {});
        }, 300);
      } catch (injectErr) {
        // 无法注入的页面（如 chrome:// 页面），忽略
      }
    }
  }
});
