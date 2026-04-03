// Wails v2 application main logic

// Add debug logging as soon as script loads
console.log('[BOOT] app.js loaded');

// Main app state
const state = {
    savePath: '',
    currentFormat: 'MP4',
    currentQuality: 'Best Quality',
    wailsReady: false  // Track if Wails is ready
};

// Wait counter to prevent infinite loops
let wailsWaitAttempts = 0;
const MAX_WAILS_WAIT_ATTEMPTS = 100; // 10 seconds max (100 * 100ms)

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
            console.log('[BOOT] Default path set:', path);
        } catch (err) {
            console.error('[BOOT] Error loading path:', err);
        }
    } else {
        console.log('[BOOT] Wails not ready, using browser-only mode (no file access)');
        state.savePath = '/Downloads'; // Fallback path display
        document.getElementById('savePath').value = '[Wails not ready]';
        document.getElementById('batchSavePath').value = '[Wails not ready]';
    }
    
    // Setup event listeners
    console.log('[BOOT] Setting up event listeners...');
    setupTabs();
    setupSingleTab();
    setupBatchTab();
    setupGoEvents();
    
    console.log('[BOOT] Initialization complete!');
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
                console.log('[EVENTS] video-title:', title);
                // Remove quotes if any
                const cleanTitle = title.replace(/^["']|["']$/g, '');
                document.getElementById('videoTitle').textContent = cleanTitle;
            });
            
            window.runtime.EventsOn('download-complete', (path) => {
                console.log('[EVENTS] download-complete');
                showSuccess('✅ Download complete!');
            });
            
            window.runtime.EventsOn('download-error', (error) => {
                console.log('[EVENTS] download-error:', error);
                showError('❌ ' + error);
            });
            
            window.runtime.EventsOn('batch-status', (data) => {
                console.log('[EVENTS] batch-status:', data);
                updateBatchStatus(data.index, data.status);
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
    
    const singleBtn = document.querySelector('[data-tab="single"]');
    const batchBtn = document.querySelector('[data-tab="batch"]');
    const singleTab = document.getElementById('single');
    const batchTab = document.getElementById('batch');
    
    console.log('[TABS] Elements found:', {
        singleBtn: !!singleBtn,
        batchBtn: !!batchBtn,
        singleTab: !!singleTab,
        batchTab: !!batchTab
    });
    
    if (!singleBtn || !batchBtn) {
        console.error('[TABS] Buttons not found!');
        return;
    }
    
    singleBtn.addEventListener('click', function(e) {
        console.log('[TABS] Single button clicked');
        e.preventDefault();
        singleBtn.classList.add('active');
        batchBtn.classList.remove('active');
        singleTab.classList.add('active');
        batchTab.classList.remove('active');
    });
    
    batchBtn.addEventListener('click', function(e) {
        console.log('[TABS] Batch button clicked');
        e.preventDefault();
        batchBtn.classList.add('active');
        singleBtn.classList.remove('active');
        batchTab.classList.add('active');
        singleTab.classList.remove('active');
    });
    
    console.log('[TABS] Tab setup complete');
}

// === SINGLE TAB ===
function setupSingleTab() {
    console.log('[SINGLE] Setting up Single Tab...');
    
    // Get all elements
    const urlInput = document.getElementById('singleUrl');
    const clearBtn = document.getElementById('clearSingle');
    const browseBtn = document.getElementById('browseBtn');
    const startBtn = document.getElementById('startDownloadBtn');
    const formatSelect = document.getElementById('formatSelect');
    const qualitySelect = document.getElementById('qualitySelect');
    const qualityRow = document.getElementById('qualityRow');
    
    console.log('[SINGLE] Elements found:', {
        urlInput: !!urlInput,
        clearBtn: !!clearBtn,
        browseBtn: !!browseBtn,
        startBtn: !!startBtn,
        formatSelect: !!formatSelect,
        qualitySelect: !!qualitySelect
    });
    
    if (!urlInput || !clearBtn || !browseBtn || !startBtn) {
        console.error('[SINGLE] Missing critical elements!');
        return;
    }
    
    // Clear button
    clearBtn.addEventListener('click', function(e) {
        console.log('[SINGLE] Clear button clicked');
        e.preventDefault();
        urlInput.value = '';
        startBtn.disabled = true;
        document.getElementById('videoTitle').textContent = '-';
        clearProgress();
    });
    
    // Browse button
    browseBtn.addEventListener('click', async function(e) {
        console.log('[SINGLE] Browse button clicked');
        e.preventDefault();
        
        if (!state.wailsReady) {
            console.error('[SINGLE] Wails not ready yet');
            showError('App is still initializing... Please try again in a moment');
            return;
        }
        
        try {
            if (!window.go || !window.go.main || !window.go.main.App) {
                throw new Error('Wails Go bindings unavailable');
            }
            const path = await window.go.main.App.OpenFolderDialog();
            if (path) {
                console.log('[SINGLE] Path selected:', path);
                document.getElementById('savePath').value = path;
                state.savePath = path;
            }
        } catch (err) {
            console.error('[SINGLE] Browse error:', err);
            showError('Error: ' + err.message);
        }
    });
    
    // URL input - enable/disable Start button
    urlInput.addEventListener('input', function(e) {
        const hasText = e.target.value.trim().length > 0;
        startBtn.disabled = !hasText;
        console.log('[SINGLE] URL input changed, Start button disabled:', startBtn.disabled);
    });
    
    // Format select
    formatSelect.addEventListener('change', function(e) {
        state.currentFormat = e.target.value;
        qualityRow.style.display = (e.target.value === 'MP3') ? 'none' : 'flex';
        console.log('[SINGLE] Format changed to:', state.currentFormat);
    });
    
    // Quality select
    qualitySelect.addEventListener('change', function(e) {
        state.currentQuality = e.target.value;
        console.log('[SINGLE] Quality changed to:', state.currentQuality);
    });
    
    // Start download button
    startBtn.addEventListener('click', async function(e) {
        console.log('[SINGLE] Start button clicked');
        e.preventDefault();
        
        if (!state.wailsReady) {
            console.error('[SINGLE] Wails not ready yet');
            showError('App is still initializing... Please try again in a moment');
            return;
        }
        
        const url = urlInput.value.trim();
        if (!url) {
            showError('Please enter a URL');
            return;
        }
        
        startBtn.disabled = true;
        startBtn.textContent = 'Downloading...';
        clearProgress();
        
        try {
            if (!window.go || !window.go.main || !window.go.main.App) {
                throw new Error('Wails Go bindings unavailable');
            }
            const result = await window.go.main.App.StartDownload(
                url,
                state.currentFormat,
                state.currentQuality,
                state.savePath
            );
            console.log('[SINGLE] Download started:', result);
        } catch (err) {
            console.error('[SINGLE] Download error:', err);
            showError('Error: ' + err.message);
            startBtn.disabled = false;
            startBtn.textContent = '▶ Start Download';
        }
    });
    
    console.log('[SINGLE] Setup complete');
}

// === BATCH TAB ===
function setupBatchTab() {
    console.log('[BATCH] Setting up Batch Tab...');
    
    // Get all elements
    const pasteBtn = document.getElementById('pasteBtn');
    const clearBtn = document.getElementById('clearBatchBtn');
    const browseBtn = document.getElementById('browseBatchBtn');
    const startBtn = document.getElementById('startBatchBtn');
    const textarea = document.getElementById('batchUrls');
    const formatSelect = document.getElementById('batchFormatSelect');
    const qualitySelect = document.getElementById('batchQualitySelect');
    const qualityRow = document.getElementById('batchQualityRow');
    
    console.log('[BATCH] Elements found:', {
        pasteBtn: !!pasteBtn,
        clearBtn: !!clearBtn,
        browseBtn: !!browseBtn,
        startBtn: !!startBtn,
        textarea: !!textarea,
        formatSelect: !!formatSelect
    });
    
    if (!pasteBtn || !clearBtn || !browseBtn || !startBtn || !textarea) {
        console.error('[BATCH] Missing critical elements!');
        return;
    }
    
    // Paste button
    pasteBtn.addEventListener('click', async function(e) {
        console.log('[BATCH] Paste button clicked');
        e.preventDefault();
        try {
            const text = await navigator.clipboard.readText();
            textarea.value = text;
            console.log('[BATCH] Pasted URLs');
        } catch (err) {
            console.error('[BATCH] Paste error:', err);
            showError('Cannot access clipboard');
        }
    });
    
    // Clear button
    clearBtn.addEventListener('click', function(e) {
        console.log('[BATCH] Clear button clicked');
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
        } else if (percentage >= 100 && (data.speed === 'Done' || !data.speed)) {
            const btn = document.getElementById('startDownloadBtn');
            if (btn) {
                btn.disabled = false;
                btn.textContent = '▶ Start Download';
            }
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
