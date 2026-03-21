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

    // Directory browser state
    browsePath: '',
    browseEntries: [],
    browseLoading: false,

    // Resume picker state
    showResumePicker: false,
    resumableSessions: [],
    resumeLoading: false,

    // File browser state
    sessionFiles: [],
    fileTreeData: [],
    gitInfo: null,
    viewerFile: null,
    viewerMode: 'diff',
    viewerDiffs: [],
    viewerDiffHtml: '',
    viewerContent: '',
    viewerLoading: false,
    viewerBinary: false,
    viewerTruncated: false,
    fileContentCache: {},
    viewerFullHtml: '',
    viewerFileType: '',
    renderedContentCache: {},

    // Toast
    showToast: false,
    toastMessage: '',
    toastTimer: null,

    // Activity Status Pills
    activityPills: [],
    stalenessTimer: null,
    heartbeatTimer: null,
    lastEventTime: null,
    currentPillStart: null,

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
          // Auto-select first session if none selected
          if (!this.selectedSessionId && this.sessions.length > 0) {
            this.selectSession(this.sessions[0].id);
          } else if (this.selectedSessionId) {
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

    get visibleFileNodes() {
      const nodes = [];
      const walk = (items) => {
        for (const node of items) {
          nodes.push(node);
          if (node.isDir && node.open && node.children) {
            walk(node.children);
          }
        }
      };
      walk(this.fileTreeData);
      return nodes;
    },

    get viewerFileName() {
      if (!this.viewerFile) return '';
      return this.viewerFile.split('/').pop();
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
      if (this.selectedSessionId === id) return;
      this.selectedSessionId = id;
      this.stopSessionSSE();
      this.clearActivityPills();
      this.closeFileViewer();
      this.sessionFiles = [];
      this.fileTreeData = [];
      this.fileContentCache = {};
      this.renderedContentCache = {};

      const sess = this.sessions.find(s => s.id === this.selectedSessionId);
      if (sess && sess.mode === 'managed') {
        await this.fetchManagedMessages(this.selectedSessionId);
        if (sess.status === 'running') {
          this.startSessionSSE(this.selectedSessionId);
        }
      } else {
        await this.fetchTranscript(this.selectedSessionId, true);
      }
      this.loadSessionFiles(this.selectedSessionId);
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
            this.chatMessages = [];
            const remaining = this.sessions.filter(s => s.id !== id);
            if (remaining.length > 0) {
              this.selectSession(remaining[0].id);
            } else {
              this.selectedSessionId = null;
            }
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

      // Intercept /resume command for managed sessions
      if (sess && sess.mode === 'managed' && this.inputText.trim().toLowerCase() === '/resume') {
        this.chatMessages.push({ role: 'user', content: '/resume', msg_type: 'text', timestamp: new Date().toISOString() });
        this.$nextTick(() => this.scrollToBottom(true));
        this.inputText = '';
        await this.openResumePicker();
        return;
      }

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

    async openNewSessionModal() {
      this.showNewSessionModal = true;
      this.newSessionCWD = '';
      this.browsePath = '';
      this.browseEntries = [];
      await this.browseTo('');
    },

    async browseTo(path) {
      this.browseLoading = true;
      try {
        const url = '/api/browse' + (path ? '?path=' + encodeURIComponent(path) : '');
        const res = await fetch(url, {
          headers: { 'Authorization': 'Bearer ' + this.apiKey }
        });
        if (!res.ok) throw new Error(await res.text());
        const data = await res.json();
        this.browsePath = data.current;
        this.browseEntries = data.entries || [];
        this.newSessionCWD = data.current;
      } catch (e) {
        this.toast('Error browsing: ' + e.message);
      }
      this.browseLoading = false;
    },

    get breadcrumbs() {
      if (!this.browsePath) return [];
      const home = this.browsePath.split('/').slice(0, 3).join('/'); // e.g. /Users/username
      const parts = this.browsePath.split('/').filter(Boolean);
      const crumbs = [];
      let accumulated = '';
      for (let i = 0; i < parts.length; i++) {
        accumulated += '/' + parts[i];
        const isHome = accumulated === home;
        crumbs.push({
          label: isHome ? '~' : parts[i],
          path: accumulated,
          skipPrior: isHome
        });
      }
      const homeIdx = crumbs.findIndex(c => c.skipPrior);
      return homeIdx >= 0 ? crumbs.slice(homeIdx) : crumbs;
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

        // Start activity pills
        this.clearActivityPills();
        this.addActivityPill('Thinking...', 'active');
        this.resetStalenessTimer();

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
      this.resetHeartbeatTimer();

      this.sessionSSE.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data);

          // Heartbeat: just reset the timer and return
          if (data.type === 'heartbeat') {
            this.resetHeartbeatTimer();
            return;
          }

          // Reset both timers on any real event
          this.resetStalenessTimer();
          this.resetHeartbeatTimer();

          if (data.type === 'done' || data.type === 'result') {
            // Mark the current active pill as completed (don't clear — pills persist until next message)
            const activePill = this.activityPills.find(p => p.state === 'active');
            if (activePill) {
              const elapsed = Math.round((Date.now() - (this.currentPillStart || Date.now())) / 1000);
              activePill.state = 'completed';
              activePill.duration = elapsed + 's';
            }
            this.clearStalenessTimer();
          }
          if (data.type === 'done') {
            this.stopSessionSSE();
            return;
          }

          // Activity pill: tool_use blocks in assistant messages
          if (data.type === 'assistant' && data.message && Array.isArray(data.message.content)) {
            for (const block of data.message.content) {
              if (block.type === 'tool_use') {
                const label = this.extractToolContext(block);
                this.addActivityPill(label, 'active');
              }
            }
          }

          // Activity pill: tool_result means Claude is thinking again
          if (data.type === 'tool_result') {
            this.addActivityPill('Thinking...', 'active');
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
          // Extract file paths from tool_use events for the file tree
          if (data.type === 'assistant' && data.message && data.message.content && Array.isArray(data.message.content)) {
            for (const block of data.message.content) {
              if (block.type === 'tool_use' && block.input && block.input.file_path) {
                if (['Edit', 'Write', 'Read'].includes(block.name)) {
                  const action = block.name.toLowerCase();
                  const existing = this.sessionFiles.find(f => f.path === block.input.file_path);
                  if (existing) {
                    // Update action on existing entry (file already in tree from filetree endpoint)
                    if (!existing.action || (action === 'edit' || action === 'write')) {
                      existing.action = action;
                      this.fileTreeData = this.buildFileTree(this.sessionFiles);
                    }
                  } else {
                    this.sessionFiles.push({ path: block.input.file_path, action, git_status: 'M' });
                    this.fileTreeData = this.buildFileTree(this.sessionFiles);
                  }
                }
              }
            }
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
      this.clearActivityPills();
      this.clearHeartbeatTimer();
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

    async openResumePicker() {
      this.resumeLoading = true;
      this.showResumePicker = true;
      this.resumableSessions = [];
      try {
        const res = await fetch(`/api/sessions/${this.selectedSessionId}/resumable`, {
          headers: { 'Authorization': 'Bearer ' + this.apiKey }
        });
        if (res.status === 404) {
          this.toast('No previous CLI sessions found for this project');
          this.showResumePicker = false;
          this.resumeLoading = false;
          return;
        }
        if (!res.ok) throw new Error(await res.text());
        const data = await res.json();
        this.resumableSessions = data.sessions || [];
      } catch (e) {
        this.toast('Error: ' + e.message);
        this.showResumePicker = false;
      }
      this.resumeLoading = false;
    },

    async resumeSession(claudeSessionId, summary) {
      try {
        const res = await fetch(`/api/sessions/${this.selectedSessionId}/resume`, {
          method: 'POST',
          headers: { 'Authorization': 'Bearer ' + this.apiKey, 'Content-Type': 'application/json' },
          body: JSON.stringify({ session_id: claudeSessionId })
        });
        if (!res.ok) throw new Error(await res.text());
        const data = await res.json();
        this.showResumePicker = false;

        // Show recent messages from the resumed session for context
        this.chatMessages = (data.recent_messages || []).map(m => ({
          role: m.role,
          content: m.content,
          msg_type: 'text',
          timestamp: new Date().toISOString()
        }));
        this.$nextTick(() => this.scrollToBottom(true));
        this.toast('Resumed: ' + (summary || 'session'));
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
          .filter(m => m.role === 'user' || m.role === 'assistant' || (m.role === 'system' && m.content && m.content.includes('"error"')))
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

    // File browser methods
    async loadSessionFiles(sessionId) {
      this.renderedContentCache = {};
      if (!sessionId) { this.sessionFiles = []; this.fileTreeData = []; this.gitInfo = null; return; }
      try {
        const resp = await fetch(`/api/sessions/${sessionId}/filetree`, {
          headers: { 'Authorization': 'Bearer ' + this.apiKey }
        });
        if (!resp.ok) {
          // Fallback to old endpoint if filetree not available
          const fallback = await fetch(`/api/sessions/${sessionId}/files`, {
            headers: { 'Authorization': 'Bearer ' + this.apiKey }
          });
          if (!fallback.ok) { this.sessionFiles = []; this.fileTreeData = []; this.gitInfo = null; return; }
          const data = await fallback.json();
          this.sessionFiles = data.files || [];
          this.fileTreeData = this.buildFileTree(this.sessionFiles);
          this.gitInfo = null;
          return;
        }
        const data = await resp.json();
        this.sessionFiles = data.files || [];
        this.fileTreeData = this.buildFileTree(this.sessionFiles);
        this.gitInfo = data.git || null;
      } catch (e) {
        this.sessionFiles = [];
        this.fileTreeData = [];
        this.gitInfo = null;
      }
    },

    buildFileTree(files) {
      if (!files || files.length === 0) return [];
      const paths = files.map(f => f.path);
      const prefix = this.commonPrefix(paths);
      const root = {};
      for (const file of files) {
        const rel = file.path.substring(prefix.length).replace(/^\//, '');
        const parts = rel.split('/');
        let node = root;
        for (let i = 0; i < parts.length; i++) {
          if (!node[parts[i]]) node[parts[i]] = {};
          if (i < parts.length - 1) { node = node[parts[i]]; }
          else { node[parts[i]]._file = file; }
        }
      }
      const toArray = (obj, depth, parentPath) => {
        const entries = Object.entries(obj).filter(([k]) => k !== '_file');
        entries.sort(([a, aVal], [b, bVal]) => {
          const aDir = !aVal._file; const bDir = !bVal._file;
          if (aDir !== bDir) return aDir ? -1 : 1;
          return a.localeCompare(b);
        });
        const result = [];
        for (const [name, val] of entries) {
          if (val._file) {
            result.push({
              name, path: val._file.path,
              action: val._file.action || null,
              gitStatus: val._file.git_status || null,
              isDir: false, depth, open: false, children: []
            });
          } else {
            const dirPath = parentPath ? parentPath + '/' + name : prefix + name;
            const children = toArray(val, depth + 1, dirPath);
            // Directories are collapsed by default; open if small or has changes
            const hasChanges = children.some(c => c.gitStatus || c.action || (c.isDir && c.open));
            result.push({ name, path: dirPath, isDir: true, depth, open: hasChanges, children, action: null, gitStatus: null });
          }
        }
        return result;
      };
      return toArray(root, 0, '');
    },

    commonPrefix(paths) {
      if (paths.length === 0) return '';
      if (paths.length === 1) return paths[0].substring(0, paths[0].lastIndexOf('/') + 1);
      let prefix = paths[0];
      for (let i = 1; i < paths.length; i++) {
        while (prefix.length > 0 && !paths[i].startsWith(prefix)) {
          prefix = prefix.substring(0, prefix.lastIndexOf('/'));
        }
      }
      if (prefix && !prefix.endsWith('/')) prefix = prefix.substring(0, prefix.lastIndexOf('/') + 1);
      return prefix || '/';
    },

    toggleDir(node) { node.open = !node.open; },

    async openFileViewer(filePath) {
      if (this.viewerFile === filePath) { this.closeFileViewer(); return; }
      this.viewerFile = filePath;
      this.viewerMode = 'full';
      this.viewerContent = '';
      this.viewerLoading = false;
      this.viewerBinary = false;
      this.viewerTruncated = false;
      this.viewerDiffs = [];
      this.viewerDiffHtml = '';
      this.viewerFullHtml = '';
      this.viewerFileType = '';

      // Default to full view — fetch and render immediately
      await this.switchToFullView();
    },

    async switchToDiffView() {
      this.viewerMode = 'diff';
      if (!this.viewerFile || this.viewerDiffHtml) return;

      this.viewerLoading = true;
      try {
        const params = new URLSearchParams({ path: this.viewerFile, session_id: this.selectedSessionId });
        const resp = await fetch('/api/files/diff?' + params, {
          headers: { 'Authorization': 'Bearer ' + this.apiKey }
        });
        if (resp.ok) {
          const data = await resp.json();
          this.viewerDiffHtml = this.renderDiffHtml(data);
        }
      } catch (e) {
        // Ignore — will show empty diff
      }
      this.viewerLoading = false;
    },

    async switchToFullView() {
      this.viewerMode = 'full';
      if (!this.viewerFile) return;

      const cacheKey = this.viewerFile + '::' + this.selectedSessionId;

      if (this.renderedContentCache[cacheKey]) {
        const cached = this.renderedContentCache[cacheKey];
        this.viewerFullHtml = cached.html;
        this.viewerFileType = cached.fileType;
        this.viewerBinary = cached.binary;
        this.viewerTruncated = cached.truncated;
        return;
      }

      const rawCacheKey = this.viewerFile + ':' + this.selectedSessionId;
      let data;
      if (this.fileContentCache[rawCacheKey]) {
        data = this.fileContentCache[rawCacheKey];
      } else {
        this.viewerLoading = true;
        try {
          const params = new URLSearchParams({ path: this.viewerFile, session_id: this.selectedSessionId });
          const resp = await fetch('/api/files/content?' + params, {
            headers: { 'Authorization': 'Bearer ' + this.apiKey }
          });
          if (!resp.ok) { this.viewerFullHtml = '<pre>' + this.escapeHtml('Error loading file.') + '</pre>'; return; }
          data = await resp.json();
          if (!data.exists) { this.viewerFullHtml = '<pre>' + this.escapeHtml('File no longer exists on disk.') + '</pre>'; return; }
          this.fileContentCache[rawCacheKey] = data;
        } catch (e) {
          this.viewerFullHtml = '<pre>' + this.escapeHtml('Error loading file.') + '</pre>';
          return;
        } finally {
          this.viewerLoading = false;
        }
      }

      this.viewerBinary = data.binary || false;
      this.viewerTruncated = data.truncated || false;

      const ext = this.viewerFile.split('.').pop().toLowerCase();
      const imageExts = ['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp', 'ico', 'bmp'];
      if (data.binary && !imageExts.includes(ext)) {
        this.viewerFullHtml = '';
        this.viewerFileType = '';
        return;
      }

      const renderer = this.getRenderer(this.viewerFile);
      this.viewerFileType = renderer.label;
      this.viewerFullHtml = renderer.render(data.content, this.viewerFile);

      this.renderedContentCache[cacheKey] = {
        html: this.viewerFullHtml,
        fileType: this.viewerFileType,
        binary: this.viewerBinary,
        truncated: this.viewerTruncated,
      };
    },

    renderDiffHtml(data) {
      const esc = s => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
      if (data.status === 'new' && data.content) {
        return '<pre class="git-diff-content">' +
          data.content.split('\n').map(l => '<span class="diff-line diff-add">' + esc('+ ' + l) + '</span>').join('\n') +
          '</pre>';
      }
      if (data.diff) {
        return '<pre class="git-diff-content">' +
          data.diff.split('\n').map(l => {
            let cls = '';
            if (l.startsWith('+') && !l.startsWith('+++')) cls = 'diff-add';
            else if (l.startsWith('-') && !l.startsWith('---')) cls = 'diff-remove';
            else if (l.startsWith('@@')) cls = 'diff-hunk';
            else if (l.startsWith('diff ') || l.startsWith('index ') || l.startsWith('---') || l.startsWith('+++')) cls = 'diff-meta';
            return '<span class="diff-line' + (cls ? ' ' + cls : '') + '">' + esc(l) + '</span>';
          }).join('\n') +
          '</pre>';
      }
      return '';
    },

    closeFileViewer() {
      this.viewerFile = null;
      this.viewerDiffs = [];
      this.viewerDiffHtml = '';
      this.viewerContent = '';
      this.viewerFullHtml = '';
      this.viewerFileType = '';
    },

    // Bubble rendering
    // --- Activity Status Pills ---

    addActivityPill(label, state) {
        const now = Date.now();
        const activePill = this.activityPills.find(p => p.state === 'active');
        if (activePill) {
            const elapsed = Math.round((now - (this.currentPillStart || now)) / 1000);
            activePill.state = 'completed';
            activePill.duration = elapsed + 's';
        }
        this.activityPills.push({ label, originalLabel: label, state, duration: null });
        this.currentPillStart = now;
        const completed = this.activityPills.filter(p => p.state === 'completed');
        if (completed.length > 10) {
            const idx = this.activityPills.indexOf(completed[0]);
            this.activityPills.splice(idx, 1);
        }
        this.$nextTick(() => this.scrollToBottom());
    },

    clearActivityPills() {
        this.activityPills = [];
        this.currentPillStart = null;
        this.clearStalenessTimer();
    },

    extractToolContext(block) {
        const name = block.name || 'Tool';
        const input = block.input || {};
        let context = '';
        if (input.file_path) {
            const parts = input.file_path.split('/');
            context = parts[parts.length - 1];
        } else if (input.command) {
            context = input.command.substring(0, 30);
        } else if (input.pattern) {
            context = input.pattern.substring(0, 30);
        }
        const full = context ? `${name} ${context}` : name;
        return full.length > 40 ? full.substring(0, 37) + '...' : full;
    },

    resetStalenessTimer() {
        this.clearStalenessTimer();
        this.lastEventTime = Date.now();
        const stalePill = this.activityPills.find(p => p.state === 'stale');
        if (stalePill) {
            stalePill.state = 'active';
            stalePill.label = stalePill.originalLabel;
        }
        this.stalenessTimer = setTimeout(() => {
            const activePill = this.activityPills.find(p => p.state === 'active');
            if (activePill) {
                const elapsed = Math.round((Date.now() - this.lastEventTime) / 1000);
                activePill.originalLabel = activePill.label;
                activePill.state = 'stale';
                activePill.label = `${activePill.label} — ${elapsed}s, may be stalled`;
            }
        }, 60000);
    },

    clearStalenessTimer() {
        if (this.stalenessTimer) {
            clearTimeout(this.stalenessTimer);
            this.stalenessTimer = null;
        }
    },

    resetHeartbeatTimer() {
        this.clearHeartbeatTimer();
        this.heartbeatTimer = setTimeout(() => {
            this.addActivityPill('Connection lost — server may be down', 'disconnected');
            this.clearStalenessTimer();
        }, 30000);
    },

    clearHeartbeatTimer() {
        if (this.heartbeatTimer) {
            clearTimeout(this.heartbeatTimer);
            this.heartbeatTimer = null;
        }
    },

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
        if (msg.role === 'assistant' && typeof marked !== 'undefined') {
          const r = new marked.Renderer();
          r.link = function(href, title, text) {
            const h = typeof href === 'object' ? href.href : href;
            const t = typeof href === 'object' ? href.title : title;
            const tx = typeof href === 'object' ? href.text : text;
            const titleAttr = t ? ` title="${t}"` : '';
            return `<a href="${h}" target="_blank" rel="noopener noreferrer"${titleAttr}>${tx}</a>`;
          };
          const html = marked.parse(msg.content || '', { renderer: r, breaks: true });
          return `<div class="markdown-content">${html}</div>${time}`;
        }
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
    },

    escapeHtml(str) {
      if (!str) return '';
      return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#039;');
    },

    sanitizeHtml(html) {
      const allowed = new Set([
        'p', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
        'ul', 'ol', 'li', 'a', 'strong', 'em', 'b', 'i',
        'code', 'pre', 'blockquote',
        'table', 'thead', 'tbody', 'tr', 'th', 'td',
        'img', 'br', 'hr', 'span', 'div', 'del', 'sup', 'sub'
      ]);
      return html
        .replace(/<\/?([a-zA-Z][a-zA-Z0-9]*)\b[^>]*>/g, (match, tag) => {
          if (!allowed.has(tag.toLowerCase())) return '';
          return match
            .replace(/\s+on\w+\s*=\s*("[^"]*"|'[^']*'|[^\s>]*)/gi, '')
            .replace(/\s+href\s*=\s*"javascript:[^"]*"/gi, '')
            .replace(/\s+href\s*=\s*'javascript:[^']*'/gi, '');
        });
    },

    getRenderer(filePath) {
      const name = filePath.split('/').pop();
      const ext = name.includes('.') ? name.split('.').pop().toLowerCase() : '';

      const extMap = {
        'md': { render: (c) => this.renderMarkdown(c), label: 'Markdown' },
        'mdx': { render: (c) => this.renderMarkdown(c), label: 'Markdown' },
        'json': { render: (c) => this.renderJSON(c), label: 'JSON' },
        'csv': { render: (c) => this.renderCSV(c, ','), label: 'CSV' },
        'tsv': { render: (c) => this.renderCSV(c, '\t'), label: 'TSV' },
        'html': { render: (c) => this.renderHTMLPreview(c), label: 'HTML' },
        'htm': { render: (c) => this.renderHTMLPreview(c), label: 'HTML' },
        'png': { render: (c, f) => this.renderImage(c, f), label: 'Image' },
        'jpg': { render: (c, f) => this.renderImage(c, f), label: 'Image' },
        'jpeg': { render: (c, f) => this.renderImage(c, f), label: 'Image' },
        'gif': { render: (c, f) => this.renderImage(c, f), label: 'Image' },
        'svg': { render: (c, f) => this.renderImage(c, f), label: 'SVG' },
        'webp': { render: (c, f) => this.renderImage(c, f), label: 'Image' },
        'ico': { render: (c, f) => this.renderImage(c, f), label: 'Image' },
        'bmp': { render: (c, f) => this.renderImage(c, f), label: 'Image' },
      };

      const codeExts = [
        'py', 'go', 'js', 'ts', 'jsx', 'tsx', 'rb', 'java', 'rs', 'css', 'scss',
        'sh', 'bash', 'zsh', 'sql', 'yaml', 'yml', 'toml', 'xml', 'c', 'cpp',
        'h', 'hpp', 'swift', 'kt', 'lua', 'r', 'php', 'pl', 'ex', 'erl', 'hs',
        'scala', 'clj', 'dart', 'vim', 'dockerfile'
      ];
      const langMap = {
        'py': 'python', 'js': 'javascript', 'ts': 'typescript', 'rb': 'ruby',
        'rs': 'rust', 'sh': 'bash', 'bash': 'bash', 'zsh': 'bash',
        'yml': 'yaml', 'kt': 'kotlin', 'ex': 'elixir', 'erl': 'erlang',
        'hs': 'haskell', 'clj': 'clojure', 'hpp': 'cpp', 'h': 'c',
        'jsx': 'javascript', 'tsx': 'typescript', 'scss': 'scss',
        'pl': 'perl', 'dockerfile': 'dockerfile'
      };

      if (extMap[ext]) return extMap[ext];

      if (codeExts.includes(ext)) {
        const lang = langMap[ext] || ext;
        return { render: (c) => this.renderCode(c, lang), label: lang.charAt(0).toUpperCase() + lang.slice(1) };
      }

      const filenameMap = {
        'Dockerfile': { lang: 'dockerfile', label: 'Dockerfile' },
        'Makefile': { lang: 'makefile', label: 'Makefile' },
        'Gemfile': { lang: 'ruby', label: 'Ruby' },
        'Rakefile': { lang: 'ruby', label: 'Ruby' },
        'Vagrantfile': { lang: 'ruby', label: 'Ruby' },
        'Procfile': { lang: 'yaml', label: 'YAML' },
        '.gitignore': { lang: 'plaintext', label: 'Git Ignore' },
        '.env': { lang: 'bash', label: 'Env' },
        '.dockerignore': { lang: 'plaintext', label: 'Docker Ignore' },
      };
      if (filenameMap[name]) {
        const fm = filenameMap[name];
        return { render: (c) => this.renderCode(c, fm.lang), label: fm.label };
      }

      return { render: (c) => this.renderPlainText(c), label: '' };
    },

    renderCode(content, language) {
      if (!content) return '<pre class="code-viewer"><code></code></pre>';

      let highlighted;
      let detectedLang = language || '';

      if (typeof hljs !== 'undefined') {
        try {
          if (language && language !== 'plaintext') {
            const result = hljs.highlight(content, { language, ignoreIllegals: true });
            highlighted = result.value;
            detectedLang = result.language || language;
          } else if (language === 'plaintext') {
            highlighted = this.escapeHtml(content);
          } else {
            const result = hljs.highlightAuto(content);
            highlighted = result.value;
            detectedLang = result.language || '';
          }
        } catch (e) {
          highlighted = this.escapeHtml(content);
        }
      } else {
        highlighted = this.escapeHtml(content);
      }

      const lines = highlighted.split('\n');
      const lineHtml = lines.map(line => '<span class="line">' + (line || ' ') + '</span>').join('');

      const badge = detectedLang ? '<span class="language-badge">' + this.escapeHtml(detectedLang) + '</span>' : '';

      return '<div class="code-viewer-wrapper">' + badge +
        '<pre class="code-viewer"><code>' + lineHtml + '</code></pre></div>';
    },

    renderPlainText(content) {
      if (!content) return '<pre class="full-file-content-pre"></pre>';
      return '<pre class="full-file-content-pre">' + this.escapeHtml(content) + '</pre>';
    },

    renderMarkdown(content) {
      if (!content) return '<div class="markdown-content"></div>';

      if (typeof marked === 'undefined') return this.renderPlainText(content);

      const renderer = new marked.Renderer();
      const self = this;
      renderer.link = function(href, title, text) {
        const h = typeof href === 'object' ? href.href : href;
        const t = typeof href === 'object' ? href.title : title;
        const tx = typeof href === 'object' ? href.text : text;
        const titleAttr = t ? ` title="${self.escapeHtml(t)}"` : '';
        return `<a href="${self.escapeHtml(h)}" target="_blank" rel="noopener noreferrer"${titleAttr}>${tx}</a>`;
      };
      renderer.code = function(code, language) {
        const text = typeof code === 'object' ? code.text : code;
        const lang = typeof code === 'object' ? code.lang : language;
        if (typeof hljs !== 'undefined' && lang) {
          try {
            return '<pre><code class="hljs">' + hljs.highlight(text, { language: lang, ignoreIllegals: true }).value + '</code></pre>';
          } catch (e) {}
        }
        return '<pre><code>' + self.escapeHtml(text) + '</code></pre>';
      };

      const html = marked.parse(content, { renderer, breaks: true });
      return '<div class="markdown-content">' + this.sanitizeHtml(html) + '</div>';
    },

    renderJSON(content) {
      if (!content) return '<div class="json-tree"></div>';

      let parsed;
      try {
        parsed = JSON.parse(content);
      } catch (e) {
        return this.renderCode(content, 'json');
      }

      const topLevelCount = Array.isArray(parsed) ? parsed.length :
        (typeof parsed === 'object' && parsed !== null) ? Object.keys(parsed).length : 0;
      const defaultExpanded = topLevelCount < 100;

      return '<div class="json-tree">' + this.buildJSONTree(parsed, null, 0, defaultExpanded) + '</div>';
    },

    buildJSONTree(value, key, depth, expanded) {
      const esc = (s) => this.escapeHtml(String(s));
      const keyPrefix = key !== null ? '<span class="json-key">' + esc(key) + '</span>: ' : '';
      const indent = depth * 16;

      if (value === null) {
        return '<div class="json-line" style="padding-left:' + indent + 'px">' + keyPrefix + '<span class="json-null">null</span></div>';
      }
      if (typeof value === 'boolean') {
        return '<div class="json-line" style="padding-left:' + indent + 'px">' + keyPrefix + '<span class="json-boolean">' + value + '</span></div>';
      }
      if (typeof value === 'number') {
        return '<div class="json-line" style="padding-left:' + indent + 'px">' + keyPrefix + '<span class="json-number">' + value + '</span></div>';
      }
      if (typeof value === 'string') {
        return '<div class="json-line" style="padding-left:' + indent + 'px">' + keyPrefix + '<span class="json-string">"' + esc(value) + '"</span></div>';
      }

      const isArray = Array.isArray(value);
      const entries = isArray ? value.map((v, i) => [i, v]) : Object.entries(value);
      const open = isArray ? '[' : '{';
      const close = isArray ? ']' : '}';
      const shouldExpand = expanded && depth < 5;
      const id = 'jt-' + Math.random().toString(36).substr(2, 8);

      let html = '<div class="json-line" style="padding-left:' + indent + 'px">';
      html += '<span class="json-toggle" onclick="var el=document.getElementById(\'' + id + '\');var t=this;if(el.style.display===\'none\'){el.style.display=\'block\';t.textContent=\'▼\'}else{el.style.display=\'none\';t.textContent=\'▶\'}">' + (shouldExpand ? '▼' : '▶') + '</span> ';
      html += keyPrefix + '<span class="json-bracket">' + open + '</span>';
      html += ' <span class="json-count">' + entries.length + (isArray ? ' items' : ' keys') + '</span>';
      html += '</div>';

      html += '<div id="' + id + '" style="display:' + (shouldExpand ? 'block' : 'none') + '">';
      for (const [k, v] of entries) {
        html += this.buildJSONTree(v, isArray ? null : k, depth + 1, shouldExpand);
      }
      html += '<div class="json-line" style="padding-left:' + indent + 'px"><span class="json-bracket">' + close + '</span></div>';
      html += '</div>';

      return html;
    },

    renderCSV(content, delimiter) {
      if (!content) return '<div class="csv-table-wrapper"></div>';

      const lines = content.split('\n').filter(l => l.trim());
      if (lines.length === 0) return this.renderPlainText(content);

      const parseLine = (line, delim) => {
        const fields = [];
        let current = '';
        let inQuotes = false;
        for (let i = 0; i < line.length; i++) {
          const ch = line[i];
          if (inQuotes) {
            if (ch === '"' && line[i + 1] === '"') {
              current += '"';
              i++;
            } else if (ch === '"') {
              inQuotes = false;
            } else {
              current += ch;
            }
          } else {
            if (ch === '"') {
              inQuotes = true;
            } else if (ch === delim) {
              fields.push(current);
              current = '';
            } else {
              current += ch;
            }
          }
        }
        fields.push(current);
        return fields;
      };

      const rows = lines.map(l => parseLine(l, delimiter));

      if (rows.length > 1) {
        const headerLen = rows[0].length;
        const badRows = rows.filter(r => Math.abs(r.length - headerLen) / headerLen > 0.2).length;
        if (badRows > rows.length * 0.2) {
          return this.renderCode(content, delimiter === '\t' ? 'tsv' : 'csv');
        }
      }

      const esc = (s) => this.escapeHtml(s);
      let html = '<div class="csv-table-wrapper"><table class="csv-table">';

      html += '<thead><tr>';
      for (const cell of rows[0]) {
        html += '<th>' + esc(cell) + '</th>';
      }
      html += '</tr></thead>';

      html += '<tbody>';
      for (let i = 1; i < rows.length; i++) {
        html += '<tr>';
        for (const cell of rows[i]) {
          html += '<td>' + esc(cell) + '</td>';
        }
        html += '</tr>';
      }
      html += '</tbody></table></div>';

      return html;
    },

    renderImage(content, filePath) {
      if (!content) return '<div class="image-preview">No image data</div>';

      const ext = filePath.split('.').pop().toLowerCase();
      const mimeMap = {
        'png': 'image/png', 'jpg': 'image/jpeg', 'jpeg': 'image/jpeg',
        'gif': 'image/gif', 'svg': 'image/svg+xml', 'webp': 'image/webp',
        'ico': 'image/x-icon', 'bmp': 'image/bmp'
      };
      const mime = mimeMap[ext] || 'image/png';
      const fileName = filePath.split('/').pop();

      let src;
      if (ext === 'svg') {
        src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(content);
      } else {
        src = 'data:' + mime + ';base64,' + content;
      }

      return '<div class="image-preview">' +
        '<img src="' + src + '" alt="' + this.escapeHtml(fileName) + '">' +
        '<div class="image-filename">' + this.escapeHtml(fileName) + '</div>' +
        '</div>';
    },

    renderHTMLPreview(content) {
      if (!content) return '<div class="html-preview"></div>';

      const srcdocContent = content.replace(/&/g, '&amp;').replace(/"/g, '&quot;');

      return '<div class="html-preview">' +
        '<div class="html-preview-tabs">' +
          '<button class="html-tab active" onclick="this.classList.add(\'active\');this.nextElementSibling.classList.remove(\'active\');this.parentElement.nextElementSibling.style.display=\'block\';this.parentElement.nextElementSibling.nextElementSibling.style.display=\'none\'">Preview</button>' +
          '<button class="html-tab" onclick="this.classList.add(\'active\');this.previousElementSibling.classList.remove(\'active\');this.parentElement.nextElementSibling.style.display=\'none\';this.parentElement.nextElementSibling.nextElementSibling.style.display=\'block\'">Source</button>' +
        '</div>' +
        '<iframe class="html-preview-frame" srcdoc="' + srcdocContent + '" sandbox=""></iframe>' +
        '<div class="html-source" style="display:none">' + this.renderCode(content, 'html') + '</div>' +
        '</div>';
    },
  }));
});
