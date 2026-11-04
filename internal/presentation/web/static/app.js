class TerminalManager {
    constructor() {
        this.tabs = new Map();
        this.activeTabId = null;
        this.nextTabId = 1;
        this.tabBar = document.getElementById('tab-bar');
        this.terminalArea = document.getElementById('terminal-area');
        this.newTabBtn = document.getElementById('new-tab-btn');
        this.welcomeMessage = document.getElementById('welcome-message');
        this.screenBtn = document.getElementById('screen-btn');
        this.token = this.readToken();

        this.newTabBtn.addEventListener('click', () => this.createTab());
        this.screenBtn.addEventListener('click', () => this.toggleScreen());
        this.refreshScreenButton(null);
    }

    toggleScreen() {
        const tab = this.tabs.get(this.activeTabId);
        if (tab) {
            tab.screen.toggle();
            this.refreshScreenButton(tab);
        }
    }

    // The screen button shows whether the active tab's session streams a
    // screen: lit when it does, dimmed with the reason as its tooltip when not,
    // disabled without a tab.
    refreshScreenButton(tab) {
        if (tab && tab.id !== this.activeTabId) {
            return;
        }
        const screen = tab ? tab.screen : null;
        this.screenBtn.disabled = !tab;
        this.screenBtn.classList.toggle('active', !!screen && screen.enabled);
        this.screenBtn.classList.toggle('available', !!screen && screen.available === true);
        this.screenBtn.classList.toggle('unavailable', !!screen && screen.available === false);
        this.screenBtn.title = screen ? screen.describe() : 'Open a tab to see its screen';
        this.screenBtn.setAttribute('aria-label', this.screenBtn.title);
    }

    // The daemon's binding token arrives once as ?token= and stays in this tab's
    // sessionStorage, off the address bar.
    readToken() {
        const url = new URL(window.location.href);
        const fromURL = url.searchParams.get('token');
        if (fromURL) {
            sessionStorage.setItem('infer-token', fromURL);
            url.searchParams.delete('token');
            history.replaceState(null, '', url.toString());
        }
        return sessionStorage.getItem('infer-token') || '';
    }

    createTab() {
        const tabId = this.nextTabId++;
        const tab = new TerminalTab(tabId, this);
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
        }
        this.refreshScreenButton(newTab);
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
                this.refreshScreenButton(null);
            }
        }
    }
}

class TerminalTab {
    constructor(id, manager) {
        this.id = id;
        this.manager = manager;
        this.socket = null;
        this.term = null;
        this.fitAddon = null;
        this.tabElement = null;
        this.containerElement = null;
        this.connected = false;
        this.sessionID = null;

        this.createUI();
        this.createTerminal();
        this.screen = new ScreenView(this);
        this.connect();
    }

    createUI() {
        this.tabElement = document.createElement('div');
        this.tabElement.className = 'tab';
        this.tabElement.innerHTML = `
            <span class="tab-title">Terminal ${this.id}</span>
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

    connect() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}${window.location.pathname.replace(/\/?$/, '/')}ws`;

        this.socket = new WebSocket(wsUrl);

        this.socket.onopen = () => {
            console.log(`Tab ${this.id}: WebSocket connected`);
            this.connected = true;

            this.fitAddon.fit();

            this.socket.send(JSON.stringify({
                type: 'init',
                token: this.manager.token,
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
                const msg = this.parseFrame(event.data);
                if (msg && msg.type === 'init_response') {
                    this.sessionID = msg.session_id;
                    this.screen.watch();
                    return;
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
            this.screen.stop();
            this.screen.setAvailable(false, 'the terminal connection closed');
            this.term.write('\r\n\x1b[1;33mConnection closed\x1b[0m\r\n');
        };
    }

    parseFrame(text) {
        try {
            return JSON.parse(text);
        } catch (e) {
            return null;
        }
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
        this.screen.stop();
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

// ScreenView shows the latest frame of the tab's session worker, polled from
// the daemon's /screen/ route, which answers wherever the session runs.
class ScreenView {
    constructor(tab) {
        this.tab = tab;
        this.enabled = false;
        this.timer = null;
        this.probeTimer = null;
        this.available = null;
        this.reason = 'Waiting for the session to start';
        this.element = document.createElement('div');
        this.element.className = 'screen-view hidden';
        this.element.innerHTML = `
            <div class="screen-header"><h3>Screen</h3><button class="screen-close" title="Hide">×</button></div>
            <div class="screen-status">Waiting for the first frame…</div>
            <img class="screen-image hidden" alt="The session's screen" />
        `;
        this.status = this.element.querySelector('.screen-status');
        this.image = this.element.querySelector('.screen-image');
        this.element.querySelector('.screen-close').addEventListener('click', () => this.toggle());
        tab.containerElement.appendChild(this.element);
    }

    toggle() {
        this.enabled = !this.enabled;
        this.element.classList.toggle('hidden', !this.enabled);
        clearInterval(this.timer);
        this.timer = null;
        if (this.enabled && this.tab.connected) {
            this.fetchFrame();
            this.timer = setInterval(() => this.fetchFrame(), 2000);
        }
    }

    // stop ends all polling for good, once the tab's terminal is gone, so a
    // page left open never keeps the session's worker from going idle.
    stop() {
        clearInterval(this.timer);
        clearInterval(this.probeTimer);
        this.timer = null;
        this.probeTimer = null;
    }

    // watch probes the session's screen once it has an id, and every ten
    // seconds after, so the screen button reflects it before the view opens.
    watch() {
        this.probe();
        this.probeTimer = setInterval(() => {
            if (!this.enabled) {
                this.probe();
            }
        }, 10000);
    }

    async probe() {
        try {
            const response = await this.fetchScreen();
            this.setAvailable(response.ok, response.ok ? '' : await response.text());
        } catch (error) {
            this.setAvailable(false, `Could not reach the daemon: ${error}`);
        }
    }

    setAvailable(available, reason) {
        this.available = available;
        this.reason = reason;
        this.tab.manager.refreshScreenButton(this.tab);
    }

    describe() {
        if (this.available === true) {
            return this.enabled ? "Hide the session's screen" : "Show the session's screen";
        }
        return `Screen unavailable: ${this.reason}`;
    }

    fetchScreen() {
        const base = window.location.pathname.replace(/terminal\/?$/, '');
        return fetch(`${base}screen/?id=${encodeURIComponent(this.tab.sessionID)}`, {
            headers: { Authorization: `Bearer ${this.tab.manager.token}` }
        });
    }

    async fetchFrame() {
        if (!this.tab.sessionID) {
            return;
        }
        try {
            const response = await this.fetchScreen();
            if (!response.ok) {
                this.status.textContent = await response.text();
                this.status.classList.remove('hidden');
                this.setAvailable(false, this.status.textContent);
                return;
            }
            this.setAvailable(true, '');
            const url = URL.createObjectURL(await response.blob());
            const previous = this.image.src;
            this.image.src = url;
            this.image.classList.remove('hidden');
            this.status.classList.add('hidden');
            if (previous) {
                URL.revokeObjectURL(previous);
            }
        } catch (error) {
            this.status.textContent = `Could not reach the daemon: ${error}`;
            this.status.classList.remove('hidden');
            this.setAvailable(false, this.status.textContent);
        }
    }
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
    const terminalManager = new TerminalManager();
});
