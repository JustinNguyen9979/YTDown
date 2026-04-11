// Wails v2 application main logic

// Add debug logging as soon as script loads
console.log('[BOOT] app.js loaded');

// Main app state
const state = {
    savePath: '',
    currentFormat: 'MP4',
    currentQuality: 'Best Quality',
    batchThreads: 3,
    galleryThreads: 3,
    wailsReady: false,
    selectedCompressFiles: [],
    cookieConfig: {
        mode: 'none',
        selected_browser: '',
        manual_header: ''
    },
    availableBrowsers: [],
    temporaryCookieDraft: '',
    maskedCookieValue: '',
    isSubmittingCookie: false,
    cookieHasError: false,
    downloadedFiles: {}, // Store index -> localFilePath
    batchSessionStatus: 'idle',
    gallerySessionStatus: 'idle'
};

// Wait counter to prevent infinite loops
let wailsWaitAttempts = 0;
const MAX_WAILS_WAIT_ATTEMPTS = 100; // 10 seconds max (100 * 100ms)

function populateSelectOptions() {
  // Threads array dùng chung cho 2 select
  const threads = Array.from({ length: 10 }, (_, i) => ({
    value: String(i + 1),
    text: String(i + 1)
  }));

  const selectConfig = [
    {
      id: 'batchFormatSelect',
      defaultValue: 'MP4',
      options: [
        { value: 'MP4',  text: 'MP4' },
        { value: 'MP3',  text: 'MP3' }
      ]
    },
    {
      id: 'batchQualitySelect',
      defaultValue: 'Best Quality',
      options: [
        { value: 'Best Quality', text: 'Best Quality' },
        { value: '1080p',        text: '1080p' },
        { value: '720p',         text: '720p' },
        { value: '480p',         text: '480p' },
        { value: '360p',         text: '360p' }
      ]
    },
    { id: 'batchThreadsSelect',   defaultValue: '3', options: threads },
    { id: 'galleryThreadsSelect', defaultValue: '3', options: threads },
    {
      id: 'galleryBrowserSelect',
      defaultValue: '',
      options: [
        { value: '',         text: 'None'    },
        { value: 'chrome',   text: 'Chrome'  },
        { value: 'firefox',  text: 'Firefox' },
        { value: 'safari',   text: 'Safari'  },
        { value: 'edge',     text: 'Edge'    },
        { value: 'opera',    text: 'Opera'   },
        { value: 'vivaldi',  text: 'Vivaldi' }
      ]
    },
    {
      id: 'compressType',
      defaultValue: 'image',
      options: [
        { value: 'image', text: 'Image' },
        { value: 'video', text: 'Video' }
      ]
    },
    {
      id: 'selectionMode',
      defaultValue: 'file',
      options: [
        { value: 'file',   text: 'Files'  },
        { value: 'folder', text: 'Folder' }
      ]
    },
    {
      id: 'compressQuality',
      defaultValue: 'medium',
      options: [
        { value: 'medium', text: 'Medium (Balanced)'  },
        { value: 'high',   text: 'High (Keep Quality)' },
        { value: 'low',    text: 'Low (Smallest Size)' },
        { value: 'custom', text: 'Custom Quality'      }
      ]
    }
  ];

  selectConfig.forEach(({ id, options, defaultValue }) => {
    const select = document.getElementById(id);
    if (!select) return;
    select.innerHTML = options
      .map(opt =>
        `<option value="${opt.value}"${opt.value === defaultValue ? ' selected' : ''}>${opt.text}</option>`
      )
      .join('');
  });
}

function renderTableHeaders() {
  const tableHeaders = {
    batchTable:    ['#', 'Thumbnail', 'Video Title', 'Status', 'Progress'],
    galleryTable:  ['#', 'Image Title', 'Status', 'Progress'],
    compressTable: ['#', 'File Name', 'Status', 'Progress']
  };

  Object.entries(tableHeaders).forEach(([tableId, headers]) => {
    const table = document.getElementById(tableId);
    if (!table) return;
    const thead = document.createElement('thead');
    thead.innerHTML = `<tr>${headers.map(h => `<th>${h}</th>`).join('')}</tr>`;
    table.prepend(thead);
  });
}

function renderTabs() {
  const tabs = [
    { id: 'batch',    label: 'Download Video' },
    { id: 'gallery',  label: 'Download Images' },
    { id: 'compress', label: 'Compress' },
    { id: 'info',     label: 'ℹ️ Info', style: 'margin-left: auto;' }
  ];

  document.getElementById('mainTabs').innerHTML = tabs.map((tab, i) =>
    `<button type="button" class="tab-btn${i === 0 ? ' active' : ''}" 
      data-tab="${tab.id}"${tab.style ? ` style="${tab.style}"` : ''}>${tab.label}</button>`
  ).join('');
}

function truncateMiddle(fullStr, strLen, separator) {
    if (fullStr.length <= strLen) return fullStr;
    
    separator = separator || '...';
    
    var sepLen = separator.length,
        charsToShow = strLen - sepLen,
        frontChars = Math.ceil(charsToShow / 2),
        backChars = Math.floor(charsToShow / 2);
    
    return fullStr.substr(0, frontChars) + 
           separator + 
           fullStr.substr(fullStr.length - backChars);
}

// Initialize app
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initApp);
    console.log('[BOOT] Waiting for DOMContentLoaded');
} else {
    console.log('[BOOT] DOM already loaded, initializing');
    initApp();
}

function initApp() {
    console.log('[BOOT] Initializing...');
    waitForWails();
}

function waitForWails() {
    wailsWaitAttempts++;
    
    // Check for Wails v2 Go bindings
    if (typeof window !== 'undefined' && window.go && window.go.main && window.go.main.App) {
        console.log('[BOOT] Wails runtime ready after', wailsWaitAttempts, 'attempts!');
        state.wailsReady = true;
        wailsWaitAttempts = 0; // Reset counter
        initializeApp();
    } else if (wailsWaitAttempts < MAX_WAILS_WAIT_ATTEMPTS) {
        console.log(`[BOOT] Waiting for Wails... (attempt ${wailsWaitAttempts}/${MAX_WAILS_WAIT_ATTEMPTS})`);
        setTimeout(waitForWails, 100);
    } else {
        console.error('[BOOT] Wails never initialized! Running in browser-only mode.');
        state.wailsReady = false;
        // Still initialize the app even if Wails is not available
        initializeApp();
    }
}

async function initializeApp() {
    console.log('[BOOT] App initialization started');

    populateSelectOptions();
    renderTabs();
    renderTableHeaders();
    renderOptionRows();
    
    // Load default save path and cookie config
    if (state.wailsReady) {
        try {
            const path = await window.go.main.App.GetDefaultSavePath();
            state.savePath = path;
            const bsp = document.getElementById('batchSavePath');
            if (bsp) bsp.value = path;
            const csp = document.getElementById('compressSavePath');
            if (csp) csp.value = path;
            const gsp = document.getElementById('gallerySavePath');
            if (gsp) gsp.value = path;

            // Load cookie config and browsers
            state.cookieConfig = await window.go.main.App.GetCookieConfig();
            state.availableBrowsers = await window.go.main.App.GetAvailableBrowsers();
            console.log('[BOOT] Cookie config loaded:', state.cookieConfig);
            console.log('[BOOT] Available browsers:', state.availableBrowsers);
        } catch (err) {
            console.error('[BOOT] Error loading initialization data:', err);
        }
    } else {
        console.log('[BOOT] Wails not ready, using browser-only mode');
        state.savePath = '/Downloads';
        const bsp = document.getElementById('batchSavePath');
        if (bsp) bsp.value = '[Wails not ready]';
        const csp = document.getElementById('compressSavePath');
        if (csp) csp.value = '[Wails not ready]';
        const gsp = document.getElementById('gallerySavePath');
        if (gsp) gsp.value = '[Wails not ready]';
    }
    
    setupTabs();
    setupCookieDropdown();
    setupBatchTab();
    setupGalleryTab();
    setupGlobalStatusTooltip();
    setupCompressTab();
    setupGoEvents();
    setupWindowAutoHug();
    renderInfoTab();
    setupInfoTab();
    
    if (state.wailsReady) {
        checkUpdates();
        refreshCookieUI();
    }
    
    console.log('[BOOT] Initialization complete!');
}

