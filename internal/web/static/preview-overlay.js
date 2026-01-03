/**
 * ScreenshotOverlay manages the screenshot streaming overlay UI
 */
class ScreenshotOverlay {
    constructor(terminalTab) {
        this.terminalTab = terminalTab;
        this.enabled = false;
        this.screenshotPort = null;
        this.pollInterval = null;
        this.pollingFrequency = 2000; // 2 seconds
        this.overlayElement = null;
        this.imageElement = null;
        this.timestampElement = null;
        this.dimensionsElement = null;
        this.statusElement = null;
        this.errorElement = null;

        this.createUI();
    }

    /**
     * Creates the overlay DOM structure
     */
    createUI() {
        // Create overlay container
        this.overlayElement = document.createElement('div');
        this.overlayElement.className = 'screenshot-overlay hidden';
        this.overlayElement.innerHTML = `
            <div class="screenshot-header">
                <h3>Live Preview</h3>
                <button class="screenshot-close" title="Close overlay">×</button>
            </div>
            <div class="screenshot-content">
                <div class="screenshot-status">Connecting...</div>
                <div class="screenshot-error hidden"></div>
                <img class="screenshot-image" alt="Remote preview" />
                <div class="screenshot-info">
                    <div class="screenshot-timestamp">—</div>
                    <div class="screenshot-dimensions">—</div>
                </div>
            </div>
        `;

        // Cache DOM elements
        this.imageElement = this.overlayElement.querySelector('.screenshot-image');
        this.timestampElement = this.overlayElement.querySelector('.screenshot-timestamp');
        this.dimensionsElement = this.overlayElement.querySelector('.screenshot-dimensions');
        this.statusElement = this.overlayElement.querySelector('.screenshot-status');
        this.errorElement = this.overlayElement.querySelector('.screenshot-error');

        // Bind close button
        const closeBtn = this.overlayElement.querySelector('.screenshot-close');
        closeBtn.addEventListener('click', () => this.hide());

        // Append to terminal container
        if (this.terminalTab && this.terminalTab.containerElement) {
            this.terminalTab.containerElement.appendChild(this.overlayElement);
        } else {
            document.body.appendChild(this.overlayElement);
        }
    }

    /**
     * Starts screenshot polling with the given session ID
     * @param {string} sessionID - Session ID for this terminal tab
     */
    startPolling(sessionID) {
        if (this.pollInterval) {
            clearInterval(this.pollInterval);
        }

        this.sessionID = sessionID;
        console.log(`Screenshot overlay: starting polling for session ${sessionID}`);

        // Show status
        this.updateStatus('Loading preview...');
        this.hideError();

        // Fetch immediately
        this.fetchLatest();

        // Start polling
        this.pollInterval = setInterval(() => {
            this.fetchLatest();
        }, this.pollingFrequency);
    }

    /**
     * Stops screenshot polling
     */
    stopPolling() {
        if (this.pollInterval) {
            clearInterval(this.pollInterval);
            this.pollInterval = null;
        }
        this.updateStatus('Disconnected');
    }

    /**
     * Fetches the latest screenshot from the API
     */
    async fetchLatest() {
        if (!this.sessionID) {
            this.showError('Session ID not configured');
            return;
        }

        try {
            const url = `/api/screenshots/latest?session_id=${encodeURIComponent(this.sessionID)}`;
            const response = await fetch(url);

            if (!response.ok) {
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }

            const screenshot = await response.json();
            this.updateDisplay(screenshot);
            this.hideError();
            this.hideStatus();

        } catch (error) {
            console.error('Failed to fetch screenshot:', error);
            this.showError(`Failed to fetch preview: ${error.message}`);
            this.updateStatus('Connection error');
        }
    }

    /**
     * Updates the overlay display with screenshot data
     * @param {Object} screenshot - Screenshot data with id, timestamp, data, width, height, format, method
     */
    updateDisplay(screenshot) {
        if (!screenshot || !screenshot.data) {
            this.showError('Invalid preview data');
            return;
        }

        // Update image
        this.imageElement.src = `data:image/${screenshot.format};base64,${screenshot.data}`;
        this.imageElement.style.display = 'block';

        // Update timestamp
        const timestamp = new Date(screenshot.timestamp);
        this.timestampElement.textContent = timestamp.toLocaleTimeString();

        // Update dimensions
        this.dimensionsElement.textContent = `${screenshot.width}×${screenshot.height}`;

        // Update status
        this.updateStatus(`Live (${screenshot.method})`);
    }

    /**
     * Shows the overlay
     */
    show() {
        this.enabled = true;
        this.overlayElement.classList.remove('hidden');

        // Resume polling if port is configured
        if (this.screenshotPort && !this.pollInterval) {
            this.startPolling(this.screenshotPort);
        }
    }

    /**
     * Hides the overlay
     */
    hide() {
        this.enabled = false;
        this.overlayElement.classList.add('hidden');
        this.stopPolling();
    }

    /**
     * Toggles the overlay visibility
     */
    toggle() {
        if (this.enabled) {
            this.hide();
        } else {
            this.show();
        }
    }

    /**
     * Updates the status message
     * @param {string} message - Status message
     */
    updateStatus(message) {
        this.statusElement.textContent = message;
        this.statusElement.classList.remove('hidden');
    }

    /**
     * Hides the status message
     */
    hideStatus() {
        this.statusElement.classList.add('hidden');
    }

    /**
     * Shows an error message
     * @param {string} message - Error message
     */
    showError(message) {
        this.errorElement.textContent = message;
        this.errorElement.classList.remove('hidden');
    }

    /**
     * Hides the error message
     */
    hideError() {
        this.errorElement.classList.add('hidden');
    }

    /**
     * Cleans up resources
     */
    destroy() {
        this.stopPolling();
        if (this.overlayElement && this.overlayElement.parentNode) {
            this.overlayElement.parentNode.removeChild(this.overlayElement);
        }
    }
}
