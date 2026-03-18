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
    connected: true,

    // SSE
    eventSource: null,
    sseFailCount: 0,
    pollInterval: null,

    // Instruction
    instructionText: '',
    instructionSending: false,
    instructionSuccess: false,

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
          this.sessions = data.sessions || [];
          this.prompts = data.prompts || [];
          this.connected = true;
          this.sseFailCount = 0;
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
    },

    // Actions
    async respondToPrompt(promptId, response) {
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
