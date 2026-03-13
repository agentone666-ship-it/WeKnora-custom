(function () {
  'use strict';

  // 防止重复注入 — 用版本号区分
  var KA_VERSION = 4;
  if (window.__kaContentVersion === KA_VERSION) {
    // 已注入过相同版本，但确保消息监听器仍然存在
    if (typeof window.ensureMessageListener === 'function') {
      window.ensureMessageListener();
    }
    return;
  }
  window.__kaContentVersion = KA_VERSION;

  // === 智能剪藏 ===
  function smartClip() {
    var content = extractMainContent();
    var title = document.title || location.hostname;

    chrome.runtime.sendMessage({
      type: 'SAVE_CLIP',
      payload: {
        type: 'smart-clip',
        content: '## ' + title + '\n\n' + content + '\n\n---\n来源: ' + location.href,
        title: '智能剪藏 - ' + title,
        meta: { url: location.href, title: title }
      }
    }, function (resp) {
      if (resp && resp.success) {
        showNotification('智能剪藏成功', 'success');
        chrome.runtime.sendMessage({
          type: 'CLIP_RESULT',
          payload: { title: '智能剪藏 - ' + title, content: content }
        });
      } else {
        showNotification('剪藏失败', 'error');
      }
    });
  }

  function extractMainContent() {
    var selectors = ['article', 'main', '[role="main"]', '.post-content', '.article-content', '.entry-content', '.content', '#content'];
    var el = null;
    for (var i = 0; i < selectors.length; i++) {
      el = document.querySelector(selectors[i]);
      if (el && el.textContent.trim().length > 100) break;
      el = null;
    }

    if (!el) {
      var paras = document.querySelectorAll('p');
      var texts = [];
      paras.forEach(function (p) {
        var t = p.textContent.trim();
        if (t.length > 30) texts.push(t);
      });
      return texts.join('\n\n') || document.body.innerText.substring(0, 3000);
    }

    var clone = el.cloneNode(true);
    var removes = clone.querySelectorAll('script, style, nav, header, footer, .ads, .ad, .advertisement, .sidebar, .comment, .comments, [role="navigation"], [role="banner"]');
    removes.forEach(function (r) { r.remove(); });

    return clone.textContent.trim().substring(0, 5000);
  }

  // === 选择剪藏 — 截屏式区域框选 ===
  var clipState = {
    active: false,
    overlay: null,
    startX: 0,
    startY: 0,
    rect: null,
    toolbar: null,
    dimTop: null,
    dimBottom: null,
    dimLeft: null,
    dimRight: null
  };

  function startSelectClip() {
    if (clipState.active) return;
    clipState.active = true;

    // 创建全屏遮罩
    var overlay = document.createElement('div');
    overlay.id = 'ka-clip-overlay';
    overlay.innerHTML =
      '<div class="ka-clip-dim ka-clip-dim-top"></div>' +
      '<div class="ka-clip-dim ka-clip-dim-bottom"></div>' +
      '<div class="ka-clip-dim ka-clip-dim-left"></div>' +
      '<div class="ka-clip-dim ka-clip-dim-right"></div>' +
      '<div class="ka-clip-selection"></div>' +
      '<div class="ka-clip-tip">拖拽鼠标框选要截取的区域，松开后确认</div>';
    document.body.appendChild(overlay);
    clipState.overlay = overlay;

    clipState.dimTop = overlay.querySelector('.ka-clip-dim-top');
    clipState.dimBottom = overlay.querySelector('.ka-clip-dim-bottom');
    clipState.dimLeft = overlay.querySelector('.ka-clip-dim-left');
    clipState.dimRight = overlay.querySelector('.ka-clip-dim-right');
    clipState.rect = overlay.querySelector('.ka-clip-selection');

    // 初始暗幕覆盖全屏
    var w = window.innerWidth;
    var h = window.innerHeight;
    setDims(0, 0, w, h);
    clipState.rect.style.display = 'none';

    overlay.addEventListener('mousedown', onClipMouseDown);
    document.addEventListener('keydown', onClipKeyDown);
  }

  function setDims(x, y, x2, y2) {
    var w = window.innerWidth;
    var h = window.innerHeight;
    // 确保有效范围
    var left = Math.min(x, x2);
    var top = Math.min(y, y2);
    var right = Math.max(x, x2);
    var bottom = Math.max(y, y2);

    // 如果选区大小为0，全屏暗幕
    if (right - left < 2 || bottom - top < 2) {
      clipState.dimTop.style.cssText = 'top:0;left:0;width:' + w + 'px;height:' + h + 'px;';
      clipState.dimBottom.style.cssText = 'display:none;';
      clipState.dimLeft.style.cssText = 'display:none;';
      clipState.dimRight.style.cssText = 'display:none;';
      return;
    }

    // 上方暗幕
    clipState.dimTop.style.cssText = 'top:0;left:0;width:' + w + 'px;height:' + top + 'px;';
    // 下方暗幕
    clipState.dimBottom.style.cssText = 'top:' + bottom + 'px;left:0;width:' + w + 'px;height:' + (h - bottom) + 'px;';
    // 左侧暗幕
    clipState.dimLeft.style.cssText = 'top:' + top + 'px;left:0;width:' + left + 'px;height:' + (bottom - top) + 'px;';
    // 右侧暗幕
    clipState.dimRight.style.cssText = 'top:' + top + 'px;left:' + right + 'px;width:' + (w - right) + 'px;height:' + (bottom - top) + 'px;';
  }

  function onClipMouseDown(e) {
    if (e.button !== 0) return;
    // 如果点击在工具栏或 handle 上，不要重新框选
    if (e.target.closest && (e.target.closest('.ka-clip-toolbar') || e.target.closest('.ka-clip-handle'))) return;
    e.preventDefault();
    e.stopPropagation();

    // 移除之前的工具栏和 handles
    if (clipState.toolbar) {
      clipState.toolbar.remove();
      clipState.toolbar = null;
    }
    var oldHandles = clipState.overlay.querySelectorAll('.ka-clip-handle');
    oldHandles.forEach(function (h) { h.remove(); });

    clipState.startX = e.clientX;
    clipState.startY = e.clientY;
    clipState.rect.style.display = 'block';
    clipState.rect.style.left = e.clientX + 'px';
    clipState.rect.style.top = e.clientY + 'px';
    clipState.rect.style.width = '0';
    clipState.rect.style.height = '0';

    // 隐藏提示
    var tip = clipState.overlay.querySelector('.ka-clip-tip');
    if (tip) tip.style.display = 'none';

    document.addEventListener('mousemove', onClipMouseMove);
    document.addEventListener('mouseup', onClipMouseUp);
  }

  function onClipMouseMove(e) {
    e.preventDefault();
    var x = Math.min(e.clientX, clipState.startX);
    var y = Math.min(e.clientY, clipState.startY);
    var w = Math.abs(e.clientX - clipState.startX);
    var h = Math.abs(e.clientY - clipState.startY);

    clipState.rect.style.left = x + 'px';
    clipState.rect.style.top = y + 'px';
    clipState.rect.style.width = w + 'px';
    clipState.rect.style.height = h + 'px';

    setDims(x, y, x + w, y + h);
  }

  function onClipMouseUp(e) {
    document.removeEventListener('mousemove', onClipMouseMove);
    document.removeEventListener('mouseup', onClipMouseUp);

    var x = Math.min(e.clientX, clipState.startX);
    var y = Math.min(e.clientY, clipState.startY);
    var w = Math.abs(e.clientX - clipState.startX);
    var h = Math.abs(e.clientY - clipState.startY);

    if (w < 10 || h < 10) {
      // 太小，忽略
      clipState.rect.style.display = 'none';
      setDims(0, 0, window.innerWidth, window.innerHeight);
      var tip = clipState.overlay.querySelector('.ka-clip-tip');
      if (tip) tip.style.display = 'block';
      return;
    }

    // 允许拖拽调整大小 — 添加 resize handles
    addResizeHandles(x, y, w, h);

    // 显示确认工具栏
    showClipToolbar(x, y, w, h);
  }

  function addResizeHandles(x, y, w, h) {
    // 给选区添加 8 个拖拽点
    var handles = ['nw', 'n', 'ne', 'e', 'se', 's', 'sw', 'w'];
    handles.forEach(function (pos) {
      var handle = document.createElement('div');
      handle.className = 'ka-clip-handle ka-clip-handle-' + pos;
      handle.dataset.pos = pos;
      clipState.rect.appendChild(handle);

      handle.addEventListener('mousedown', function (e) {
        e.preventDefault();
        e.stopPropagation();
        startResize(pos, e);
      });
    });
  }

  function startResize(pos, e) {
    var startX = e.clientX;
    var startY = e.clientY;
    var origLeft = parseInt(clipState.rect.style.left);
    var origTop = parseInt(clipState.rect.style.top);
    var origW = parseInt(clipState.rect.style.width);
    var origH = parseInt(clipState.rect.style.height);

    function onMove(ev) {
      ev.preventDefault();
      var dx = ev.clientX - startX;
      var dy = ev.clientY - startY;
      var newLeft = origLeft, newTop = origTop, newW = origW, newH = origH;

      if (pos.indexOf('e') !== -1) newW = Math.max(20, origW + dx);
      if (pos.indexOf('w') !== -1) { newW = Math.max(20, origW - dx); newLeft = origLeft + dx; }
      if (pos.indexOf('s') !== -1) newH = Math.max(20, origH + dy);
      if (pos.indexOf('n') !== -1) { newH = Math.max(20, origH - dy); newTop = origTop + dy; }

      clipState.rect.style.left = newLeft + 'px';
      clipState.rect.style.top = newTop + 'px';
      clipState.rect.style.width = newW + 'px';
      clipState.rect.style.height = newH + 'px';

      setDims(newLeft, newTop, newLeft + newW, newTop + newH);

      // 更新工具栏位置
      if (clipState.toolbar) {
        clipState.toolbar.style.left = newLeft + 'px';
        clipState.toolbar.style.top = (newTop + newH + 8) + 'px';
      }
    }

    function onUp() {
      document.removeEventListener('mousemove', onMove);
      document.removeEventListener('mouseup', onUp);
    }

    document.removeEventListener('mousemove', onClipMouseMove);
    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup', onUp);
  }

  function showClipToolbar(x, y, w, h) {
    if (clipState.toolbar) clipState.toolbar.remove();

    var bar = document.createElement('div');
    bar.className = 'ka-clip-toolbar';
    bar.style.left = x + 'px';
    bar.style.top = (y + h + 8) + 'px';

    // 选区尺寸提示
    bar.innerHTML =
      '<span class="ka-clip-size">' + Math.round(w) + ' × ' + Math.round(h) + '</span>' +
      '<button class="ka-clip-btn ka-clip-btn-cancel">取消 <kbd>Esc</kbd></button>' +
      '<button class="ka-clip-btn ka-clip-btn-confirm">确认截取 <kbd>↵</kbd></button>';

    clipState.overlay.appendChild(bar);
    clipState.toolbar = bar;

    bar.querySelector('.ka-clip-btn-cancel').addEventListener('click', cancelClip);
    bar.querySelector('.ka-clip-btn-confirm').addEventListener('click', confirmClip);
  }

  function onClipKeyDown(e) {
    if (!clipState.active) return;
    if (e.key === 'Escape') {
      cancelClip();
    } else if (e.key === 'Enter') {
      confirmClip();
    }
  }

  function confirmClip() {
    if (!clipState.rect) return;

    var rectLeft = parseInt(clipState.rect.style.left);
    var rectTop = parseInt(clipState.rect.style.top);
    var rectW = parseInt(clipState.rect.style.width);
    var rectH = parseInt(clipState.rect.style.height);

    if (isNaN(rectW) || isNaN(rectH) || rectW < 10 || rectH < 10) {
      showNotification('选区太小，请重新框选', 'error');
      return;
    }

    // 1. 先提取选区内的文字内容
    var content = extractTextInRect(
      rectLeft + window.scrollX,
      rectTop + window.scrollY,
      rectW,
      rectH
    );

    // 2. 保存裁剪参数（异步回调中使用）
    var cropLeft = rectLeft;
    var cropTop = rectTop;
    var cropW = rectW;
    var cropH = rectH;

    // 3. 逐个隐藏截图工具元素，确保截图干净
    //    先隐藏选区框（绿色边框）、resize handles（小方块）、工具栏
    if (clipState.rect) clipState.rect.style.display = 'none';
    if (clipState.toolbar) clipState.toolbar.style.display = 'none';
    // 隐藏所有 handles（以防有残留的）
    if (clipState.overlay) {
      var handles = clipState.overlay.querySelectorAll('.ka-clip-handle');
      handles.forEach(function (h) { h.style.display = 'none'; });
      // 隐藏提示文字
      var tip = clipState.overlay.querySelector('.ka-clip-tip');
      if (tip) tip.style.display = 'none';
    }
    // 最后隐藏整个 overlay（包括暗幕遮罩）
    if (clipState.overlay) clipState.overlay.style.display = 'none';

    // 4. 双重 requestAnimationFrame 确保浏览器完成渲染后再截图
    requestAnimationFrame(function () {
      requestAnimationFrame(function () {
        chrome.runtime.sendMessage({ type: 'CAPTURE_SCREENSHOT' }, function (resp) {
          // 检查连接错误
          var lastErr = chrome.runtime.lastError;

          // 立即清除遮罩
          cancelClip();

          if (lastErr || !resp || !resp.success || !resp.dataUrl) {
            // 截图失败，回退到纯文字保存
            doSaveSelectClip(content, null);
            return;
          }

          // 5. 用 canvas 裁剪选区部分
          var img = new Image();
          img.onload = function () {
            var dpr = window.devicePixelRatio || 1;
            var canvas = document.createElement('canvas');
            canvas.width = cropW * dpr;
            canvas.height = cropH * dpr;
            var ctx = canvas.getContext('2d');
            ctx.drawImage(img,
              cropLeft * dpr, cropTop * dpr, cropW * dpr, cropH * dpr,
              0, 0, cropW * dpr, cropH * dpr
            );
            var croppedDataUrl = canvas.toDataURL('image/jpeg', 0.8);
            doSaveSelectClip(content, croppedDataUrl);
          };
          img.onerror = function () {
            doSaveSelectClip(content, null);
          };
          img.src = resp.dataUrl;
        });
      });
    });
  }

  // 保存选择剪藏数据（独立函数，不依赖 clipState）
  function doSaveSelectClip(textContent, screenshotDataUrl) {
    // 只要有截图或有文字，就保存
    var hasText = textContent && textContent.trim().length > 0;
    var hasScreenshot = !!screenshotDataUrl;

    if (!hasText && !hasScreenshot) {
      showNotification('选区内没有可保存的内容', 'error');
      return;
    }

    var title = document.title || location.hostname;
    var clipData = {
      type: 'select-clip',
      content: hasText ? textContent.trim() : '（截图内容）',
      title: '截图 - ' + title,
      meta: { url: location.href, title: title, timestamp: Date.now() }
    };

    if (hasScreenshot) {
      clipData.screenshot = screenshotDataUrl;
    }

    chrome.runtime.sendMessage({
      type: 'SAVE_CLIP',
      payload: clipData
    }, function (resp) {
      var err = chrome.runtime.lastError;
      if (err) {
        showNotification('保存失败: ' + err.message, 'error');
        return;
      }
      if (resp && resp.success) {
        showNotification('内容已截取保存', 'success');
        chrome.runtime.sendMessage({
          type: 'CLIP_RESULT',
          payload: { title: clipData.title, content: clipData.content }
        });
      } else {
        showNotification('保存失败: ' + ((resp && resp.error) || '未知错误'), 'error');
      }
    });
  }

  function extractTextInRect(absX, absY, w, h) {
    // 隐藏遮罩以免干扰元素查找
    if (clipState.overlay) clipState.overlay.style.display = 'none';

    var texts = [];
    var walker = document.createTreeWalker(
      document.body,
      NodeFilter.SHOW_TEXT,
      null,
      false
    );

    var node;
    while (node = walker.nextNode()) {
      var text = node.textContent.trim();
      if (!text) continue;

      var range = document.createRange();
      range.selectNodeContents(node);
      var rects = range.getClientRects();

      for (var i = 0; i < rects.length; i++) {
        var r = rects[i];
        var nodeAbsX = r.left + window.scrollX;
        var nodeAbsY = r.top + window.scrollY;

        // 检查是否在选区内（有交集）
        if (nodeAbsX + r.width > absX &&
            nodeAbsX < absX + w &&
            nodeAbsY + r.height > absY &&
            nodeAbsY < absY + h) {
          texts.push(text);
          break;
        }
      }
    }

    if (clipState.overlay) clipState.overlay.style.display = '';

    // 去重并保持顺序
    var seen = {};
    var unique = [];
    texts.forEach(function (t) {
      if (!seen[t]) {
        seen[t] = true;
        unique.push(t);
      }
    });

    return unique.join('\n');
  }

  function cancelClip() {
    clipState.active = false;
    document.removeEventListener('keydown', onClipKeyDown);
    document.removeEventListener('mousemove', onClipMouseMove);
    document.removeEventListener('mouseup', onClipMouseUp);
    if (clipState.overlay) {
      clipState.overlay.remove();
      clipState.overlay = null;
    }
    clipState.rect = null;
    clipState.toolbar = null;
    clipState.dimTop = null;
    clipState.dimBottom = null;
    clipState.dimLeft = null;
    clipState.dimRight = null;
  }

  // === 页面内通知 ===
  function showNotification(msg, type) {
    var n = document.createElement('div');
    n.className = 'ka-notification ka-notification-' + (type || 'info');
    n.textContent = msg;
    document.body.appendChild(n);

    requestAnimationFrame(function () {
      n.classList.add('ka-notification-show');
    });

    setTimeout(function () {
      n.classList.remove('ka-notification-show');
      setTimeout(function () { n.remove(); }, 300);
    }, 2500);
  }

  // === 消息监听 ===
  function ensureMessageListener() {
    // 移除旧版监听器（如果有）
    if (window.__kaMessageListener) {
      try { chrome.runtime.onMessage.removeListener(window.__kaMessageListener); } catch (e) {}
    }

    window.__kaMessageListener = function (msg, sender, sendResponse) {
      if (msg.type === 'SMART_CLIP') {
        smartClip();
        sendResponse({ success: true });
      }
      if (msg.type === 'SELECT_CLIP') {
        startSelectClip();
        sendResponse({ success: true });
      }
      if (msg.type === 'SHOW_NOTIFICATION' && msg.payload) {
        showNotification(msg.payload.msg, msg.payload.status);
        sendResponse({ success: true });
      }
      return true;
    };

    chrome.runtime.onMessage.addListener(window.__kaMessageListener);
  }

  // === 选中文字气泡 ===
  var selBubble = null;
  var selBubbleHideTimer = null;

  function removeSelBubble() {
    if (selBubble) {
      selBubble.remove();
      selBubble = null;
    }
    if (selBubbleHideTimer) {
      clearTimeout(selBubbleHideTimer);
      selBubbleHideTimer = null;
    }
  }

  function createSelBubble(text, rect) {
    removeSelBubble();

    var bubble = document.createElement('div');
    bubble.className = 'ka-sel-bubble';

    // 保存到笔记按钮
    var saveBtn = document.createElement('button');
    saveBtn.className = 'ka-sel-btn ka-sel-btn-save';
    saveBtn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z"/></svg>保存到笔记';

    // 分隔线
    var divider = document.createElement('div');
    divider.className = 'ka-sel-divider';

    // 问 WeKnora 按钮
    var askBtn = document.createElement('button');
    askBtn.className = 'ka-sel-btn ka-sel-btn-ask';
    askBtn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>问 WeKnora';

    bubble.appendChild(saveBtn);
    bubble.appendChild(divider);
    bubble.appendChild(askBtn);

    // 小三角
    var arrow = document.createElement('div');
    arrow.className = 'ka-sel-arrow';
    bubble.appendChild(arrow);

    document.body.appendChild(bubble);
    selBubble = bubble;

    // 定位：在选区上方居中
    var bubbleW = bubble.offsetWidth;
    var bubbleH = bubble.offsetHeight;
    var left = rect.left + rect.width / 2 - bubbleW / 2 + window.scrollX;
    var top = rect.top - bubbleH - 10 + window.scrollY;

    // 防止溢出左右
    if (left < 8) left = 8;
    if (left + bubbleW > document.documentElement.scrollWidth - 8) {
      left = document.documentElement.scrollWidth - bubbleW - 8;
    }
    // 如果上方空间不够，放到下方
    if (rect.top - bubbleH - 10 < 0) {
      top = rect.bottom + 10 + window.scrollY;
      arrow.className = 'ka-sel-arrow ka-sel-arrow-top';
    }

    bubble.style.left = left + 'px';
    bubble.style.top = top + 'px';

    // "问 WeKnora" — 发送到 sidepanel
    askBtn.addEventListener('click', function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      chrome.runtime.sendMessage({
        type: 'ASK_WEKNORA',
        payload: { text: text }
      }, function () {
        void chrome.runtime.lastError;
      });
      removeSelBubble();
      window.getSelection().removeAllRanges();
    });

    // "保存到笔记" — 保存到 ka_clips
    saveBtn.addEventListener('click', function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      var clipTitle = '选中文本 - ' + (document.title || location.hostname);
      var clipPayload = {
        type: 'select-clip',
        content: text,
        title: clipTitle,
        meta: { url: location.href, title: document.title || '' }
      };

      function doSave(retryCount) {
        chrome.runtime.sendMessage({
          type: 'SAVE_SELECTION',
          payload: clipPayload
        }, function (saveResp) {
          var err = chrome.runtime.lastError;
          if (err) {
            // service worker 可能休眠，重试一次
            if (retryCount < 1) {
              setTimeout(function () { doSave(retryCount + 1); }, 500);
            } else {
              showNotification('保存失败: ' + err.message, 'error');
            }
            return;
          }
          if (saveResp && saveResp.success) {
            showNotification('已保存到笔记', 'success');
          } else {
            showNotification('保存失败: ' + ((saveResp && saveResp.error) || '未知错误'), 'error');
          }
        });
      }

      doSave(0);
      removeSelBubble();
      window.getSelection().removeAllRanges();
    });

    // 点击气泡外关闭
    setTimeout(function () {
      document.addEventListener('mousedown', onBubbleOutsideClick);
    }, 50);
  }

  function onBubbleOutsideClick(e) {
    if (selBubble && !selBubble.contains(e.target)) {
      removeSelBubble();
      document.removeEventListener('mousedown', onBubbleOutsideClick);
    }
  }

  // 监听鼠标抬起 → 检测选中文字
  document.addEventListener('mouseup', function (e) {
    // 如果正在框选剪藏或点击在气泡内，不触发
    if (clipState.active) return;
    if (selBubble && selBubble.contains(e.target)) return;
    if (imgBubble && imgBubble.contains(e.target)) return;

    // 延迟一点让 selection 更新
    setTimeout(function () {
      var sel = window.getSelection();
      var text = sel ? sel.toString().trim() : '';

      if (text.length < 2) {
        // 没有选中有效文字，移除气泡
        removeSelBubble();
        document.removeEventListener('mousedown', onBubbleOutsideClick);
        return;
      }

      // 获取选区矩形
      if (sel.rangeCount > 0) {
        var range = sel.getRangeAt(0);
        var rects = range.getClientRects();
        if (rects.length > 0) {
          // 使用第一个 rect 的顶部和整体范围来定位
          var boundRect = range.getBoundingClientRect();
          createSelBubble(text, boundRect);
        }
      }
    }, 10);
  });

  // === 右键图片气泡 ===
  var imgBubble = null;

  function removeImgBubble() {
    if (imgBubble) {
      imgBubble.remove();
      imgBubble = null;
    }
  }

  function createImgBubble(imgSrc, imgAlt, rect) {
    removeImgBubble();
    removeSelBubble(); // 同时移除文字气泡

    var bubble = document.createElement('div');
    bubble.className = 'ka-sel-bubble ka-img-bubble';

    // 保存图片按钮
    var saveBtn = document.createElement('button');
    saveBtn.className = 'ka-sel-btn ka-sel-btn-save';
    saveBtn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z"/></svg>保存图片到笔记';

    // 分隔线
    var divider = document.createElement('div');
    divider.className = 'ka-sel-divider';

    // 问 WeKnora 按钮
    var askBtn = document.createElement('button');
    askBtn.className = 'ka-sel-btn ka-sel-btn-ask';
    askBtn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>问 WeKnora';

    bubble.appendChild(saveBtn);
    bubble.appendChild(divider);
    bubble.appendChild(askBtn);

    // 小三角
    var arrow = document.createElement('div');
    arrow.className = 'ka-sel-arrow';
    bubble.appendChild(arrow);

    document.body.appendChild(bubble);
    imgBubble = bubble;

    // 定位：在图片上方居中
    var bubbleW = bubble.offsetWidth;
    var bubbleH = bubble.offsetHeight;
    var left = rect.left + rect.width / 2 - bubbleW / 2 + window.scrollX;
    var top = rect.top - bubbleH - 10 + window.scrollY;

    // 防止溢出左右
    if (left < 8) left = 8;
    if (left + bubbleW > document.documentElement.scrollWidth - 8) {
      left = document.documentElement.scrollWidth - bubbleW - 8;
    }
    // 如果上方空间不够，放到下方
    if (rect.top - bubbleH - 10 < 0) {
      top = rect.bottom + 10 + window.scrollY;
      arrow.className = 'ka-sel-arrow ka-sel-arrow-top';
    }

    bubble.style.left = left + 'px';
    bubble.style.top = top + 'px';

    // "保存图片到笔记"
    saveBtn.addEventListener('click', function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      var clipTitle = '图片收藏 - ' + (document.title || location.hostname);
      chrome.runtime.sendMessage({
        type: 'SAVE_IMAGE',
        payload: {
          type: 'image-clip',
          content: '![' + (imgAlt || '图片') + '](' + imgSrc + ')',
          title: clipTitle,
          meta: { url: location.href, title: document.title || '', imageUrl: imgSrc }
        }
      }, function (saveResp) {
        void chrome.runtime.lastError;
        if (saveResp && saveResp.success) {
          showNotification('图片已保存到笔记', 'success');
        } else {
          showNotification('保存失败', 'error');
        }
      });
      removeImgBubble();
    });

    // "问 WeKnora" — 发送图片描述到 sidepanel
    askBtn.addEventListener('click', function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      var question = '请描述这张图片的内容: ' + imgSrc;
      chrome.runtime.sendMessage({
        type: 'ASK_WEKNORA',
        payload: { text: question }
      }, function () {
        void chrome.runtime.lastError;
      });
      removeImgBubble();
    });

    // 点击气泡外关闭
    setTimeout(function () {
      document.addEventListener('mousedown', onImgBubbleOutsideClick);
    }, 50);
  }

  function onImgBubbleOutsideClick(e) {
    if (imgBubble && !imgBubble.contains(e.target)) {
      removeImgBubble();
      document.removeEventListener('mousedown', onImgBubbleOutsideClick);
    }
  }

  // 监听右键点击图片 → 弹出气泡
  document.addEventListener('contextmenu', function (e) {
    // 如果正在框选剪藏，不触发
    if (clipState.active) return;

    var target = e.target;
    // 检查是否右键点击了图片
    if (target.tagName === 'IMG' && target.src) {
      // 延迟一点，让浏览器原生右键菜单先出现
      setTimeout(function () {
        var imgRect = target.getBoundingClientRect();
        createImgBubble(target.src, target.alt || '', imgRect);
      }, 100);
    } else {
      // 不是图片，移除图片气泡
      removeImgBubble();
    }
  });

  // === Option + Command 快捷键直接触发选择剪藏 ===
  // Chrome 扩展 commands API 不支持 Alt+Command 组合，所以在页面层监听
  var optCmdState = { altDown: false, metaDown: false };

  document.addEventListener('keydown', function (e) {
    // 如果正在剪藏中，不响应
    if (clipState.active) return;

    // 记录按键状态
    if (e.key === 'Alt') optCmdState.altDown = true;
    if (e.key === 'Meta') optCmdState.metaDown = true;

    // 同时按住 Option(Alt) + Command(Meta) 触发选择剪藏
    if (optCmdState.altDown && optCmdState.metaDown) {
      e.preventDefault();
      optCmdState.altDown = false;
      optCmdState.metaDown = false;
      startSelectClip();
    }
  });

  document.addEventListener('keyup', function (e) {
    if (e.key === 'Alt') optCmdState.altDown = false;
    if (e.key === 'Meta') optCmdState.metaDown = false;
  });

  // 窗口失焦时重置状态，防止按键卡住
  window.addEventListener('blur', function () {
    optCmdState.altDown = false;
    optCmdState.metaDown = false;
  });

  // 暴露到 window 上以便防重复注入时调用
  window.ensureMessageListener = ensureMessageListener;

  ensureMessageListener();
})();
