(function () {
  'use strict';

  // === DOM Helpers ===
  function $(sel) { return document.querySelector(sel); }
  function $$(sel) { return document.querySelectorAll(sel); }

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
    });
  });

  // === 新建知识库按钮 ===
  var emptyBtn = $('.empty-btn');
  if (emptyBtn) {
    emptyBtn.addEventListener('click', function () {
      alert('新建知识库功能即将上线');
    });
  }

  // === FAB 按钮 ===
  var fabBtn = $('.fab-btn');
  if (fabBtn) {
    fabBtn.addEventListener('click', function () {
      alert('新建知识库功能即将上线');
    });
  }

  // === 右侧对话面板 - 输入交互 ===
  var chatTextarea = $('.chat-input-card textarea');
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
          alert('发送: ' + text);
          chatTextarea.value = '';
          chatTextarea.style.height = 'auto';
        }
      }
    });
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

  // === 工具栏按钮交互 ===
  $$('.chat-tool-btn').forEach(function (btn) {
    btn.addEventListener('click', function () {
      // 切换 mode-active 状态
      if (btn.classList.contains('mode-active')) {
        btn.classList.remove('mode-active');
      } else {
        $$('.chat-tool-btn').forEach(function (b) { b.classList.remove('mode-active'); });
        btn.classList.add('mode-active');
      }
    });
  });

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

})();