function setupCookieDropdown() {
    const mainBtns = [
        document.getElementById('cookieMainBtn'),
        document.getElementById('galleryCookieMainBtn')
    ];
    const menu = document.getElementById('cookieDropdownMenu');
    const browserList = document.getElementById('browserList');

    if (!menu || !browserList) return;

    // Populate browser list
    const browserNames = {
        chrome: 'Chrome',
        brave: 'Brave',
        edge: 'Edge',
        safari: 'Safari',
        firefox: 'Firefox',
        opera: 'Opera',
        vivaldi: 'Vivaldi'
    };

    const updateBrowserList = () => {
        browserList.innerHTML = '';
        if (state.availableBrowsers.length === 0) {
            const item = document.createElement('div');
            item.className = 'cookie-dropdown-item';
            item.style.opacity = '0.5';
            item.style.fontSize = '11px';
            item.textContent = 'No browsers detected';
            browserList.appendChild(item);
            return;
        }

        state.availableBrowsers.forEach(id => {
            const item = document.createElement('div');
            item.className = 'cookie-dropdown-item';
            if (state.cookieConfig.mode === 'browser' && state.cookieConfig.selected_browser === id) {
                item.classList.add('selected');
            }
            item.innerHTML = `🌐 ${browserNames[id] || id}`;
            item.onclick = async (e) => {
                e.stopPropagation();
                try {
                    await window.go.main.App.UpdateCookieConfig('browser', id);
                    state.cookieConfig.mode = 'browser';
                    state.cookieConfig.selected_browser = id;
                    state.cookieConfig.manual_header = '';
                    menu.hidden = true;
                    refreshCookieUI();
                } catch (err) {
                    console.error('Failed to update cookie config:', err);
                }
            };
            browserList.appendChild(item);
        });
    };

    mainBtns.forEach(btn => {
        if (!btn) return;
        btn.onclick = (e) => {
            e.stopPropagation();
            const willShow = menu.hidden;
            closeAllDropdowns();
            const container = btn.parentElement;
            container.appendChild(menu);
            menu.hidden = !willShow;
            if (!menu.hidden) {
                updateBrowserList();
            }
        };
    });

    // Manual mode click
    const manualItem = menu.querySelector('[data-mode="manual"]');
    if (manualItem) {
        manualItem.onclick = (e) => {
            e.stopPropagation();
            menu.hidden = true;
            // Open the manual input inline (either video or gallery depending on current tab)
            const activeTab = document.querySelector('.tab-content.active').id;
            const inlineId = activeTab === 'gallery' ? 'galleryCookieInline' : 'cookieInline';
            const inputId = activeTab === 'gallery' ? 'galleryCookieInput' : 'cookieInput';
            
            const inline = document.getElementById(inlineId);
            const input = document.getElementById(inputId);
            
            if (inline && input) {
                inline.hidden = false;
                state.cookieHasError = false;
                input.value = state.temporaryCookieDraft || state.maskedCookieValue || '';
                setTimeout(() => input.focus(), 0);
            }
        };
    }

    // Clear Cookie click
    const clearCookieBtn = document.getElementById('clearCookieBtn');
    if (clearCookieBtn) {
        clearCookieBtn.onclick = async (e) => {
            e.stopPropagation();
            menu.hidden = true;
            try {
                await window.go.main.App.ClearCookieConfig();
                state.cookieConfig = {
                    mode: 'none',
                    selected_browser: '',
                    manual_header: ''
                };
                state.maskedCookieValue = '';
                state.temporaryCookieDraft = '';
                refreshCookieUI();
            } catch (err) {
                console.error('Failed to clear cookie config:', err);
            }
        };
    }

    // Close menu or manual input when clicking outside
    document.addEventListener('click', (event) => {
        // Close dropdown menu
        menu.hidden = true;

        // Close manual input overlays if clicked outside
        const inlines = [
            document.getElementById('cookieInline'),
            document.getElementById('galleryCookieInline')
        ];
        
        inlines.forEach(inline => {
            if (inline && !inline.hidden) {
                const target = event.target;
                if (!inline.contains(target)) {
                    inline.hidden = true;
                    const input = inline.querySelector('input');
                    if (input) input.value = '';
                }
            }
        });
    });
}

function refreshCookieUI() {
    const mainBtns = [
        document.getElementById('cookieMainBtn'),
        document.getElementById('galleryCookieMainBtn')
    ];
    const badges = [
        document.getElementById('cookieAddedBadge'),
        document.getElementById('galleryCookieAddedBadge')
    ];
    const galleryBrowserRow = document.getElementById('galleryBrowserRow');

    const browserNames = {
        chrome: 'Chrome',
        brave: 'Brave',
        edge: 'Edge',
        safari: 'Safari',
        firefox: 'Firefox',
        opera: 'Opera',
        vivaldi: 'Vivaldi'
    };

    let btnText = 'Add cookie';
    let badgeHidden = true;

    if (state.cookieConfig.mode === 'browser' && state.cookieConfig.selected_browser) {
        btnText = `Cookie: ${browserNames[state.cookieConfig.selected_browser] || state.cookieConfig.selected_browser}`;
        badgeHidden = false;
        if (galleryBrowserRow) galleryBrowserRow.style.display = 'none';
    } else if (state.cookieConfig.mode === 'manual') {
        btnText = 'Cookie: Manual';
        badgeHidden = false;
        if (galleryBrowserRow) galleryBrowserRow.style.display = 'flex';
    } else {
        if (galleryBrowserRow) galleryBrowserRow.style.display = 'flex';
    }

    mainBtns.forEach(btn => { if (btn) btn.textContent = btnText; });
    badges.forEach(badge => { if (badge) badge.hidden = badgeHidden; });
}

