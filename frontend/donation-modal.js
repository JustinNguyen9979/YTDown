const DonationModal = (() => {
  // ── Cấu hình thông tin thanh toán ─────────────────────────────
  const CONFIG = {
    bank: {
      name:       'MB Bank',
      accountNo:  '07988888 88888',  
      displayNo:  '079 88888 88888', 
      holder:     'NGUYEN DUC HUY',
      memo:       'Support YTDown',
      qrUrl: `https://img.vietqr.io/image/MB-0798888888888-compact.png`,
    },
    momo: {
      phone:      '0982579098',       
      displayPhone: '0982 579 098',   
      holder:     'NGUYEN DUC HUY',
      qrUrl:      `https://img.vietqr.io/image/momo-0982579098-compact.png`,
    },
    kofi:   { url: 'https://ko-fi.com/justinnguyenvn' },
    paypal: { 
      url: 'https://paypal.me/hades011097',
      email: 'duchuy_1997@hotmail.com'
    },  
  };

  // ── DOM References ─────────────────────────────────────────────
  let backdrop, modal, closeBtn, tabs, panes, copyBtns;
  let initialized = false;

  function init() {
    if (initialized) return;

    backdrop  = document.getElementById('donationBackdrop');
    modal     = document.getElementById('donationModal');
    closeBtn  = document.getElementById('dmCloseBtn');
    tabs      = document.querySelectorAll('.dm-tab');
    panes     = document.querySelectorAll('.dm-pane');
    copyBtns  = document.querySelectorAll('.dm-copy-btn');

    if (!backdrop) {
      console.warn('[DonationModal] #donationBackdrop not found in DOM.');
      return;
    }

    // Populate data from CONFIG into DOM
    _populateData();

    // Tab switching
    tabs.forEach(tab => {
      tab.addEventListener('click', () => _switchTab(tab.dataset.tab));
    });

    // Copy buttons
    copyBtns.forEach(btn => {
      btn.addEventListener('click', () => _copyText(btn.dataset.copy, btn));
    });

    // Make intl cards clickable
    document.querySelectorAll('.dm-intl-card').forEach(card => {
      card.addEventListener('click', (e) => {
        // Prevent recursive clicking if <a> was the target
        if (e.target.tagName === 'A' || e.target.closest('a')) {
          e.preventDefault();
        }
        
        const link = card.querySelector('a');
        if (link && window.runtime && window.runtime.BrowserOpenURL) {
          window.runtime.BrowserOpenURL(link.href);
        } else if (link) {
          // Fallback for browser testing
          window.open(link.href, '_blank');
        }
      });
    });

    // Close on backdrop click (outside modal)
    backdrop.addEventListener('click', (e) => {
      if (e.target === backdrop) close();
    });

    // Close on button ✕
    closeBtn.addEventListener('click', close);

    // Close on Escape key
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && backdrop.classList.contains('open')) close();
    });

    initialized = true;
  }

  // ── Populate Data from CONFIG into HTML ─────────────────────────
  function _populateData() {
    // Bank
    const stkEl = document.getElementById('dmBankSTK');
    if (stkEl) stkEl.textContent = CONFIG.bank.displayNo;

    const bankQR = document.getElementById('dmBankQR');
    if (bankQR) bankQR.src = CONFIG.bank.qrUrl;

    // MoMo
    const momoPhoneEl = document.getElementById('dmMomoPhone');
    if (momoPhoneEl) momoPhoneEl.textContent = CONFIG.momo.displayPhone;

    const momoQR = document.getElementById('dmMomoQR');
    if (momoQR) momoQR.src = CONFIG.momo.qrUrl;

    // Update data-copy attributes
    const copyBtnList = document.querySelectorAll('.dm-copy-btn');
    const copyValues = [
      CONFIG.bank.accountNo,
      CONFIG.bank.memo,
      CONFIG.momo.phone,
    ];
    copyValues.forEach((val, i) => {
      if (copyBtnList[i]) copyBtnList[i].dataset.copy = val;
    });

    // Ko-fi & PayPal links
    const kofiBtn  = document.querySelector('.dm-intl-btn.kofi');
    const paypalBtn = document.querySelector('.dm-intl-btn.paypal');
    if (kofiBtn)   kofiBtn.href   = CONFIG.kofi.url;
    if (paypalBtn) paypalBtn.href = CONFIG.paypal.url;

    const paypalDesc = document.getElementById('dmPaypalDesc');
    if (paypalDesc) paypalDesc.textContent = CONFIG.paypal.email;
  }

  // ── Chuyển tab ─────────────────────────────────────────────────
  function _switchTab(tabName) {
    tabs.forEach(t => t.classList.toggle('active', t.dataset.tab === tabName));
    panes.forEach(p => p.classList.toggle('active', p.id === `dm-pane-${tabName}`));
  }

  // ── Copy to clipboard ─────────────────────────────────────────
  const COPY_ICON = `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2">
    <rect x="9" y="9" width="13" height="13" rx="2"/>
    <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>
  </svg>`;
  const CHECK_ICON = `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
    <polyline points="20 6 9 17 4 12"/>
  </svg>`;

  function _copyText(text, btn) {
    if (!text) return;

    // Wails app có thể dùng clipboard API của browser hoặc Go backend
    // navigator.clipboard hoạt động tốt trong Wails WebView
    navigator.clipboard.writeText(text).then(() => {
      btn.classList.add('copied');
      btn.innerHTML = CHECK_ICON;
      setTimeout(() => {
        btn.classList.remove('copied');
        btn.innerHTML = COPY_ICON;
      }, 1800);
    }).catch(() => {
      // Fallback cho môi trường không hỗ trợ clipboard API
      const el = document.createElement('textarea');
      el.value = text;
      el.style.position = 'fixed';
      el.style.opacity = '0';
      document.body.appendChild(el);
      el.select();
      document.execCommand('copy');
      document.body.removeChild(el);

      btn.classList.add('copied');
      btn.innerHTML = CHECK_ICON;
      setTimeout(() => {
        btn.classList.remove('copied');
        btn.innerHTML = COPY_ICON;
      }, 1800);
    });
  }

  // ── Public API ─────────────────────────────────────────────────
  function open() {
    init();
    if (!backdrop) return;
    backdrop.classList.add('open');
    // Reset về tab đầu tiên mỗi lần mở
    _switchTab('bank');
  }

  function close() {
    if (!backdrop) return;
    backdrop.classList.remove('open');
  }

  return { open, close };
})();