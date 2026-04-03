// Wails v2 application main logic

// Add debug logging as soon as script loads
console.log('[BOOT] app.js loaded');

// Main app state
const state = {
    savePath: '',
    currentFormat: 'MP4',
    currentQuality: 'Best Quality',
    wailsReady: false,
    selectedCompressFiles: [] // New state
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
        // This allows testing UI without the Go backend
        initializeApp();
    }
}

async function initializeApp() {
    console.log('[BOOT] App initialization started');
    
    // Load default save path
    if (state.wailsReady) {
        try {
            // Use Wails v2 Go bindings
            const path = await window.go.main.App.GetDefaultSavePath();
            state.savePath = path;
            document.getElementById('savePath').value = path;
            document.getElementById('batchSavePath').value = path;
            document.getElementById('compressSavePath').value = path; // NEW
            console.log('[BOOT] Default path set:', path);
        } catch (err) {
            console.error('[BOOT] Error loading path:', err);
        }
    } else {
        console.log('[BOOT] Wails not ready, using browser-only mode (no file access)');
        state.savePath = '/Downloads'; // Fallback path display
        document.getElementById('savePath').value = '[Wails not ready]';
        document.getElementById('batchSavePath').value = '[Wails not ready]';
        document.getElementById('compressSavePath').value = '[Wails not ready]'; // NEW
    }
    
    // Setup event listeners
    console.log('[BOOT] Setting up event listeners...');
    setupTabs();
    setupBatchTab();
    setupCompressTab();
    setupGoEvents();
    
    // Add dynamic window resizing (Auto-hug)
    setupWindowAutoHug();
    
    console.log('[BOOT] Initialization complete!');
}

let lastSetHeight = 0;

function setupWindowAutoHug() {
    if (typeof window === 'undefined' || !window.runtime || !window.runtime.WindowSetSize) return;

    const updateHeight = () => {
        const container = document.querySelector('.container');
        if (!container) return;

        // Use getBoundingClientRect for sub-pixel accuracy or offsetHeight
        const contentHeight = Math.ceil(container.getBoundingClientRect().height);
        
        // MacOS/Windows title bar offset + small bottom air
        const windowHeight = contentHeight + 40; 
        
        // Prevent infinite loop: Only resize if the change is more than 5px
        if (contentHeight > 200 && Math.abs(windowHeight - lastSetHeight) > 5) {
            console.log('[UI] Auto-hugging with gap to:', windowHeight);
            lastSetHeight = windowHeight;
            window.runtime.WindowSetSize(700, windowHeight);
        }
    };

    // Use ResizeObserver for automatic detection
    const container = document.querySelector('.container');
    if (container) {
        const resizeObserver = new ResizeObserver(() => {
            // Use requestAnimationFrame to ensure we measure after layout is ready
            requestAnimationFrame(updateHeight);
        });
        resizeObserver.observe(container);
    }

    // Force update when tabs change
    document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            // Wait for tab animation to finish before measuring
            setTimeout(updateHeight, 100); 
        });
    });
}