function setupGalleryTab() {
    const clearBtn = document.getElementById('clearGalleryBtn');
    const browseBtn = document.getElementById('browseGalleryBtn');
    const startBtn = document.getElementById('startGalleryBtn');
    const cancelBtn = document.getElementById('cancelGalleryBtn');
    const urlsTextarea = document.getElementById('galleryUrls');
    const threadsSelect = document.getElementById('galleryThreadsSelect');
    const browserSelect = document.getElementById('galleryBrowserSelect');
    const ugoiraCheckbox = document.getElementById('galleryUgoiraToWebm');
    const savePathInput = document.getElementById('gallerySavePath');
    const openFolderBtn = document.getElementById('openGalleryFolderBtn');
    const cookieInline = document.getElementById('galleryCookieInline');
    const cookieInput = document.getElementById('galleryCookieInput');
    const confirmCookieBtn = document.getElementById('confirmGalleryCookieBtn');
    const cookieAddedBadge = document.getElementById('galleryCookieAddedBadge');

    if (!clearBtn || !startBtn || !urlsTextarea || !savePathInput) return;

    // --- Media Formats Dynamic Rendering ---
    const mediaFormats = [
        { 
            group: 'Images', 
            items: [
                { ext: 'jpg', default: true },
                { ext: 'jpeg', default: true },
                { ext: 'png', default: true },
                { ext: 'webp', default: false },
                { ext: 'gif', default: false },
                { ext: 'heic', default: false },
                { ext: 'avif', default: false }
            ]
        },
        { 
            group: 'Videos', 
            items: [
                { ext: 'mp4', default: false },
                { ext: 'webm', default: false },
                { ext: 'mkv', default: false },
                { ext: 'mov', default: false },
                { ext: 'avi', default: false }
            ]
        }
    ];

    const formatsContainer = document.getElementById('galleryFormatsOptions');
    const mediaListContainer = document.getElementById('galleryMediaList');
    const formatsTrigger = document.getElementById('galleryFormatsTrigger');
    const allCheckbox = document.getElementById('fmt-all');

    if (mediaListContainer && formatsTrigger) {
        mediaListContainer.innerHTML = ''; // Clear
        
        mediaFormats.forEach(group => {
            // Group header
            const header = document.createElement('div');
            header.className = 'cookie-dropdown-header';
            header.style.padding = '8px 12px';
            header.style.fontSize = '10px';
            header.style.color = 'var(--text-secondary)';
            header.style.textTransform = 'uppercase';
            header.textContent = group.group === 'Images' ? '🖼 Images' : '🎬 Videos';
            mediaListContainer.appendChild(header);

            // Group items
            group.items.forEach(item => {
                const opt = document.createElement('div');
                opt.className = 'custom-option multi';
                opt.dataset.value = item.ext;
                
                const checkbox = document.createElement('input');
                checkbox.type = 'checkbox';
                checkbox.id = `fmt-${item.ext}`;
                checkbox.checked = item.default;
                
                const label = document.createElement('label');
                label.htmlFor = `fmt-${item.ext}`;
                label.textContent = item.ext.toUpperCase();
                
                opt.appendChild(checkbox);
                opt.appendChild(label);
                mediaListContainer.appendChild(opt);

                opt.onclick = (e) => {
                    if (e.target !== checkbox && e.target !== label) {
                        checkbox.checked = !checkbox.checked;
                        checkbox.dispatchEvent(new Event('change'));
                    }
                };
            });
        });

        const formatCheckboxes = mediaListContainer.querySelectorAll('input[type="checkbox"]');
        
        const updateTriggerText = () => {
            const selected = Array.from(formatCheckboxes)
                .filter(cb => cb.checked)
                .map(cb => cb.id.replace('fmt-', '').toUpperCase());
            
            if (selected.length === 0) {
                formatsTrigger.textContent = 'None';
            } else if (selected.length === formatCheckboxes.length) {
                formatsTrigger.textContent = 'All Formats';
            } else if (selected.length > 3) {
                // Show first 3 and ... to avoid layout break
                formatsTrigger.textContent = selected.slice(0, 3).join(', ') + '...';
            } else {
                formatsTrigger.textContent = selected.join(', ');
            }
        };

        allCheckbox?.addEventListener('change', () => {
            formatCheckboxes.forEach(cb => cb.checked = allCheckbox.checked);
            updateTriggerText();
        });

        formatCheckboxes.forEach(cb => {
            cb.addEventListener('change', () => {
                const allChecked = Array.from(formatCheckboxes).every(c => c.checked);
                if (allCheckbox) allCheckbox.checked = allChecked;
                updateTriggerText();
            });
        });

        document.getElementById('galleryFormatsTrigger').onclick = (e) => {
            e.stopPropagation();
            const isOpen = document.getElementById('galleryFormatsContainer').classList.contains('open');
            closeAllDropdowns(); 
            if (!isOpen) document.getElementById('galleryFormatsContainer').classList.add('open');
        };

        updateTriggerText();
    }
    // --- End Media Formats Logic ---

    if (threadsSelect) {
        threadsSelect.value = String(state.galleryThreads);
        threadsSelect.addEventListener('change', (event) => {
            const value = Number.parseInt(event.target.value, 10);
            state.galleryThreads = Number.isNaN(value) ? 3 : Math.min(Math.max(value, 1), 10);
            threadsSelect.value = String(state.galleryThreads);
        });
    }

    applyGalleryControlState('idle');

    savePathInput.value = state.savePath;

    clearBtn.addEventListener('click', () => {
        urlsTextarea.value = '';
        const tbody = document.getElementById('galleryTableBody');
        if (tbody) tbody.innerHTML = '';
        clearGalleryResultMessage();
    });

    if (cookieInline && cookieInput && confirmCookieBtn) {
        cookieInput.addEventListener('paste', (event) => {
            const pasted = event.clipboardData?.getData('text') || '';
            if (!pasted) return;
            event.preventDefault();
            state.temporaryCookieDraft = pasted;
            cookieInput.value = pasted;
        });

        cookieInput.addEventListener('input', (event) => {
            state.temporaryCookieDraft = event.target.value;
        });

        confirmCookieBtn.addEventListener('click', async () => {
            const rawCookie = state.temporaryCookieDraft.trim();
            if (!rawCookie) {
                if (!state.maskedCookieValue) {
                    showGalleryError('Cookie error: please paste a Cookie header first');
                } else {
                    cookieInline.hidden = true;
                }
                return;
            }

            try {
                await window.go.main.App.SetTemporaryYouTubeCookie(rawCookie);
                state.cookieConfig.mode = 'manual';
                state.maskedCookieValue = maskCookieValue(rawCookie);
                state.temporaryCookieDraft = '';
                cookieInline.hidden = true;
                cookieInput.value = '';
                clearGalleryResultMessage();
                refreshCookieUI();
            } catch (err) {
                showGalleryError('Cookie error: ' + (err?.message || err));
            }
        });
    }

    browseBtn.addEventListener('click', async () => {
        if (!state.wailsReady) return;
        const path = await window.go.main.App.OpenFolderDialog();
        if (path) {
            savePathInput.value = path;
            state.savePath = path;
        }
    });

    if (openFolderBtn) {
        openFolderBtn.addEventListener('click', () => {
            const currentPath = savePathInput.value || state.savePath;
            if (state.wailsReady) window.go.main.App.OpenSaveFolder(currentPath);
        });
    }

    startBtn.addEventListener('click', async () => {
        const urls = urlsTextarea.value.split('\n').map(u => u.trim()).filter(u => u.length > 0);
        if (urls.length === 0) {
            showGalleryError('Please enter at least one gallery URL');
            return;
        }

        const tbody = document.getElementById('galleryTableBody');
        tbody.innerHTML = ''; 

        urls.forEach((url, i) => {
            const row = document.createElement('tr');
            row.id = `gallery-row-${i}`;
            row.innerHTML = `
                <td>${i + 1}</td>
                <td title="${url}">${truncateMiddle(url, 40)}</td>
                <td><span class="status-icon">⏳</span> Waiting</td>
                <td><div class="batch-progress-bar"><div class="batch-progress-fill" style="width: 0%;"></div></div></td>
            `;
            tbody.appendChild(row);
        });

        applyGalleryControlState('running');
        clearGalleryResultMessage();

        const options = {
            savePath: savePathInput.value,
            threads: parseInt(threadsSelect.value),
            browser: browserSelect.value,
            ugoiraToWebm: ugoiraCheckbox.checked,
            formats: Array.from(formatCheckboxes).filter(cb => cb.checked).map(cb => cb.closest('.custom-option').dataset.value),
            archive: document.getElementById('galleryArchive')?.checked || false,
            extraArgs: document.getElementById('galleryArgs')?.value || ''
        };

        try {
            const result = await window.go.main.App.StartGalleryBatchDownload(urls, options);
            if (typeof result === 'string' && result.startsWith('Error:')) {
                throw new Error(result);
            }
        } catch (err) {
            applyGalleryControlState('idle');
            showGalleryError('Error: ' + (err?.message || err));
        }
    });

    if (cancelBtn) {
        cancelBtn.addEventListener('click', async () => {
            cancelBtn.disabled = true;
            try {
                await window.go.main.App.CancelGalleryDownload();
            } catch (err) {
                cancelBtn.disabled = false;
                showGalleryError('Error: ' + (err?.message || err));
            }
        });
    }

    setTimeout(initCustomSelects, 0);
}

function applyGalleryControlState(status) {
    state.gallerySessionStatus = status;
    const startBtn = document.getElementById('startGalleryBtn');
    const cancelBtn = document.getElementById('cancelGalleryBtn');
    const clearBtn = document.getElementById('clearGalleryBtn');
    const urlsTextarea = document.getElementById('galleryUrls');
    const browseBtn = document.getElementById('browseGalleryBtn');
    const threadsSelect = document.getElementById('galleryThreadsSelect');
    const browserSelect = document.getElementById('galleryBrowserSelect');
    const ugoiraCheckbox = document.getElementById('galleryUgoiraToWebm');

    if (!startBtn || !cancelBtn) return;

    const isRunning = status === 'running';
    
    startBtn.disabled = isRunning;
    startBtn.innerHTML = isRunning ? '🖼 Downloading...' : '🖼 Start Download Images';
    cancelBtn.hidden = !isRunning;
    cancelBtn.disabled = !isRunning;

    if (clearBtn) clearBtn.disabled = isRunning;
    if (urlsTextarea) urlsTextarea.disabled = isRunning;
    if (browseBtn) browseBtn.disabled = isRunning;
    if (threadsSelect) threadsSelect.disabled = isRunning;
    if (browserSelect) browserSelect.disabled = isRunning;
    if (ugoiraCheckbox) ugoiraCheckbox.disabled = isRunning;

    refreshCustomSelectStates();
}

