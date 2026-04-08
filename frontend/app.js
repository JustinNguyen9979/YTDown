// Wails v2 application main logic

// Add debug logging as soon as script loads
console.log('[BOOT] app.js loaded');

// Main app state
const state = {
    savePath: '',
    currentFormat: 'MP4',
    currentQuality: 'Best Quality',
    wailsReady: false,
    selectedCompressFiles: [],
    temporaryCookieRaw: '',
    temporaryCookieDraft: '',
    maskedCookieValue: '',
    isSubmittingCookie: false,
    cookieHasError: false,
    downloadedFiles: {} // Store index -> localFilePath
};

// Wait counter to prevent infinite loops
let wailsWaitAttempts = 0;
const MAX_WAILS_WAIT_ATTEMPTS = 100; // 10 seconds max (100 * 100ms)

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
    
    // Load default save path
    if (state.wailsReady) {
        try {
            const path = await window.go.main.App.GetDefaultSavePath();
            state.savePath = path;
            const bsp = document.getElementById('batchSavePath');
            if (bsp) bsp.value = path;
            const csp = document.getElementById('compressSavePath');
            if (csp) csp.value = path;
            console.log('[BOOT] Default path set:', path);
        } catch (err) {
            console.error('[BOOT] Error loading path:', err);
        }
    } else {
        console.log('[BOOT] Wails not ready, using browser-only mode');
        state.savePath = '/Downloads';
        const bsp = document.getElementById('batchSavePath');
        if (bsp) bsp.value = '[Wails not ready]';
        const csp = document.getElementById('compressSavePath');
        if (csp) csp.value = '[Wails not ready]';
    }
    
    setupTabs();
    setupBatchTab();
    setupGlobalStatusTooltip();
    setupCompressTab();
    setupGoEvents();
    setupWindowAutoHug();
    setupInfoTab();
    
    if (state.wailsReady) {
        checkUpdates();
    }
    
    console.log('[BOOT] Initialization complete!');
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
                const btn = document.getElementById('startBatchBtn');
                if (btn) {
                    btn.disabled = false;
                    btn.textContent = '▶ Start Download';
                }
            });
            
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

function setupBatchTab() {
    const clearBtn = document.getElementById('clearBatchBtn');
    const toggleCookieBtn = document.getElementById('toggleCookieBtn');
    const browseBtn = document.getElementById('browseBatchBtn');
    const startBtn = document.getElementById('startBatchBtn');
    const textarea = document.getElementById('batchUrls');
    const formatSelect = document.getElementById('batchFormatSelect');
    const qualitySelect = document.getElementById('batchQualitySelect');
    const qualityRow = document.getElementById('batchQualityRow');
    const savePathInput = document.getElementById('batchSavePath');
    const cookieInline = document.getElementById('cookieInline');
    const cookieInput = document.getElementById('cookieInput');
    const confirmCookieBtn = document.getElementById('confirmCookieBtn');
    const cookieAddedBadge = document.getElementById('cookieAddedBadge');

    if (!clearBtn || !startBtn) return;

    clearBtn.addEventListener('click', () => {
        textarea.value = '';
        const tbody = document.getElementById('batchTableBody');
        if (tbody) tbody.innerHTML = '';
    });

    if (toggleCookieBtn && cookieInline && cookieInput && confirmCookieBtn) {
        toggleCookieBtn.addEventListener('click', () => {
            const willShow = cookieInline.hidden;
            cookieInline.hidden = !willShow;
            if (!cookieInline.hidden) {
                state.cookieHasError = false;
                if (cookieAddedBadge) cookieAddedBadge.hidden = true;
                cookieInput.value = state.temporaryCookieDraft || state.maskedCookieValue || '';
                setTimeout(() => cookieInput.focus(), 0);
            }
        });

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

        cookieInput.addEventListener('blur', () => {
            if (state.isSubmittingCookie) return;
            setTimeout(() => {
                const active = document.activeElement;
                if (active === cookieInput || active === confirmCookieBtn || active === toggleCookieBtn) {
                    return;
                }
                closeCookieEditor();
            }, 0);
        });

        confirmCookieBtn.addEventListener('mousedown', () => {
            state.isSubmittingCookie = true;
        });

        confirmCookieBtn.addEventListener('click', async () => {
            const rawCookie = state.temporaryCookieDraft.trim();
            if (!rawCookie) {
                state.isSubmittingCookie = false;
                if (!state.maskedCookieValue) {
                    state.cookieHasError = true;
                    showError('Cookie error: please paste a YouTube Cookie header first');
                } else {
                    closeCookieEditor();
                }
                return;
            }

            try {
                await window.go.main.App.SetTemporaryYouTubeCookie(rawCookie);
                state.temporaryCookieRaw = rawCookie;
                state.temporaryCookieDraft = '';
                state.maskedCookieValue = maskCookieValue(rawCookie);
                state.cookieHasError = false;
                cookieInline.hidden = true;
                cookieInput.value = '';
                clearResultMessage();
                if (cookieAddedBadge) cookieAddedBadge.hidden = false;
                updateCookieButtonState();
            } catch (err) {
                state.cookieHasError = true;
                showError('Cookie error: ' + (err?.message || err));
            } finally {
                state.isSubmittingCookie = false;
            }
        });

        document.addEventListener('click', (event) => {
            if (cookieInline.hidden) return;
            if (state.isSubmittingCookie) return;

            const target = event.target;
            if (target.closest('#cookieInline') || target.closest('#toggleCookieBtn')) {
                return;
            }

            closeCookieEditor();
        });
    }

    updateCookieButtonState();

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
        const urls = textarea.value.split('\n').map(u => u.trim()).filter(u => u.length > 0);
        if (urls.length === 0) {
            showError('Please enter at least one URL');
            return;
        }
        
        startBtn.disabled = true;
        const tbody = document.getElementById('batchTableBody');
        tbody.innerHTML = '';
        
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
            await window.go.main.App.StartBatchDownload(urls, formatSelect.value, qualitySelect.value, savePathInput.value);
        } catch (err) {
            showError('Error: ' + err.message);
            startBtn.disabled = false;
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
            e.stopPropagation();
            const isOpen = container.classList.contains('open');
            document.querySelectorAll('.custom-select-container').forEach(c => c.classList.remove('open'));
            if (!isOpen) container.classList.add('open');
        });
    });
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
        retrying: ['Retrying with temporary cookie...']
    };
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

    const icons = { downloading: '⏳', done: '✅', error: '❌', waiting: '⏳', retrying: '🔄' };
    const texts = { downloading: 'Downloading', done: 'Done', error: 'Error', waiting: 'Waiting', retrying: 'Retrying' };
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

function updateCookieButtonState() {
    const toggleCookieBtn = document.getElementById('toggleCookieBtn');
    const cookieAddedBadge = document.getElementById('cookieAddedBadge');
    if (!toggleCookieBtn) return;
    toggleCookieBtn.textContent = state.maskedCookieValue ? 'Cookie added' : 'Add Cookie ytb';
    if (cookieAddedBadge) cookieAddedBadge.hidden = !state.maskedCookieValue;
}

function closeCookieEditor() {
    const cookieInline = document.getElementById('cookieInline');
    const cookieInput = document.getElementById('cookieInput');
    if (cookieInline) cookieInline.hidden = true;
    if (cookieInput) cookieInput.value = '';
    state.temporaryCookieDraft = '';
    state.cookieHasError = false;
    clearResultMessage();
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
