(function () {
  'use strict';

  function $(id) { return document.getElementById(id); }

  var messagesEl = $('messages');
  var inputEl = $('sp-input');
  var sendBtn = $('sp-send');
  var welcomeEl = $('welcome');
  var typingEl = $('typing');

  // === 消息发送 ===
  inputEl.addEventListener('input', function () {
    sendBtn.classList.toggle('active', inputEl.value.trim().length > 0);
  });

  inputEl.addEventListener('keydown', function (e) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      sendMessage();
    }
  });

  sendBtn.addEventListener('click', sendMessage);

  function sendMessage(text) {
    var query = text || inputEl.value.trim();
    if (!query) return;

    if (welcomeEl) welcomeEl.style.display = 'none';

    appendMsg('user', query);
    inputEl.value = '';
    sendBtn.classList.remove('active');

    showTyping(true);
    var replied = false;

    // 发送到 background
    chrome.runtime.sendMessage({ type: 'CHAT_QUERY', payload: { query: query } }, function (resp) {
      if (replied) return;
      if (resp && resp.success && resp.data) {
        replied = true;
        showTyping(false);
        appendMsg('bot', resp.data);
      }
    });

    // 超时兜底：3 秒无回复则显示模拟回复
    setTimeout(function () {
      if (replied) return;
      replied = true;
      showTyping(false);
      appendMsg('bot', '收到你的问题：「' + query + '」\n\n目前知识库正在配置中，稍后将为你提供准确回答。');
    }, 3000);
  }

  function appendMsg(role, text) {
    var div = document.createElement('div');
    div.className = 'msg msg-' + (role === 'user' ? 'user' : 'bot');
    div.textContent = text;
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
      chrome.runtime.sendMessage({
        type: 'SAVE_NOTE',
        payload: { type: 'clip', content: '## ' + title + '\n\n' + content }
      }, function (resp) {
        if (resp && resp.success) {
          this.textContent = '已保存';
          this.disabled = true;
        }
      }.bind(this));
    });

    messagesEl.insertBefore(card, typingEl);
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  function showTyping(show) {
    typingEl.classList.toggle('show', show);
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  function escapeHtml(s) {
    var d = document.createElement('div');
    d.textContent = s;
    return d.innerHTML;
  }

  // === 清空对话 ===
  $('btn-clear').addEventListener('click', function () {
    var msgs = messagesEl.querySelectorAll('.msg, .clip-card');
    msgs.forEach(function (m) { m.remove(); });
    if (welcomeEl) welcomeEl.style.display = 'block';
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

  // 知识库简称映射
  var kbShortNames = { 'all': '全部', 'quick-note': '笔记', 'web-collect': '收藏', 'product-doc': '文档', 'tech-spec': '规范' };

  // 定位下拉菜单 — 使用 fixed 定位避免 overflow 裁剪
  function positionKbMenu() {
    var rect = kbBtn.getBoundingClientRect();
    kbMenu.style.display = 'block';
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
    if (kbMenu.classList.contains('show')) {
      kbMenu.classList.remove('show');
    } else {
      kbMenu.classList.add('show');
      positionKbMenu();
    }
  });

  // 知识库选项
  kbMenu.querySelectorAll('.sp-dropdown-item').forEach(function (item) {
    item.addEventListener('click', function (e) {
      e.stopPropagation();
      var kb = item.getAttribute('data-kb');
      $('sp-kb-name').textContent = kbShortNames[kb] || item.textContent.trim();
      kbMenu.querySelectorAll('.sp-dropdown-item').forEach(function (i) { i.classList.remove('selected'); });
      item.classList.add('selected');
      kbMenu.classList.remove('show');
    });
  });

  // 模式选项
  kbMenu.querySelectorAll('.sp-mode-item').forEach(function (item) {
    item.addEventListener('click', function (e) {
      e.stopPropagation();
      kbMenu.querySelectorAll('.sp-mode-item').forEach(function (i) { i.classList.remove('selected'); });
      item.classList.add('selected');
      // 模式选中后不关闭菜单，让用户继续操作
    });
  });

  // 点击其他地方关闭下拉
  document.addEventListener('click', function () {
    kbMenu.classList.remove('show');
  });

  // 菜单内部点击不冒泡关闭
  kbMenu.addEventListener('click', function (e) {
    e.stopPropagation();
  });

  // === 监听来自 popup / content 的消息 ===
  chrome.runtime.onMessage.addListener(function (msg, sender, sendResponse) {
    if (msg.type === 'CHAT_QUERY' && msg.payload) {
      // 清除 storage 中的待处理标记（避免重复）
      chrome.storage.local.remove('ka_pending_query');
      sendMessage(msg.payload.query);
      sendResponse({ success: true });
    }
    if (msg.type === 'CLIP_RESULT' && msg.payload) {
      appendClipCard(msg.payload.title || '网页剪藏', msg.payload.content || '');
      sendResponse({ success: true });
    }
    return true;
  });

  // === 初始化：检查是否有从 popup 传来的待处理问题 ===
  chrome.storage.local.get('ka_pending_query', function (data) {
    if (data && data.ka_pending_query && data.ka_pending_query.query) {
      var pending = data.ka_pending_query;
      // 只处理 5 秒内的问题（防止旧数据）
      if (Date.now() - (pending.ts || 0) < 5000) {
        chrome.storage.local.remove('ka_pending_query');
        // 延迟一点让页面渲染完成
        setTimeout(function () {
          sendMessage(pending.query);
        }, 200);
      } else {
        chrome.storage.local.remove('ka_pending_query');
      }
    }
  });
})();
