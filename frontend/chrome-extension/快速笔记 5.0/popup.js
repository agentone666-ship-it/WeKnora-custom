(function () {
  'use strict';

  // === State ===
  var currentUser = null; // { type: 'ka'|'wk', name, avatar, badge }
  var clipKbId = '';      // 剪藏目标知识库 ID
  var clipKbName = '';    // 剪藏目标知识库名称

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

    // 先保存配置
    sendMsg({ type: 'SET_CONFIG', payload: { baseUrl: url, apiKey: key } }).then(function () {
      // 通过调用知识库列表来验证 API Key 和服务地址是否有效
      return sendMsg({ type: 'VALIDATE_CONFIG' });
    }).then(function (resp) {
      // VALIDATE_CONFIG 现在实际调用 GET /knowledge-bases，成功说明连通且认证有效
      if (resp && resp.success) {
        // API Key 认证成功，不需要调用 /auth/me（那个需要 Bearer token）
        sendMsg({ type: 'SET_AUTH', payload: { type: 'ka', name: '快速笔记用户', avatar: kaAvatarUrl } });
        msg.textContent = '验证通过';
        msg.className = 'ka-test-msg ok';
        setTimeout(function () {
          enterMain('ka', '快速笔记用户', kaAvatarUrl, '快速笔记 5.0');
          toast('登录成功', 'success');
        }, 500);
      } else {
        msg.textContent = '验证未通过，请检查服务地址和 API Key';
        msg.className = 'ka-test-msg err';
      }
    });
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
      // 验证连接
      return sendMsg({ type: 'VALIDATE_CONFIG' });
    }).then(function (resp) {
      if (resp && resp.success) {
        // API Key 验证成功
        sendMsg({ type: 'SET_AUTH', payload: { type: 'wk', name: 'WeKnora 用户', avatar: '' } });
        $('wk-test-msg').textContent = '验证通过，配置已保存';
        $('wk-test-msg').className = 'wk-test-result ok';
        setTimeout(function () {
          enterMain('wk', 'WeKnora 用户', '', 'WeKnora');
        }, 500);
      } else {
        $('wk-test-msg').textContent = '验证未通过，请检查服务地址和 API Key';
        $('wk-test-msg').className = 'wk-test-result err';
      }
    });
  });

  // === 更新用户信息显示 ===
  function updateUserDisplay(type, name, avatarUrl, badge) {
    if (avatarUrl) {
      $('main-avatar-img').src = avatarUrl;
      $('main-avatar-img').style.display = 'block';
      $('main-letter').style.display = 'none';
    } else {
      $('main-avatar-img').style.display = 'none';
      $('main-letter').style.display = 'flex';
      $('main-letter').textContent = (name || 'U').charAt(0).toUpperCase();
      $('main-letter').style.background = '#07C160';
    }
    $('main-name').textContent = name;
    $('main-badge').textContent = badge;
    $('main-badge').className = 'main-badge ' + (type === 'ka' ? 'badge-ka' : 'badge-wk');
  }

  // === 进入主界面 ===
  function enterMain(type, name, avatarUrl, badge) {
    currentUser = { type: type, name: name, avatar: avatarUrl, badge: badge };
    updateUserDisplay(type, name, avatarUrl, badge);
    // 从 API 获取真实用户信息
    sendMsg({ type: 'GET_USER_INFO' }).then(function (resp) {
      if (resp && resp.success && resp.data) {
        var user = resp.data.user || resp.data;
        var realName = user.username || user.name || name;
        var realAvatar = user.avatar || avatarUrl;
        currentUser.name = realName;
        currentUser.avatar = realAvatar;
        // 同步更新 auth 存储
        sendMsg({ type: 'SET_AUTH', payload: { type: type, name: realName, avatar: realAvatar } });
        updateUserDisplay(type, realName, realAvatar, badge);
      }
    });
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
    loadAgentsForDropdown();
  }

  // === 从 API 加载知识库列表到下拉菜单 ===
  function loadKnowledgeBasesForDropdown() {
    sendMsg({ type: 'LIST_KNOWLEDGE_BASES' }).then(function (resp) {
      if (resp && resp.success && resp.data) {
        var kbList = Array.isArray(resp.data) ? resp.data : (resp.data.items || []);
        popupAllKbs = kbList;
        filterAndRenderPopupKbs();
        // 检查剪藏知识库选择
        checkClipKbSelection(kbList);
      }
    });
  }

  var popupAllKbs = [];
  var popupAgents = [];

  // === 根据当前选中智能体配置过滤知识库 ===
  function filterAndRenderPopupKbs() {
    var filtered = popupAllKbs;
    var agent = null;
    for (var i = 0; i < popupAgents.length; i++) {
      if (popupAgents[i].id === popupAgentId) { agent = popupAgents[i]; break; }
    }
    if (agent && agent.config) {
      var mode = agent.config.kb_selection_mode || 'all';
      if (mode === 'none') {
        filtered = [];
      } else if (mode === 'selected' && agent.config.knowledge_bases) {
        var allowedIds = agent.config.knowledge_bases;
        filtered = popupAllKbs.filter(function (kb) {
          return allowedIds.indexOf(kb.id) !== -1;
        });
      }
    }
    renderKbDropdownItems(filtered);
  }

  // === 从 API 加载智能体列表到模式下拉 ===
  function loadAgentsForDropdown() {
    sendMsg({ type: 'LIST_AGENTS' }).then(function (resp) {
      if (resp && resp.success && resp.data) {
        popupAgents = Array.isArray(resp.data) ? resp.data : (resp.data.data || []);
        if (popupAgents.length > 0) {
          // 恢复持久化的模式选择
          chrome.storage.local.get('ka_selected_agent', function (stored) {
            if (!popupAgentId && stored && stored.ka_selected_agent && stored.ka_selected_agent.agentId) {
              var saved = stored.ka_selected_agent;
              for (var i = 0; i < popupAgents.length; i++) {
                if (popupAgents[i].id === saved.agentId) {
                  popupAgentId = saved.agentId;
                  popupAgentEnabled = !!saved.agentEnabled;
                  break;
                }
              }
            }
            if (!popupAgentId) {
              popupAgentId = popupAgents[0].id;
              var isQA = popupAgents[0].id === 'builtin-quick-answer' || (popupAgents[0].config && popupAgents[0].config.agent_mode === 'quick-answer');
              popupAgentEnabled = !isQA;
            }
            // 设置当前选中智能体的图片上传能力
            for (var j = 0; j < popupAgents.length; j++) {
              if (popupAgents[j].id === popupAgentId) {
                popupAgentImageUpload = !!(popupAgents[j].config && popupAgents[j].config.image_upload_enabled);
                break;
              }
            }
            popupUpdateImageUI();
            renderAgentModeItems(popupAgents);
            // 加载全部知识库（前端根据智能体配置过滤）
            loadKnowledgeBasesForDropdown();
          });
        } else {
          loadKnowledgeBasesForDropdown();
        }
      } else {
        loadKnowledgeBasesForDropdown();
      }
    });
  }

  function renderAgentModeItems(agentList) {
    var modeMenu = $('popup-mode-menu');
    if (!modeMenu) return;
    // 移除旧的模式选项
    var oldItems = modeMenu.querySelectorAll('.kb-mode-item');
    oldItems.forEach(function (item) { item.remove(); });

    agentList.forEach(function (agent, idx) {
      var item = document.createElement('div');
      var isQA = agent.id === 'builtin-quick-answer' || (agent.config && agent.config.agent_mode === 'quick-answer');
      var isSelected = popupAgentId ? (agent.id === popupAgentId) : (idx === 0);
      item.className = 'kb-mode-item' + (isSelected ? ' selected' : '');
      item.setAttribute('data-agent-id', agent.id);
      item.innerHTML = '<span class="kb-radio"></span> ' + (function (s) {
        var d = document.createElement('div'); d.textContent = s; return d.innerHTML;
      })(agent.name);
      if (isSelected) {
        $('mode-name').textContent = agent.name;
      }
      item.addEventListener('click', function (e) {
        e.stopPropagation();
        popupAgentId = agent.id;
        popupAgentEnabled = !isQA;
        modeMenu.querySelectorAll('.kb-mode-item').forEach(function (i) { i.classList.remove('selected'); });
        item.classList.add('selected');
        $('mode-name').textContent = agent.name;
        modeMenu.classList.remove('show');
        // 持久化模式选择，同步给 sidepanel / weknora
        chrome.storage.local.set({ ka_selected_agent: { agentId: agent.id, agentEnabled: !isQA } });
        // 更新图片上传按钮可见性
        popupAgentImageUpload = !!(agent.config && agent.config.image_upload_enabled);
        popupUpdateImageUI();
        if (!popupAgentImageUpload && popupPendingImages.length > 0) popupClearImages();
        // 切换智能体后根据配置过滤知识库
        filterAndRenderPopupKbs();
      });
      modeMenu.appendChild(item);
    });
  }

  function renderKbDropdownItems(kbList) {
    var kbMenu = $('kb-menu');
    if (!kbMenu) return;
    // 移除旧的知识库选项（保留分隔线、模式等）
    var oldItems = kbMenu.querySelectorAll('.kb-dropdown-item');
    oldItems.forEach(function (item) { item.remove(); });
    // 在第一个分隔线之前插入新选项
    var firstDivider = kbMenu.querySelector('.kb-dropdown-divider');
    // 插入 "全部" 选项
    var allItem = createKbDropdownItem('all', '全部知识库', true);
    kbMenu.insertBefore(allItem, firstDivider);
    // 插入真实知识库
    kbList.forEach(function (kb) {
      var item = createKbDropdownItem(kb.id, kb.name, false);
      kbMenu.insertBefore(item, firstDivider);
    });
  }

  function createKbDropdownItem(kbId, name, isSelected) {
    var div = document.createElement('div');
    div.className = 'kb-dropdown-item' + (isSelected ? ' selected' : '');
    div.setAttribute('data-kb', kbId);
    div.innerHTML = '<span class="kb-radio"></span> ' + (function (s) {
      var d = document.createElement('div');
      d.textContent = s;
      return d.innerHTML;
    })(name);
    div.addEventListener('click', function (e) {
      e.stopPropagation();
      selectedKb = kbId;
      $('kb-name').textContent = name.length > 4 ? name.substring(0, 4) : name;
      $('kb-menu').querySelectorAll('.kb-dropdown-item').forEach(function (i) { i.classList.remove('selected'); });
      div.classList.add('selected');
      $('kb-menu').classList.remove('show');
    });
    return div;
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
  var popupAgentId = '';
  var popupAgentEnabled = false;
  var popupAgentImageUpload = false;

  // 图片上传
  var popupPendingImages = [];
  var POPUP_MAX_IMAGES = 5;
  var POPUP_ALLOWED_TYPES = ['image/jpeg', 'image/png', 'image/gif', 'image/webp'];
  var POPUP_MAX_IMAGE_SIZE = 10 * 1024 * 1024;
  var popupImageInput = $('popup-image-input');
  var popupImageBtn = $('popup-image-btn');
  var popupImagePreviews = $('popup-image-previews');

  function popupAddImages(files) {
    if (!popupAgentImageUpload) return;
    for (var i = 0; i < files.length; i++) {
      if (popupPendingImages.length >= POPUP_MAX_IMAGES) { break; }
      var f = files[i];
      if (POPUP_ALLOWED_TYPES.indexOf(f.type) === -1 || f.size > POPUP_MAX_IMAGE_SIZE) continue;
      popupPendingImages.push({ file: f, preview: URL.createObjectURL(f) });
    }
    popupRenderPreviews();
  }

  function popupRemoveImage(idx) {
    if (idx >= 0 && idx < popupPendingImages.length) {
      URL.revokeObjectURL(popupPendingImages[idx].preview);
      popupPendingImages.splice(idx, 1);
    }
    popupRenderPreviews();
  }

  function popupClearImages() {
    popupPendingImages.forEach(function (img) { URL.revokeObjectURL(img.preview); });
    popupPendingImages = [];
    popupRenderPreviews();
  }

  function popupRenderPreviews() {
    if (!popupImagePreviews) return;
    if (popupPendingImages.length === 0) {
      popupImagePreviews.style.display = 'none';
      popupImagePreviews.innerHTML = '';
      return;
    }
    popupImagePreviews.style.display = 'flex';
    var html = '';
    for (var i = 0; i < popupPendingImages.length; i++) {
      html += '<div class="popup-img-item" data-idx="' + i + '">'
        + '<img class="popup-img-thumb" src="' + popupPendingImages[i].preview + '">'
        + '<span class="popup-img-remove">&times;</span></div>';
    }
    popupImagePreviews.innerHTML = html;
    popupImagePreviews.querySelectorAll('.popup-img-remove').forEach(function (btn) {
      btn.addEventListener('click', function () {
        popupRemoveImage(parseInt(btn.parentElement.getAttribute('data-idx')));
      });
    });
  }

  function popupUpdateImageUI() {
    if (popupImageBtn) popupImageBtn.style.display = popupAgentImageUpload ? '' : 'none';
    if (!popupAgentImageUpload && popupPendingImages.length > 0) popupClearImages();
  }

  function popupFileToBase64(file) {
    return new Promise(function (resolve, reject) {
      var reader = new FileReader();
      reader.onload = function () { resolve(reader.result); };
      reader.onerror = reject;
      reader.readAsDataURL(file);
    });
  }

  if (popupImageInput) {
    popupImageInput.addEventListener('change', function () {
      if (popupImageInput.files) popupAddImages(Array.from(popupImageInput.files));
      popupImageInput.value = '';
    });
  }
  if (popupImageBtn) {
    popupImageBtn.addEventListener('click', function () {
      if (popupImageInput) popupImageInput.click();
    });
  }

  chatInput.addEventListener('paste', function (e) {
    if (!popupAgentImageUpload) return;
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
      popupAddImages(imageFiles);
    }
  });

  chatInput.addEventListener('input', function () {
    sendBtn.classList.toggle('active', chatInput.value.trim().length > 0);
    // 自动调整高度
    chatInput.style.height = 'auto';
    chatInput.style.height = Math.min(chatInput.scrollHeight, 100) + 'px';
  });

  chatInput.addEventListener('keydown', function (e) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      sendChat();
    }
    // Shift+Enter 默认行为即换行，无需额外处理
  });

  sendBtn.addEventListener('click', sendChat);

  function sendChat() {
    var text = chatInput.value.trim();
    if (!text) return;
    var queryData = { query: text, user: currentUser, kb: selectedKb, ts: Date.now(), agentId: popupAgentId, agentEnabled: popupAgentEnabled };
    // 如果选中了具体知识库，传递知识库 ID
    if (selectedKb && selectedKb !== 'all') {
      queryData.knowledgeBaseIds = [selectedKb];
    }

    function doPopupSend(data) {
      if (chrome && chrome.storage) {
        chrome.storage.local.set({ ka_pending_query: data });
      }
      if (chrome && chrome.runtime && chrome.runtime.sendMessage) {
        chrome.runtime.sendMessage({ type: 'CHAT_QUERY', payload: data }).catch(function () {});
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

    if (popupPendingImages.length > 0) {
      var promises = popupPendingImages.map(function (img) { return popupFileToBase64(img.file); });
      Promise.all(promises).then(function (dataURIs) {
        queryData.images = dataURIs.map(function (d) { return { data: d }; });
        popupClearImages();
        doPopupSend(queryData);
      });
    } else {
      doPopupSend(queryData);
    }
  }

  // === 知识库下拉 ===
  var kbMenu = $('kb-menu');
  var kbBtn = $('btn-kb-select');

  function positionKbMenu() {
    var rect = kbBtn.getBoundingClientRect();
    var menuH = kbMenu.offsetHeight;
    var top = rect.top - menuH - 6;
    if (top < 4) top = rect.bottom + 6;
    kbMenu.style.left = rect.left + 'px';
    kbMenu.style.top = top + 'px';
  }

  kbBtn.addEventListener('click', function (e) {
    e.stopPropagation();
    popupModeMenu.classList.remove('show');
    var willShow = !kbMenu.classList.contains('show');
    kbMenu.classList.toggle('show');
    if (willShow) {
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

  // === 模式下拉（独立） ===
  var popupModeMenu = $('popup-mode-menu');
  var modeBtn = $('btn-mode-select');

  function positionModeMenu() {
    var rect = modeBtn.getBoundingClientRect();
    var menuH = popupModeMenu.offsetHeight;
    var top = rect.top - menuH - 6;
    if (top < 4) top = rect.bottom + 6;
    popupModeMenu.style.left = rect.left + 'px';
    popupModeMenu.style.top = top + 'px';
  }

  modeBtn.addEventListener('click', function (e) {
    e.stopPropagation();
    kbMenu.classList.remove('show');
    var willShow = !popupModeMenu.classList.contains('show');
    popupModeMenu.classList.toggle('show');
    if (willShow) {
      requestAnimationFrame(positionModeMenu);
    }
  });

  popupModeMenu.addEventListener('click', function (e) {
    e.stopPropagation();
  });

  // 点击其他地方关闭下拉
  document.addEventListener('click', function () {
    kbMenu.classList.remove('show');
    popupModeMenu.classList.remove('show');
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

    // 1. 提取代码块，替换为占位符
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

    // 3. 表格处理 — 提取为占位符
    var tables = [];
    processed = processed.replace(/((?:\|.+\|\n)+)/g, function (tableBlock) {
      var rows = tableBlock.trim().split('\n');
      if (rows.length < 2) return tableBlock;
      var out = '<table class="md-table">';
      rows.forEach(function (row, i) {
        if (/^\|[\s\-:|]+\|$/.test(row)) return;
        var tag = i === 0 ? 'th' : 'td';
        var cells = row.split('|').filter(function (c, ci, arr) { return ci > 0 && ci < arr.length - 1; });
        out += '<tr>';
        cells.forEach(function (cell) {
          out += '<' + tag + '>' + cell.trim() + '</' + tag + '>';
        });
        out += '</tr>';
      });
      out += '</table>';
      var idx = tables.length;
      tables.push(out);
      return '\n\x00TABLE' + idx + '\x00\n';
    });

    // 4. 逐行解析为块元素
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
        html += tables[parseInt(tbMatch[1])];
        i++; continue;
      }

      // 标题
      if (/^(#{1,6})\s+(.+)/.test(line)) {
        var level = RegExp.$1.length;
        html += '<h' + level + ' class="md-h">' + inlineFormat(RegExp.$2) + '</h' + level + '>';
        i++; continue;
      }

      // 引用块
      if (/^>\s*(.*)/.test(line)) {
        var bqLines = [];
        while (i < lines.length && /^>\s*(.*)/.test(lines[i])) {
          bqLines.push(RegExp.$1);
          i++;
        }
        html += '<blockquote class="md-bq">' + inlineFormat(bqLines.join('<br>')) + '</blockquote>';
        continue;
      }

      // 无序列表
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

      // 普通段落
      var pLines = [];
      while (i < lines.length) {
        var pl = lines[i];
        if (pl.trim() === '' || /^#{1,6}\s/.test(pl) || /^>\s/.test(pl) ||
            /^(\s*)[*\-]\s+/.test(pl) || /^(\s*)\d+\.\s+/.test(pl) ||
            /^\x00CODEBLOCK/.test(pl) || /^\x00TABLE/.test(pl) ||
            /^[-*_]{3,}\s*$/.test(pl.trim())) break;
        pLines.push(pl);
        i++;
      }
      html += '<p class="md-p">' + inlineFormat(pLines.join('<br>')) + '</p>';
    }

    // 5. 恢复行内代码占位符
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
      // 图片（必须在链接之前）
      s = s.replace(/!\[([^\]]*)\]\(([^)]+)\)/g, '<img src="$2" alt="$1" style="max-width:100%;border-radius:6px;margin:4px 0;">');
      // 链接
      s = s.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>');
      s = s.replace(/\*\*\*(.+?)\*\*\*/g, '<strong><em>$1</em></strong>');
      s = s.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
      s = s.replace(/(?<!\*)\*([^\s*][^*]*?)\*(?!\*)/g, '<em>$1</em>');
      s = s.replace(/~~(.+?)~~/g, '<del>$1</del>');
      s = s.replace(/\x00INLINE(\d+)\x00/g, function (_, idx) {
        return inlineCodes[parseInt(idx)];
      });
      return s;
    }
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

  $('btn-settings-close').addEventListener('click', function () {
    $('settings-overlay').classList.remove('show');
  });

  // === 剪藏知识库选择 ===
  var clipKbOverlay = $('clip-kb-overlay');

  // 检查是否需要引导用户选择剪藏知识库
  function checkClipKbSelection(kbList) {
    chrome.storage.local.get(['clipKbId', 'clipKbName'], function (data) {
      clipKbId = data.clipKbId || '';
      clipKbName = data.clipKbName || '';

      // 验证保存的 KB 是否仍存在
      if (clipKbId) {
        var found = kbList.some(function (kb) { return kb.id === clipKbId; });
        if (!found) {
          clipKbId = '';
          clipKbName = '';
          chrome.storage.local.remove(['clipKbId', 'clipKbName']);
        }
      }

      // 更新设置面板显示
      updateClipKbDisplay();

      // 首次使用 — 引导选择
      if (!clipKbId) {
        showClipKbSelector(kbList);
      }
    });
  }

  function updateClipKbDisplay() {
    var el = $('set-clip-kb');
    if (el) {
      el.textContent = clipKbName ? (clipKbName + ' ›') : '未选择 ›';
    }
  }

  function showClipKbSelector(kbList) {
    var list = kbList || popupAllKbs;
    var listEl = $('clip-kb-list');
    var emptyEl = $('clip-kb-empty');

    if (!list || list.length === 0) {
      listEl.style.display = 'none';
      emptyEl.style.display = 'block';
    } else {
      listEl.style.display = 'block';
      emptyEl.style.display = 'none';
      var html = '';
      for (var i = 0; i < list.length; i++) {
        var kb = list[i];
        var selected = kb.id === clipKbId ? ' selected' : '';
        html += '<div class="clip-kb-card' + selected + '" data-kb-id="' + kb.id + '" data-kb-name="' + escapeHtml(kb.name) + '">'
          + '<div class="clip-kb-card-name">' + escapeHtml(kb.name) + '</div>'
          + '<div class="clip-kb-card-desc">' + escapeHtml(kb.description || '暂无描述') + '</div>'
          + '<div class="clip-kb-card-meta">' + (kb.document_count || 0) + ' 文档 · ' + (kb.chunk_count || 0) + ' 分段</div>'
          + '</div>';
      }
      listEl.innerHTML = html;

      // 点击选择
      listEl.querySelectorAll('.clip-kb-card').forEach(function (card) {
        card.addEventListener('click', function () {
          clipKbId = card.getAttribute('data-kb-id');
          clipKbName = card.getAttribute('data-kb-name');
          chrome.storage.local.set({ clipKbId: clipKbId, clipKbName: clipKbName });
          updateClipKbDisplay();
          clipKbOverlay.classList.remove('show');
          toast('已选择知识库: ' + clipKbName, 'success');
        });
      });
    }

    clipKbOverlay.classList.add('show');
  }

  // 设置面板中点击"剪藏知识库"
  $('set-clip-kb-row').addEventListener('click', function () {
    $('settings-overlay').classList.remove('show');
    showClipKbSelector();
  });

  // 关闭按钮
  $('clip-kb-close').addEventListener('click', function () {
    clipKbOverlay.classList.remove('show');
  });

  // 背景点击关闭
  clipKbOverlay.addEventListener('click', function (e) {
    if (e.target === clipKbOverlay) {
      clipKbOverlay.classList.remove('show');
    }
  });

  // "前往创建"按钮 — 打开 WeKnora 主站
  $('clip-kb-goto').addEventListener('click', function () {
    clipKbOverlay.classList.remove('show');
    sendMsg({ type: 'GET_CONFIG' }).then(function (resp) {
      if (resp && resp.success && resp.data && resp.data.baseUrl) {
        var baseUrl = resp.data.baseUrl.replace(/\/api\/v1\/?$/, '');
        if (chrome && chrome.tabs) {
          chrome.tabs.create({ url: baseUrl });
        }
      } else {
        toast('请先配置服务地址', 'error');
      }
    });
  });

  // === 打开对话面板（sidebar）===
  $('btn-sidebar').addEventListener('click', function () {
    moreMenu.classList.remove('show');
    if (chrome && chrome.tabs) {
      chrome.tabs.query({ active: true, currentWindow: true }, function (tabs) {
        if (tabs && tabs[0]) {
          chrome.sidePanel.open({ tabId: tabs[0].id }).catch(function () {});
        }
        window.close();
      });
    }
  });

  // === 打开笔记/知识库页面 ===
  function openNotesPage() {
    if (!currentUser || !chrome || !chrome.tabs) return;
    if (currentUser.type === 'wk') {
      // 自部署 WeKnora — 用配置的 baseUrl
      sendMsg({ type: 'GET_CONFIG' }).then(function (resp) {
        if (resp && resp.success && resp.data && resp.data.baseUrl) {
          var url = resp.data.baseUrl.replace(/\/api\/v1\/?$/, '');
          chrome.tabs.create({ url: url });
        } else {
          toast('请先配置服务地址', 'error');
        }
      });
    } else {
      // 知识助手官网用户
      chrome.tabs.create({ url: 'https://weknora.weixin.qq.com' });
    }
  }

  // === 查看笔记按钮 ===
  $('btn-clips').addEventListener('click', function () {
    moreMenu.classList.remove('show');
    openNotesPage();
  });

  // === 最新内容卡片 — 查看全部 ===
  $('btn-latest-more').addEventListener('click', function (e) {
    e.stopPropagation();
    openNotesPage();
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
          openNotesPage();
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
