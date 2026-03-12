(function () {
  'use strict';

  // === State ===
  var currentUser = null; // { type: 'ka'|'wk', name, avatar, badge }

  // === DOM Helpers ===
  function $(id) { return document.getElementById(id); }

  function goPage(id) {
    document.querySelectorAll('.page').forEach(function (p) { p.classList.remove('active'); });
    $(id).classList.add('active');
  }

  function toast(msg, type) {
    var el = $('toast');
    el.textContent = msg;
    el.className = 'toast show' + (type ? ' ' + type : '');
    setTimeout(function () { el.classList.remove('show'); }, 2000);
  }

  // === Chrome Storage Helpers ===
  function sendMsg(data) {
    return new Promise(function (resolve) {
      if (chrome && chrome.runtime && chrome.runtime.sendMessage) {
        try {
          chrome.runtime.sendMessage(data, function (resp) {
            // 静默处理 lastError，防止 Chrome 报错
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

  // 卡通头像
  var kaAvatarUrl = "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 120 120'%3E%3Cdefs%3E%3ClinearGradient id='bg' x1='0' y1='0' x2='1' y2='1'%3E%3Cstop offset='0%25' stop-color='%2307C160'/%3E%3Cstop offset='100%25' stop-color='%2306ad54'/%3E%3C/linearGradient%3E%3C/defs%3E%3Crect fill='url(%23bg)' width='120' height='120' rx='60'/%3E%3Ccircle cx='60' cy='48' r='22' fill='%23fff'/%3E%3Ccircle cx='52' cy='44' r='3' fill='%2307C160'/%3E%3Ccircle cx='68' cy='44' r='3' fill='%2307C160'/%3E%3Cpath d='M54 54 Q60 60 66 54' stroke='%2307C160' stroke-width='2.5' fill='none' stroke-linecap='round'/%3E%3Cellipse cx='60' cy='90' rx='28' ry='20' fill='%23fff'/%3E%3C/svg%3E";

  // === Page Navigation ===

  // 快速笔记 5.0 → 进入登录方式选择页
  $('btn-ka').addEventListener('click', function () {
    goPage('pg-ka-login');
    initQrCode();
  });

  // 返回首页
  $('btn-ka-back').addEventListener('click', function () {
    goPage('pg-login');
  });

  // === Tab 切换 ===
  document.querySelectorAll('.ka-tab').forEach(function (tab) {
    tab.addEventListener('click', function () {
      var target = tab.getAttribute('data-tab');
      document.querySelectorAll('.ka-tab').forEach(function (t) { t.classList.remove('active'); });
      tab.classList.add('active');
      document.querySelectorAll('.ka-panel').forEach(function (p) { p.classList.remove('active'); });
      $('panel-' + target).classList.add('active');
    });
  });

  // === 模拟二维码 ===
  function initQrCode() {
    var canvasArea = $('qr-canvas-area');
    var qrBox = $('qr-box');
    var statusEl = $('qr-status');
    var statusText = $('qr-status-text');
    var refreshBtn = $('btn-qr-refresh');

    // 清理旧 canvas
    var old = qrBox.querySelector('canvas');
    if (old) old.remove();

    var canvas = document.createElement('canvas');
    canvas.width = 180;
    canvas.height = 180;
    canvas.style.width = '180px';
    canvas.style.height = '180px';
    canvas.style.borderRadius = '6px';
    canvas.style.cursor = 'pointer';
    canvas.title = '点击模拟扫码登录';

    var ctx = canvas.getContext('2d');
    drawFakeQr(ctx, 180, 180);

    canvasArea.style.display = 'none';
    qrBox.insertBefore(canvas, qrBox.firstChild);

    statusEl.className = 'qr-status';
    statusText.textContent = '等待扫码…';
    refreshBtn.style.display = 'none';

    canvas.addEventListener('click', function () {
      simulateScan();
    });
  }

  function drawFakeQr(ctx, w, h) {
    var cell = 6;
    var cols = Math.floor(w / cell);
    var rows = Math.floor(h / cell);
    var ox = (w - cols * cell) / 2;
    var oy = (h - rows * cell) / 2;

    ctx.fillStyle = '#fff';
    ctx.fillRect(0, 0, w, h);

    function finder(x, y) {
      ctx.fillStyle = '#222';
      ctx.fillRect(ox + x * cell, oy + y * cell, 7 * cell, 7 * cell);
      ctx.fillStyle = '#fff';
      ctx.fillRect(ox + (x + 1) * cell, oy + (y + 1) * cell, 5 * cell, 5 * cell);
      ctx.fillStyle = '#222';
      ctx.fillRect(ox + (x + 2) * cell, oy + (y + 2) * cell, 3 * cell, 3 * cell);
    }
    finder(1, 1);
    finder(cols - 9, 1);
    finder(1, rows - 9);

    ctx.fillStyle = '#222';
    for (var r = 0; r < rows; r++) {
      for (var c = 0; c < cols; c++) {
        if ((r < 10 && c < 10) || (r < 10 && c > cols - 11) || (r > rows - 11 && c < 10)) continue;
        if (Math.random() > 0.55) {
          ctx.fillRect(ox + c * cell, oy + r * cell, cell, cell);
        }
      }
    }
  }

  function simulateScan() {
    var statusEl = $('qr-status');
    var statusText = $('qr-status-text');

    statusEl.className = 'qr-status scanned';
    statusText.textContent = '扫码成功，正在确认…';

    setTimeout(function () {
      statusText.textContent = '登录成功！';
      sendMsg({ type: 'SET_AUTH', payload: { type: 'ka', name: '快速笔记用户', avatar: kaAvatarUrl } });
      setTimeout(function () {
        enterMain('ka', '快速笔记用户', kaAvatarUrl, '快速笔记 5.0');
        toast('登录成功', 'success');
      }, 500);
    }, 1000);
  }

  $('btn-qr-refresh').addEventListener('click', function () {
    initQrCode();
  });

  // === API Key 登录 ===
  $('btn-ka-save').addEventListener('click', function () {
    var url = $('ka-url').value.trim();
    var key = $('ka-key').value.trim();
    var msg = $('ka-test-msg');

    if (!url) { msg.textContent = '请填写服务地址'; msg.className = 'ka-test-msg err'; return; }
    if (!key) { msg.textContent = '请填写 API Key'; msg.className = 'ka-test-msg err'; return; }

    msg.textContent = '正在验证…';
    msg.className = 'ka-test-msg';

    setTimeout(function () {
      sendMsg({ type: 'SET_CONFIG', payload: { baseUrl: url, apiKey: key } }).then(function () {
        sendMsg({ type: 'SET_AUTH', payload: { type: 'ka', name: '快速笔记用户', avatar: kaAvatarUrl } });
        msg.textContent = '验证通过';
        msg.className = 'ka-test-msg ok';
        setTimeout(function () {
          enterMain('ka', '快速笔记用户', kaAvatarUrl, '快速笔记 5.0');
          toast('登录成功', 'success');
        }, 500);
      });
    }, 800);
  });

  // WeKnora 配置页
  $('btn-wk').addEventListener('click', function () {
    goPage('pg-weknora');
    // 读取已有配置
    sendMsg({ type: 'GET_CONFIG' }).then(function (resp) {
      if (resp && resp.success && resp.data) {
        $('wk-url').value = resp.data.baseUrl || '';
        $('wk-key').value = resp.data.apiKey || '';
      }
    });
  });

  // WeKnora 返回
  $('btn-wk-back').addEventListener('click', function () {
    goPage('pg-login');
  });

  // WeKnora 保存并登录
  $('btn-wk-save').addEventListener('click', function () {
    var url = $('wk-url').value.trim();
    var key = $('wk-key').value.trim();
    if (!url) {
      $('wk-test-msg').textContent = '请填写服务地址';
      $('wk-test-msg').className = 'wk-test-result err';
      return;
    }
    if (!key) {
      $('wk-test-msg').textContent = '请填写 API Key';
      $('wk-test-msg').className = 'wk-test-result err';
      return;
    }
    $('wk-test-msg').textContent = '正在验证…';
    $('wk-test-msg').className = 'wk-test-result';
    // 保存配置
    sendMsg({ type: 'SET_CONFIG', payload: { baseUrl: url, apiKey: key } }).then(function () {
      sendMsg({ type: 'SET_AUTH', payload: { type: 'wk', name: 'WeKnora 用户', avatar: '' } });
      $('wk-test-msg').textContent = '配置已保存';
      $('wk-test-msg').className = 'wk-test-result ok';
      setTimeout(function () {
        enterMain('wk', 'WeKnora 用户', '', 'WeKnora');
      }, 500);
    });
  });

  // === 进入主界面 ===
  function enterMain(type, name, avatarUrl, badge) {
    currentUser = { type: type, name: name, avatar: avatarUrl, badge: badge };
    // 统一使用字母头像：圆形绿色底 + 字母 S
    $('main-avatar-img').style.display = 'none';
    $('main-letter').style.display = 'flex';
    $('main-letter').textContent = 'S';
    $('main-letter').style.background = '#07C160';
    // 设置名称和标签
    $('main-name').textContent = name;
    $('main-badge').textContent = badge;
    $('main-badge').className = 'main-badge ' + (type === 'ka' ? 'badge-ka' : 'badge-wk');
    // 设置面板数据
    $('set-type').textContent = badge;
    $('set-name').textContent = name;
    if (type === 'wk') {
      $('set-url-row').style.display = 'flex';
      $('set-key-row').style.display = 'flex';
      sendMsg({ type: 'GET_CONFIG' }).then(function (resp) {
        if (resp && resp.success && resp.data) {
          $('set-url').textContent = resp.data.baseUrl || '-';
          $('set-key').textContent = '••••••••' + (resp.data.apiKey || '').slice(-4);
        }
      });
    } else {
      // 快速笔记账号 — 检查是否通过 API Key 登录
      sendMsg({ type: 'GET_CONFIG' }).then(function (resp) {
        if (resp && resp.success && resp.data && resp.data.baseUrl) {
          $('set-url-row').style.display = 'flex';
          $('set-key-row').style.display = 'flex';
          $('set-url').textContent = resp.data.baseUrl || '-';
          $('set-key').textContent = '••••••••' + (resp.data.apiKey || '').slice(-4);
        } else {
          $('set-url-row').style.display = 'none';
          $('set-key-row').style.display = 'none';
        }
      });
    }
    goPage('pg-main');
    loadLatestClip();
  }

  // === 加载并显示最新一条内容 ===
  var latestClipData = null; // 缓存最新一条笔记的原始数据
  function loadLatestClip() {
    chrome.storage.local.get(['ka_clips', 'ka_notes'], function (data) {
      var all = [];
      if (data.ka_clips) all = all.concat(data.ka_clips);
      if (data.ka_notes) {
        data.ka_notes.forEach(function (n) {
          var exists = all.some(function (c) { return c.id === n.id; });
          if (!exists) all.push(n);
        });
      }
      // 按时间排序取最新一条
      all.sort(function (a, b) {
        return new Date(b.createdAt || 0) - new Date(a.createdAt || 0);
      });

      var emptyEl = $('latest-clip-empty');
      var contentEl = $('latest-clip-content');

      if (all.length === 0) {
        emptyEl.style.display = '';
        contentEl.style.display = 'none';
        latestClipData = null;
        return;
      }

      var clip = all[0];
      latestClipData = clip; // 缓存原始数据
      emptyEl.style.display = 'none';
      contentEl.style.display = '';

      // 时间
      $('latest-clip-time').textContent = formatTimeShort(clip.createdAt);

      // 类型标签
      var tagEl = $('latest-clip-tag');
      var hasScreenshot = clip.type === 'select-clip' && clip.screenshot;
      if (clip.type === 'markdown') {
        tagEl.textContent = '速记';
        tagEl.className = 'latest-clip-tag tag-markdown';
      } else if (clip.type === 'smart-clip') {
        tagEl.textContent = '智能剪藏';
        tagEl.className = 'latest-clip-tag';
      } else if (clip.type === 'image-clip') {
        tagEl.textContent = '图片收藏';
        tagEl.className = 'latest-clip-tag tag-image';
      } else if (hasScreenshot) {
        tagEl.textContent = '截图';
        tagEl.className = 'latest-clip-tag';
      } else {
        tagEl.textContent = '文本';
        tagEl.className = 'latest-clip-tag tag-text';
      }

      // 内容
      var bodyEl = $('latest-clip-body');
      if (hasScreenshot) {
        bodyEl.innerHTML = '<img src="' + clip.screenshot + '" alt="截图">';
      } else {
        bodyEl.innerHTML = renderMarkdown(clip.content || '');
      }
      // 判断内容是否溢出，只在溢出时才显示底部渐隐
      setTimeout(function () {
        if (bodyEl.scrollHeight > bodyEl.clientHeight + 2) {
          bodyEl.classList.add('overflowing');
        } else {
          bodyEl.classList.remove('overflowing');
        }
        syncFlipperHeight();
      }, 50);
    });
  }

  function formatTimeShort(iso) {
    if (!iso) return '';
    var d = new Date(iso);
    var now = new Date();
    var diff = now - d;
    if (diff < 60000) return '刚刚';
    if (diff < 3600000) return Math.floor(diff / 60000) + '分钟前';
    if (diff < 86400000) return Math.floor(diff / 3600000) + '小时前';
    if (diff < 604800000) return Math.floor(diff / 86400000) + '天前';
    return (d.getMonth() + 1) + '月' + d.getDate() + '日';
  }

  // === 对话功能 ===
  var chatInput = $('chat-input');
  var sendBtn = $('btn-chat-send');
  var selectedKb = 'all';
  var selectedMode = 'fast';

  chatInput.addEventListener('input', function () {
    sendBtn.classList.toggle('active', chatInput.value.trim().length > 0);
  });

  chatInput.addEventListener('keydown', function (e) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      sendChat();
    }
  });

  sendBtn.addEventListener('click', sendChat);

  function sendChat() {
    var text = chatInput.value.trim();
    if (!text) return;
    var queryData = { query: text, user: currentUser, kb: selectedKb, mode: selectedMode, ts: Date.now() };

    if (chrome && chrome.storage) {
      chrome.storage.local.set({ ka_pending_query: queryData });
    }
    if (chrome && chrome.runtime && chrome.runtime.sendMessage) {
      chrome.runtime.sendMessage({ type: 'CHAT_QUERY', payload: queryData }).catch(function () {});
    }
    if (chrome && chrome.tabs) {
      chrome.tabs.query({ active: true, currentWindow: true }, function (tabs) {
        if (tabs && tabs[0]) {
          chrome.sidePanel.open({ tabId: tabs[0].id }).catch(function () {});
        }
        window.close();
      });
    }
  }

  // === 知识库下拉（合并模式选项） ===
  var kbMenu = $('kb-menu');
  var kbBtn = $('btn-kb-select');

  function positionKbMenu() {
    var rect = kbBtn.getBoundingClientRect();
    var menuH = kbMenu.offsetHeight;
    // 优先向上弹出；空间不足则向下
    var top = rect.top - menuH - 6;
    if (top < 4) top = rect.bottom + 6;
    kbMenu.style.left = rect.left + 'px';
    kbMenu.style.top = top + 'px';
  }

  kbBtn.addEventListener('click', function (e) {
    e.stopPropagation();
    var willShow = !kbMenu.classList.contains('show');
    kbMenu.classList.toggle('show');
    if (willShow) {
      // 先显示再计算高度
      requestAnimationFrame(positionKbMenu);
    }
  });
  // 知识库选择
  var kbShortNames = { 'all': '全部', 'quick-note': '笔记', 'web-collect': '收藏', 'product-doc': '文档', 'tech-spec': '技术' };
  kbMenu.querySelectorAll('.kb-dropdown-item').forEach(function (item) {
    item.addEventListener('click', function (e) {
      e.stopPropagation();
      selectedKb = item.getAttribute('data-kb');
      $('kb-name').textContent = kbShortNames[selectedKb] || item.textContent.trim();
      kbMenu.querySelectorAll('.kb-dropdown-item').forEach(function (i) { i.classList.remove('selected'); });
      item.classList.add('selected');
      kbMenu.classList.remove('show');
    });
  });
  // 模式选择（合并在知识库菜单内）
  kbMenu.querySelectorAll('.kb-mode-item').forEach(function (item) {
    item.addEventListener('click', function (e) {
      e.stopPropagation();
      selectedMode = item.getAttribute('data-mode');
      kbMenu.querySelectorAll('.kb-mode-item').forEach(function (i) { i.classList.remove('selected'); });
      item.classList.add('selected');
      kbMenu.classList.remove('show');
    });
  });

  // 点击其他地方关闭下拉
  document.addEventListener('click', function () {
    kbMenu.classList.remove('show');
    if (moreMenu) moreMenu.classList.remove('show');
  });

  // === 网页采集 ===
  var collectWrap = $('collect-wrap');
  var cardFlipper = $('card-flipper');
  var isFlipped = false;

  // 同步 flipper 容器高度为当前可见面的高度
  function syncFlipperHeight() {
    var front = $('latest-clip');
    var back = $('note-back');
    if (isFlipped) {
      // 背面使用固定高度，不随 textarea 内容滚动而变化
      var frontH = front.scrollHeight;
      var h = Math.max(frontH, 220);
      cardFlipper.style.height = h + 'px';
      back.style.height = h + 'px';
    } else {
      var fh = Math.max(front.scrollHeight, 220);
      cardFlipper.style.height = fh + 'px';
    }
  }

  var editingClipId = null; // 正在编辑的笔记 id（双击编辑时有值）

  function flipToNote(editClip) {
    if (isFlipped) return;
    isFlipped = true;
    // 如果传入了要编辑的笔记，填充到 textarea
    if (editClip && editClip.content) {
      $('note-input').value = editClip.content;
      editingClipId = editClip.id || null;
    } else {
      editingClipId = null;
    }
    // 先设背面高度不小于正面
    var frontH = $('latest-clip').offsetHeight;
    $('note-back').style.minHeight = Math.max(frontH, 220) + 'px';
    collectWrap.classList.add('flipped');
    syncFlipperHeight();
    // 翻转动画完成后聚焦
    setTimeout(function () {
      var inp = $('note-input');
      inp.focus();
      // 光标放到末尾
      inp.selectionStart = inp.selectionEnd = inp.value.length;
      syncFlipperHeight();
    }, 650);
  }

  function flipToFront() {
    if (!isFlipped) return;
    isFlipped = false;
    editingClipId = null;
    collectWrap.classList.remove('flipped');
    syncFlipperHeight();
  }

  // 页面加载后初始化高度
  setTimeout(syncFlipperHeight, 50);

  $('btn-quick-note').addEventListener('click', function () {
    flipToNote();
  });

  $('btn-note-close').addEventListener('click', function () {
    flipToFront();
  });

  $('btn-smart-clip').addEventListener('click', function () {
    chrome.tabs.query({ active: true, currentWindow: true }, function (tabs) {
      if (!tabs || !tabs[0]) return;
      var tab = tabs[0];
      var tabId = tab.id;

      // 检查是否为不支持注入的页面
      var url = tab.url || '';
      if (url.startsWith('chrome://') || url.startsWith('chrome-extension://') || url.startsWith('edge://') || url.startsWith('about:') || url === '' || url.startsWith('chrome:')) {
        toast('此页面不支持剪藏功能', 'error');
        return;
      }

      chrome.tabs.sendMessage(tabId, { type: 'SMART_CLIP' }, function (resp) {
        if (chrome.runtime.lastError) {
          // 需要先注入 content script
          sendMsg({ type: 'INJECT_SCRIPT', payload: { tabId: tabId } }).then(function (injectResp) {
            if (injectResp && injectResp.success) {
              setTimeout(function () {
                chrome.tabs.sendMessage(tabId, { type: 'SMART_CLIP' }, function () {
                  void chrome.runtime.lastError;
                });
              }, 600);
            }
          });
        }
      });
      toast('正在智能提取…', 'success');
    });
  });

  $('btn-select-clip').addEventListener('click', function () {
    chrome.tabs.query({ active: true, currentWindow: true }, function (tabs) {
      if (!tabs || !tabs[0]) return;
      var tab = tabs[0];
      var tabId = tab.id;

      // 检查是否为不支持注入的页面
      var url = tab.url || '';
      if (url.startsWith('chrome://') || url.startsWith('chrome-extension://') || url.startsWith('edge://') || url.startsWith('about:') || url === '' || url.startsWith('chrome:')) {
        toast('此页面不支持剪藏功能', 'error');
        return;
      }

      // 先关闭 popup，避免 popup 关闭时切断消息通道
      // 通过 background 转发消息到 content script
      chrome.tabs.sendMessage(tabId, { type: 'SELECT_CLIP' }, function (resp) {
        if (chrome.runtime.lastError) {
          // content script 未注入，先注入再发消息
          sendMsg({ type: 'INJECT_SCRIPT', payload: { tabId: tabId } }).then(function (injectResp) {
            if (injectResp && injectResp.success) {
              setTimeout(function () {
                chrome.tabs.sendMessage(tabId, { type: 'SELECT_CLIP' }, function () {
                  // 忽略可能的错误
                  void chrome.runtime.lastError;
                });
              }, 600);
            }
          });
        }
      });
      // 延迟一点关闭，让消息先发出去
      setTimeout(function () { window.close(); }, 150);
    });
  });

  // === 轻量级 Markdown → HTML 渲染 ===
  function escapeHtml(s) {
    var d = document.createElement('div');
    d.textContent = s || '';
    return d.innerHTML;
  }

  function renderMarkdown(text) {
    if (!text) return '';
    var html = text;
    html = html.replace(/```(\w*)\n([\s\S]*?)```/g, function (_, lang, code) {
      return '<pre><code>' + escapeHtml(code.replace(/\n$/, '')) + '</code></pre>';
    });
    html = html.replace(/`([^`]+)`/g, '<code>$1</code>');
    html = html.replace(/((?:\|.+\|\n)+)/g, function (tableBlock) {
      var rows = tableBlock.trim().split('\n');
      if (rows.length < 2) return tableBlock;
      var out = '<table>';
      rows.forEach(function (row, i) {
        if (/^\|[\s\-:|]+\|$/.test(row)) return;
        var tag = i === 0 ? 'th' : 'td';
        var cells = row.split('|').filter(function (c, ci, arr) { return ci > 0 && ci < arr.length - 1; });
        out += '<tr>';
        cells.forEach(function (cell) { out += '<' + tag + '>' + cell.trim() + '</' + tag + '>'; });
        out += '</tr>';
      });
      out += '</table>';
      return out;
    });
    var lines = html.split('\n');
    var result = [];
    var inList = false;
    var listType = '';
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i];
      if (line.indexOf('<pre>') !== -1 || line.indexOf('<table>') !== -1) {
        if (inList) { result.push('</' + listType + '>'); inList = false; }
        result.push(line); continue;
      }
      if (/^####\s+(.+)/.test(line)) { if (inList) { result.push('</' + listType + '>'); inList = false; } result.push('<h4>' + RegExp.$1 + '</h4>'); continue; }
      if (/^###\s+(.+)/.test(line)) { if (inList) { result.push('</' + listType + '>'); inList = false; } result.push('<h3>' + RegExp.$1 + '</h3>'); continue; }
      if (/^##\s+(.+)/.test(line)) { if (inList) { result.push('</' + listType + '>'); inList = false; } result.push('<h2>' + RegExp.$1 + '</h2>'); continue; }
      if (/^#\s+(.+)/.test(line)) { if (inList) { result.push('</' + listType + '>'); inList = false; } result.push('<h1>' + RegExp.$1 + '</h1>'); continue; }
      if (/^---+$/.test(line.trim())) { if (inList) { result.push('</' + listType + '>'); inList = false; } result.push('<hr>'); continue; }
      if (/^>\s*(.*)/.test(line)) { if (inList) { result.push('</' + listType + '>'); inList = false; } result.push('<blockquote>' + RegExp.$1 + '</blockquote>'); continue; }
      if (/^[\-\*]\s+(.+)/.test(line)) {
        if (!inList || listType !== 'ul') { if (inList) result.push('</' + listType + '>'); result.push('<ul>'); inList = true; listType = 'ul'; }
        result.push('<li>' + RegExp.$1 + '</li>'); continue;
      }
      if (/^\d+\.\s+(.+)/.test(line)) {
        if (!inList || listType !== 'ol') { if (inList) result.push('</' + listType + '>'); result.push('<ol>'); inList = true; listType = 'ol'; }
        result.push('<li>' + RegExp.$1 + '</li>'); continue;
      }
      if (inList) { result.push('</' + listType + '>'); inList = false; }
      if (line.trim() === '') continue;
      result.push('<p>' + line + '</p>');
    }
    if (inList) result.push('</' + listType + '>');
    html = result.join('\n');
    html = html.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
    html = html.replace(/\*(.+?)\*/g, '<em>$1</em>');
    html = html.replace(/!\[([^\]]*)\]\(([^)]+)\)/g, '<img src="$2" alt="$1">');
    html = html.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank">$1</a>');
    return html;
  }

  // === Markdown 速记 ===
  var noteInput = $('note-input');
  var notePreview = $('note-preview');
  var noteSaveBtn = $('btn-note-save');
  var previewBtn = $('btn-note-preview');
  var isPreviewMode = false;

  // === Markdown 工具栏 ===
  // 在 textarea 中插入文本的辅助函数
  function insertMd(before, after, placeholder) {
    noteInput.focus();
    var start = noteInput.selectionStart;
    var end = noteInput.selectionEnd;
    var text = noteInput.value;
    var selected = text.substring(start, end);
    var insert = selected || placeholder || '';
    var newText = text.substring(0, start) + before + insert + (after || '') + text.substring(end);
    noteInput.value = newText;
    // 设置光标位置
    if (selected) {
      noteInput.selectionStart = start + before.length;
      noteInput.selectionEnd = start + before.length + insert.length;
    } else {
      noteInput.selectionStart = start + before.length;
      noteInput.selectionEnd = start + before.length + insert.length;
    }
    noteInput.dispatchEvent(new Event('input'));
  }

  // 在行首插入文本的辅助函数
  function insertLinePrefix(prefix) {
    noteInput.focus();
    var start = noteInput.selectionStart;
    var text = noteInput.value;
    // 找到当前行的开头
    var lineStart = text.lastIndexOf('\n', start - 1) + 1;
    var newText = text.substring(0, lineStart) + prefix + text.substring(lineStart);
    noteInput.value = newText;
    noteInput.selectionStart = noteInput.selectionEnd = start + prefix.length;
    noteInput.dispatchEvent(new Event('input'));
  }

  // 绑定所有工具栏按钮
  document.querySelectorAll('.note-tool-btn[data-md]').forEach(function (btn) {
    btn.addEventListener('click', function (e) {
      e.preventDefault();
      var action = btn.getAttribute('data-md');
      switch (action) {
        case 'bold':
          insertMd('**', '**', '粗体文本');
          break;
        case 'heading':
          insertLinePrefix('## ');
          break;
        case 'ul':
          insertLinePrefix('- ');
          break;
        case 'ol':
          insertLinePrefix('1. ');
          break;
        case 'link':
          insertMd('[', '](url)', '链接文本');
          break;
        case 'image':
          insertMd('![', '](url)', '图片描述');
          break;
        case 'preview':
          var content = noteInput.value.trim();
          if (!content && !isPreviewMode) { toast('请先输入内容'); return; }
          isPreviewMode = !isPreviewMode;
          if (isPreviewMode) {
            notePreview.innerHTML = renderMarkdown(content);
            noteInput.style.display = 'none';
            notePreview.style.display = 'block';
            previewBtn.classList.add('active');
          } else {
            noteInput.style.display = '';
            notePreview.style.display = 'none';
            previewBtn.classList.remove('active');
            noteInput.focus();
          }
          break;
      }
    });
  });

  noteSaveBtn.addEventListener('click', function () {
    var text = noteInput.value.trim();
    if (!text) {
      toast('请先输入内容');
      return;
    }
    // 提取第一行作为标题
    var firstLine = text.split('\n')[0].replace(/^#+\s*/, '').trim() || '速记';
    var clip = {
      type: 'markdown',
      content: text,
      title: firstLine
    };
    // 如果是编辑已有笔记，带上原始 id
    if (editingClipId) {
      clip.id = editingClipId;
    }

    noteSaveBtn.disabled = true;

    // 通过 background service worker 保存，避免 popup 关闭中断异步操作
    sendMsg({ type: 'SAVE_CLIP', payload: clip }).then(function (resp) {
      noteSaveBtn.disabled = false;
      if (resp && resp.success) {
        toast('笔记已保存', 'success');
        noteInput.value = '';
        editingClipId = null;
        // 退出预览模式
        if (isPreviewMode) {
          isPreviewMode = false;
          noteInput.style.display = '';
          notePreview.style.display = 'none';
          notePreview.innerHTML = '';
          previewBtn.classList.remove('active');
        }
        // 刷新最新内容卡片
        loadLatestClip();
        // 翻转回正面
        setTimeout(function () {
          flipToFront();
        }, 300);
      } else {
        toast('保存失败: ' + ((resp && resp.error) || '未知错误'), 'error');
      }
    });
  });

  // === 三点菜单 ===
  var moreMenu = $('more-menu');
  $('btn-more').addEventListener('click', function (e) {
    e.stopPropagation();
    moreMenu.classList.toggle('show');
  });

  // === 设置面板 ===
  $('btn-settings').addEventListener('click', function () {
    moreMenu.classList.remove('show');
    $('settings-overlay').classList.add('show');
  });

  $('settings-overlay').addEventListener('click', function (e) {
    if (e.target === $('settings-overlay')) {
      $('settings-overlay').classList.remove('show');
    }
  });

  // === 主页按钮 ===
  $('btn-home').addEventListener('click', function () {
    moreMenu.classList.remove('show');
    // 打开 WeKnora 管理页面，弹窗保留
    if (chrome && chrome.tabs) {
      chrome.tabs.create({ url: chrome.runtime.getURL('weknora.html') });
    } else {
      goPage('pg-login');
    }
  });

  // === 查看收藏按钮 ===
  $('btn-clips').addEventListener('click', function () {
    moreMenu.classList.remove('show');
    if (chrome && chrome.tabs) {
      chrome.tabs.create({ url: chrome.runtime.getURL('clips.html') });
    }
  });

  // === 最新内容卡片 — 查看全部 ===
  $('btn-latest-more').addEventListener('click', function (e) {
    e.stopPropagation();
    if (chrome && chrome.tabs) {
      chrome.tabs.create({ url: chrome.runtime.getURL('clips.html') });
    }
  });

  // 点击最新内容卡片本身也打开列表页；双击翻转进入速记编辑
  (function () {
    var clickTimer = null;
    $('latest-clip').addEventListener('click', function (e) {
      // 如果点击的是「查看全部」按钮，不处理
      if (e.target.closest('#btn-latest-more')) return;
      if (clickTimer) {
        // 双击：取消单击，翻转卡片并带入当前笔记内容
        clearTimeout(clickTimer);
        clickTimer = null;
        flipToNote(latestClipData);
      } else {
        // 单击：延迟执行，等看有没有双击
        clickTimer = setTimeout(function () {
          clickTimer = null;
          if (chrome && chrome.tabs) {
            chrome.tabs.create({ url: chrome.runtime.getURL('clips.html') });
          }
        }, 250);
      }
    });
  })();

  // === 退出登录（在设置面板内） ===
  $('btn-logout').addEventListener('click', function () {
    $('settings-overlay').classList.remove('show');
    sendMsg({ type: 'CLEAR_AUTH' }).then(function () {
      currentUser = null;
      goPage('pg-login');
      toast('已退出登录');
    });
  });

  // === 监听 storage 变化，实时刷新最新内容 ===
  // 当其他页面（content.js 网页保存、sidepanel 等）修改了数据时，popup 自动刷新
  if (chrome && chrome.storage && chrome.storage.onChanged) {
    chrome.storage.onChanged.addListener(function (changes, area) {
      if (area === 'local' && (changes.ka_clips || changes.ka_notes)) {
        // 只在主界面可见时刷新
        if (currentUser && $('pg-main').classList.contains('active') && !isFlipped) {
          loadLatestClip();
        }
      }
    });
  }

  // === 初始化：检查已登录状态 ===
  (function init() {
    sendMsg({ type: 'GET_AUTH' }).then(function (resp) {
      if (resp && resp.success && resp.data) {
        var auth = resp.data;
        if (auth.type === 'ka') {
          enterMain('ka', auth.name || '快速笔记用户', auth.avatar || '', '快速笔记 5.0');
        } else if (auth.type === 'wk') {
          enterMain('wk', auth.name || 'Saras', auth.avatar || '', 'WeKnora');
        }
      }
    });
  })();
})();
