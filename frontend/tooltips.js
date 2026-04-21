const tooltipsData = {
    "format": "Select the preferred video/audio format. MP4 is widely compatible, while MKV offers better quality for some sources.",
    "quality": "Choose the maximum video resolution. 'Best' will select the highest available quality.",
    "threads": "Number of concurrent video downloads. Higher values download more videos at once but use more CPU/RAM.",
    "connections": "Number of simultaneous connections per video. Increasing this can speed up downloads by splitting the file into parts."
};

// Function to initialize tooltips
function initTooltips() {
    const tooltipIcons = document.querySelectorAll('.info-icon');
    tooltipIcons.forEach(icon => {
        const tooltipId = icon.getAttribute('data-tooltip');
        if (tooltipsData[tooltipId]) {
            const tooltipText = icon.querySelector('.tooltip-text');
            if (tooltipText) {
                tooltipText.textContent = tooltipsData[tooltipId];
            }
        }
    });
}

// Export if needed or just use globally
window.tooltipsData = tooltipsData;
window.initTooltips = initTooltips;