function showGalleryError(message) {
    const el = document.getElementById('galleryResultMessage');
    if (el) {
        el.className = 'result-message error';
        el.textContent = message;
        el.style.display = 'block';
    }
}

function clearGalleryResultMessage() {
    const el = document.getElementById('galleryResultMessage');
    if (el) {
        el.textContent = '';
        el.style.display = 'none';
        el.className = 'result-message';
    }
}

async function checkUpdates() {
    try {
        const versions = await window.go.main.App.GetVersionStatus();
        const ytdlp = versions.find(v => v.name === 'yt-dlp');
        
        if (ytdlp && ytdlp.canUpgrade) {
            const banner = document.getElementById('update-banner');
            if (banner) {
                banner.innerHTML = `
                    <span>🚀 A new version of yt-dlp is available (v${ytdlp.current} → v${ytdlp.latest})</span>
                    <button class="upgrade-btn" id="upgradeBtn">Upgrade Now</button>
                `;
                banner.style.display = 'flex';
                
                document.getElementById('upgradeBtn').addEventListener('click', async () => {
                    const btn = document.getElementById('upgradeBtn');
                    const span = banner.querySelector('span');
                    btn.style.display = 'none';
                    if (span) span.innerText = 'Initializing upgrade...';
                    try {
                        await window.go.main.App.UpgradeBinary('yt-dlp');
                        banner.innerHTML = '<span>✅ yt-dlp upgraded successfully! Please restart the app.</span>';
                        setTimeout(() => banner.style.display = 'none', 8000);
                    } catch (err) {
                        banner.innerHTML = `<span>❌ Upgrade failed: ${err}. Try running 'yt-dlp -U' manually.</span>`;
                        setTimeout(() => banner.style.display = 'none', 10000);
                    }
                });
            }
        }
    } catch (err) {
        console.error('[UPDATER] Error checking updates:', err);
    }
}

let lastSetHeight = 0;

function setupWindowAutoHug() {
    if (typeof window === 'undefined' || !window.runtime || !window.runtime.WindowSetSize) return;

    const updateHeight = () => {
        const container = document.querySelector('.container');
        if (!container) return;

        const contentHeight = Math.ceil(container.getBoundingClientRect().height);
        const windowHeight = contentHeight + 40; 
        
        if (contentHeight > 200 && Math.abs(windowHeight - lastSetHeight) > 5) {
            console.log('[UI] Auto-hugging to:', windowHeight);
            lastSetHeight = windowHeight;
            window.runtime.WindowSetSize(700, windowHeight);
        }
    };

    const container = document.querySelector('.container');
    if (container) {
        const resizeObserver = new ResizeObserver(() => {
            requestAnimationFrame(updateHeight);
        });
        resizeObserver.observe(container);
    }

    document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            setTimeout(updateHeight, 100); 
        });
    });
}

