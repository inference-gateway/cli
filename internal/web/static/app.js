class TerminalManager {
    constructor() {
        this.tabs = new Map();
        this.activeTabId = null;
        this.nextTabId = 1;
        this.tabBar = document.getElementById('tab-bar');
        this.terminalArea = document.getElementById('terminal-area');
        this.newTabBtn = document.getElementById('new-tab-btn');
        this.serverSelector = document.getElementById('server-selector');
        this.welcomeMessage = document.getElementById('welcome-message');
        this.screenshotToggleBtn = document.getElementById('screenshot-toggle-btn');
        this.servers = [];
        this.currentServerID = 'local';

        this.loadServers();
        this.newTabBtn.addEventListener('click', () => this.createTab());
        this.serverSelector.addEventListener('change', (e) => {
            this.currentServerID = e.target.value;
        });
        this.screenshotToggleBtn.addEventListener('click', () => this.toggleScreenshot());
    }

    async loadServers() {
        try {
            const response = await fetch('/api/servers');
            const data = await response.json();
            this.servers = data.servers;

            this.serverSelector.innerHTML = '';
            this.servers.forEach(server => {
                const option = document.createElement('option');
                option.value = server.id;
                option.textContent = server.name;
                if (server.description) {
                    option.title = server.description;
                }
                this.serverSelector.appendChild(option);
            });

            this.serverSelector.value = 'local';
            this.currentServerID = 'local';
        } catch (error) {
            console.error('Failed to load servers:', error);
        }
    }

    createTab() {
        const tabId = this.nextTabId++;
        const serverID = this.currentServerID;
        const tab = new TerminalTab(tabId, this, serverID);
        this.tabs.set(tabId, tab);
        this.switchTab(tabId);
        this.hideWelcomeMessage();
    }

    hideWelcomeMessage() {
        if (this.welcomeMessage) {
            this.welcomeMessage.classList.add('hidden');
        }
    }

    showWelcomeMessage() {
        if (this.welcomeMessage && this.tabs.size === 0) {
            this.welcomeMessage.classList.remove('hidden');
        }
    }

    switchTab(tabId) {
        if (this.activeTabId !== null) {
            const currentTab = this.tabs.get(this.activeTabId);
            if (currentTab) {
                currentTab.deactivate();
            }
        }

        this.activeTabId = tabId;
        const newTab = this.tabs.get(tabId);
        if (newTab) {
            newTab.activate();
            this.updateScreenshotButton(newTab);
        }
    }

    updateScreenshotButton(tab) {
        if (tab && tab.screenshotOverlay) {
            // Show button for tabs with screenshot overlay
            this.screenshotToggleBtn.classList.remove('hidden');
            // Update active state
            if (tab.screenshotOverlay.enabled) {
                this.screenshotToggleBtn.classList.add('active');
            } else {
                this.screenshotToggleBtn.classList.remove('active');
            }
        } else {
            // Hide button for tabs without screenshot overlay
            this.screenshotToggleBtn.classList.add('hidden');
            this.screenshotToggleBtn.classList.remove('active');
        }
    }

    toggleScreenshot() {
        if (this.activeTabId === null) return;

        const activeTab = this.tabs.get(this.activeTabId);
        if (activeTab && activeTab.screenshotOverlay) {
            activeTab.screenshotOverlay.toggle();
            // Update button state
            if (activeTab.screenshotOverlay.enabled) {
                this.screenshotToggleBtn.classList.add('active');
            } else {
                this.screenshotToggleBtn.classList.remove('active');
            }
        }
    }

    closeTab(tabId) {
        const tab = this.tabs.get(tabId);
        if (!tab) return;

        tab.destroy();
        this.tabs.delete(tabId);

        if (this.activeTabId === tabId) {
            const remainingTabs = Array.from(this.tabs.keys());
            if (remainingTabs.length > 0) {
                this.switchTab(remainingTabs[remainingTabs.length - 1]);
            } else {
                this.activeTabId = null;
                this.showWelcomeMessage();
            }
        }
    }
}

class TerminalTab {
    constructor(id, manager, serverID = 'local') {
        this.id = id;
        this.manager = manager;
        this.serverID = serverID;
        this.socket = null;
        this.term = null;
        this.fitAddon = null;
        this.tabElement = null;
        this.containerElement = null;
        this.connected = false;
        this.screenshotOverlay = null;

        this.createUI();
        this.createTerminal();
        this.createScreenshotOverlay();
        this.connect();
    }

    createUI() {
        const server = this.manager.servers.find(s => s.id === this.serverID);
        const serverName = server ? server.name : 'Local';

        this.tabElement = document.createElement('div');
        this.tabElement.className = 'tab';
        this.tabElement.innerHTML = `
            <span class="tab-title">Terminal ${this.id} (${serverName})</span>
            <span class="tab-close">×</span>
        `;

        this.tabElement.querySelector('.tab-title').addEventListener('click', () => {
            this.manager.switchTab(this.id);
        });

        this.tabElement.querySelector('.tab-close').addEventListener('click', (e) => {
            e.stopPropagation();
            this.manager.closeTab(this.id);
        });

        this.manager.tabBar.insertBefore(this.tabElement, this.manager.newTabBtn);

        this.containerElement = document.createElement('div');
        this.containerElement.className = 'terminal-tab';
        this.containerElement.id = `terminal-${this.id}`;
        this.manager.terminalArea.appendChild(this.containerElement);
    }

