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
// the daemon's /screen/ route, which answers wherever the session runs. Take
// over drives it: the image takes focus, and the pointer, wheel and keys
// inside it go to the session's screen; Done, or Esc twice, hands it back.
class ScreenView {
    constructor(tab) {
        this.tab = tab;
        this.enabled = false;
        this.timer = null;
        this.probeTimer = null;
        this.available = null;
        this.reason = 'Waiting for the session to start';
        this.held = false;
        this.frameWidth = 0;
        this.frameHeight = 0;
        this.lastEsc = 0;
        this.sending = Promise.resolve();
        this.element = document.createElement('div');
        this.element.className = 'screen-view hidden';
        this.element.innerHTML = `
            <div class="screen-header"><h3>Screen</h3><span class="screen-held hidden">you are driving</span><button class="screen-takeover" title="Drive the session's screen yourself">Take over</button><button class="screen-close" title="Hide">×</button></div>
            <div class="screen-status">Waiting for the first frame…</div>
            <img class="screen-image hidden" alt="The session's screen" tabindex="0" />
        `;
        this.status = this.element.querySelector('.screen-status');
        this.image = this.element.querySelector('.screen-image');
        this.takeoverBtn = this.element.querySelector('.screen-takeover');
        this.heldBadge = this.element.querySelector('.screen-held');
        this.element.querySelector('.screen-close').addEventListener('click', () => this.toggle());
        this.takeoverBtn.addEventListener('click', () => this.toggleHeld());
        this.image.addEventListener('mousedown', (e) => this.sendClick(e));
        this.image.addEventListener('wheel', (e) => this.sendWheel(e), { passive: false });
        this.image.addEventListener('contextmenu', (e) => e.preventDefault());
        this.image.addEventListener('keydown', (e) => this.sendKey(e));
        tab.containerElement.appendChild(this.element);
    }

    toggle() {
        this.enabled = !this.enabled;
        this.element.classList.toggle('hidden', !this.enabled);
        if (!this.enabled) {
            this.release();
        }
        this.resetPoll();
    }

    // pollDelay is how often the view asks for a frame: 250 ms while the
    // person drives the screen, so typing feels live, two seconds otherwise.
    pollDelay() {
        return this.held ? 250 : 2000;
    }

    resetPoll() {
        clearInterval(this.timer);
        this.timer = null;
        if (this.enabled && this.tab.connected) {
            this.fetchFrame();
            this.timer = setInterval(() => this.fetchFrame(), this.pollDelay());
        }
    }

    // stop ends all polling for good, once the tab's terminal is gone, so a
    // page left open never keeps the session's worker from going idle.
    stop() {
        clearInterval(this.timer);
        clearInterval(this.probeTimer);
        this.timer = null;
        this.probeTimer = null;
        this.release();
    }

    // release hands a still-held screen back fire-and-forget, so closing
    // the view or the tab never leaves the worker parked on a screen
    // nobody drives.
    release() {
        if (!this.held) {
            return;
        }
        this.held = false;
        this.setHeldUI(false);
        this.postScreen({ action: 'hand_back' }, true).catch(() => {});
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
        const headers = { Authorization: `Bearer ${this.tab.manager.token}` };
        if (this.frameTag) {
            headers['If-None-Match'] = this.frameTag;
        }
        return fetch(`${base}screen/?id=${encodeURIComponent(this.tab.sessionID)}`, { headers });
    }

    async fetchFrame() {
        if (!this.tab.sessionID) {
            return;
        }
        try {
            const response = await this.fetchScreen();
            if (response.status === 304) {
                this.setAvailable(true, '');
                return;
            }
            if (!response.ok) {
                this.status.textContent = await response.text();
                this.status.classList.remove('hidden');
                this.setAvailable(false, this.status.textContent);
                return;
            }
            this.setAvailable(true, '');
            this.frameWidth = parseInt(response.headers.get('X-Frame-Width') || '0', 10);
            this.frameHeight = parseInt(response.headers.get('X-Frame-Height') || '0', 10);
            this.frameTag = response.headers.get('ETag');
            const url = URL.createObjectURL(await response.blob());
            const previous = this.image.src;
            this.image.src = url;
            this.image.classList.remove('hidden');
            if (!this.held) {
                this.status.classList.add('hidden');
            }
            if (previous) {
                URL.revokeObjectURL(previous);
            }
        } catch (error) {
            this.status.textContent = `Could not reach the daemon: ${error}`;
            this.status.classList.remove('hidden');
            this.setAvailable(false, this.status.textContent);
        }
    }

    async toggleHeld() {
        if (this.held) {
            await this.handBack();
            return;
        }
        await this.takeOver();
    }

    async takeOver() {
        try {
            const response = await this.postScreen({ action: 'take_over' });
            if (response.status === 409) {
                // Another tab of this person holds the screen, or one that
                // died holding it: adopt the hold so Done can hand it back.
                this.setHeld(true);
                return;
            }
            if (!response.ok) {
                this.status.textContent = await response.text();
                this.status.classList.remove('hidden');
                return;
            }
            this.setHeld(true);
        } catch (error) {
            this.status.textContent = `Could not reach the daemon: ${error}`;
            this.status.classList.remove('hidden');
        }
    }

    async handBack() {
        this.lastEsc = 0;
        try {
            const response = await this.postScreen({ action: 'hand_back' });
            if (!response.ok) {
                this.status.textContent = await response.text();
                this.status.classList.remove('hidden');
                return;
            }
        } catch (error) {
            this.status.textContent = `Could not reach the daemon: ${error}`;
            this.status.classList.remove('hidden');
            return;
        }
        this.setHeld(false);
    }