// Setup Wails events
function setupGoEvents() {
    console.log('[EVENTS] Setting up Wails events...');
    try {
        // Use Wails v2 runtime for events
        if (window.runtime && window.runtime.EventsOn) {
            window.runtime.EventsOn('progress-update', (data) => {
                console.log('[EVENTS] progress-update:', data);
                updateProgress(data);
            });
            
            window.runtime.EventsOn('video-title', (title) => {
                // Batch title update: titles are emitted sequentially as they start
                // But we don't always know which index it is.
                // However, the progress-update events carry the index.
                // For simplicity, we can also look for currently 'downloading' rows without a proper title.
                console.log('[EVENTS] video-title:', title);
                const rows = document.querySelectorAll('#batchTableBody tr');
                rows.forEach(row => {
                    const statusCell = row.querySelector('td:nth-child(3)');
                    const titleCell = row.querySelector('td:nth-child(2)');
                    if (statusCell && statusCell.innerText.includes('Downloading') && titleCell && titleCell.innerText.startsWith('http')) {
                         titleCell.innerText = truncateMiddle(title.replace(/^["']|["']$/g, ''), 40);
                         titleCell.title = title; // Show full title on hover
                    }
                });
            });
            
            window.runtime.EventsOn('binary-error', (error) => {
                showError('⚠️ Missing Tool: ' + error);
            });

            window.runtime.EventsOn('binary-warning', (warning) => {
                console.warn('[EVENTS] binary-warning:', warning);
                // Just log it or show a subtle message
                showError('⚠️ Warning: ' + warning);
            });

            window.runtime.EventsOn('batch-complete', (results) => {
                console.log('[EVENTS] batch-complete:', results);
                const btn = document.getElementById('startBatchBtn');
                if (btn) {
                    btn.disabled = false;
                    btn.textContent = '▶ Start Download';
                }
            });
            
            window.runtime.EventsOn('batch-status', (data) => {
                console.log('[EVENTS] batch-status:', data);
                updateBatchStatus(data.index, data.status);
            });

            // New compression events
            window.runtime.EventsOn('compression-status', (data) => {
                console.log('[EVENTS] compression-status:', data);
                updateCompressStatus(data.index, data.status);
            });

            window.runtime.EventsOn('compression-progress', (data) => {
                console.log('[EVENTS] compression-progress:', data);
                updateCompressProgress(data.index, data.status, data.message);
            });

            window.runtime.EventsOn('compression-error', (data) => {
                console.error('[EVENTS] compression-error:', data);
                updateCompressError(data.index, data.error);
            });

            window.runtime.EventsOn('compression-complete', (msg) => {
                console.log('[EVENTS] compression-complete:', msg);
                const btn = document.getElementById('startCompressBtn');
                if (btn) {
                    btn.disabled = false;
                    btn.textContent = '⚡ Start Compression';
                }
                // Success message removed as per request
            });
            
            console.log('[EVENTS] Event listeners registered');
        } else {
            console.warn('[EVENTS] Wails runtime events not available');
        }
    } catch (err) {
        console.error('[EVENTS] Error setting up events:', err);
    }
}

// === TAB SWITCHING ===
function setupTabs() {
    console.log('[TABS] Setting up Tab Switching...');
    
    const batchBtn = document.querySelector('[data-tab="batch"]');
    const compressBtn = document.querySelector('[data-tab="compress"]');
    const batchTab = document.getElementById('batch');
    const compressTab = document.getElementById('compress');
    
    if (!batchBtn || !compressBtn) {
        console.error('[TABS] Buttons not found!');
        return;
    }
    
    function switchTab(tab) {
        [batchBtn, compressBtn].forEach(b => b.classList.remove('active'));
        [batchTab, compressTab].forEach(t => t.classList.remove('active'));
        
        document.querySelector(`[data-tab="${tab}"]`).classList.add('active');
        document.getElementById(tab).classList.add('active');
    }
    
    batchBtn.addEventListener('click', () => switchTab('batch'));
    compressBtn.addEventListener('click', () => switchTab('compress'));
    
    console.log('[TABS] Tab setup complete');
}

// === BATCH TAB ===
function setupBatchTab() {
    console.log('[BATCH] Setting up Batch Tab...');
    
    // Get all elements
    const clearBtn = document.getElementById('clearBatchBtn');
    const browseBtn = document.getElementById('browseBatchBtn');
    const startBtn = document.getElementById('startBatchBtn');
    const textarea = document.getElementById('batchUrls');
    const formatSelect = document.getElementById('batchFormatSelect');
    const qualitySelect = document.getElementById('batchQualitySelect');
    const qualityRow = document.getElementById('batchQualityRow');

    console.log('[BATCH] Elements found:', {
        clearBtn: !!clearBtn,
        browseBtn: !!browseBtn,
        startBtn: !!startBtn,
        textarea: !!textarea,
        formatSelect: !!formatSelect
    });

    if (!clearBtn || !browseBtn || !startBtn || !textarea) {
        console.error('[BATCH] Missing critical elements!');
        return;
    }

    // Clear button
    clearBtn.addEventListener('click', function(e) {        console.log('[BATCH] Clear button clicked');
        e.preventDefault();
        textarea.value = '';
        const tbody = document.getElementById('batchTableBody');
        if (tbody) tbody.innerHTML = '';
    });
    
    // Browse button
    browseBtn.addEventListener('click', async function(e) {
        console.log('[BATCH] Browse button clicked');
        e.preventDefault();
        
        if (!state.wailsReady) {
            console.error('[BATCH] Wails not ready yet');
            showError('App is still initializing... Please try again in a moment');
            return;
        }
        
        try {
            if (!window.go || !window.go.main || !window.go.main.App) {
                throw new Error('Wails Go bindings unavailable');
            }
            const path = await window.go.main.App.OpenFolderDialog();
            if (path) {
                console.log('[BATCH] Path selected:', path);
                document.getElementById('batchSavePath').value = path;
                state.savePath = path;
            }
        } catch (err) {
            console.error('[BATCH] Browse error:', err);
            showError('Error: ' + err.message);
        }
    });

    // Manual path input listener
    document.getElementById('batchSavePath').addEventListener('input', (e) => {
        state.savePath = e.target.value;
    });
    
    // Format select
    formatSelect.addEventListener('change', function(e) {
        qualityRow.style.display = (e.target.value === 'MP3') ? 'none' : 'flex';
        console.log('[BATCH] Format changed to:', e.target.value);
    });
    
    // Quality select
    qualitySelect.addEventListener('change', function(e) {
        console.log('[BATCH] Quality changed to:', e.target.value);
    });
    
    // Start button
    startBtn.addEventListener('click', async function(e) {
        console.log('[BATCH] Start button clicked');
        e.preventDefault();
        
        if (!state.wailsReady) {
            console.error('[BATCH] Wails not ready yet');
            showError('App is still initializing... Please try again in a moment');
            return;
        }
        
        const urls = textarea.value
            .split('\n')
            .map(u => u.trim())
            .filter(u => u.length > 0);
        
        if (urls.length === 0) {
            showError('Please enter at least one URL');
            return;
        }
        
        const format = formatSelect.value;
        const quality = qualitySelect.value;
        
        console.log('[BATCH] Starting batch download:', { count: urls.length, format, quality });
        
        startBtn.disabled = true;
        const tbody = document.getElementById('batchTableBody');
        tbody.innerHTML = '';
        
        // Create table rows
        urls.forEach((url, i) => {
            const row = document.createElement('tr');
            row.id = `batch-row-${i}`;
            row.innerHTML = `
                <td>${i + 1}</td>
                <td>${url}</td>
                <td><span class="status-icon">⏳</span> Waiting</td>
                <td><div style="width: 100%; height: 3px; background: rgba(255,255,255,0.1); border-radius: 2px;"><div style="height: 100%; background: #0a84ff; width: 0%;"></div></div></td>
            `;
            tbody.appendChild(row);
        });
        
        try {
            if (!window.go || !window.go.main || !window.go.main.App) {
                throw new Error('Wails Go bindings unavailable');
            }
            await window.go.main.App.StartBatchDownload(urls, format, quality, state.savePath);
            console.log('[BATCH] All downloads queued');
        } catch (err) {
            console.error('[BATCH] Download error:', err);
            showError('Error: ' + err.message);
            startBtn.disabled = false;
        }
    });
    
    console.log('[BATCH] Setup complete');
}

// === COMPRESS TAB ===
function setupCompressTab() {
    console.log('[COMPRESS] Setting up Compress Tab...');
    
    const selectBtn = document.getElementById('selectFilesBtn');
    const startBtn = document.getElementById('startCompressBtn');
    const typeSelect = document.getElementById('compressType');
    const modeSelect = document.getElementById('selectionMode');
    const formatSelect = document.getElementById('compressFormat');
    const browseBtn = document.getElementById('browseCompressBtn');
    const savePathInput = document.getElementById('compressSavePath');
    
    if (!selectBtn || !startBtn || !typeSelect) return;
    
    // Select files or folder
    selectBtn.addEventListener('click', async () => {
        try {
            let files = [];
            if (modeSelect.value === 'file') {
                files = await window.go.main.App.SelectFiles(typeSelect.value);
            } else {
                files = await window.go.main.App.SelectFolder(typeSelect.value);
            }
            
            if (files && files.length > 0) {
                state.selectedCompressFiles = files;
                renderCompressFiles();
                startBtn.disabled = false;
            } else if (modeSelect.value === 'folder') {
                showError('No matching files found in the selected folder.');
            }
        } catch (err) {
            console.error('[COMPRESS] Selection error:', err);
        }
    });
    
    // Type change - update format dropdown
    typeSelect.addEventListener('change', () => {
        // Clear list and disable start button
        state.selectedCompressFiles = [];
        renderCompressFiles();
        startBtn.disabled = true;

        const type = typeSelect.value;
        formatSelect.innerHTML = '';
        
        const originalOpt = document.createElement('option');
        originalOpt.value = 'original';
        originalOpt.textContent = 'Keep Original';
        formatSelect.appendChild(originalOpt);
        
        if (type === 'video') {
            const mp4Opt = document.createElement('option');
            mp4Opt.value = 'mp4';
            mp4Opt.textContent = 'MP4';
            formatSelect.appendChild(mp4Opt);
            formatSelect.value = 'original';
        } else {
            const formats = [
                { val: 'webp', label: 'WebP' },
                { val: 'jpg', label: 'JPG' },
                { val: 'png', label: 'PNG' }
            ];
            formats.forEach(f => {
                const opt = document.createElement('option');
                opt.value = f.val;
                opt.textContent = f.label;
                formatSelect.appendChild(opt);
            });
            formatSelect.value = 'webp';
        }
    });

    // Selection Mode change - clear list
    modeSelect.addEventListener('change', () => {
        state.selectedCompressFiles = [];
        renderCompressFiles();
        startBtn.disabled = true;
    });
    
    // Trigger initial state
    typeSelect.dispatchEvent(new Event('change'));
    
    // Browse button
    browseBtn.addEventListener('click', async () => {
        try {
            const path = await window.go.main.App.OpenFolderDialog();
            if (path) {
                savePathInput.value = path;
                state.savePath = path;
            }
        } catch (err) {
            console.error('[COMPRESS] Browse error:', err);
        }
    });
    
    // Start compression
    startBtn.addEventListener('click', async () => {
        if (state.selectedCompressFiles.length === 0) return;
        
        startBtn.disabled = true;
        startBtn.textContent = '⚡ Compressing...';
        
        const msgEl = document.getElementById('compressMessage');
        if (msgEl) {
            msgEl.textContent = '';
            msgEl.className = 'result-message';
        }
        
        const options = {
            type: typeSelect.value,
            quality: document.getElementById('compressQuality').value,
            format: formatSelect.value,
            savePath: savePathInput.value
        };
        
        try {
            await window.go.main.App.StartCompression(state.selectedCompressFiles, options);
        } catch (err) {
            console.error('[COMPRESS] Start error:', err);
            startBtn.disabled = false;
            startBtn.textContent = '⚡ Start Compression';
        }
    });
}

function renderCompressFiles() {
    const tbody = document.getElementById('compressTableBody');
    tbody.innerHTML = '';
    
    state.selectedCompressFiles.forEach((file, i) => {
        const filename = file.split('/').pop().split('\\').pop();
        const row = document.createElement('tr');
        row.id = `compress-row-${i}`;
        row.innerHTML = `
            <td>${i + 1}</td>
            <td title="${file}">${filename}</td>
            <td class="compress-status">Waiting</td>
            <td>
                <div class="batch-progress-bar">
                    <div class="batch-progress-fill" id="compress-progress-${i}"></div>
                </div>
            </td>
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
            if (fill) fill.style.width = '50%'; // Indeterminate state
            if (statusCell) statusCell.textContent = 'Processing...';
        } else if (status === 'done') {
            if (fill) {
                fill.style.width = '100%';
                fill.style.backgroundColor = '#34c759';
            }
            if (statusCell) {
                statusCell.textContent = '✅ Done';
                statusCell.style.color = '#34c759';
            }
        }
    }
}

function updateCompressError(index, error) {
    const row = document.getElementById(`compress-row-${index}`);
    if (row) {
        const statusCell = row.querySelector('.compress-status');
        const fill = document.getElementById(`compress-progress-${index}`);
        if (statusCell) {
            statusCell.textContent = '❌ Error';
            statusCell.style.color = '#ff3b30';
            statusCell.title = error;
        }
        if (fill) fill.style.backgroundColor = '#ff3b30';
    }
}

// === UI HELPERS ===
function updateProgress(data) {
    if (!data) return;
    
    // Parse percentage
    let percentage = 0;
    if (typeof data.percentage === 'number') {
        percentage = data.percentage;
    } else if (typeof data.percentage === 'string') {
        percentage = parseFloat(data.percentage) || 0;
    }
    
    // Clamp between 0 and 100
    percentage = Math.max(0, Math.min(100, percentage));
    
    // Check if it's a batch download or single download
    const index = (typeof data.index !== 'undefined') ? data.index : -1;
    
    if (index === -1) {
        // SINGLE DOWNLOAD UI UPDATE
        const progressFill = document.getElementById('progressFill');
        const progressPercent = document.getElementById('progressPercent');
        const progressSpeed = document.getElementById('progressSpeed');
        const progressETA = document.getElementById('progressETA');
        
        if (progressFill) {
            progressFill.style.width = percentage + '%';
            progressFill.classList.add('active');
        }
        
        if (progressPercent) progressPercent.textContent = Math.round(percentage) + '%';
        if (progressSpeed) progressSpeed.textContent = data.speed || 'Initializing...';
        if (progressETA) progressETA.textContent = data.eta || 'Calculating...';
        
        if (percentage >= 100 && (data.speed === 'Processing...' || data.speed === 'Finalizing...')) {
            if (progressFill) progressFill.classList.add('processing-pulse');
        }
    } else {
        // BATCH DOWNLOAD UI UPDATE
        const row = document.getElementById(`batch-row-${index}`);
        if (row) {
            const progressFill = row.querySelector('.batch-progress-fill') || row.querySelector('div > div');
            const statusCell = row.querySelector('td:nth-child(3)');
            
            if (progressFill) {
                progressFill.style.width = percentage + '%';
                // Ensure it's visible and has color
                progressFill.style.backgroundColor = '#0a84ff';
            }
            
            if (statusCell) {
                if (percentage >= 100 && data.speed === 'Processing...') {
                    statusCell.innerHTML = `<span class="status-icon">⚙️</span> Processing...`;
                } else if (percentage >= 100) {
                    statusCell.innerHTML = `<span class="status-icon">✅</span> Done`;
                } else {
                    statusCell.innerHTML = `<span class="status-icon">⏳</span> ${Math.round(percentage)}% (${data.speed || ''})`;
                }
            }
        }
    }
}

function clearProgress() {
    document.getElementById('progressFill').style.width = '0%';
    document.getElementById('progressPercent').textContent = '0%';
    document.getElementById('progressSpeed').textContent = '0 MB/s';
    document.getElementById('progressETA').textContent = '—';
    document.getElementById('resultMessage').innerHTML = '';
    document.getElementById('resultMessage').className = '';
}

function showSuccess(message) {
    const el = document.getElementById('resultMessage');
    el.textContent = message;
    el.className = 'result-message success';
}

function showError(message) {
    const el = document.getElementById('resultMessage');
    el.textContent = message;
    el.className = 'result-message error';
}

function updateBatchStatus(index, status) {
    const row = document.getElementById(`batch-row-${index}`);
    if (!row) return;
    
    const icons = {
        'downloading': '⏳',
        'done': '✅',
        'error': '❌',
        'waiting': '⏳'
    };
    
    const texts = {
        'downloading': 'Downloading',
        'done': 'Done',
        'error': 'Error',
        'waiting': 'Waiting'
    };
    
    const statusCell = row.querySelector('td:nth-child(3)');
    statusCell.innerHTML = `<span class="status-icon">${icons[status] || '?'}</span> ${texts[status] || status}`;
}

// === DEBUG TEST ===
// This will run after a short delay to verify buttons can be clicked
setTimeout(() => {
    console.log('[DEBUG] Test: Verifying button clicks are working...');
    const clearBtn = document.getElementById('clearSingle');
    if (clearBtn) {
        clearBtn.style.outline = '2px solid lime';
        clearBtn.style.outlineOffset = '2px';
        console.log('[DEBUG] Clear button marked - should have lime outline');
    }
    
    const urlInput = document.getElementById('singleUrl');
    if (urlInput) {
        // Test real input
        urlInput.value = '';
        urlInput.dispatchEvent(new Event('input', { bubbles: true }));
        console.log('[DEBUG] Simulated URL input - Start button should now be enabled');
        
        const startBtn = document.getElementById('startDownloadBtn');
        if (startBtn) {
            console.log('[DEBUG] Start button state - Disabled:', startBtn.disabled);
        }
    }
}, 2000);

console.log('[BOOT] app.js fully loaded and ready');
