(function () {
  'use strict';

  function $(id) { return document.getElementById(id); }

  var clipList = $('clip-list');
  var emptyState = $('empty-state');
  var totalEl = $('clips-total');
  var searchInput = $('search-input');
  var filterType = $('filter-type');
  var contentTitle = $('content-title');
  var allClips = []; // 每条记录增加 _source 字段: 'ka_clips' | 'ka_notes'

  // === 图片灯箱 ===
  var lightbox = $('lightbox');
  var lightboxImg = $('lightbox-img');
  var lightboxClose = $('lightbox-close');

  function openLightbox(src) {
    lightboxImg.src = src;
    lightbox.classList.add('show');
    document.body.style.overflow = 'hidden';
  }

  function closeLightbox() {
    lightbox.classList.remove('show');
    lightboxImg.src = '';
    document.body.style.overflow = '';
  }

  // 点击遮罩关闭（不点击图片本身）
  lightbox.addEventListener('click', function (e) {
    if (e.target === lightbox) closeLightbox();
  });
  lightboxClose.addEventListener('click', closeLightbox);
  // ESC 关闭
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && lightbox.classList.contains('show')) {
      closeLightbox();
    }
  });

  // 分类名称映射
  var categoryLabels = {
    'all': '全部内容',
    'select-clip': '截图',
    'smart-clip': '智能剪藏',
    'markdown': '速记笔记',
    'image-clip': '图片收藏'
  };

  // === Toast ===
  function toast(msg, type) {
    var el = $('toast');
    el.textContent = msg;
    el.className = 'toast show' + (type ? ' ' + type : '');
    setTimeout(function () { el.classList.remove('show'); }, 2000);
  }

  // === 轻量级 Markdown → HTML 渲染 ===
  function renderMarkdown(text) {
    if (!text) return '';
    var html = text;

    // 代码块 (```...```) — 先处理，避免内部被其他规则干扰
    html = html.replace(/```(\w*)\n([\s\S]*?)```/g, function (_, lang, code) {
      return '<pre><code>' + escapeHtml(code.replace(/\n$/, '')) + '</code></pre>';
    });

    // 行内代码
    html = html.replace(/`([^`]+)`/g, '<code>$1</code>');

    // 表格（简单支持）
    html = html.replace(/((?:\|.+\|\n)+)/g, function (tableBlock) {
      var rows = tableBlock.trim().split('\n');
      if (rows.length < 2) return tableBlock;
      var out = '<table>';
      rows.forEach(function (row, i) {
        // 跳过分隔行 |---|---|
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
      return out;
    });

    // 将剩余文本按行处理
    var lines = html.split('\n');
    var result = [];
    var inList = false;
    var listType = '';

    for (var i = 0; i < lines.length; i++) {
      var line = lines[i];

      // 已渲染的 <pre>/<table> 块直接保留
      if (line.indexOf('<pre>') !== -1 || line.indexOf('<table>') !== -1) {
        if (inList) { result.push('</' + listType + '>'); inList = false; }
        result.push(line);
        continue;
      }

      // 标题
      if (/^####\s+(.+)/.test(line)) { if (inList) { result.push('</' + listType + '>'); inList = false; } result.push('<h4>' + RegExp.$1 + '</h4>'); continue; }
      if (/^###\s+(.+)/.test(line)) { if (inList) { result.push('</' + listType + '>'); inList = false; } result.push('<h3>' + RegExp.$1 + '</h3>'); continue; }
      if (/^##\s+(.+)/.test(line)) { if (inList) { result.push('</' + listType + '>'); inList = false; } result.push('<h2>' + RegExp.$1 + '</h2>'); continue; }
      if (/^#\s+(.+)/.test(line)) { if (inList) { result.push('</' + listType + '>'); inList = false; } result.push('<h1>' + RegExp.$1 + '</h1>'); continue; }

      // 分隔线
      if (/^---+$/.test(line.trim())) { if (inList) { result.push('</' + listType + '>'); inList = false; } result.push('<hr>'); continue; }

      // 引用
      if (/^>\s*(.*)/.test(line)) { if (inList) { result.push('</' + listType + '>'); inList = false; } result.push('<blockquote>' + RegExp.$1 + '</blockquote>'); continue; }

      // 无序列表
      if (/^[\-\*]\s+(.+)/.test(line)) {
        if (!inList || listType !== 'ul') {
          if (inList) result.push('</' + listType + '>');
          result.push('<ul>'); inList = true; listType = 'ul';
        }
        result.push('<li>' + RegExp.$1 + '</li>');
        continue;
      }

      // 有序列表
      if (/^\d+\.\s+(.+)/.test(line)) {
        if (!inList || listType !== 'ol') {
          if (inList) result.push('</' + listType + '>');
          result.push('<ol>'); inList = true; listType = 'ol';
        }
        result.push('<li>' + RegExp.$1 + '</li>');
        continue;
      }

      // 其他：关闭列表，空行跳过，非空行包裹 <p>
      if (inList) { result.push('</' + listType + '>'); inList = false; }
      if (line.trim() === '') { continue; }
      result.push('<p>' + line + '</p>');
    }
    if (inList) result.push('</' + listType + '>');

    html = result.join('\n');

    // 行内样式（在所有块级元素处理完之后）
    html = html.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
    html = html.replace(/\*(.+?)\*/g, '<em>$1</em>');
    html = html.replace(/!\[([^\]]*)\]\(([^)]+)\)/g, '<img src="$2" alt="$1">');
    html = html.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank">$1</a>');

    return html;
  }

  // === 加载收藏列表 ===
  function loadClips() {
    chrome.runtime.sendMessage({ type: 'GET_CLIPS' }, function (resp) {
      if (resp && resp.success) {
        allClips = (resp.data || []).map(function (c) { c._source = 'ka_clips'; return c; });
        // 同时加载 ka_notes 中的旧数据（向后兼容）
        chrome.runtime.sendMessage({ type: 'GET_NOTES' }, function (notesResp) {
          if (notesResp && notesResp.success && notesResp.data) {
            notesResp.data.forEach(function (note) {
              // 避免重复（相同 id 的数据已在 ka_clips 中）
              var exists = allClips.some(function (c) { return c.id === note.id; });
              if (!exists) {
                note._source = 'ka_notes';
                allClips.push(note);
              }
            });
          }
          // 按时间排序
          allClips.sort(function (a, b) {
            return new Date(b.createdAt || 0) - new Date(a.createdAt || 0);
          });
          renderClips();
        });
      }
    });
  }

  // === 编辑工具栏辅助函数 ===
  function clipInsertMd(textarea, before, after, placeholder) {
    textarea.focus();
    var start = textarea.selectionStart;
    var end = textarea.selectionEnd;
    var text = textarea.value;
    var selected = text.substring(start, end);
    var insert = selected || placeholder || '';
    textarea.value = text.substring(0, start) + before + insert + (after || '') + text.substring(end);
    textarea.selectionStart = start + before.length;
    textarea.selectionEnd = start + before.length + insert.length;
    textarea.dispatchEvent(new Event('input'));
  }

  function clipInsertLinePrefix(textarea, prefix) {
    textarea.focus();
    var start = textarea.selectionStart;
    var text = textarea.value;
    var lineStart = text.lastIndexOf('\n', start - 1) + 1;
    textarea.value = text.substring(0, lineStart) + prefix + text.substring(lineStart);
    textarea.selectionStart = textarea.selectionEnd = start + prefix.length;
    textarea.dispatchEvent(new Event('input'));
  }

  // === 渲染列表 ===
  function renderClips() {
    var keyword = searchInput.value.trim().toLowerCase();
    var typeFilter = filterType.value;

    // 更新右侧标题
    contentTitle.textContent = categoryLabels[typeFilter] || '全部内容';

    // 更新左侧分类计数
    updateCategoryCounts();

    var filtered = allClips.filter(function (clip) {
      if (typeFilter !== 'all' && clip.type !== typeFilter) return false;
      if (keyword) {
        var text = ((clip.title || '') + ' ' + (clip.content || '')).toLowerCase();
        return text.indexOf(keyword) !== -1;
      }
      return true;
    });

    totalEl.textContent = filtered.length;

    if (filtered.length === 0) {
      clipList.innerHTML = '';
      emptyState.style.display = 'block';
      return;
    }

    emptyState.style.display = 'none';

    // Markdown 编辑工具栏 HTML（复用）
    function buildEditToolbarHtml() {
      return '<div class="clip-edit-toolbar">'
        + '      <div class="clip-edit-toolbar-group">'
        + '        <button class="clip-edit-tool-btn" data-md="bold" title="加粗 **文本**"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M15.6 10.79c.97-.67 1.65-1.77 1.65-2.79 0-2.26-1.75-4-4-4H7v14h7.04c2.09 0 3.71-1.7 3.71-3.79 0-1.52-.86-2.82-2.15-3.42zM10 6.5h3c.83 0 1.5.67 1.5 1.5s-.67 1.5-1.5 1.5h-3v-3zm3.5 9H10v-3h3.5c.83 0 1.5.67 1.5 1.5s-.67 1.5-1.5 1.5z"/></svg></button>'
        + '      </div>'
        + '      <div class="clip-edit-toolbar-divider"></div>'
        + '      <div class="clip-edit-toolbar-group">'
        + '        <button class="clip-edit-tool-btn" data-md="heading" title="标题 ## 文本"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M5 4v3h5.5v12h3V7H19V4H5z"/></svg></button>'
        + '        <button class="clip-edit-tool-btn" data-md="ul" title="无序列表 - 项目"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M4 10.5c-.83 0-1.5.67-1.5 1.5s.67 1.5 1.5 1.5 1.5-.67 1.5-1.5-.67-1.5-1.5-1.5zm0-6c-.83 0-1.5.67-1.5 1.5S3.17 7.5 4 7.5 5.5 6.83 5.5 6 4.83 4.5 4 4.5zm0 12c-.83 0-1.5.68-1.5 1.5s.68 1.5 1.5 1.5 1.5-.68 1.5-1.5-.67-1.5-1.5-1.5zM7 19h14v-2H7v2zm0-6h14v-2H7v2zm0-8v2h14V5H7z"/></svg></button>'
        + '        <button class="clip-edit-tool-btn" data-md="ol" title="有序列表 1. 项目"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M2 17h2v.5H3v1h1v.5H2v1h3v-4H2v1zm1-9h1V4H2v1h1v3zm-1 3h1.8L2 13.1v.9h3v-1H3.2L5 10.9V10H2v1zm5-6v2h14V5H7zm0 14h14v-2H7v2zm0-6h14v-2H7v2z"/></svg></button>'
        + '      </div>'
        + '      <div class="clip-edit-toolbar-divider"></div>'
        + '      <div class="clip-edit-toolbar-group">'
        + '        <button class="clip-edit-tool-btn" data-md="link" title="插入链接 [文本](url)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg></button>'
        + '        <button class="clip-edit-tool-btn" data-md="image" title="插入图片 ![描述](url)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="8.5" cy="8.5" r="1.5"/><polyline points="21 15 16 10 5 21"/></svg></button>'
        + '      </div>'
        + '      <div class="clip-edit-toolbar-divider"></div>'
        + '      <div class="clip-edit-toolbar-group">'
        + '        <button class="clip-edit-tool-btn" data-md="preview" title="预览 Markdown 效果"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg></button>'
        + '      </div>'
        + '      <div class="clip-edit-toolbar-spacer"></div>'
        + '      <div class="clip-edit-actions">'
        + '        <button class="clip-edit-btn cancel">取消</button>'
        + '        <button class="clip-edit-btn save">保存</button>'
        + '      </div>'
        + '    </div>';
    }

    var html = '';
    filtered.forEach(function (clip) {
      var url = (clip.meta && clip.meta.url) || '';
      var time = formatTime(clip.createdAt);
      var isMarkdown = clip.type === 'markdown';
      var isSmartClip = clip.type === 'smart-clip';
      var isImageClip = clip.type === 'image-clip';
      var isSelectClip = clip.type === 'select-clip';
      var hasScreenshot = isSelectClip && clip.screenshot;

      // 类型标签
      var typeBadge, tagClass;
      if (isMarkdown) {
        typeBadge = '速记';
        tagClass = 'clip-card-tag tag-markdown';
      } else if (isSmartClip) {
        typeBadge = '智能剪藏';
        tagClass = 'clip-card-tag';
      } else if (isImageClip) {
        typeBadge = '图片收藏';
        tagClass = 'clip-card-tag tag-image';
      } else if (hasScreenshot) {
        typeBadge = '截图';
        tagClass = 'clip-card-tag';
      } else {
        typeBadge = '文本';
        tagClass = 'clip-card-tag tag-text';
      }

      // 内容区
      var contentHtml;
      if (hasScreenshot) {
        // 选择剪藏有截图：显示缩略图
        contentHtml = '<img class="clip-screenshot-thumb" src="' + clip.screenshot + '" alt="截图">';
      } else {
        // 所有非截图内容统一使用 markdown 渲染（速记笔记、智能剪藏、选中文字保存、图片收藏）
        contentHtml = '<div class="clip-card-content md-rendered">' + renderMarkdown(clip.content || '') + '</div>';
      }

      // 编辑区标题栏
      var editHeaderHtml = '<div class="clip-edit-header">'
        + '<span class="clip-edit-header-title">编辑 · ' + escapeHtml(typeBadge) + '</span>'
        + '<button class="clip-edit-header-close" title="关闭"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>'
        + '</div>';

      // 编辑区：选择剪藏有截图时用左右分栏布局
      var editAreaHtml;
      if (hasScreenshot) {
        editAreaHtml = '<div class="clip-card-edit-area">'
          + editHeaderHtml
          + '<div class="clip-edit-split">'
          + '  <div class="clip-edit-split-left">'
          + '    <img src="' + clip.screenshot + '" alt="截图原图">'
          + '    <span class="split-label">截图预览（点击放大）</span>'
          + '  </div>'
          + '  <div class="clip-edit-split-right">'
          + '    <div class="clip-edit-card">'
          + '      <textarea class="clip-edit-textarea" placeholder="# 标题&#10;&#10;在这里编辑截图中的文字…"></textarea>'
          + buildEditToolbarHtml()
          + '    </div>'
          + '  </div>'
          + '</div>'
          + '</div>';
      } else {
        editAreaHtml = '<div class="clip-card-edit-area">'
          + editHeaderHtml
          + '  <div class="clip-edit-card">'
          + '    <textarea class="clip-edit-textarea" placeholder="# 标题&#10;&#10;在这里编辑内容…"></textarea>'
          + buildEditToolbarHtml()
          + '  </div>'
          + '</div>';
      }

      html += '<div class="clip-card" data-id="' + clip.id + '" data-source="' + (clip._source || 'ka_clips') + '">'
        + '<div class="clip-card-header">'
        + '  <span class="clip-card-time">' + time + '</span>'
        + '</div>'
        + '<div class="clip-card-body">'
        + '  ' + contentHtml
        + '</div>'
        + editAreaHtml
        + '<div class="clip-card-footer">'
        + '  <span class="' + tagClass + '">' + typeBadge + '</span>'
        + '  <div class="clip-card-footer-spacer"></div>'
        + '  <div class="clip-card-actions">'
        + '    <button class="clip-more-btn" title="更多操作"><svg viewBox="0 0 24 24" fill="currentColor"><circle cx="5" cy="12" r="2"/><circle cx="12" cy="12" r="2"/><circle cx="19" cy="12" r="2"/></svg></button>'
        + '    <div class="clip-more-menu">'
        + (url ? '      <button class="clip-more-menu-item menu-open-link" data-url="' + escapeHtml(url) + '"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>打开链接</button>' : '')
        + '      <button class="clip-more-menu-item menu-edit"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>编辑</button>'
        + '      <button class="clip-more-menu-item menu-copy"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>复制</button>'
        + '      <button class="clip-more-menu-item menu-delete"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>删除</button>'
        + '    </div>'
        + '  </div>'
        + '</div>'
        + '</div>';
    });

    clipList.innerHTML = html;

    // 绑定三点菜单
    clipList.querySelectorAll('.clip-more-btn').forEach(function (btn) {
      btn.addEventListener('click', function (e) {
        e.stopPropagation();
        var menu = btn.nextElementSibling;
        var card = btn.closest('.clip-card');
        // 先关闭其他已打开的菜单
        clipList.querySelectorAll('.clip-more-menu.show').forEach(function (m) {
          if (m !== menu) {
            m.classList.remove('show');
            var otherCard = m.closest('.clip-card');
            if (otherCard) otherCard.classList.remove('menu-open');
          }
        });
        // 切换当前菜单
        var isOpen = menu.classList.toggle('show');
        if (isOpen) {
          card.classList.add('menu-open');
        } else {
          card.classList.remove('menu-open');
        }
      });
    });

    // 点击其他地方关闭菜单
    document.addEventListener('click', function () {
      clipList.querySelectorAll('.clip-more-menu.show').forEach(function (m) {
        m.classList.remove('show');
        var card = m.closest('.clip-card');
        if (card) card.classList.remove('menu-open');
      });
    });

    // 绑定复制事件
    clipList.querySelectorAll('.menu-copy').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var card = btn.closest('.clip-card');
        var id = card.dataset.id;
        var clip = allClips.find(function (c) { return c.id === id; });
        if (clip) {
          navigator.clipboard.writeText(clip.content || '').then(function () {
            toast('已复制到剪贴板', 'success');
          });
        }
      });
    });

    // 绑定删除事件
    clipList.querySelectorAll('.menu-delete').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var card = btn.closest('.clip-card');
        var id = card.dataset.id;
        var source = card.dataset.source;
        if (confirm('确定要删除这条收藏吗？')) {
          var msgType = source === 'ka_notes' ? 'DELETE_NOTE' : 'DELETE_CLIP';
          chrome.runtime.sendMessage({ type: msgType, payload: { id: id } }, function () {
            allClips = allClips.filter(function (c) { return c.id !== id; });
            renderClips();
            toast('已删除', 'success');
          });
        }
      });
    });

    // 绑定打开链接事件
    clipList.querySelectorAll('.menu-open-link').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var linkUrl = btn.getAttribute('data-url');
        if (linkUrl) {
          window.open(linkUrl, '_blank');
        }
      });
    });

    // 绑定编辑事件
    clipList.querySelectorAll('.menu-edit').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var card = btn.closest('.clip-card');
        enterEditMode(card);
      });
    });

    // 双击卡片内容进入编辑模式
    clipList.querySelectorAll('.clip-card').forEach(function (card) {
      card.addEventListener('dblclick', function (e) {
        // 如果已在编辑态，或点击的是按钮/链接/菜单，不触发
        if (card.classList.contains('editing')) return;
        var tag = e.target.tagName;
        if (tag === 'BUTTON' || tag === 'A' || tag === 'TEXTAREA' || tag === 'INPUT') return;
        if (e.target.closest('.clip-card-actions')) return;
        if (e.target.closest('.clip-more-menu')) return;
        enterEditMode(card);
      });
    });

    // 点击卡片内容展开/收起（非 markdown 类型才有折叠效果）
    clipList.querySelectorAll('.clip-card-content:not(.md-rendered)').forEach(function (el) {
      el.addEventListener('click', function () {
        el.classList.toggle('expanded');
        var fade = el.querySelector('.clip-card-fade');
        if (fade) fade.style.display = el.classList.contains('expanded') ? 'none' : '';
      });
    });

    // Markdown 卡片点击展开/收起（无 fade 渐变）
    clipList.querySelectorAll('.clip-card-content.md-rendered').forEach(function (el) {
      el.addEventListener('click', function () {
        el.classList.toggle('expanded');
      });
    });

    // 编辑态截图 → 点击打开灯箱查看原图（列表缩略图不放大）
    clipList.querySelectorAll('.clip-edit-split-left img').forEach(function (img) {
      img.addEventListener('click', function (e) {
        e.stopPropagation();
        if (img.src) openLightbox(img.src);
      });
    });
  }

  // === 进入编辑模式（独立函数，供菜单和双击调用） ===
  var editOverlay = $('edit-overlay');
  var _editingCard = null; // 当前编辑中的卡片

  function exitEditMode() {
    if (_editingCard) {
      _editingCard.classList.remove('editing');
      _editingCard = null;
    }
    editOverlay.classList.remove('show');
    document.body.style.overflow = '';
  }

  // 点击遮罩退出编辑
  editOverlay.addEventListener('click', exitEditMode);

  // ESC 退出编辑
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && _editingCard) {
      // 如果灯箱正在显示，优先关闭灯箱
      if (lightbox.classList.contains('show')) return;
      exitEditMode();
    }
  });

  function enterEditMode(card) {
    if (!card || card.classList.contains('editing')) return;
    var id = card.dataset.id;
    var clip = allClips.find(function (c) { return c.id === id; });
    if (!clip) return;

    // 关闭其他菜单
    clipList.querySelectorAll('.clip-more-menu.show').forEach(function (m) {
      m.classList.remove('show');
      var c = m.closest('.clip-card');
      if (c) c.classList.remove('menu-open');
    });

    // 如果已有其他卡片在编辑，先退出
    if (_editingCard && _editingCard !== card) {
      _editingCard.classList.remove('editing');
    }

    // 显示遮罩 + 进入编辑态
    _editingCard = card;
    editOverlay.classList.add('show');
    document.body.style.overflow = 'hidden';
    card.classList.add('editing');

    var textarea = card.querySelector('.clip-edit-textarea');
    textarea.value = clip.content || '';
    textarea.style.height = 'auto';
    textarea.style.height = Math.max(300, textarea.scrollHeight) + 'px';
    textarea.focus();

    // 输入时自动调整高度
    textarea.oninput = function () {
      textarea.style.height = 'auto';
      textarea.style.height = Math.max(300, textarea.scrollHeight) + 'px';
    };

    // 标题栏关闭按钮
    var headerClose = card.querySelector('.clip-edit-header-close');
    if (headerClose) {
      headerClose.onclick = function () { exitEditMode(); };
    }

    // 工具栏按钮事件
    card.querySelectorAll('.clip-edit-tool-btn[data-md]').forEach(function (toolBtn) {
      toolBtn.onclick = function (e) {
        e.preventDefault();
        var action = toolBtn.getAttribute('data-md');
        switch (action) {
          case 'bold':
            clipInsertMd(textarea, '**', '**', '粗体文本');
            break;
          case 'heading':
            clipInsertLinePrefix(textarea, '## ');
            break;
          case 'ul':
            clipInsertLinePrefix(textarea, '- ');
            break;
          case 'ol':
            clipInsertLinePrefix(textarea, '1. ');
            break;
          case 'link':
            clipInsertMd(textarea, '[', '](url)', '链接文本');
            break;
          case 'image':
            clipInsertMd(textarea, '![', '](url)', '图片描述');
            break;
          case 'preview':
            if (!textarea.value.trim() && textarea.style.display !== 'none') { toast('请先输入内容'); return; }
            var previewArea = card.querySelector('.clip-edit-preview');
            if (!previewArea) {
              previewArea = document.createElement('div');
              previewArea.className = 'clip-edit-preview md-rendered';
              previewArea.style.cssText = 'padding:12px 14px;font-size:13px;line-height:1.7;color:#555;overflow-y:auto;min-height:120px;display:none;';
              textarea.parentNode.insertBefore(previewArea, textarea.nextSibling);
            }
            var isPreviewing = textarea.style.display === 'none';
            if (isPreviewing) {
              textarea.style.display = '';
              previewArea.style.display = 'none';
              toolBtn.classList.remove('clip-preview-active');
              textarea.focus();
            } else {
              previewArea.innerHTML = renderMarkdown(textarea.value);
              textarea.style.display = 'none';
              previewArea.style.display = 'block';
              toolBtn.classList.add('clip-preview-active');
            }
            break;
        }
      };
    });

    // 取消按钮
    var cancelBtn = card.querySelector('.clip-edit-btn.cancel');
    cancelBtn.onclick = function () {
      exitEditMode();
    };

    // 保存按钮
    var saveBtn = card.querySelector('.clip-edit-btn.save');
    saveBtn.onclick = function () {
      var newContent = textarea.value;
      var source = card.dataset.source;
      var storageKey = source === 'ka_notes' ? 'ka_notes' : 'ka_clips';

      saveBtn.disabled = true;
      saveBtn.textContent = '保存中...';

      chrome.storage.local.get(storageKey, function (data) {
        var items = data[storageKey] || [];
        var found = false;
        for (var i = 0; i < items.length; i++) {
          if (items[i].id === id) {
            items[i].content = newContent;
            items[i].updatedAt = new Date().toISOString();
            found = true;
            break;
          }
        }
        if (!found) {
          saveBtn.disabled = false;
          saveBtn.textContent = '保存';
          toast('未找到对应记录', '');
          return;
        }
        var setData = {};
        setData[storageKey] = items;
        chrome.storage.local.set(setData, function () {
          saveBtn.disabled = false;
          saveBtn.textContent = '保存';
          if (chrome.runtime.lastError) {
            toast('保存失败，请重试', '');
            return;
          }
          clip.content = newContent;
          if (clip.type === 'markdown') {
            clip.title = newContent.split('\n')[0].replace(/^#+\s*/, '').substring(0, 50) || '未命名笔记';
          }
          exitEditMode();
          renderClips();
          toast('已保存', 'success');
        });
      });
    };
  }

  // === 工具函数 ===
  function escapeHtml(s) {
    var d = document.createElement('div');
    d.textContent = s || '';
    return d.innerHTML;
  }

  function formatTime(iso) {
    if (!iso) return '';
    var d = new Date(iso);
    var now = new Date();
    var diff = now - d;

    if (diff < 60000) return '刚刚';
    if (diff < 3600000) return Math.floor(diff / 60000) + ' 分钟前';
    if (diff < 86400000) return Math.floor(diff / 3600000) + ' 小时前';
    if (diff < 604800000) return Math.floor(diff / 86400000) + ' 天前';

    var m = d.getMonth() + 1;
    var day = d.getDate();
    var h = d.getHours().toString().padStart(2, '0');
    var min = d.getMinutes().toString().padStart(2, '0');
    return m + '月' + day + '日 ' + h + ':' + min;
  }

  // === 分类计数更新 ===
  function updateCategoryCounts() {
    var countAll = allClips.length;
    var countSelect = 0;
    var countSmart = 0;
    var countMarkdown = 0;
    var countImage = 0;
    allClips.forEach(function (c) {
      if (c.type === 'select-clip') countSelect++;
      else if (c.type === 'smart-clip') countSmart++;
      else if (c.type === 'markdown') countMarkdown++;
      else if (c.type === 'image-clip') countImage++;
    });
    $('count-all').textContent = countAll;
    $('count-select').textContent = countSelect;
    $('count-smart').textContent = countSmart;
    $('count-markdown').textContent = countMarkdown;
    if ($('count-image')) $('count-image').textContent = countImage;
  }

  // === 左侧分类点击 ===
  document.querySelectorAll('.sidebar-item').forEach(function (item) {
    item.addEventListener('click', function () {
      document.querySelectorAll('.sidebar-item').forEach(function (i) { i.classList.remove('active'); });
      item.classList.add('active');
      filterType.value = item.getAttribute('data-filter');
      renderClips();
    });
  });

  // === 事件绑定 ===
  searchInput.addEventListener('input', renderClips);

  $('btn-refresh').addEventListener('click', function () {
    loadClips();
    toast('已刷新', 'success');
  });

  $('btn-clear-all').addEventListener('click', function () {
    if (allClips.length === 0) return;
    if (confirm('确定要清空所有收藏吗？此操作不可恢复。')) {
      // 同时清空 ka_clips 和 ka_notes
      chrome.storage.local.set({ ka_clips: [], ka_notes: [] }, function () {
        allClips = [];
        renderClips();
        toast('已清空', 'success');
      });
    }
  });

  // === 监听 storage 变化，实时刷新列表 ===
  // 当其他页面保存了新内容时，列表自动更新
  if (chrome && chrome.storage && chrome.storage.onChanged) {
    chrome.storage.onChanged.addListener(function (changes, area) {
      if (area === 'local' && (changes.ka_clips || changes.ka_notes)) {
        loadClips();
      }
    });
  }

  // === 初始化 ===
  loadClips();
})();