    // setHeld flips the held state: the button, the badge, the poll rate
    // and the image focus, which the frame swap leaves in place.
    setHeld(held) {
        this.held = held;
        this.setHeldUI(held);
        this.resetPoll();
        if (held) {
            this.image.focus();
        }
    }

    setHeldUI(held) {
        this.element.classList.toggle('held', held);
        this.takeoverBtn.textContent = held ? 'Done' : 'Take over';
        this.takeoverBtn.title = held ? "Hand the screen back to the agent" : "Drive the session's screen yourself";
        this.heldBadge.classList.toggle('hidden', !held);
    }

    postScreen(body, keepalive) {
        const base = window.location.pathname.replace(/terminal\/?$/, '');
        return fetch(`${base}screen/?id=${encodeURIComponent(this.tab.sessionID)}`, {
            method: 'POST',
            keepalive: keepalive === true,
            headers: { Authorization: `Bearer ${this.tab.manager.token}`, 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        });
    }

    // postInput sends the inputs one at a time, in the order the person
    // made them, so fast typing never lands scrambled.
    postInput(input) {
        this.sending = this.sending.then(() => this.sendInput(input));
        return this.sending;
    }

    async sendInput(input) {
        try {
            const response = await this.postScreen({ input });
            if (response.status === 409) {
                // The tab handed the screen back at the worker, or lost it
                // to another viewer: stop driving.
                this.setHeld(false);
                this.status.textContent = 'The screen is no longer held';
                this.status.classList.remove('hidden');
            }
        } catch (error) {
            this.status.textContent = `Could not reach the daemon: ${error}`;
            this.status.classList.remove('hidden');
        }
    }

    // framePos maps a pointer position onto the frame space the last frame
    // answered in, from its X-Frame-Width and X-Frame-Height headers.
    framePos(event) {
        const rect = this.image.getBoundingClientRect();
        const width = this.frameWidth || this.image.naturalWidth;
        const height = this.frameHeight || this.image.naturalHeight;
        if (!rect.width || !rect.height || width <= 0 || height <= 0) {
            return null;
        }
        return {
            x: Math.min(Math.max(Math.round((event.clientX - rect.left) * width / rect.width), 0), width),
            y: Math.min(Math.max(Math.round((event.clientY - rect.top) * height / rect.height), 0), height)
        };
    }

    sendClick(event) {
        if (!this.held) {
            return;
        }
        event.preventDefault();
        const at = this.framePos(event);
        if (!at) {
            return;
        }
        const button = event.button === 2 ? 'right' : event.button === 1 ? 'middle' : 'left';
        this.postInput({ kind: 'click', x: at.x, y: at.y, button: button });
    }

    // sendWheel maps the wheel onto scroll inputs, a positive amount
    // wheeling down or right, the direction the backend scrolls it from.
    sendWheel(event) {
        if (!this.held) {
            return;
        }
        event.preventDefault();
        const clicks = (delta) => Math.sign(delta) * Math.min(Math.max(Math.round(Math.abs(delta) / 100), 1), 10);
        if (event.deltaY) {
            this.postInput({ kind: 'scroll', direction: 'vertical', amount: clicks(event.deltaY) });
        }
        if (event.deltaX) {
            this.postInput({ kind: 'scroll', direction: 'horizontal', amount: clicks(event.deltaX) });
        }
    }

    // sendKey maps one keydown: printables type, modifiers and specials
    // tap, and Esc twice hands the screen back - the first still goes to
    // the screen, so a dialog can be dismissed with it.
    sendKey(event) {
        if (!this.held) {
            return;
        }
        if (event.key === 'Escape') {
            event.preventDefault();
            const now = Date.now();
            if (now - this.lastEsc < 2000) {
                this.handBack();
                return;
            }
            this.lastEsc = now;
            this.status.textContent = 'Esc again to hand the screen back';
            this.status.classList.remove('hidden');
            this.postInput({ kind: 'key', combo: 'escape' });
            return;
        }
        event.preventDefault();
        if (event.repeat) {
            return;
        }
        const combo = keyCombo(event);
        if (combo) {
            this.postInput({ kind: 'key', combo: combo });
            return;
        }
        if (event.key.length === 1) {
            this.postInput({ kind: 'type', text: event.key });
        }
    }
}

// SCREEN_SPECIAL_KEYS is the robotgo key name a KeyboardEvent's key maps
// to; anything printable instead types, so the input loop keeps a text
// path. The names are what the worker's SendKeyCombo understands.
const SCREEN_SPECIAL_KEYS = {
    Backspace: 'backspace', Enter: 'enter', Tab: 'tab', Space: 'space',
    Delete: 'delete', Home: 'home', End: 'end', PageUp: 'pageup', PageDown: 'pagedown',
    ArrowUp: 'up', ArrowDown: 'down', ArrowLeft: 'left', ArrowRight: 'right'
};

// keyCombo builds the key input a keydown taps: the modifier prefixes and
// the key name, or '' when the key is plain printable text.
function keyCombo(event) {
    if (!event.ctrlKey && !event.metaKey && !event.altKey) {
        return SCREEN_SPECIAL_KEYS[event.key] || (/^F\d{1,2}$/.test(event.key) ? event.key.toLowerCase() : '');
    }
    const mods = [];
    if (event.ctrlKey) { mods.push('ctrl'); }
    if (event.metaKey) { mods.push('cmd'); }
    if (event.altKey) { mods.push('alt'); }
    if (event.shiftKey) { mods.push('shift'); }
    let key = SCREEN_SPECIAL_KEYS[event.key];
    if (!key && event.key.length === 1) {
        key = event.key.toLowerCase();
    }
    if (!key) {
        return '';
    }
    return mods.join('+') + '+' + key;
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
    const terminalManager = new TerminalManager();
});