    createTerminal() {
        this.term = new Terminal({
            cursorBlink: true,
            fontSize: 14,
            fontFamily: 'Menlo, Monaco, "Courier New", monospace',
            theme: {
                background: '#1a1b26',
                foreground: '#a9b1d6',
                cursor: '#c0caf5',
                black: '#15161e',
                red: '#f7768e',
                green: '#9ece6a',
                yellow: '#e0af68',
                blue: '#7aa2f7',
                magenta: '#bb9af7',
                cyan: '#7dcfff',
                white: '#a9b1d6',
                brightBlack: '#414868',
                brightRed: '#f7768e',
                brightGreen: '#9ece6a',
                brightYellow: '#e0af68',
                brightBlue: '#7aa2f7',
                brightMagenta: '#bb9af7',
                brightCyan: '#7dcfff',
                brightWhite: '#c0caf5'
            }
        });

        this.fitAddon = new FitAddon.FitAddon();
        this.term.loadAddon(this.fitAddon);
        this.term.open(this.containerElement);

        this.term.onData(data => {
            if (this.socket && this.socket.readyState === WebSocket.OPEN) {
                this.socket.send(data);
            }
        });

        window.addEventListener('resize', () => {
            if (this.tabElement.classList.contains('active')) {
                this.fitAddon.fit();
                this.sendResize();
            }
        });
    }

    createScreenshotOverlay() {
        // Only create overlay for remote SSH sessions
        if (this.serverID !== 'local' && typeof ScreenshotOverlay !== 'undefined') {
            this.screenshotOverlay = new ScreenshotOverlay(this);
            console.log(`Tab ${this.id}: Screenshot overlay created for ${this.serverID}`);
        }
    }

    connect() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws`;

        this.socket = new WebSocket(wsUrl);

        this.socket.onopen = () => {
            console.log(`Tab ${this.id}: WebSocket connected to server: ${this.serverID}`);
            this.connected = true;

            this.fitAddon.fit();

            // Send init message with server selection
            this.socket.send(JSON.stringify({
                type: 'init',
                server_id: this.serverID,
                cols: this.term.cols,
                rows: this.term.rows
            }));

            requestAnimationFrame(() => {
                this.term.focus();
            });
        };

        this.socket.onmessage = (event) => {
            if (event.data instanceof Blob) {
                event.data.arrayBuffer().then(buffer => {
                    this.term.write(new Uint8Array(buffer));
                });
            } else {
                try {
                    const msg = JSON.parse(event.data);
                    if (msg.type === 'init_response') {
                        this.sessionID = msg.session_id;
                        console.log(`Tab ${this.id}: Session initialized with ID: ${this.sessionID}`);
                        return;
                    }
                    if (msg.type === 'screenshot_port') {
                        console.log(`Tab ${this.id}: Received screenshot port: ${msg.port}, session: ${this.sessionID}`);
                        if (this.screenshotOverlay && this.sessionID) {
                            this.screenshotOverlay.startPolling(this.sessionID);
                        }
                        return;
                    }
                } catch (e) {
                    // Not JSON, treat as terminal data
                }
                this.term.write(event.data);
            }
        };

        this.socket.onerror = (error) => {
            console.error(`Tab ${this.id}: WebSocket error:`, error);
            this.term.write('\r\n\x1b[1;31mConnection error\x1b[0m\r\n');
        };

        this.socket.onclose = () => {
            console.log(`Tab ${this.id}: WebSocket closed`);
            this.connected = false;
            this.term.write('\r\n\x1b[1;33mConnection closed\x1b[0m\r\n');
        };
    }

    sendResize() {
        if (this.socket && this.socket.readyState === WebSocket.OPEN) {
            this.socket.send(JSON.stringify({
                type: 'resize',
                cols: this.term.cols,
                rows: this.term.rows
            }));
        }
    }

    activate() {
        this.tabElement.classList.add('active');
        this.containerElement.classList.add('active');
        requestAnimationFrame(() => {
            this.fitAddon.fit();
            this.term.focus();
            this.sendResize();
        });
    }

    deactivate() {
        this.tabElement.classList.remove('active');
        this.containerElement.classList.remove('active');
    }

    destroy() {
        if (this.screenshotOverlay) {
            this.screenshotOverlay.destroy();
            this.screenshotOverlay = null;
        }
        if (this.socket) {
            this.socket.close();
        }
        if (this.term) {
            this.term.dispose();
        }
        if (this.tabElement) {
            this.tabElement.remove();
        }
        if (this.containerElement) {
            this.containerElement.remove();
        }
    }
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
    const terminalManager = new TerminalManager();
});
