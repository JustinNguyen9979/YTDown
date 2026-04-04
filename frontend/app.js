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
    setupCompressTab();
    setupGoEvents();
    setupWindowAutoHug();
    
    console.log('[BOOT] Initialization complete!');
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
            
            window.runtime.EventsOn('video-title', (title) => {
                const rows = document.querySelectorAll('#batchTableBody tr');
                rows.forEach(row => {
                    const statusCell = row.querySelector('td:nth-child(3)');
                    const titleCell = row.querySelector('td:nth-child(2)');
                    if (statusCell && statusCell.innerText.includes('Downloading') && titleCell && titleCell.innerText.startsWith('http')) {
                         titleCell.innerText = truncateMiddle(title.replace(/^["']|["']$/g, ''), 40);
                         titleCell.title = title;
                    }
                });
            });
            
            window.runtime.EventsOn('binary-error', (error) => showError('⚠️ Missing Tool: ' + error));
            window.runtime.EventsOn('binary-warning', (warning) => showError('⚠️ Warning: ' + warning));
            window.runtime.EventsOn('batch-complete', () => {
                const btn = document.getElementById('startBatchBtn');
                if (btn) {
                    btn.disabled = false;
                    btn.textContent = '▶ Start Download';
                }
            });
            
            window.runtime.EventsOn('batch-status', (data) => updateBatchStatus(data.index, data.status));
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
    const browseBtn = document.getElementById('browseBatchBtn');
    const startBtn = document.getElementById('startBatchBtn');
    const textarea = document.getElementById('batchUrls');
    const formatSelect = document.getElementById('batchFormatSelect');
    const qualitySelect = document.getElementById('batchQualitySelect');
    const qualityRow = document.getElementById('batchQualityRow');
    const savePathInput = document.getElementById('batchSavePath');

    if (!clearBtn || !startBtn) return;

    clearBtn.addEventListener('click', () => {
        textarea.value = '';
        const tbody = document.getElementById('batchTableBody');
        if (tbody) tbody.innerHTML = '';
    });
    
    browseBtn.addEventListener('click', async () => {
        if (!state.wailsReady) return;
        const path = await window.go.main.App.OpenFolderDialog();
        if (path) {
            savePathInput.value = path;
            state.savePath = path;
        }
    });

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
            const statusCell = row.querySelector('td:nth-child(3)');
            if (statusCell) {
                if (percentage >= 100) statusCell.innerHTML = `<span class="status-icon">✅</span> Done`;
                else statusCell.innerHTML = `<span class="status-icon">⏳</span> ${Math.round(percentage)}%`;
            }
        }
    }
}

function showError(message) {
    const el = document.getElementById('resultMessage');
    if (el) {
        el.textContent = message;
        el.style.display = 'block';
        el.className = 'result-message error';
    }
}

function updateBatchStatus(index, status) {
    const row = document.getElementById(`batch-row-${index}`);
    if (!row) return;
    const icons = { 'downloading': '⏳', 'done': '✅', 'error': '❌', 'waiting': '⏳' };
    const texts = { 'downloading': 'Downloading', 'done': 'Done', 'error': 'Error', 'waiting': 'Waiting' };
    const statusCell = row.querySelector('td:nth-child(3)');
    if (statusCell) statusCell.innerHTML = `<span class="status-icon">${icons[status] || '?'}</span> ${texts[status] || status}`;
}