function setupGoEvents() {
    try {
        if (window.runtime && window.runtime.EventsOn) {
            window.runtime.EventsOn('progress-update', updateProgress);
            
            window.runtime.EventsOn('video-info', (data) => {
                const row = document.getElementById(`batch-row-${data.index}`);
                if (row) {
                    const thumbCell = row.querySelector('td:nth-child(2)');
                    const titleCell = row.querySelector('td:nth-child(3)');
                    if (thumbCell && data.thumbnail) {
                        const playIconSvg = `<svg class="play-icon" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>`;
                        // Thumbnail initially dimmed until download finishes
                        thumbCell.innerHTML = `
                            <div class="video-thumbnail-container" id="thumb-container-${data.index}" style="cursor: default; filter: grayscale(1);">
                                <img src="${data.thumbnail}" class="video-thumbnail" alt="thumb">
                                <div class="play-overlay" id="play-overlay-${data.index}">${playIconSvg}</div>
                            </div>`;
                    }
                    if (titleCell && data.title && (titleCell.innerText.startsWith('http') || titleCell.innerText.includes('...'))) {
                         titleCell.innerText = truncateMiddle(data.title.replace(/^["']|["']$/g, ''), 40);
                         titleCell.title = data.title;
                    }
                }
            });

            window.runtime.EventsOn('download-complete', (data) => {
                console.log('[DL] Download complete:', data);
                state.downloadedFiles[data.index] = data.filePath;
                
                // Enable the play button on the thumbnail
                const thumbContainer = document.getElementById(`thumb-container-${data.index}`);
                if (thumbContainer) {
                    thumbContainer.style.cursor = 'pointer';
                    thumbContainer.style.filter = 'none';
                    thumbContainer.onclick = () => {
                        window.go.main.App.OpenFile(data.filePath);
                    };
                }
            });

            window.runtime.EventsOn('video-title', (title) => {
                const rows = document.querySelectorAll('#batchTableBody tr');
                rows.forEach(row => {
                    const statusCell = row.querySelector('td:nth-child(4)');
                    const titleCell = row.querySelector('td:nth-child(3)');
                    if (statusCell && statusCell.innerText.includes('Downloading') && titleCell && titleCell.innerText.startsWith('http')) {
                         titleCell.innerText = truncateMiddle(title.replace(/^["']|["']$/g, ''), 40);
                         titleCell.title = title;
                    }
                });
            });
            
            window.runtime.EventsOn('gallery-progress', (data) => {
                const row = document.getElementById(`gallery-row-${data.index}`);
                if (row) {
                    const statusCell = row.querySelector('td:nth-child(3)');
                    if (statusCell) {
                        statusCell.innerHTML = `
                            <div class="status-with-tooltip" data-tooltip-html="${escapeHtml(data.speed)}">
                                <span class="status-icon">⏳</span>
                                <span>Downloading</span>
                            </div>
                        `;
                    }
                    const fill = row.querySelector('.batch-progress-fill');
                    if (fill) {
                        // If we have percentage in data, use it
                        if (data.percentage > 0) {
                            fill.style.width = data.percentage + '%';
                        } else {
                            fill.style.width = '50%'; // Intermediate progress
                        }
                    }
                }
            });

            window.runtime.EventsOn('gallery-status', (data) => {
                const row = document.getElementById(`gallery-row-${data.index}`);
                if (row) {
                    const statusCell = row.querySelector('td:nth-child(3)');
                    if (statusCell) {
                        const icons = { waiting: '⏳', downloading: '⏳', done: '✅', error: '❌', canceled: '✕' };
                        const status = data.status || 'waiting';
                        statusCell.innerHTML = `
                            <div class="status-with-tooltip" data-tooltip-html="${escapeHtml(data.message || status)}">
                                <span class="status-icon">${icons[status] || '?'}</span>
                                <span>${status.charAt(0).toUpperCase() + status.slice(1)}</span>
                            </div>
                        `;
                    }
                    if (data.status === 'done') {
                        const fill = row.querySelector('.batch-progress-fill');
                        if (fill) {
                            fill.style.width = '100%';
                            fill.style.backgroundColor = '#34c759';
                        }
                    }
                }
            });

            window.runtime.EventsOn('gallery-batch-complete', () => {
                applyGalleryControlState('idle');
            });

            window.runtime.EventsOn('gallery-title', (data) => {
                // data might be a string or object depending on how it's emitted
                const title = typeof data === 'string' ? data : data.title;
                const index = typeof data === 'object' ? data.index : 0;
                const row = document.getElementById(`gallery-row-${index}`);
                if (row) {
                    const titleCell = row.querySelector('td:nth-child(2)');
                    if (titleCell) {
                        titleCell.innerText = truncateMiddle(title, 40);
                        titleCell.title = title;
                    }
                }
            });

            window.runtime.EventsOn('gallery-complete', (data) => {
                const row = document.getElementById(`gallery-row-${data.index}`);
                if (row) {
                    const statusCell = row.querySelector('td:nth-child(3)');
                    if (statusCell) {
                        statusCell.className = 'status-cell status-done';
                        statusCell.innerHTML = `
                            <div class="status-with-tooltip" data-tooltip-html="All images downloaded.">
                                <span class="status-icon">✅</span>
                                <span>Done</span>
                            </div>
                        `;
                    }
                    const fill = row.querySelector('.batch-progress-fill');
                    if (fill) {
                        fill.style.width = '100%';
                        fill.style.backgroundColor = '#34c759';
                    }
                }
                const btn = document.getElementById('startGalleryBtn');
                if (btn) {
                    btn.disabled = false;
                    btn.innerHTML = '🖼 Start Download Images';
                }
            });

            window.runtime.EventsOn('gallery-error', (err) => {
                showError('Gallery Error: ' + err);
                const btn = document.getElementById('startGalleryBtn');
                if (btn) {
                    btn.disabled = false;
                    btn.innerHTML = '🖼 Start Download Images';
                }
            });

            window.runtime.EventsOn('binary-error', (error) => showError('⚠️ Missing Tool: ' + error));
            window.runtime.EventsOn('binary-warning', (warning) => showError('⚠️ Warning: ' + warning));
            window.runtime.EventsOn('upgrade-status', (status) => {
                const banner = document.getElementById('update-banner');
                if (banner && banner.style.display !== 'none') {
                    // Prepend or replace? Let's just update the text content if it exists
                    const span = banner.querySelector('span');
                    if (span) span.innerText = status;
                }
            });
            window.runtime.EventsOn('batch-complete', () => {
                applyBatchControlState('completed');
            });
            window.runtime.EventsOn('batch-paused', () => applyBatchControlState('paused'));
            window.runtime.EventsOn('batch-resumed', () => applyBatchControlState('running'));
            window.runtime.EventsOn('batch-canceled', () => applyBatchControlState('canceled'));
            
            window.runtime.EventsOn('batch-status', (data) => updateBatchStatus(data.index, data.status));
            window.runtime.EventsOn('batch-error', (data) => updateBatchError(data));
            window.runtime.EventsOn('compression-status', (data) => updateCompressStatus(data.index, data.status));
            window.runtime.EventsOn('compression-progress', (data) => updateCompressProgress(data.index, data.status, data.message));
            window.runtime.EventsOn('compression-error', (data) => updateCompressError(data.index, data.error));
            window.runtime.EventsOn('compression-complete', () => {
                const btn = document.getElementById('startCompressBtn');
                if (btn) {
                    btn.disabled = false;
                    btn.textContent = '⚡ Start Compression';
                }
            });
        }
    } catch (err) {
        console.error('[EVENTS] Error:', err);
    }
}

function setupTabs() {
    const btns = document.querySelectorAll('.tab-btn');
    const tabs = document.querySelectorAll('.tab-content');
    
    btns.forEach(btn => {
        btn.addEventListener('click', () => {
            const target = btn.dataset.tab;
            btns.forEach(b => b.classList.toggle('active', b === btn));
            tabs.forEach(t => t.classList.toggle('active', t.id === target));
        });
    });
}

function closeAllDropdowns() {
    const cookieMenu = document.getElementById('cookieDropdownMenu');
    if (cookieMenu) cookieMenu.hidden = true;
    document.querySelectorAll('.custom-select-container').forEach(c => c.classList.remove('open'));
    const galleryFormats = document.getElementById('galleryFormatsContainer');
    if (galleryFormats) galleryFormats.classList.remove('open');
}

function refreshCustomSelectStates() {
    document.querySelectorAll('.custom-select-container').forEach(container => {
        const select = container.querySelector('select');
        container.classList.toggle('disabled', !!select?.disabled);
        if (select?.disabled) {
            container.classList.remove('open');
        }
    });
}

function setBatchProgressValue(index, percentage, color) {
    const row = document.getElementById(`batch-row-${index}`);
    if (!row) return;
    const fill = row.querySelector('.batch-progress-fill');
    if (!fill) return;
    fill.style.width = `${percentage}%`;
    fill.style.backgroundColor = color || '';
}

function applyBatchControlState(status) {
    state.batchSessionStatus = status;

    const startBtn = document.getElementById('startBatchBtn');
    const pauseBtn = document.getElementById('pauseBatchBtn');
    const cancelBtn = document.getElementById('cancelBatchBtn');
    const clearBtn = document.getElementById('clearBatchBtn');
    const textarea = document.getElementById('batchUrls');
    const formatSelect = document.getElementById('batchFormatSelect');
    const qualitySelect = document.getElementById('batchQualitySelect');
    const threadsSelect = document.getElementById('batchThreadsSelect');
    const browseBtn = document.getElementById('browseBatchBtn');
    const savePathInput = document.getElementById('batchSavePath');

    if (!startBtn || !pauseBtn || !cancelBtn || !clearBtn) return;

    const isRunning = status === 'running';
    const isPaused = status === 'paused';
    const hasSession = isRunning || isPaused;

    startBtn.hidden = false;
    pauseBtn.hidden = !hasSession;
    cancelBtn.hidden = !hasSession;

    if (isRunning) {
        startBtn.textContent = '▶ Running';
        startBtn.disabled = true;
        pauseBtn.textContent = '⏸ Pause';
        pauseBtn.disabled = false;
        cancelBtn.disabled = false;
    } else if (isPaused) {
        startBtn.textContent = '▶ Resume';
        startBtn.disabled = false;
        pauseBtn.textContent = '⏸ Paused';
        pauseBtn.disabled = true;
        cancelBtn.disabled = false;
    } else {
        startBtn.textContent = '▶ Start Download Videos';
        startBtn.disabled = false;
        pauseBtn.textContent = '⏸ Pause';
        pauseBtn.disabled = false;
        cancelBtn.disabled = false;
    }

    clearBtn.disabled = isRunning || isPaused;

    if (textarea) textarea.disabled = isRunning || isPaused;
    if (formatSelect) formatSelect.disabled = isRunning || isPaused;
    if (qualitySelect) qualitySelect.disabled = isRunning;
    if (threadsSelect) threadsSelect.disabled = isRunning;
    if (browseBtn) browseBtn.disabled = isRunning || isPaused;
    if (savePathInput) savePathInput.disabled = isRunning || isPaused;

    refreshCustomSelectStates();
}

function setupBatchTab() {
    const clearBtn = document.getElementById('clearBatchBtn');
    const browseBtn = document.getElementById('browseBatchBtn');
    const startBtn = document.getElementById('startBatchBtn');
    const pauseBtn = document.getElementById('pauseBatchBtn');
    const cancelBtn = document.getElementById('cancelBatchBtn');
    const textarea = document.getElementById('batchUrls');
    const formatSelect = document.getElementById('batchFormatSelect');
    const qualitySelect = document.getElementById('batchQualitySelect');
    const threadsSelect = document.getElementById('batchThreadsSelect');
    const qualityRow = document.getElementById('batchQualityRow');
    const savePathInput = document.getElementById('batchSavePath');
    const cookieInline = document.getElementById('cookieInline');
    const cookieInput = document.getElementById('cookieInput');
    const confirmCookieBtn = document.getElementById('confirmCookieBtn');

    if (!clearBtn || !startBtn || !pauseBtn || !cancelBtn) return;

    if (threadsSelect) {
        threadsSelect.value = String(state.batchThreads);
        threadsSelect.addEventListener('change', (event) => {
            const value = Number.parseInt(event.target.value, 10);
            state.batchThreads = Number.isNaN(value) ? 3 : Math.min(Math.max(value, 1), 10);
            threadsSelect.value = String(state.batchThreads);
        });
    }

    applyBatchControlState(state.batchSessionStatus);

    clearBtn.addEventListener('click', () => {
        textarea.value = '';
        const tbody = document.getElementById('batchTableBody');
        if (tbody) tbody.innerHTML = '';
        state.downloadedFiles = {};
    });

    if (cookieInline && cookieInput && confirmCookieBtn) {
        cookieInput.addEventListener('paste', (event) => {
            const pasted = event.clipboardData?.getData('text') || '';
            if (!pasted) return;
            event.preventDefault();
            state.temporaryCookieDraft = pasted;
            cookieInput.value = pasted;
        });

        cookieInput.addEventListener('input', (event) => {
            state.temporaryCookieDraft = event.target.value;
        });

        confirmCookieBtn.addEventListener('click', async () => {
            const rawCookie = state.temporaryCookieDraft.trim();
            if (!rawCookie) {
                if (!state.maskedCookieValue) {
                    showError('Cookie error: please paste a YouTube Cookie header first');
                } else {
                    cookieInline.hidden = true;
                }
                return;
            }

            try {
                await window.go.main.App.SetTemporaryYouTubeCookie(rawCookie);
                state.cookieConfig.mode = 'manual';
                state.maskedCookieValue = maskCookieValue(rawCookie);
                state.temporaryCookieDraft = '';
                cookieInline.hidden = true;
                cookieInput.value = '';
                clearResultMessage();
                refreshCookieUI();
            } catch (err) {
                showError('Cookie error: ' + (err?.message || err));
            }
        });
    }

    browseBtn.addEventListener('click', async () => {
        if (!state.wailsReady) return;
        const path = await window.go.main.App.OpenFolderDialog();
        if (path) {
            savePathInput.value = path;
            state.savePath = path;
        }
    });

    const openFolderBtn = document.getElementById('openBatchFolderBtn');
    if (openFolderBtn) {
        openFolderBtn.addEventListener('click', () => {
            const currentPath = savePathInput.value || state.savePath;
            if (state.wailsReady) window.go.main.App.OpenSaveFolder(currentPath);
        });
    }

    formatSelect.addEventListener('change', (e) => {
        qualityRow.style.display = (e.target.value === 'MP3') ? 'none' : 'flex';
    });
    
    startBtn.addEventListener('click', async () => {
        const maxConcurrent = threadsSelect ? Number.parseInt(threadsSelect.value, 10) || 3 : 3;

        if (state.batchSessionStatus === 'paused') {
            try {
                applyBatchControlState('running');
                const result = await window.go.main.App.ResumeBatchDownload(
                    formatSelect.value,
                    qualitySelect.value,
                    savePathInput.value,
                    maxConcurrent
                );
                if (typeof result === 'string' && result.startsWith('Error:')) {
                    throw new Error(result);
                }
            } catch (err) {
                applyBatchControlState('paused');
                showError('Error: ' + (err?.message || err));
            }
            return;
        }

        const urls = textarea.value.split('\n').map(u => u.trim()).filter(u => u.length > 0);
        if (urls.length === 0) {
            showError('Please enter at least one URL');
            return;
        }

        const tbody = document.getElementById('batchTableBody');
        tbody.innerHTML = '';
        state.downloadedFiles = {};

        urls.forEach((url, i) => {
            const row = document.createElement('tr');
            row.id = `batch-row-${i}`;
            row.innerHTML = `
                <td>${i + 1}</td>
                <td class="thumb-cell">⏳</td>
                <td>${url}</td>
                <td><span class="status-icon">⏳</span> Waiting</td>
                <td><div class="batch-progress-bar"><div class="batch-progress-fill" style="width: 0%;"></div></div></td>
            `;
            tbody.appendChild(row);
        });

        try {
            applyBatchControlState('running');
            const result = await window.go.main.App.StartBatchDownload(
                urls,
                formatSelect.value,
                qualitySelect.value,
                savePathInput.value,
                maxConcurrent
            );
            if (typeof result === 'string' && result.startsWith('Error:')) {
                throw new Error(result);
            }
        } catch (err) {
            applyBatchControlState('idle');
            showError('Error: ' + (err?.message || err));
        }
    });

    pauseBtn.addEventListener('click', async () => {
        pauseBtn.disabled = true;
        try {
            await window.go.main.App.PauseBatchDownload();
        } catch (err) {
            pauseBtn.disabled = false;
            showError('Error: ' + (err?.message || err));
        }
    });

    cancelBtn.addEventListener('click', async () => {
        startBtn.disabled = true;
        pauseBtn.disabled = true;
        cancelBtn.disabled = true;
        try {
            await window.go.main.App.CancelBatchDownload();
        } catch (err) {
            applyBatchControlState(state.batchSessionStatus);
            showError('Error: ' + (err?.message || err));
        }
    });
}

function setupCompressTab() {
    const selectBtn = document.getElementById('selectFilesBtn');
    const startBtn = document.getElementById('startCompressBtn');
    const typeSelect = document.getElementById('compressType');
    const modeSelect = document.getElementById('selectionMode');
    const formatSelect = document.getElementById('compressFormat');
    const browseBtn = document.getElementById('browseCompressBtn');
    const savePathInput = document.getElementById('compressSavePath');
    const qualitySelect = document.getElementById('compressQuality');
    const customContainer = document.getElementById('customQualityContainer');
    const slider = document.getElementById('customQualitySlider');
    const label = document.getElementById('customQualityLabel');
    const slowPresetCheckbox = document.getElementById('useSlowPreset');

    if (!selectBtn || !startBtn) return;

    const updateQualityLabel = (val) => {
        let text = 'Medium Quality';
        if (val > 80) text = 'Highest Quality';
        else if (val > 60) text = 'High Quality';
        else if (val > 40) text = 'Medium Quality';
        else if (val > 20) text = 'Low Quality';
        else text = 'Lowest Quality';
        label.textContent = `Quality: ${val} - ${text}`;
    };

    selectBtn.addEventListener('click', async () => {
        try {
            let files = (modeSelect.value === 'file') ? 
                await window.go.main.App.SelectFiles(typeSelect.value) : 
                await window.go.main.App.SelectFolder(typeSelect.value);
            
            if (files && files.length > 0) {
                state.selectedCompressFiles = files;
                renderCompressFiles();
                startBtn.disabled = false;
            }
        } catch (err) { console.error(err); }
    });
    
    typeSelect.addEventListener('change', () => {
        state.selectedCompressFiles = [];
        renderCompressFiles();
        startBtn.disabled = true;
        formatSelect.innerHTML = '';
        
        if (typeSelect.value === 'video') {
            const mp4 = new Option('MP4', 'mp4');
            const orig = new Option('Keep Original', 'original');
            formatSelect.add(mp4);
            formatSelect.add(orig);
            formatSelect.value = 'original';
        } else {
            formatSelect.add(new Option('JPG', 'jpg'));
            formatSelect.add(new Option('PNG', 'png'));
            formatSelect.value = 'jpg';
        }
        initCustomSelects();
    });

    qualitySelect.addEventListener('change', () => {
        customContainer.style.display = (qualitySelect.value === 'custom') ? 'flex' : 'none';
    });

    slider.addEventListener('input', (e) => updateQualityLabel(e.target.value));
    updateQualityLabel(slider.value);

    browseBtn.addEventListener('click', async () => {
        const path = await window.go.main.App.OpenFolderDialog();
        if (path) {
            savePathInput.value = path;
            state.savePath = path;
        }
    });
    
    const openCompressFolderBtn = document.getElementById('openCompressFolderBtn');
    if (openCompressFolderBtn) {
        openCompressFolderBtn.addEventListener('click', () => {
            const currentPath = savePathInput.value || state.savePath;
            if (state.wailsReady) window.go.main.App.OpenSaveFolder(currentPath);
        });
    }
    
    startBtn.addEventListener('click', async () => {
        if (state.selectedCompressFiles.length === 0) return;
        startBtn.disabled = true;
        startBtn.textContent = '⚡ Compressing...';
        
        const options = {
            type: typeSelect.value,
            quality: qualitySelect.value,
            customQuality: parseInt(slider.value),
            useSlowPreset: slowPresetCheckbox.checked,
            format: formatSelect.value,
            savePath: savePathInput.value
        };
        
        try {
            await window.go.main.App.StartCompression(state.selectedCompressFiles, options);
        } catch (err) {
            startBtn.disabled = false;
            startBtn.textContent = '⚡ Start Compression';
        }
    });

    // Init state
    typeSelect.value = 'image';
    typeSelect.dispatchEvent(new Event('change'));
    setTimeout(initCustomSelects, 200);

    document.addEventListener('click', (e) => {
        if (!e.target.closest('.custom-select-container')) {
            document.querySelectorAll('.custom-select-container').forEach(c => c.classList.remove('open'));
        }
    });
}

function initCustomSelects() {
    const selects = document.querySelectorAll('select');
    selects.forEach(select => {
        let container = select.parentElement;
        if (!container.classList.contains('custom-select-container')) {
            container = document.createElement('div');
            container.className = 'custom-select-container';
            select.parentNode.insertBefore(container, select);
            container.appendChild(select);
        } else {
            // Clear existing custom UI elements to rebuild them
            const trig = container.querySelector('.custom-select-trigger');
            const opts = container.querySelector('.custom-select-options');
            if (trig) trig.remove();
            if (opts) opts.remove();
        }

        select.classList.add('hidden-select');
        container.classList.toggle('disabled', !!select.disabled);
        const trigger = document.createElement('div');
        trigger.className = 'custom-select-trigger';
        // Set initial trigger text
        trigger.textContent = select.options[select.selectedIndex]?.textContent || 'Select...';
        container.appendChild(trigger);
        
        const optionsDiv = document.createElement('div');
        optionsDiv.className = 'custom-select-options';
        
        Array.from(select.options).forEach(opt => {
            const customOpt = document.createElement('div');
            customOpt.className = 'custom-option' + (opt.selected ? ' selected' : '');
            customOpt.textContent = opt.textContent;
            customOpt.addEventListener('click', (e) => {
                e.stopPropagation();
                select.value = opt.value;
                trigger.textContent = opt.textContent;
                container.classList.remove('open');
                select.dispatchEvent(new Event('change', { bubbles: true }));
                // Don't call initCustomSelects recursively here, 
                // just update classes for performance
                optionsDiv.querySelectorAll('.custom-option').forEach(o => o.classList.toggle('selected', o.textContent === opt.textContent));
            });
            optionsDiv.appendChild(customOpt);
        });
        container.appendChild(optionsDiv);
        trigger.addEventListener('click', (e) => {
            if (select.disabled) return;
            e.stopPropagation();
            const isOpen = container.classList.contains('open');
            closeAllDropdowns(); // ← thay thế dòng forEach cũ
            if (!isOpen) container.classList.add('open');
        });
    });

    refreshCustomSelectStates();
}

function renderCompressFiles() {
    const tbody = document.getElementById('compressTableBody');
    if (!tbody) return;
    tbody.innerHTML = '';
    state.selectedCompressFiles.forEach((file, i) => {
        const filename = file.split('/').pop().split('\\').pop();
        const row = document.createElement('tr');
        row.id = `compress-row-${i}`;
        row.innerHTML = `
            <td>${i + 1}</td>
            <td title="${file}">${filename}</td>
            <td class="compress-status">Waiting</td>
            <td><div class="batch-progress-bar"><div class="batch-progress-fill" id="compress-progress-${i}"></div></div></td>
        `;
        tbody.appendChild(row);
    });
}

function updateCompressStatus(index, status) {
    const row = document.getElementById(`compress-row-${index}`);
    if (row) {
        const statusCell = row.querySelector('.compress-status');
        if (statusCell) statusCell.textContent = status.charAt(0).toUpperCase() + status.slice(1);
    }
}

function updateCompressProgress(index, status, message) {
    const row = document.getElementById(`compress-row-${index}`);
    if (row) {
        const fill = document.getElementById(`compress-progress-${index}`);
        const statusCell = row.querySelector('.compress-status');
        if (status === 'compressing') {
            if (fill) fill.style.width = '50%';
            if (statusCell) statusCell.textContent = 'Processing...';
        } else if (status === 'done') {
            if (fill) { fill.style.width = '100%'; fill.style.backgroundColor = '#34c759'; }
            if (statusCell) { statusCell.textContent = '✅ Done'; statusCell.style.color = '#34c759'; }
        }
    }
}

function updateCompressError(index, error) {
    const row = document.getElementById(`compress-row-${index}`);
    if (row) {
        const statusCell = row.querySelector('.compress-status');
        if (statusCell) {
            statusCell.innerHTML = `<span style="cursor: pointer; text-decoration: underline;">❌ Error</span>`;
            statusCell.style.color = '#ff3b30';
            statusCell.onclick = () => alert("Compression Error Details:\n\n" + error);
        }
    }
}

function updateProgress(data) {
    let percentage = Math.max(0, Math.min(100, (typeof data.percentage === 'number' ? data.percentage : parseFloat(data.percentage) || 0)));
    const index = (typeof data.index !== 'undefined') ? data.index : -1;
    
    if (index === -1) {
        const fill = document.getElementById('progressFill');
        if (fill) fill.style.width = percentage + '%';
    } else {
        const row = document.getElementById(`batch-row-${index}`);
        if (row) {
            const fill = row.querySelector('.batch-progress-fill');
            if (fill) fill.style.width = percentage + '%';
            if (percentage >= 100) renderBatchStatusCell(index, 'done', ['Download complete.']);
            else renderBatchStatusCell(index, 'downloading', [`${Math.round(percentage)}% complete`]);
        }
    }
}

function showError(message) {
    const el = document.getElementById('resultMessage');
    if (el) {
        el.className = 'result-message error';
        el.style.display = 'block';
        
        // Check if this is a dependency error
        if (message.includes('yt-dlp') || message.includes('ffmpeg') || message.includes('Dependencies')) {
            el.innerHTML = `
                <div style="display: flex; align-items: center; justify-content: space-between; gap: 10px;">
                    <span>${message}</span>
                    <button id="setupDepsBtn" style="
                        background: #ff3b30; 
                        color: white; 
                        border: none; 
                        padding: 4px 10px; 
                        border-radius: 4px; 
                        cursor: pointer;
                        font-weight: bold;
                        white-space: nowrap;
                    ">🛠 Setup Now</button>
                </div>
            `;
            
            const btn = document.getElementById('setupDepsBtn');
            if (btn) {
                btn.addEventListener('click', async () => {
                    if (btn.textContent.includes('Check Status')) {
                        // RE-CHECK MODE
                        btn.textContent = 'Checking...';
                        try {
                            const status = await window.go.main.App.CheckBinaries();
                            if (status.ytdlp && status.ffmpeg) {
                                clearResultMessage();
                                // Success!
                                console.log('[SETUP] Dependencies found!');
                            } else {
                                btn.textContent = '🔄 Still Missing - Check again';
                                setTimeout(() => { btn.textContent = '🔄 Check Status'; }, 2000);
                            }
                        } catch (err) {
                            console.error('[SETUP] Check error:', err);
                        }
                        return;
                    }

                    // INITIAL SETUP MODE
                    btn.disabled = true;
                    btn.textContent = 'Launching...';
                    try {
                        await window.go.main.App.LaunchSetupTerminal();
                        btn.disabled = false;
                        btn.style.background = '#34c759'; 
                        btn.textContent = '🔄 Installing...';
                        
                        const span = el.querySelector('span');
                        if (span) span.innerText = 'Setting up... Wait for Terminal to finish.';

                        // AUTO-CHECK every 2 seconds
                        const interval = setInterval(async () => {
                            const status = await window.go.main.App.CheckBinaries();
                            if (status.ytdlp && status.ffmpeg) {
                                clearInterval(interval);
                                clearResultMessage();
                                console.log('[SETUP] Dependencies detected automatically!');
                            }
                        }, 2000);

                    } catch (err) {
                        alert('Failed to launch Terminal: ' + err);
                        btn.disabled = false;
                        btn.textContent = '🛠 Setup Now';
                    }
                });
            }
        } else {
            el.textContent = message;
        }
    }
}

function showSuccess(message) {
    const el = document.getElementById('resultMessage');
    if (el) {
        el.textContent = message;
        el.style.display = 'block';
        el.className = 'result-message success';
    }
}

function clearResultMessage() {
    const el = document.getElementById('resultMessage');
    if (el) {
        el.textContent = '';
        el.style.display = 'none';
        el.className = 'result-message';
    }
}

function updateBatchStatus(index, status) {
    const details = {
        downloading: ['Downloading...'],
        done: ['Download complete.'],
        error: ['Download failed.'],
        waiting: ['Waiting for download slot.'],
        retrying: ['Retrying with temporary cookie...'],
        paused: ['Paused. Resume sẽ tải lại từ đầu.'],
        canceled: ['Canceled.']
    };

    if (status === 'waiting' || status === 'paused' || status === 'canceled') {
        setBatchProgressValue(index, 0);
    }

    renderBatchStatusCell(index, status, details[status] || [status]);
}

function updateBatchError(data) {
    if (!data || typeof data.index === 'undefined') return;

    const details = Array.isArray(data.details) && data.details.length > 0
        ? data.details
        : [data.error || 'Download failed.'];

    if (data.requiresCookie) {
        details.unshift('Video này cần xác thực.');
    }

    renderBatchStatusCell(data.index, 'error', details);
}

function renderBatchStatusCell(index, status, details = []) {
    const row = document.getElementById(`batch-row-${index}`);
    if (!row) return;

    const icons = { downloading: '⏳', done: '✅', error: '❌', waiting: '⏳', retrying: '🔄', paused: '⏸', canceled: '✕' };
    const texts = { downloading: 'Downloading', done: 'Done', error: 'Error', waiting: 'Waiting', retrying: 'Retrying', paused: 'Paused', canceled: 'Canceled' };
    const statusCell = row.querySelector('td:nth-child(4)');
    if (!statusCell) return;

    const safeDetails = details
        .filter(Boolean)
        .map(line => escapeHtml(line));
    const tooltipHtml = safeDetails.join('<br>');

    statusCell.className = `status-cell status-${status}`;
    statusCell.innerHTML = `
        <div class="status-with-tooltip" data-tooltip-html="${tooltipHtml}">
            <span class="status-icon">${icons[status] || '?'}</span>
            <span>${texts[status] || status}</span>
        </div>
    `;
}

function setupGlobalStatusTooltip() {
    if (document.getElementById('globalStatusTooltip')) return;

    const tooltip = document.createElement('div');
    tooltip.id = 'globalStatusTooltip';
    tooltip.className = 'global-status-tooltip';
    document.body.appendChild(tooltip);

    document.addEventListener('mouseover', (event) => {
        const trigger = event.target.closest('.status-with-tooltip');
        if (!trigger) return;

        const html = trigger.dataset.tooltipHtml;
        if (!html) return;

        tooltip.innerHTML = html;
        tooltip.classList.add('visible');
        positionGlobalStatusTooltip(event, tooltip);
    });

    document.addEventListener('mousemove', (event) => {
        if (!tooltip.classList.contains('visible')) return;
        if (!event.target.closest('.status-with-tooltip')) return;
        positionGlobalStatusTooltip(event, tooltip);
    });

    document.addEventListener('mouseout', (event) => {
        if (!event.target.closest('.status-with-tooltip')) return;
        const related = event.relatedTarget;
        if (related && related.closest && related.closest('.status-with-tooltip') === event.target.closest('.status-with-tooltip')) {
            return;
        }
        tooltip.classList.remove('visible');
    });
}

function positionGlobalStatusTooltip(event, tooltip) {
    const offset = 14;
    const maxLeft = window.innerWidth - tooltip.offsetWidth - 12;
    const maxTop = window.innerHeight - tooltip.offsetHeight - 12;
    const left = Math.min(event.clientX + offset, Math.max(12, maxLeft));
    const top = Math.min(event.clientY + offset, Math.max(12, maxTop));

    tooltip.style.left = `${left}px`;
    tooltip.style.top = `${top}px`;
}

function maskCookieValue(raw) {
    const normalized = raw.trim().replace(/\s+/g, ' ');
    if (normalized.length <= 16) return normalized.replace(/.(?=.{0,2}$)/g, '*');
    return `${normalized.slice(0, 10)}****${normalized.slice(-8)}`;
}

function escapeHtml(value) {
    return String(value)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}

// Video Modal Logic
function openVideoModal(videoId) {
    const modal = document.getElementById('videoModal');
    const container = document.getElementById('playerContainer');
    if (!modal || !container || !videoId || videoId === 'undefined') return;

    // Use YouTube NoCookie embed for better privacy
    container.innerHTML = `<iframe src="https://www.youtube-nocookie.com/embed/${videoId}?autoplay=1" allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture" allowfullscreen></iframe>`;
    modal.style.display = 'block';
}

function closeVideoModal() {
    const modal = document.getElementById('videoModal');
    const container = document.getElementById('playerContainer');
    if (modal) modal.style.display = 'none';
    if (container) container.innerHTML = ''; // Stop video playback
}

async function setupInfoTab() {
    if (state.wailsReady) {
        try {
            const appInfo = await window.go.main.App.GetAppInfo();
            const versionEl = document.getElementById('appVersion');
            if (versionEl && appInfo.version) {
                versionEl.textContent = appInfo.version;
            }
        } catch (err) {
            console.error('[INFO] Error loading app info:', err);
        }
    }
}

// Global modal event listeners
document.addEventListener('DOMContentLoaded', () => {
    const closeBtn = document.querySelector('.close-modal');
    const modal = document.getElementById('videoModal');

    if (closeBtn) {
        closeBtn.onclick = closeVideoModal;
    }

    window.addEventListener('click', (event) => {
        if (event.target == modal) {
            closeVideoModal();
        }
    });
});

// Thêm vào app.js - gọi trong initializeApp()
function renderOptionRows() {
  const galleryOptions = [
    { id: 'galleryUgoiraToWebm', label: 'Convert Pixiv Ugoira to WebM', checked: true },
    { id: 'galleryArchive',      label: "Use Archive (Don't redownload)", checked: false }
  ];
  const galleryContainer = document.getElementById('galleryOptionRows');
  if (galleryContainer) {
    galleryContainer.innerHTML = galleryOptions.map(opt => `
      <label class="option-row">
        <input type="checkbox" id="${opt.id}" class="option-checkbox" ${opt.checked ? 'checked' : ''}>
        <span>${opt.label}</span>
      </label>
    `).join('');
  }

  const compressOptions = [
    { id: 'useSlowPreset', label: 'Slower for better compression (Recommended)', checked: true }
  ];
  const compressContainer = document.getElementById('compressOptionRows');
  if (compressContainer) {
    compressContainer.innerHTML = compressOptions.map(opt => `
      <label class="option-row">
        <input type="checkbox" id="${opt.id}" class="option-checkbox" ${opt.checked ? 'checked' : ''}>
        <span>${opt.label}</span>
      </label>
    `).join('');
  }
}

function renderInfoTab() {
  const el = document.getElementById('infoContent');
  if (!el) return;

  const features = [
    '🎬 Batch download videos from YouTube, TikTok, Facebook, Instagram and more',
    '🖼️ Download image galleries from Imgur, Twitter, Pixiv and more',
    '⚡ Multi-threaded downloading with up to 10 concurrent threads',
    '🗜️ Compress images and videos to reduce file size',
    '🍪 Cookie support for restricted or age-gated content',
    '🔄 Auto-update to the latest version automatically'
  ];

  const tools = [
    { name: 'yt-dlp',      desc: 'Video extraction engine' },
    { name: 'FFmpeg',      desc: 'Media processing & compression' },
    { name: 'gallery-dl',  desc: 'Image gallery downloader' }
  ];

  el.innerHTML = `
    <div class="info-container">

      <div class="info-header">
        <h2>🎬 YTDown</h2>
        <p class="info-tagline">Fast & simple media downloader for your desktop</p>
        <p class="info-tagline" style="margin-top:6px;">
          Version: <code id="appVersion">Loading...</code>
        </p>
      </div>

      <div class="info-section">
        <h3>✨ Features</h3>
        <ul>
          ${features.map(f => `<li>${f}</li>`).join('')}
        </ul>
      </div>

      <div class="info-section">
        <h3>🔧 Built with</h3>
        <ul>
          ${tools.map(t => `<li><strong>${t.name}</strong> — ${t.desc}</li>`).join('')}
        </ul>
      </div>

      <div class="info-section">
        <h3>👤 Author</h3>
        <ul>
          <li><strong>Justin Nguyen</strong></li>
          <li>Telegram: <code>Justin_Nguyen_SG</code></li>
        </ul>
      </div>

      <div class="info-section">
        <h3>💛 Support & Donate</h3>
        <p style="margin-bottom:10px;">If YTDown helps with your work, please buy me a coffee ☕</p>
        <ul>
          <li><strong>Bank:</strong> MB Bank</li>
          <li><strong>Account:</strong> <code>079 88888 88888</code></li>
          <li><strong>Holder:</strong> Nguyen Duc Huy</li>
          <li><strong>PayPal:</strong> <code>duchuy_1997@hotmail.com</code></li>
        </ul>
      </div>

      <div class="info-footer">
        <p style="color: var(--text-secondary); font-size: 13px;">
          Made with ❤️ by Justin Nguyen &nbsp;·&nbsp; 
          <span id="appVersion-year">2026</span>
        </p>
      </div>

    </div>
  `;
}