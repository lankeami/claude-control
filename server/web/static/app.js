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

    // Unified input (replaces old instructionText)
    inputText: '',
    inputSending: false,
    inputSuccess: false,

    // Managed session state
    showNewSessionModal: false,
    newSessionCWD: '',
    sessionSSE: null,

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
            const sess = this.sessions.find(s => s.id === this.selectedSessionId);
            if (sess && sess.mode === 'managed') {
              // Don't refetch managed messages on every SSE tick — they stream via per-session SSE
            } else {
              this.fetchTranscript(this.selectedSessionId);
            }
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
          const messages = await resp.json();
          // Append answered prompt responses not yet in the transcript
          // so all tabs see the user's reply immediately
          const answeredPrompts = this.prompts.filter(p =>
            p.session_id === sessionId &&
            p.status === 'answered' &&
            p.response
          );
          for (const p of answeredPrompts) {
            const responseText = p.response.trim();
            if (!responseText) continue;
            // Check if this response already appears as a recent user message
            const lastUserMsgs = messages.filter(m => m.role === 'user').slice(-3);
            const alreadyVisible = lastUserMsgs.some(m =>
              m.content.trim() === responseText
            );
            if (!alreadyVisible) {
              messages.push({
                role: 'user',
                content: responseText,
                timestamp: p.answered_at || p.created_at,
                msg_type: 'text'
              });
            }
          }
          this.chatMessages = messages;
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

    get currentSession() {
      if (!this.selectedSessionId) return null;
      return this.sessions.find(s => s.id === this.selectedSessionId) || null;
    },

    sessionName(session) {
      if (session.mode === 'managed' && session.cwd) {
        const parts = session.cwd.split('/');
        return parts[parts.length - 1] || parts[parts.length - 2] || session.cwd;
      }
      const parts = session.project_path.split('/');
      const proj = parts[parts.length - 1] || parts[parts.length - 2] || session.project_path;
      return `${session.computer_name} / ${proj}`;
    },

    sessionStatus(session) {
      if (session.mode === 'managed') {
        return session.status; // managed sessions have accurate status (idle/running)
      }
      const lastSeen = new Date(session.last_seen_at);
      const fiveMinAgo = new Date(Date.now() - 5 * 60 * 1000);
      if (lastSeen < fiveMinAgo) return 'idle';
      return session.status;
    },

    async selectSession(id) {
      this.selectedSessionId = this.selectedSessionId === id ? null : id;
      this.stopSessionSSE();

      if (!this.selectedSessionId) {
        this.chatMessages = [];
        return;
      }

      const sess = this.sessions.find(s => s.id === this.selectedSessionId);
      if (sess && sess.mode === 'managed') {
        await this.fetchManagedMessages(this.selectedSessionId);
        if (sess.status === 'running') {
          this.startSessionSSE(this.selectedSessionId);
        }
      } else {
        this.fetchTranscript(this.selectedSessionId, true);
      }
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

    async handleInput() {
      if (!this.selectedSessionId || !this.inputText.trim()) return;
      const sess = this.currentSession;

      if (sess && sess.mode === 'managed') {
        await this.sendManagedMessage();
      } else {
        await this.sendInstruction();
      }
    },

    async sendInstruction() {
      if (!this.selectedSessionId || !this.inputText.trim()) return;
      this.inputSending = true;
      try {
        const resp = await fetch(`/api/sessions/${this.selectedSessionId}/instruct`, {
          method: 'POST',
          headers: {
            'Authorization': `Bearer ${this.apiKey}`,
            'Content-Type': 'application/json'
          },
          body: JSON.stringify({ message: this.inputText.trim() })
        });
        if (resp.status === 401) { this.disconnect(); return; }
        if (resp.ok) {
          this.inputText = '';
          this.inputSuccess = true;
          setTimeout(() => this.inputSuccess = false, 1500);
        }
      } catch (e) {}
      this.inputSending = false;
    },

    async createManagedSession() {
      if (!this.newSessionCWD.trim()) return;
      try {
        const res = await fetch('/api/sessions/create', {
          method: 'POST',
          headers: { 'Authorization': 'Bearer ' + this.apiKey, 'Content-Type': 'application/json' },
          body: JSON.stringify({ cwd: this.newSessionCWD.trim() })
        });
        if (!res.ok) throw new Error(await res.text());
        this.showNewSessionModal = false;
        this.newSessionCWD = '';
        this.toast('Session created');
      } catch (e) {
        this.toast('Error: ' + e.message);
      }
    },

    async sendManagedMessage() {
      if (!this.inputText.trim() || !this.selectedSessionId) return;
      const msg = this.inputText.trim();
      this.inputText = '';
      this.inputSending = true;

      try {
        const res = await fetch(`/api/sessions/${this.selectedSessionId}/message`, {
          method: 'POST',
          headers: { 'Authorization': 'Bearer ' + this.apiKey, 'Content-Type': 'application/json' },
          body: JSON.stringify({ message: msg })
        });
        if (!res.ok) throw new Error(await res.text());

        // Add user message to chat immediately
        this.chatMessages.push({ role: 'user', content: msg, msg_type: 'text', timestamp: new Date().toISOString() });
        this.$nextTick(() => this.scrollToBottom(true));

        // Start SSE stream for this session
        this.startSessionSSE(this.selectedSessionId);
      } catch (e) {
        this.toast('Error: ' + e.message);
      }
      this.inputSending = false;
    },

    startSessionSSE(sessionId) {
      this.stopSessionSSE();
      const url = `/api/sessions/${sessionId}/stream?token=${encodeURIComponent(this.apiKey)}`;
      this.sessionSSE = new EventSource(url);

      this.sessionSSE.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data);
          if (data.type === 'done') {
            this.stopSessionSSE();
            return;
          }

          // Only display assistant messages and error events
          if (data.type === 'assistant' && data.message) {
            // Extract text blocks from message content
            let textParts = [];
            const contentArr = data.message.content || [];
            if (Array.isArray(contentArr)) {
              for (const block of contentArr) {
                if (block.type === 'text' && block.text) {
                  textParts.push(block.text);
                }
              }
            } else if (typeof data.message.content === 'string') {
              textParts.push(data.message.content);
            } else if (typeof data.message === 'string') {
              textParts.push(data.message);
            }
            if (textParts.length > 0) {
              this.chatMessages.push({
                role: 'assistant',
                content: textParts.join('\n'),
                msg_type: 'text',
                timestamp: new Date().toISOString()
              });
              this.$nextTick(() => this.scrollToBottom());
            }
          } else if (data.type === 'system' && data.error) {
            // Show error messages from the server
            this.chatMessages.push({
              role: 'system',
              content: data.stderr || 'Process error (exit code ' + data.exit_code + ')',
              msg_type: 'text',
              timestamp: new Date().toISOString()
            });
            this.$nextTick(() => this.scrollToBottom());
          }
          // Skip: system init, user echo, result, tool_use, tool_result
        } catch (e) {
          // Ignore unparseable lines
        }
      };

      this.sessionSSE.onerror = () => {
        this.stopSessionSSE();
      };
    },

    stopSessionSSE() {
      if (this.sessionSSE) {
        this.sessionSSE.close();
        this.sessionSSE = null;
      }
    },

    async interruptSession() {
      if (!this.selectedSessionId) return;
      try {
        await fetch(`/api/sessions/${this.selectedSessionId}/interrupt`, {
          method: 'POST',
          headers: { 'Authorization': 'Bearer ' + this.apiKey }
        });
        this.toast('Session interrupted');
      } catch (e) {
        this.toast('Error: ' + e.message);
      }
    },

    async fetchManagedMessages(sessionId) {
      if (!sessionId) return;
      this.chatLoading = true;
      try {
        const res = await fetch(`/api/sessions/${sessionId}/messages`, {
          headers: { 'Authorization': 'Bearer ' + this.apiKey }
        });
        if (!res.ok) return;
        const msgs = await res.json();
        this.chatMessages = (msgs || [])
          .filter(m => m.role === 'assistant' || (m.role === 'system' && m.content && m.content.includes('"error"')))
          .map(m => ({
            role: m.role,
            content: m.content,
            msg_type: 'text',
            timestamp: m.created_at
          }));
        this.$nextTick(() => this.scrollToBottom(true));
      } catch (e) {
        console.error('Failed to fetch messages:', e);
      }
      this.chatLoading = false;
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
