/* ============================================================
   chat-more-menu.js — 聊天「更多」菜单 + 左上角退出按钮交互
   （颐和·智慧康养 · 新中式康养官网 / 桌面端）
   ============================================================ */
(function () {
  var exitBtn     = document.getElementById('chatExitBtn');
  var chatPanel   = document.getElementById('chatPanel');
  var chatInput   = document.getElementById('chatInput');
  var batchBar    = document.getElementById('batchBar');
  var msgContacts = document.getElementById('msgContacts');

  /* ===== 左上角退出按钮 =====
     处于会话中 -> 退出会话，回到联系人列表
     已在联系人列表 -> 返回上级页面（首页 Tab） */
  function exitConversation() {
    if (chatPanel && !chatPanel.hidden) {
      // 1) 会话中：收起会话面板，回到联系人列表
      chatPanel.hidden = true;
      chatPanel.classList.remove('batch-mode');
      if (batchBar) batchBar.hidden = true;
      if (chatInput) chatInput.disabled = false;

      var sel = document.querySelector('#tab-chat .msg-ct.selected');
      if (sel) sel.classList.remove('selected');

      var inline = document.getElementById('inlineSearchWrapper');
      if (inline) inline.remove();
    } else {
      // 2) 列表态：清除选中并跳回首页（返回上级页面）
      if (msgContacts) msgContacts.hidden = false;
      var sel2 = document.querySelector('#tab-chat .msg-ct.selected');
      if (sel2) sel2.classList.remove('selected');

      var homeNav = document.querySelector('[data-tab="home"]');
      if (homeNav) homeNav.click();
    }

    var menu = document.getElementById('chatMoreMenu');
    if (menu) menu.hidden = true;
  }

  if (exitBtn) {
    exitBtn.addEventListener('click', function (e) {
      e.stopPropagation();
      exitConversation();
    });
  }

  /* ===== 其余：更多菜单逻辑 ===== */
  var moreBtn = document.getElementById('chatMore');
  var menu = document.getElementById('chatMoreMenu');
  if (!moreBtn || !menu) return;

  syncMenuState();

  moreBtn.addEventListener('click', function (e) {
    e.stopPropagation();
    var hidden = menu.hidden;
    menu.hidden = !hidden;
    if (!hidden) syncMenuState();
  });

  document.addEventListener('click', function (e) {
    if (!menu.contains(e.target) && e.target !== moreBtn) {
      menu.hidden = true;
    }
  });

  function syncMenuState() {
    var isBatch = chatPanel && chatPanel.classList.contains('batch-mode');
    var itemBatch = document.getElementById('moreBatch');
    var itemCancel = document.getElementById('moreBatchCancel');
    if (itemBatch) itemBatch.hidden = isBatch;
    if (itemCancel) itemCancel.hidden = !isBatch;
  }

  document.getElementById('moreSearch').addEventListener('click', function () {
    menu.hidden = true;
    showInlineSearch();
  });

  function showInlineSearch() {
    var body = document.getElementById('chatBody');
    if (!body) return;
    var existing = document.getElementById('inlineSearchWrapper');
    if (existing) { existing.remove(); return; }

    var wrapper = document.createElement('div');
    wrapper.id = 'inlineSearchWrapper';
    wrapper.innerHTML =
      '<div style="padding:8px 20px 6px;border-bottom:1px solid #E8E0D6;display:flex;gap:8px;align-items:center;background:#FAF8F5;">' +
        '<input type="search" id="inlineSearchInput" placeholder="搜索聊天记录…" style="flex:1;padding:7px 12px;border:1px solid #E8E0D6;border-radius:18px;font-size:13px;outline:none;color:#1A1611;">' +
        '<button id="inlineSearchClose" style="padding:4px 10px;border:none;background:none;cursor:pointer;font-size:16px;color:#8A8078;"><svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>' +
      '</div>';
    body.insertBefore(wrapper, body.firstChild);

    var input = document.getElementById('inlineSearchInput');
    if (input) input.focus();

    document.getElementById('inlineSearchClose').addEventListener('click', function () {
      wrapper.remove();
    });
  }

  document.getElementById('moreBatch').addEventListener('click', function () {
    menu.hidden = true;
    if (chatPanel) {
      chatPanel.classList.add('batch-mode');
      if (batchBar) batchBar.hidden = false;
      if (chatInput) chatInput.disabled = true;
      syncMenuState();
    }
  });

  document.getElementById('moreBatchCancel').addEventListener('click', function () {
    menu.hidden = true;
    if (chatPanel) {
      chatPanel.classList.remove('batch-mode');
      if (batchBar) batchBar.hidden = true;
      if (chatInput) chatInput.disabled = false;
      var checked = chatPanel.querySelectorAll('.msg-row.batch-checked');
      for (var i = 0; i < checked.length; i++) {
        checked[i].classList.remove('batch-checked');
      }
      var info = document.getElementById('batchInfo');
      if (info) info.textContent = '已选 0 条';
      var delBtn = document.getElementById('batchDel');
      if (delBtn) delBtn.textContent = '删除';
      syncMenuState();
    }
  });

  if (typeof MutationObserver !== 'undefined' && chatPanel) {
    var observer = new MutationObserver(function (mutations) {
      for (var i = 0; i < mutations.length; i++) {
        if (mutations[i].attributeName === 'class') syncMenuState();
      }
    });
    observer.observe(chatPanel, { attributes: true });
  }
})();
