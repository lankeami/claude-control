document.addEventListener('alpine:init', () => {
  Alpine.data('app', () => ({
    // Auth state
    apiKey: localStorage.getItem('apiKey') || '',
    authenticated: false,
    loginKey: '',
    loginError: '',
    loginLoading: false,

    // Data state
    sessions: [],
    prompts: [],
    selectedSessionId: null,
    chatMessages: [],
    chatLoading: false,
    responseText: '',
    responseSending: false,
    connected: true,

    // SSE
    eventSource: null,
    sseFailCount: 0,
    pollInterval: null,

    // Instruction
    instructionText: '',
    instructionSending: false,
    instructionSuccess: false,

    // Toast
    showToast: false,
    toastMessage: '',
    toastTimer: null,

    async init() {
      if (this.apiKey) {
        await this.tryConnect(this.apiKey);
      }
    },

    // Auth
    async login() {
      this.loginError = '';
      this.loginLoading = true;
      const ok = await this.tryConnect(this.loginKey);
      this.loginLoading = false;
      if (!ok) {
        this.loginError = 'Invalid API key or server unreachable';
      }
    },

    async tryConnect(key) {
      try {
        const resp = await fetch('/api/status', {
          headers: { 'Authorization': `Bearer ${key}` }
        });
        if (resp.ok) {
          this.apiKey = key;
          localStorage.setItem('apiKey', key);
          this.authenticated = true;
          this.startSSE();
          return true;
        }
      } catch (e) {}
      return false;
    },

    disconnect() {
      this.stopSSE();
      this.authenticated = false;
      this.apiKey = '';
      this.loginKey = '';
      localStorage.removeItem('apiKey');
      this.sessions = [];
      this.prompts = [];
    },

    // SSE
    startSSE() {
      this.stopSSE();
      this.sseFailCount = 0;
      const url = `/api/events?token=${encodeURIComponent(this.apiKey)}`;
      this.eventSource = new EventSource(url);

      this.eventSource.addEventListener('update', (e) => {
        try {
          const data = JSON.parse(e.data);
          const hadPending = this.currentPendingPrompt;
          this.sessions = data.sessions || [];
          this.prompts = data.prompts || [];
          this.connected = true;
          this.sseFailCount = 0;
          if (!hadPending && this.currentPendingPrompt) {
            this.toast('Respond here or in the CLI \u2014 one per turn, not both.');
          }
          if (this.selectedSessionId) {
            this.fetchTranscript(this.selectedSessionId);
          }
        } catch (err) {}
      });

      this.eventSource.onerror = () => {
        this.connected = false;
        this.sseFailCount++;
        if (this.sseFailCount >= 3) {
          this.stopSSE();
          this.startPolling();
        }
      };
    },

    stopSSE() {
      if (this.eventSource) {
        this.eventSource.close();
        this.eventSource = null;
      }
      this.stopPolling();
    },

    // Polling fallback
    startPolling() {
      this.stopPolling();
      this.pollInterval = setInterval(() => this.pollState(), 5000);
      this.pollState();
    },

    stopPolling() {
      if (this.pollInterval) {
        clearInterval(this.pollInterval);
        this.pollInterval = null;
      }
    },

    async pollState() {
      try {
        const headers = { 'Authorization': `Bearer ${this.apiKey}` };
        const [sessResp, promptResp] = await Promise.all([
          fetch('/api/sessions', { headers }),
          fetch('/api/prompts', { headers })
        ]);
        if (sessResp.status === 401 || promptResp.status === 401) {
          this.disconnect();
          return;
        }
        this.sessions = await sessResp.json();
        this.prompts = await promptResp.json();
        this.connected = true;
      } catch (e) {
        this.connected = false;
      }
    },

    async fetchTranscript(sessionId, forceScroll = false) {
      if (!sessionId) {
        this.chatMessages = [];
        return;
      }
      this.chatLoading = true;
      try {
        const resp = await fetch(`/api/sessions/${sessionId}/transcript`, {
          headers: { 'Authorization': `Bearer ${this.apiKey}` }
        });
        if (resp.status === 401) { this.disconnect(); return; }
        if (resp.ok) {
          this.chatMessages = await resp.json();
          this.$nextTick(() => this.scrollToBottom(forceScroll));
        }
      } catch (e) {}
      this.chatLoading = false;
    },

    scrollToBottom(force = false) {
      const el = document.getElementById('chat-area');
      if (!el) return;
      // Only auto-scroll if user is already near the bottom (within 150px)
      const isNearBottom = (el.scrollHeight - el.scrollTop - el.clientHeight) < 150;
      if (force || isNearBottom) {
        el.scrollTop = el.scrollHeight;
      }
    },

    toast(msg, duration = 4000) {
      this.toastMessage = msg;
      this.showToast = true;
      if (this.toastTimer) clearTimeout(this.toastTimer);
      this.toastTimer = setTimeout(() => { this.showToast = false; }, duration);
    },

    // Computed
    get filteredPrompts() {
      let p = this.prompts;
      if (this.selectedSessionId) {
        p = p.filter(pr => pr.session_id === this.selectedSessionId);
      }
      // Pending first, then by created_at desc
      return p.sort((a, b) => {
        if (a.status === 'pending' && b.status !== 'pending') return -1;
        if (b.status === 'pending' && a.status !== 'pending') return 1;
        return new Date(b.created_at) - new Date(a.created_at);
      });
    },

    pendingCountFor(sessionId) {
      return this.prompts.filter(p =>
        p.status === 'pending' && p.type === 'prompt' &&
        (!sessionId || p.session_id === sessionId)
      ).length;
    },

    get totalPendingCount() {
      return this.pendingCountFor(null);
    },

    get currentPendingPrompt() {
      if (!this.selectedSessionId) return null;
      return this.prompts.find(p =>
        p.session_id === this.selectedSessionId &&
        p.status === 'pending' &&
        p.type === 'prompt'
      ) || null;
    },

    // Check if the pending prompt's hook has likely timed out (>2min old)
    get isPromptStale() {
      const p = this.currentPendingPrompt;
      if (!p) return false;
      const age = Date.now() - new Date(p.created_at).getTime();
      return age > 2 * 60 * 1000;
    },

    sessionName(session) {
      const parts = session.project_path.split('/');
      const proj = parts[parts.length - 1] || parts[parts.length - 2] || session.project_path;
      return `${session.computer_name} / ${proj}`;
    },

    sessionStatus(session) {
      const lastSeen = new Date(session.last_seen_at);
      const fiveMinAgo = new Date(Date.now() - 5 * 60 * 1000);
      if (lastSeen < fiveMinAgo) return 'idle';
      return session.status;
    },

    selectSession(id) {
      this.selectedSessionId = this.selectedSessionId === id ? null : id;
      this.fetchTranscript(this.selectedSessionId, true);
    },

    async deleteSession(id) {
      try {
        const resp = await fetch(`/api/sessions/${id}`, {
          method: 'DELETE',
          headers: { 'Authorization': `Bearer ${this.apiKey}` }
        });
        if (resp.status === 401) { this.disconnect(); return; }
        if (resp.ok) {
          if (this.selectedSessionId === id) {
            this.selectedSessionId = null;
            this.chatMessages = [];
          }
        }
      } catch (e) {}
    },

    // Actions
    async respondToPrompt(promptId, response) {
      // Use the prompt's own session_id, not selectedSessionId, to avoid mismatches
      const prompt = this.prompts.find(p => p.id === promptId);
      const sessionId = prompt ? prompt.session_id : this.selectedSessionId;

      // If the hook has likely timed out (>2min), queue as instruction instead
      if (this.isPromptStale && sessionId) {
        try {
          const resp = await fetch(`/api/sessions/${sessionId}/instruct`, {
            method: 'POST',
            headers: {
              'Authorization': `Bearer ${this.apiKey}`,
              'Content-Type': 'application/json'
            },
            body: JSON.stringify({ message: response })
          });
          if (resp.status === 401) { this.disconnect(); return false; }
          if (resp.ok) {
            this.responseText = '';
          }
          return resp.ok;
        } catch (e) { return false; }
      }

      try {
        const resp = await fetch(`/api/prompts/${promptId}/respond`, {
          method: 'POST',
          headers: {
            'Authorization': `Bearer ${this.apiKey}`,
            'Content-Type': 'application/json'
          },
          body: JSON.stringify({ response })
        });
        if (resp.status === 401) { this.disconnect(); return; }
        if (resp.ok) {
          this.responseText = '';
          if (this.selectedSessionId) {
            this.fetchTranscript(this.selectedSessionId);
          }
        }
        return resp.ok;
      } catch (e) { return false; }
    },

    async sendInstruction() {
      if (!this.selectedSessionId || !this.instructionText.trim()) return;
      this.instructionSending = true;
      try {
        const resp = await fetch(`/api/sessions/${this.selectedSessionId}/instruct`, {
          method: 'POST',
          headers: {
            'Authorization': `Bearer ${this.apiKey}`,
            'Content-Type': 'application/json'
          },
          body: JSON.stringify({ message: this.instructionText.trim() })
        });
        if (resp.status === 401) { this.disconnect(); return; }
        if (resp.ok) {
          this.instructionText = '';
          this.instructionSuccess = true;
          setTimeout(() => this.instructionSuccess = false, 1500);
        }
      } catch (e) {}
      this.instructionSending = false;
    },

    // Bubble rendering
    bubbleClass(msg, idx) {
      const classes = [];
      if (msg.msg_type === 'text') {
        classes.push(msg.role);
        if (idx === this.chatMessages.length - 1 && msg.role === 'assistant' && this.currentPendingPrompt) {
          classes.push('waiting');
        }
      } else {
        classes.push('tool');
      }
      return classes.join(' ');
    },

    bubbleHTML(msg) {
      const esc = (s) => s ? s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;') : '';
      const time = `<span class="bubble-time">${esc(this.timeAgo(msg.timestamp))}</span>`;

      if (msg.msg_type === 'text') {
        return `${esc(msg.content)}${time}`;
      }
      if (msg.msg_type === 'edit') {
        let diff = '';
        if (msg.old_string) diff += `<div class="diff-old">${esc(msg.old_string)}</div>`;
        if (msg.new_string) diff += `<div class="diff-new">${esc(msg.new_string)}</div>`;
        return `<div class="tool-label">Edit</div><div class="tool-filepath">${esc(msg.file_path)}</div><div class="diff-block">${diff}</div>${time}`;
      }
      if (msg.msg_type === 'write') {
        return `<div class="tool-label">Write</div><div class="tool-filepath">${esc(msg.file_path)}</div>${time}`;
      }
      if (msg.msg_type === 'bash') {
        let cmd = msg.command ? `<div class="bash-cmd">${esc(msg.command)}</div>` : '';
        return `<div class="tool-label">Bash</div><div>${esc(msg.content)}</div>${cmd}${time}`;
      }
      return `${esc(msg.content)}${time}`;
    },

    // Time formatting
    timeAgo(dateStr) {
      const date = new Date(dateStr);
      const seconds = Math.floor((Date.now() - date.getTime()) / 1000);
      if (seconds < 60) return 'just now';
      const minutes = Math.floor(seconds / 60);
      if (minutes < 60) return `${minutes}m ago`;
      const hours = Math.floor(minutes / 60);
      if (hours < 24) return `${hours}h ago`;
      return `${Math.floor(hours / 24)}d ago`;
    }
  }));
});
