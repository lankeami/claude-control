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
    editingSessionName: null,
    editingNameValue: '',
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

    // Browser notifications
    prevActivityStates: {},

    // Managed session state
    showNewSessionModal: false,
    pendingPermission: null,
    newSessionCWD: '',
    sessionSSE: null,

    // Directory browser state
    browsePath: '',
    browseEntries: [],
    browseLoading: false,
    browseFilter: '',
    browseConfirmed: false,
    newProjectName: '',
    newProjectError: '',
    newProjectCreating: false,
    recentDirs: [],

    // Resume picker state
    showResumePicker: false,
    resumableSessions: [],
    resumeLoading: false,

    // File browser state
    sessionFiles: [],
    fileTreeData: [],
    gitInfo: null,

    // GitHub Issues state
    githubRepo: null,
    githubIssues: [],
    githubIssuesState: 'open',
    githubIssuesSearch: '',
    githubIssuesLimit: 10,
    githubIssuesHasMore: false,
    githubIssuesLoading: false,
    githubIssuesError: null,
    issuesExpanded: true,
    selectedIssue: null,
    selectedIssueLoading: false,
    _searchIssuesTimer: null,
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

    // Scheduled tasks state
    scheduledTasks: [],
    selectedTask: null,
    taskRuns: [],
    taskModalOpen: false,
    editingTask: null,
    taskForm: { name: '', task_type: 'shell', command: '', working_directory: '', cron_expression: '', session_id: '' },
    taskFormErrors: '',
    taskLoading: false,
    taskRunsLoading: false,
    tasksExpanded: false,

    // Toast
    showToast: false,
    toastMessage: '',
    toastTimer: null,
    toastType: 'info',

    // Settings state
    showSettingsModal: false,
    settingsFirstRun: false,
    settingsForm: { port: '', ngrok_authtoken: '', claude_bin: '', claude_args: '', claude_env: '', compact_every_n_continues: '', github_token: '' },
    settingsError: '',
    settingsSaving: false,
    settingsRestartRequired: false,

    // Usage tracking
    lastTurnThreshold: 0,
    lastThresholdSessionId: null,
    continuationCount: 0,
    isCompacting: false,

    // Shell mode
    shellMode: false,
    activeShellId: null,

    // Slash commands
    slashCommands: [],
    slashCommandsLoaded: false,
    showSlashMenu: false,
    slashFilter: '',
    slashSelectedIndex: 0,
    sessionCost: null,

    // Activity Status Pills
    stalenessTimer: null,
    heartbeatTimer: null,
    lastEventTime: null,
    currentPillStart: null,

    // Sidebar collapse/resize state
    leftCollapsed: false,
    rightCollapsed: false,
    leftWidth: null,
    rightWidth: null,
    viewerWidth: null,
    _resizing: null,
    _resizeStartX: 0,
    _resizeStartWidth: 0,

    // Mobile menu state
    mobileMenuOpen: false,
    mobileTab: 'sessions',
    mobileOverlay: null, // null | 'file' | 'issue'

    isMobile() {
      return window.innerWidth <= 768;
    },

    startResize(e, side) {
      e.preventDefault();
      const handle = e.target.closest('.resize-handle');
      if (handle) handle.classList.add('active');
      this._resizing = side;
      this._resizeStartX = e.clientX;
      if (side === 'left') {
        const sidebar = document.querySelector('.sidebar');
        this._resizeStartWidth = sidebar.offsetWidth;
      } else if (side === 'right') {
        const sidebar = document.querySelector('.file-tree-sidebar');
        this._resizeStartWidth = sidebar.offsetWidth;
      } else if (side === 'viewer') {
        const viewer = document.querySelector('.file-viewer-column');
        this._resizeStartWidth = viewer.offsetWidth;
      }
      const onMove = (ev) => {
        if (!this._resizing) return;
        const delta = ev.clientX - this._resizeStartX;
        let newWidth;
        if (this._resizing === 'left') {
          newWidth = this._resizeStartWidth + delta;
        } else {
          newWidth = this._resizeStartWidth - delta;
        }
        newWidth = Math.max(200, Math.min(900, newWidth));
        if (this._resizing === 'left') {
          this.leftWidth = newWidth;
        } else if (this._resizing === 'right') {
          this.rightWidth = newWidth;
        } else if (this._resizing === 'viewer') {
          this.viewerWidth = newWidth;
        }
      };
      const onUp = () => {
        document.querySelectorAll('.resize-handle.active').forEach(h => h.classList.remove('active'));
        this._resizing = null;
        document.removeEventListener('mousemove', onMove);
        document.removeEventListener('mouseup', onUp);
      };
      document.addEventListener('mousemove', onMove);
      document.addEventListener('mouseup', onUp);
    },

    async init() {
      if (this.apiKey) {
        await this.tryConnect(this.apiKey);
        await this.loadScheduledTasks();
        await this.checkSettingsFirstRun();
      }
      this.$watch('mobileMenuOpen', (open) => {
        document.body.style.overflow = open ? 'hidden' : '';
      });
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
          // Check turn count thresholds for toast warnings
          if (this.selectedSessionId) {
            const sess = (data.sessions || []).find(s => s.id === this.selectedSessionId);
            if (sess && sess.mode === 'managed' && sess.max_turns > 0) {
              const pct = (sess.turn_count / sess.max_turns) * 100;
              // Reset threshold tracker on session change or turn reset
              if (this.lastThresholdSessionId !== sess.id) {
                this.lastTurnThreshold = 0;
                this.lastThresholdSessionId = sess.id;
              }
              if (sess.turn_count === 0 && this.lastTurnThreshold > 0) {
                this.lastTurnThreshold = 0;
              }
              // Fire toasts at threshold crossings
              if (pct >= 100 && this.lastTurnThreshold < 100) {
                this.toast(`Turn limit reached (${sess.turn_count}/${sess.max_turns}) — auto-continuing if enabled`, 8000, 'info');
                this.lastTurnThreshold = 100;
              } else if (pct >= 90 && this.lastTurnThreshold < 90) {
                this.toast(`Turn limit critical (${sess.turn_count}/${sess.max_turns}) \u2014 session will be interrupted soon`, 6000, 'error');
                this.lastTurnThreshold = 90;
              } else if (pct >= 80 && this.lastTurnThreshold < 80) {
                this.toast(`Turn limit warning (${sess.turn_count}/${sess.max_turns}) \u2014 approaching session limit`, 6000, 'warning');
                this.lastTurnThreshold = 80;
              }
            }
          }
          this.connected = true;
          this.sseFailCount = 0;
          this.checkActivityStateNotifications(data.sessions || []);
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
        const sessions = await sessResp.json();
        this.sessions = sessions;
        this.prompts = await promptResp.json();
        this.connected = true;
        this.loadScheduledTasks();
        this.checkActivityStateNotifications(sessions);
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

    toast(msg, duration = 4000, type = 'info') {
      this.toastMessage = msg;
      this.toastType = type;
      this.showToast = true;
      if (this.toastTimer) clearTimeout(this.toastTimer);
      this.toastTimer = setTimeout(() => { this.showToast = false; }, duration);
    },

    async checkSettingsFirstRun() {
      try {
        const res = await fetch('/api/settings/exists', {
          headers: { 'Authorization': 'Bearer ' + this.apiKey }
        });
        if (!res.ok) return;
        const data = await res.json();
        if (!data.exists) {
          this.settingsFirstRun = true;
          this.showSettingsModal = true;
        }
      } catch (e) {}
    },

    async openSettingsModal() {
      this.settingsError = '';
      this.settingsFirstRun = false;
      try {
        const res = await fetch('/api/settings', {
          headers: { 'Authorization': 'Bearer ' + this.apiKey }
        });
        if (!res.ok) throw new Error(await res.text());
        const data = await res.json();
        this.settingsForm = {
          port: data.port || '',
          ngrok_authtoken: data.ngrok_authtoken || '',
          claude_bin: data.claude_bin || '',
          claude_args: data.claude_args || '',
          claude_env: data.claude_env || '',
          compact_every_n_continues: data.compact_every_n_continues || '',
          github_token: data.github_token || '',
        };
      } catch (e) {
        this.settingsForm = { port: '', ngrok_authtoken: '', claude_bin: '', claude_args: '', claude_env: '', compact_every_n_continues: '', github_token: '' };
      }
      this.showSettingsModal = true;
    },

    async saveSettings() {
      this.settingsError = '';
      this.settingsSaving = true;
      try {
        const res = await fetch('/api/settings', {
          method: 'PUT',
          headers: { 'Authorization': 'Bearer ' + this.apiKey, 'Content-Type': 'application/json' },
          body: JSON.stringify(this.settingsForm)
        });
        if (!res.ok) {
          const errText = await res.text();
          this.settingsError = errText;
          this.settingsSaving = false;
          return;
        }
        const data = await res.json();
        if (data.restart_required) {
          this.settingsRestartRequired = true;
        }
        this.showSettingsModal = false;
        this.settingsFirstRun = false;
        this.toast('Settings saved');
      } catch (e) {
        this.settingsError = 'Error: ' + e.message;
      }
      this.settingsSaving = false;
    },

    // Browser notifications
    checkActivityStateNotifications(sessions) {
      if (!('Notification' in window)) return;
      for (const session of sessions) {
        if (session.mode !== 'managed') continue;
        const prev = this.prevActivityStates[session.id];
        const curr = session.activity_state;
        if (prev === 'working' && curr === 'waiting' && session.id !== this.selectedSessionId) {
          this.sendBrowserNotification(session);
        }
        this.prevActivityStates[session.id] = curr;
      }
    },

    async sendBrowserNotification(session) {
      if (Notification.permission !== 'granted') return;
      const sessionName = this.sessionName(session);
      let eventName = 'Claude is ready for your input';
      try {
        const res = await fetch(`/api/sessions/${session.id}/messages`, {
          headers: { 'Authorization': 'Bearer ' + this.apiKey }
        });
        if (res.ok) {
          const msgs = await res.json();
          const lastAssistant = [...(msgs || [])].reverse().find(m => m.role === 'assistant');
          if (lastAssistant && lastAssistant.content) {
            let text = lastAssistant.content;
            if (text.length > 120) text = text.substring(0, 117) + '...';
            eventName = text;
          }
        }
      } catch (e) {}
      const notification = new Notification('Claude Control', {
        body: `${sessionName}\n${eventName}`,
        icon: '/static/logo-bg.png',
        tag: session.id
      });
      notification.onclick = () => {
        window.focus();
        this.selectSession(session.id);
        notification.close();
      };
      setTimeout(() => notification.close(), 10000);
    },

    requestNotificationPermission() {
      if (!('Notification' in window)) return;
      if (Notification.permission === 'default') {
        Notification.requestPermission();
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

    get selectedSession() {
      return this.sessions.find(s => s.id === this.selectedSessionId);
    },

    get turnPercent() {
      const sess = this.selectedSession;
      if (!sess || sess.mode !== 'managed' || !sess.max_turns) return 0;
      return Math.min(100, Math.round((sess.turn_count / sess.max_turns) * 100));
    },

    turnBarColor() {
      const pct = this.turnPercent;
      if (pct >= 90) return '#ef4444';
      if (pct >= 80) return '#f59e0b';
      return 'var(--accent)';
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
      if (session.name) return session.name;
      if (session.mode === 'managed' && session.cwd) {
        const parts = session.cwd.split('/');
        return parts[parts.length - 1] || parts[parts.length - 2] || session.cwd;
      }
      const parts = session.project_path.split('/');
      const proj = parts[parts.length - 1] || parts[parts.length - 2] || session.project_path;
      return `${session.computer_name} / ${proj}`;
    },

    startRenameSession(sessionId) {
      const session = this.sessions.find(s => s.id === sessionId);
      this.editingSessionName = sessionId;
      this.editingNameValue = session?.name || '';
      this._renameStartedAt = Date.now();
    },

    async saveSessionName(sessionId) {
      if (this.editingSessionName !== sessionId) return;
      if (this._renameStartedAt && Date.now() - this._renameStartedAt < 300) return;
      const name = this.editingNameValue.trim();
      try {
        const resp = await fetch(`/api/sessions/${sessionId}/name`, {
          method: 'PUT',
          headers: { 'Authorization': `Bearer ${this.apiKey}`, 'Content-Type': 'application/json' },
          body: JSON.stringify({ name })
        });
        if (resp.ok) {
          const updated = await resp.json();
          const idx = this.sessions.findIndex(s => s.id === sessionId);
          if (idx >= 0) this.sessions[idx] = updated;
        }
      } catch (e) {
        console.error('rename failed:', e);
      }
      this.editingSessionName = null;
    },

    cancelRenameSession() {
      this.editingSessionName = null;
    },

    sessionStatus(session) {
      if (session.mode === 'managed') {
        const state = session.activity_state || 'idle';
        if (state === 'working') return 'active';
        if (state === 'waiting') return 'waiting';
        if (state === 'input_needed') return 'input_needed';
        return 'idle';
      }
      const lastSeen = new Date(session.last_seen_at);
      const fiveMinAgo = new Date(Date.now() - 5 * 60 * 1000);
      if (lastSeen < fiveMinAgo) return 'idle';
      return session.status;
    },

    async selectSession(id) {
      this.requestNotificationPermission();
      this.mobileMenuOpen = false;
      this.mobileOverlay = null;
      if (this.selectedSessionId === id) return;
      this.selectedSessionId = id;
      this.stopSessionSSE();
      this.clearActivityPills();
      this.closeFileViewer();
      this.slashCommands = [];
      this.slashCommandsLoaded = false;
      this.showSlashMenu = false;
      this.sessionCost = null;
      this.continuationCount = 0;
      this.isCompacting = false;
      this.sessionFiles = [];
      this.fileTreeData = [];
      this.fileContentCache = {};
      this.renderedContentCache = {};

      const sess = this.sessions.find(s => s.id === this.selectedSessionId);
      if (sess && sess.mode === 'managed') {
        await this.fetchManagedMessages(this.selectedSessionId);
        if (sess.activity_state === 'working') {
          this.startSessionSSE(this.selectedSessionId);
        }
      } else {
        await this.fetchTranscript(this.selectedSessionId, true);
      }
      this.loadSessionFiles(this.selectedSessionId);
      if (sess && sess.mode === 'managed') {
        this.githubIssues = [];
        this.githubIssuesState = 'open';
        this.githubIssuesSearch = '';
        this.githubIssuesLimit = 10;
        this.selectedIssue = null;
        this.githubIssuesError = null;
        this.fetchGithubIssues(this.selectedSessionId);
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

    // --- Slash commands ---

    get filteredSlashCommands() {
      if (!this.showSlashMenu) return [];
      const filter = this.slashFilter.toLowerCase();
      return this.slashCommands.filter(cmd => cmd.name.toLowerCase().startsWith('/' + filter));
    },

    async loadSlashCommands() {
      if (this.slashCommandsLoaded || !this.selectedSessionId) return;
      try {
        const resp = await fetch(`/api/sessions/${this.selectedSessionId}/commands`, {
          headers: { 'Authorization': `Bearer ${this.apiKey}` }
        });
        if (resp.ok) this.slashCommands = await resp.json();
      } catch (e) {}
      this.slashCommandsLoaded = true;
    },

    onSlashInput() {
      const text = this.inputText;
      if (text.startsWith('/') && !this.shellMode && this.currentSession?.mode === 'managed') {
        this.slashFilter = text.slice(1).split(' ')[0];
        if (!text.includes(' ')) {
          this.showSlashMenu = true;
          this.slashSelectedIndex = 0;
          this.loadSlashCommands();
        } else {
          this.showSlashMenu = false;
        }
      } else {
        this.showSlashMenu = false;
      }
    },

    handleSlashKeydown(e) {
      if (this.showSlashMenu && this.filteredSlashCommands.length > 0) {
        if (e.key === 'ArrowDown') { e.preventDefault(); this.slashSelectedIndex = Math.min(this.slashSelectedIndex + 1, this.filteredSlashCommands.length - 1); return; }
        if (e.key === 'ArrowUp') { e.preventDefault(); this.slashSelectedIndex = Math.max(this.slashSelectedIndex - 1, 0); return; }
        if (e.key === 'Enter' && !e.shiftKey && !e.metaKey && !e.ctrlKey) { e.preventDefault(); this.selectSlashCommand(this.filteredSlashCommands[this.slashSelectedIndex]); return; }
        if (e.key === 'Escape') { e.preventDefault(); this.showSlashMenu = false; return; }
        if (e.key === 'Tab') { e.preventDefault(); this.selectSlashCommand(this.filteredSlashCommands[this.slashSelectedIndex]); return; }
      }
      if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') { e.preventDefault(); this.handleInput(); }
    },

    selectSlashCommand(cmd) {
      if (!cmd) return;
      this.showSlashMenu = false;
      this.inputText = cmd.hasArg ? cmd.name + ' ' : cmd.name;
    },

    async executeSlashCommand(input) {
      const spaceIdx = input.indexOf(' ');
      const cmdName = (spaceIdx > 0 ? input.substring(0, spaceIdx) : input).toLowerCase();
      const cmdArg = spaceIdx > 0 ? input.substring(spaceIdx + 1).trim() : '';

      this.chatMessages.push({ role: 'user', content: input, msg_type: 'text', timestamp: new Date().toISOString() });
      this.$nextTick(() => this.scrollToBottom(true));
      this.inputText = '';

      switch (cmdName) {
        case '/resume':
          await this.openResumePicker();
          break;
        case '/clear':
          this.chatMessages = [];
          break;
        case '/compact': {
          const compactInstr = cmdArg || 'Please compact the conversation context and summarize what we\'ve been working on.';
          this.inputText = compactInstr;
          await this.sendManagedMessage();
          break;
        }
        case '/config': {
          const sess = this.currentSession;
          const lines = [
            `**Session ID:** ${sess?.id || 'unknown'}`,
            `**Mode:** ${sess?.mode || 'unknown'}`,
            `**CWD:** ${sess?.cwd || 'not set'}`,
            `**Allowed Tools:** ${sess?.allowed_tools || 'default'}`,
            `**Max Turns:** ${sess?.max_turns || 'unlimited'}`,
            `**Max Budget:** ${sess?.max_budget_usd ? '$' + sess.max_budget_usd.toFixed(2) : 'unlimited'}`,
            `**Auto-continue:** ${sess?.auto_continue_threshold || 'off'}`,
            `**Activity:** ${sess?.activity_state || 'unknown'}`,
          ];
          this.chatMessages.push({ role: 'system', command: '/config', content: lines.join('\n'), msg_type: 'text', timestamp: new Date().toISOString() });
          this.$nextTick(() => this.scrollToBottom(true));
          break;
        }
        case '/context':
          this.inputText = 'List all context files currently loaded (CLAUDE.md files, .claude/settings.json, etc). Show their paths and a brief summary of what each contains.';
          await this.sendManagedMessage();
          break;
        case '/cost': {
          const sess = this.currentSession;
          const cost = this.sessionCost != null ? `$${this.sessionCost.toFixed(4)}` : 'no usage yet';
          const budget = sess?.max_budget_usd ? `$${sess.max_budget_usd.toFixed(2)}` : 'unlimited';
          this.chatMessages.push({ role: 'system', command: '/cost', content: `Session cost: ${cost} | Budget: ${budget}`, msg_type: 'text', timestamp: new Date().toISOString() });
          this.$nextTick(() => this.scrollToBottom(true));
          break;
        }
        case '/diff':
          this.inputText = 'Run `git diff` and `git diff --staged` in the working directory and show me the uncommitted changes. Be concise.';
          await this.sendManagedMessage();
          break;
        case '/doctor':
          this.inputText = 'Check the health of the current environment: verify git is available, check the working directory exists, confirm Claude Code version, and report any issues.';
          await this.sendManagedMessage();
          break;
        case '/effort': {
          const validLevels = ['low', 'medium', 'high', 'max'];
          if (!cmdArg || !validLevels.includes(cmdArg.toLowerCase())) {
            this.chatMessages.push({ role: 'system', command: '/effort', content: `Usage: /effort <${validLevels.join('|')}>\nSets reasoning effort level for subsequent messages.`, msg_type: 'text', timestamp: new Date().toISOString() });
          } else {
            this.inputText = `Set your reasoning effort to "${cmdArg.toLowerCase()}" for the rest of this session. Acknowledge briefly.`;
            await this.sendManagedMessage();
          }
          this.$nextTick(() => this.scrollToBottom(true));
          break;
        }
        case '/export': {
          const exportLines = this.chatMessages
            .filter(m => m.role !== 'system')
            .map(m => `## ${m.role === 'user' ? 'User' : 'Assistant'}\n\n${m.content}`);
          const markdown = exportLines.join('\n\n---\n\n');
          const blob = new Blob([markdown], { type: 'text/markdown' });
          const url = URL.createObjectURL(blob);
          const a = document.createElement('a');
          a.href = url;
          a.download = `conversation-${this.selectedSessionId?.substring(0, 8) || 'export'}-${new Date().toISOString().split('T')[0]}.md`;
          a.click();
          URL.revokeObjectURL(url);
          this.chatMessages.push({ role: 'system', command: '/export', content: 'Conversation exported as markdown.', msg_type: 'text', timestamp: new Date().toISOString() });
          this.$nextTick(() => this.scrollToBottom(true));
          break;
        }
        case '/help': {
          await this.loadSlashCommands();
          const helpLines = this.slashCommands.map(c => {
            const hint = c.argHint ? ' ' + c.argHint : '';
            const src = c.source !== 'builtin' ? ` [${c.source}]` : '';
            return `**${c.name}**${hint}${src} — ${c.description || ''}`;
          });
          this.chatMessages.push({ role: 'system', command: '/help', content: 'Available commands:\n\n' + helpLines.join('\n'), msg_type: 'text', timestamp: new Date().toISOString() });
          this.$nextTick(() => this.scrollToBottom(true));
          break;
        }
        case '/init':
          this.inputText = 'Initialize a CLAUDE.md file for this project. Analyze the codebase structure, build system, test commands, and key patterns, then create a comprehensive CLAUDE.md.';
          await this.sendManagedMessage();
          break;
        case '/model': {
          if (!cmdArg) {
            this.chatMessages.push({ role: 'system', command: '/model', content: 'Usage: /model <model>\nExamples: /model sonnet, /model opus, /model haiku', msg_type: 'text', timestamp: new Date().toISOString() });
          } else {
            this.chatMessages.push({ role: 'system', command: '/model', content: `Model switching requires restarting the session with --model ${cmdArg}. This is not yet supported mid-session.`, msg_type: 'text', timestamp: new Date().toISOString() });
          }
          this.$nextTick(() => this.scrollToBottom(true));
          break;
        }
        case '/add-dir': {
          if (!cmdArg) {
            this.chatMessages.push({ role: 'system', command: '/add-dir', content: 'Usage: /add-dir <path>\nAdds a directory to the session\'s allowed tool access paths.', msg_type: 'text', timestamp: new Date().toISOString() });
          } else {
            this.chatMessages.push({ role: 'system', command: '/add-dir', content: `Adding directories mid-session requires --add-dir at startup. This is not yet supported mid-session.`, msg_type: 'text', timestamp: new Date().toISOString() });
          }
          this.$nextTick(() => this.scrollToBottom(true));
          break;
        }
        case '/pr-comments':
          this.inputText = 'Check for pull request comments on the current branch. Use `gh pr view` and `gh pr comments` to show any review feedback.';
          await this.sendManagedMessage();
          break;
        case '/review':
          this.inputText = 'Review the recent code changes. Run `git diff HEAD~1` (or uncommitted changes if any) and provide a code review with suggestions for improvement.';
          await this.sendManagedMessage();
          break;
        case '/status': {
          const statusSess = this.currentSession;
          const lines = [
            `**Session:** ${statusSess?.name || statusSess?.id?.substring(0, 8) || 'unknown'}`,
            `**Activity:** ${statusSess?.activity_state || 'unknown'}`,
            `**Turns:** ${statusSess?.turn_count || 0}`,
            `**Cost:** ${this.sessionCost != null ? '$' + this.sessionCost.toFixed(4) : 'no usage yet'}`,
            `**CWD:** ${statusSess?.cwd || 'not set'}`,
          ];
          this.chatMessages.push({ role: 'system', command: '/status', content: lines.join('\n'), msg_type: 'text', timestamp: new Date().toISOString() });
          this.$nextTick(() => this.scrollToBottom(true));
          break;
        }
        default:
          await this.executeCustomCommand(input.substring(0, spaceIdx > 0 ? spaceIdx : input.length), cmdArg);
          break;
      }
    },

    async executeCustomCommand(cmdName, cmdArg) {
      try {
        const resp = await fetch(`/api/sessions/${this.selectedSessionId}/commands/content?name=${encodeURIComponent(cmdName)}`, {
          headers: { 'Authorization': `Bearer ${this.apiKey}` }
        });
        if (!resp.ok) {
          this.chatMessages.push({ role: 'system', content: `Unknown command: ${cmdName}`, msg_type: 'text', timestamp: new Date().toISOString() });
          this.$nextTick(() => this.scrollToBottom(true));
          return;
        }
        const data = await resp.json();
        let prompt = data.content;
        if (cmdArg) {
          prompt = prompt.includes('$ARGUMENTS') ? prompt.replace(/\$ARGUMENTS/g, cmdArg) : prompt + '\n\n' + cmdArg;
        }
        this.inputText = prompt;
        await this.sendManagedMessage();
      } catch (e) {
        this.chatMessages.push({ role: 'system', content: `Failed to execute ${cmdName}: ${e.message}`, msg_type: 'text', timestamp: new Date().toISOString() });
      }
    },

    // --- End slash commands ---

    async handleInput() {
      if (!this.selectedSessionId || !this.inputText.trim()) return;
      this.showSlashMenu = false;
      const sess = this.currentSession;

      if (sess && sess.mode === 'managed' && this.inputText.trim().startsWith('/')) {
        await this.executeSlashCommand(this.inputText.trim());
        return;
      }

      if (sess && sess.mode === 'managed') {
        if (this.shellMode) {
          await this.executeShell();
        } else {
          await this.sendManagedMessage();
        }
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
      this.browseFilter = '';
      this.browseConfirmed = false;
      this.newProjectName = '';
      this.newProjectError = '';
      this.newProjectCreating = false;
      // Fetch recent directories
      try {
        const res = await fetch('/api/sessions/recent-dirs', {
          headers: { 'Authorization': 'Bearer ' + this.apiKey }
        });
        if (res.ok) {
          const data = await res.json();
          this.recentDirs = data.directories || [];
        }
      } catch (e) {
        this.recentDirs = [];
      }
      await this.browseTo('');
    },

    async browseTo(path) {
      this.browseLoading = true;
      this.browseFilter = '';
      this.browseConfirmed = false;
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

    abbreviatePath(fullPath) {
      const parts = fullPath.split('/');
      // Assume home dir is first 3 parts: /Users/username or /home/username
      const home = parts.slice(0, 3).join('/');
      if (fullPath.startsWith(home + '/')) {
        const rest = fullPath.slice(home.length);
        // Get parent dir, not the dir itself
        const lastSlash = rest.lastIndexOf('/');
        return lastSlash > 0 ? '~' + rest.slice(0, lastSlash) : '~';
      }
      const lastSlash = fullPath.lastIndexOf('/');
      return lastSlash > 0 ? fullPath.slice(0, lastSlash) : fullPath;
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

    get filteredBrowseEntries() {
      if (!this.browseFilter) return this.browseEntries;
      const q = this.browseFilter.toLowerCase();
      return this.browseEntries.filter(e => e.name.toLowerCase().includes(q));
    },

    get isValidNewProjectName() {
      const name = this.newProjectName.trim();
      if (!name) return false;
      return /^[a-zA-Z0-9]([a-zA-Z0-9._-]{0,253}[a-zA-Z0-9])?$/.test(name);
    },

    handleBrowseInputKeydown(event) {
      if (event.key !== 'Enter') return;
      event.preventDefault();
      const val = this.newSessionCWD.trim();
      if (!val) return;

      // If the input differs from the browsed path, browse to it first
      if (val !== this.browsePath) {
        this.browseConfirmed = false;
        this.browseTo(val);
        return;
      }

      // Same path as browsed — if already confirmed, create the session
      if (this.browseConfirmed) {
        this.createManagedSession();
      } else {
        this.browseConfirmed = true;
      }
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

    async selectRecentDir(path) {
      this.newSessionCWD = path;
      try {
        const res = await fetch('/api/sessions/create', {
          method: 'POST',
          headers: { 'Authorization': 'Bearer ' + this.apiKey, 'Content-Type': 'application/json' },
          body: JSON.stringify({ cwd: path })
        });
        if (res.status === 409) {
          // Session already exists — find and select it
          const match = this.sessions.find(s => s.cwd === path);
          if (match) {
            this.showNewSessionModal = false;
            this.selectSession(match.id);
            this.toast('Switched to existing session');
            return;
          }
        }
        if (!res.ok) throw new Error(await res.text());
        const sess = await res.json();
        this.showNewSessionModal = false;
        this.selectSession(sess.id);
        this.toast('Session created');
      } catch (e) {
        this.toast('Error: ' + e.message);
      }
    },

    async createNewProject() {
      if (!this.isValidNewProjectName || this.newProjectCreating) return;
      this.newProjectCreating = true;
      this.newProjectError = '';
      try {
        const res = await fetch('/api/sessions/create-project', {
          method: 'POST',
          headers: { 'Authorization': 'Bearer ' + this.apiKey, 'Content-Type': 'application/json' },
          body: JSON.stringify({ parent_path: this.browsePath, name: this.newProjectName.trim() })
        });
        if (!res.ok) {
          const errText = await res.text();
          if (res.status === 409) {
            this.newProjectError = errText.includes('directory already exists')
              ? 'Directory already exists. Select it from the list above.'
              : 'A session already exists for this directory.';
          } else if (res.status === 400) {
            this.newProjectError = 'Invalid name. Use letters, numbers, hyphens, dots, or underscores.';
          } else {
            this.newProjectError = 'Failed to create project. Please try again.';
          }
          return;
        }
        const sess = await res.json();
        this.showNewSessionModal = false;
        this.newProjectName = '';
        this.newProjectError = '';
        this.toast('Project created');
        await this.loadSessions();
        this.selectedSessionId = sess.id;
      } catch (e) {
        this.newProjectError = 'Error: ' + e.message;
      } finally {
        this.newProjectCreating = false;
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

    async executeShell() {
      if (!this.inputText.trim() || !this.selectedSessionId) return;
      const cmd = this.inputText.trim();
      this.inputText = '';
      this.inputSending = true;

      try {
        const res = await fetch(`/api/sessions/${this.selectedSessionId}/shell`, {
          method: 'POST',
          headers: { 'Authorization': 'Bearer ' + this.apiKey, 'Content-Type': 'application/json' },
          body: JSON.stringify({ command: cmd, timeout: 30 })
        });
        if (!res.ok) throw new Error(await res.text());

        const data = await res.json();
        this.activeShellId = data.id;

        this.chatMessages.push({
          role: 'shell',
          content: cmd,
          shellId: data.id,
          cwd: this.currentSession?.cwd || '',
          timestamp: new Date().toISOString()
        });

        this.chatMessages.push({
          role: 'shell_output',
          stdout: '',
          stderr: '',
          exitCode: null,
          timedOut: false,
          shellId: data.id,
          complete: false,
          timestamp: new Date().toISOString()
        });

        this.$nextTick(() => this.scrollToBottom(true));
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

      // Check for pending permission on reconnect
      fetch(`/api/sessions/${sessionId}/pending-permission`, {
        headers: { 'Authorization': `Bearer ${this.apiKey}` }
      }).then(r => r.json()).then(data => {
        if (data.pending) {
          this.pendingPermission = data;
        }
      }).catch(() => {});

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

          // Shell events
          if (data.type === 'shell_start') {
            this.activeShellId = data.id;
            return;
          }
          if (data.type === 'shell_output') {
            const outputMsg = this.chatMessages.find(m => m.role === 'shell_output' && m.shellId === data.id);
            if (outputMsg) {
              if (data.stream === 'stderr') {
                outputMsg.stderr += data.text;
              } else {
                outputMsg.stdout += data.text;
              }
              this.$nextTick(() => this.scrollToBottom(false));
            }
            return;
          }
          if (data.type === 'shell_exit') {
            const outputMsg = this.chatMessages.find(m => m.role === 'shell_output' && m.shellId === data.id);
            if (outputMsg) {
              outputMsg.exitCode = data.code;
              outputMsg.timedOut = data.timeout || false;
              outputMsg.complete = true;
            }
            this.activeShellId = null;
            this.stopSessionSSE();
            return;
          }

          if (data.type === 'auto_continuing') {
            this.continuationCount = data.continuation_count || 0;
            this.chatMessages.push({
              id: 'auto-continue-' + Date.now(),
              role: 'system',
              content: `Auto-continuing (${data.continuation_count}/${data.max_continuations})...`,
              isAutoContinue: true
            });
            // Reset turn threshold tracker since turn count will reset
            this.lastTurnThreshold = 0;
            this.$nextTick(() => this.scrollToBottom(true));
            return;
          }

          if (data.type === 'compacting') {
            this.isCompacting = true;
            this.chatMessages.push({
              id: 'compact-' + Date.now(),
              role: 'system',
              content: 'Running /compact to reduce context size...',
              isAutoContinue: true
            });
            this.$nextTick(() => this.scrollToBottom(true));
            return;
          }

          if (data.type === 'compact_complete') {
            this.isCompacting = false;
            this.chatMessages.push({
              id: 'compact-done-' + Date.now(),
              role: 'system',
              content: 'Compact complete.',
              isAutoContinue: true
            });
            this.$nextTick(() => this.scrollToBottom(true));
            return;
          }

          if (data.type === 'auto_continue_exhausted') {
            this.chatMessages.push({
              id: 'auto-exhausted-' + Date.now(),
              role: 'system',
              content: data.reason === 'no_progress'
                ? 'Auto-continue stopped: not making progress. Send a message to continue.'
                : `Auto-continue limit reached (${data.continuation_count}). Send a message to continue.`,
              isAutoContinue: true
            });
            this.$nextTick(() => this.scrollToBottom(true));
            // Don't return — let the done handler below close the SSE
          }

          if (data.type === 'input_request') {
            this.pendingPermission = data;
            return;
          }

          if (data.type === 'done' || data.type === 'result') {
            // Track cost from result events
            if (data.type === 'result' && data.cost != null) {
              this.sessionCost = (this.sessionCost || 0) + data.cost;
            }
            // Mark the current active pill as completed (don't clear — pills persist until next message)
            const activePill = this.chatMessages.find(m => m.role === 'activity' && m.pillState === 'active');
            if (activePill) {
              const elapsed = Math.round((Date.now() - (this.currentPillStart || Date.now())) / 1000);
              activePill.pillState = 'completed';
              activePill.duration = elapsed + 's';
            }
            this.clearStalenessTimer();
          }
          if (data.type === 'done') {
            this.pendingPermission = null;
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
      this.clearHeartbeatTimer();
    },

    async respondToPermission(decision) {
      if (!this.pendingPermission || !this.selectedSessionId) return;
      try {
        await fetch(`/api/sessions/${this.selectedSessionId}/permission-respond`, {
          method: 'POST',
          headers: {
            'Authorization': `Bearer ${this.apiKey}`,
            'Content-Type': 'application/json'
          },
          body: JSON.stringify({ decision })
        });
      } catch (e) {
        console.error('Failed to respond to permission:', e);
      }
      this.pendingPermission = null;
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
          .filter(m => m.role === 'user' || m.role === 'assistant' || m.role === 'activity' || m.role === 'shell' || m.role === 'shell_output' || (m.role === 'system' && m.content && (m.content.includes('"error"') || m.content.startsWith('Auto-continu'))))
          .map(m => {
            if (m.role === 'activity') {
              return {
                role: 'activity',
                content: m.content,
                originalLabel: m.content,
                pillState: 'completed',
                duration: null,
                timestamp: m.created_at
              };
            }
            if (m.role === 'shell') {
              return { role: 'shell', content: m.content, shellId: null, cwd: '', timestamp: m.created_at };
            }
            if (m.role === 'shell_output') {
              try {
                const parsed = JSON.parse(m.content);
                return {
                  role: 'shell_output',
                  stdout: parsed.stdout || '',
                  stderr: parsed.stderr || '',
                  exitCode: parsed.exit_code,
                  timedOut: parsed.timed_out || false,
                  shellId: null,
                  complete: true,
                  timestamp: m.created_at
                };
              } catch (e) {
                return { role: 'shell_output', stdout: m.content, stderr: '', exitCode: null, timedOut: false, shellId: null, complete: true, timestamp: m.created_at };
              }
            }
            if (m.role === 'system' && m.content && m.content.startsWith('Auto-continu')) {
              return {
                role: 'system',
                content: m.content,
                isAutoContinue: true,
                timestamp: m.created_at
              };
            }
            return {
              role: m.role,
              content: m.content,
              msg_type: 'text',
              timestamp: m.created_at
            };
          });
        this.$nextTick(() => this.scrollToBottom(true));
      } catch (e) {
        console.error('Failed to fetch messages:', e);
      }
      this.chatLoading = false;
    },

    // GitHub Issues methods
    async fetchGithubIssues(sessionId) {
      if (!sessionId) return;
      this.githubIssuesLoading = true;
      this.githubIssuesError = null;
      try {
        const params = new URLSearchParams({
          state: this.githubIssuesState,
          limit: this.githubIssuesLimit,
        });
        if (this.githubIssuesSearch.trim()) {
          params.set('search', this.githubIssuesSearch.trim());
        }
        const resp = await fetch(`/api/sessions/${sessionId}/github/issues?${params}`, {
          headers: { 'Authorization': 'Bearer ' + this.apiKey }
        });
        if (resp.status === 401) { this.disconnect(); return; }
        if (!resp.ok) {
          const text = await resp.text();
          this.githubIssuesError = text || 'Failed to load issues';
          return;
        }
        const data = await resp.json();
        this.githubIssues = data.issues || [];
        this.githubIssuesHasMore = data.has_more || false;
        if (data.repo) this.githubRepo = data.repo;
      } catch (e) {
        this.githubIssuesError = e.message || 'Failed to load issues';
      } finally {
        this.githubIssuesLoading = false;
      }
    },

    async fetchIssueDetail(sessionId, number) {
      if (!sessionId) return;
      this.selectedIssueLoading = true;
      // Close any open file viewer and open issue in viewer panel
      this.closeFileViewer();
      if (this.isMobile()) { this.mobileOverlay = 'issue'; }
      try {
        const resp = await fetch(`/api/sessions/${sessionId}/github/issues/${number}`, {
          headers: { 'Authorization': 'Bearer ' + this.apiKey }
        });
        if (resp.status === 401) { this.disconnect(); return; }
        if (!resp.ok) return;
        this.selectedIssue = await resp.json();
      } catch (e) {
        // Ignore — keep selectedIssue as null
      } finally {
        this.selectedIssueLoading = false;
      }
    },

    closeIssueViewer() {
      this.selectedIssue = null;
    },

    toggleIssueState(state) {
      this.githubIssuesState = state;
      this.githubIssuesLimit = 10;
      this.selectedIssue = null;
      this.fetchGithubIssues(this.selectedSessionId);
    },

    searchIssues() {
      if (this._searchIssuesTimer) clearTimeout(this._searchIssuesTimer);
      this._searchIssuesTimer = setTimeout(() => {
        this.githubIssuesLimit = 10;
        this.fetchGithubIssues(this.selectedSessionId);
      }, 300);
    },

    loadMoreIssues() {
      this.githubIssuesLimit += 10;
      this.fetchGithubIssues(this.selectedSessionId);
    },

    generateIssuePrompt(issue) {
      const prompt = `Work on GitHub issue #${issue.number}: "${issue.title}"

Requirements:
${issue.body || '(No description provided)'}

Create a feature branch, implement the solution, and open a draft PR linking to issue #${issue.number}.`;
      this.inputText = prompt;
      this.$nextTick(() => {
        const textarea = document.querySelector('.instruction-bar textarea');
        if (textarea) {
          textarea.style.height = 'auto';
          textarea.style.height = Math.min(textarea.scrollHeight, 150) + 'px';
        }
      });
    },

    issueTimeAgo(dateStr) {
      if (!dateStr) return '';
      const date = new Date(dateStr);
      const seconds = Math.floor((Date.now() - date.getTime()) / 1000);
      if (seconds < 60) return 'just now';
      const minutes = Math.floor(seconds / 60);
      if (minutes < 60) return `${minutes}m ago`;
      const hours = Math.floor(minutes / 60);
      if (hours < 24) return `${hours}h ago`;
      return `${Math.floor(hours / 24)}d ago`;
    },

    issueLabelStyle(label) {
      if (!label || !label.color) return '';
      const hex = label.color.replace('#', '');
      const r = parseInt(hex.substring(0, 2), 16);
      const g = parseInt(hex.substring(2, 4), 16);
      const b = parseInt(hex.substring(4, 6), 16);
      return `background: rgba(${r},${g},${b},0.2); color: rgb(${r},${g},${b}); border-color: rgba(${r},${g},${b},0.4);`;
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
      this.selectedIssue = null; // Close issue viewer if open
      this.viewerFile = filePath;
      if (this.isMobile()) { this.mobileOverlay = 'file'; }
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
        // Complete the current active pill
        const activePill = this.chatMessages.find(m => m.role === 'activity' && m.pillState === 'active');
        if (activePill) {
            const elapsed = Math.round((now - (this.currentPillStart || now)) / 1000);
            activePill.pillState = 'completed';
            activePill.duration = elapsed + 's';
        }
        // Push pill into chatMessages so it appears inline chronologically
        this.chatMessages.push({
            role: 'activity',
            content: label,
            originalLabel: label,
            pillState: state,
            duration: null,
            timestamp: new Date().toISOString()
        });
        this.currentPillStart = now;
        // Enforce stacking limit: max 10 completed pills
        const completed = this.chatMessages.filter(m => m.role === 'activity' && m.pillState === 'completed');
        if (completed.length > 10) {
            const idx = this.chatMessages.indexOf(completed[0]);
            this.chatMessages.splice(idx, 1);
        }
        this.$nextTick(() => this.scrollToBottom());
    },

    clearActivityPills() {
        this.chatMessages = this.chatMessages.filter(m => m.role !== 'activity');
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
        const stalePill = this.chatMessages.find(m => m.role === 'activity' && m.pillState === 'stale');
        if (stalePill) {
            stalePill.pillState = 'active';
            stalePill.content = stalePill.originalLabel;
        }
        this.stalenessTimer = setTimeout(() => {
            const activePill = this.chatMessages.find(m => m.role === 'activity' && m.pillState === 'active');
            if (activePill) {
                const elapsed = Math.round((Date.now() - this.lastEventTime) / 1000);
                activePill.originalLabel = activePill.content;
                activePill.pillState = 'stale';
                activePill.content = `${activePill.content} — ${elapsed}s, may be stalled`;
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
        if (msg.command) {
          classes.push('assistant', 'command-response');
        } else {
          classes.push(msg.role);
        }
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
        if ((msg.role === 'assistant' || msg.command) && typeof marked !== 'undefined') {
          const r = new marked.Renderer();
          r.link = function(href, title, text) {
            const h = typeof href === 'object' ? href.href : href;
            const t = typeof href === 'object' ? href.title : title;
            const tx = typeof href === 'object' ? href.text : text;
            const titleAttr = t ? ` title="${t}"` : '';
            return `<a href="${h}" target="_blank" rel="noopener noreferrer"${titleAttr}>${tx}</a>`;
          };
          const html = marked.parse(msg.content || '', { renderer: r, breaks: true });
          const eyebrow = msg.command ? `<div class="command-label">${esc(msg.command)}</div>` : '';
          return `${eyebrow}<div class="markdown-content">${html}</div>${time}`;
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

    async loadScheduledTasks() {
        try {
            const res = await fetch('/api/tasks', {
                headers: { 'Authorization': 'Bearer ' + this.apiKey }
            });
            if (res.ok) this.scheduledTasks = await res.json();
        } catch (err) {
            console.error('Failed to load tasks:', err);
        }
    },

    openTaskModal(task) {
        if (task) {
            this.editingTask = task;
            this.taskForm = {
                name: task.name, task_type: task.task_type, command: task.command,
                working_directory: task.working_directory, cron_expression: task.cron_expression,
                session_id: task.session_id || ''
            };
            this.loadTaskRuns(task.id);
        } else {
            this.editingTask = null;
            this.taskRuns = [];
            this.taskForm = { name: '', task_type: 'shell', command: '', working_directory: '', cron_expression: '', session_id: '' };
        }
        this.taskFormErrors = '';
        this.taskModalOpen = true;
    },

    async saveTask() {
        this.taskLoading = true;
        this.taskFormErrors = '';
        try {
            const method = this.editingTask ? 'PUT' : 'POST';
            const url = this.editingTask ? '/api/tasks/' + this.editingTask.id : '/api/tasks';
            const body = { ...this.taskForm };
            if (this.editingTask) body.enabled = this.editingTask.enabled;
            const res = await fetch(url, {
                method,
                headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + this.apiKey },
                body: JSON.stringify(body)
            });
            if (!res.ok) {
                const data = await res.json();
                this.taskFormErrors = data.error || 'Failed to save';
                return;
            }
            this.taskModalOpen = false;
            await this.loadScheduledTasks();
        } catch (err) {
            this.taskFormErrors = err.message;
        } finally {
            this.taskLoading = false;
        }
    },

    async deleteTask(taskId) {
        if (!confirm('Delete this scheduled task?')) return;
        try {
            await fetch('/api/tasks/' + taskId, {
                method: 'DELETE',
                headers: { 'Authorization': 'Bearer ' + this.apiKey }
            });
            if (this.selectedTask && this.selectedTask.id === taskId) {
                this.selectedTask = null;
                this.taskRuns = [];
            }
            await this.loadScheduledTasks();
        } catch (err) {
            console.error('Failed to delete task:', err);
        }
    },

    async toggleTaskEnabled(task) {
        try {
            await fetch('/api/tasks/' + task.id, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + this.apiKey },
                body: JSON.stringify({
                    name: task.name, task_type: task.task_type, command: task.command,
                    working_directory: task.working_directory, cron_expression: task.cron_expression,
                    enabled: !task.enabled
                })
            });
            await this.loadScheduledTasks();
        } catch (err) {
            console.error('Failed to toggle task:', err);
        }
    },

    async selectTask(task) {
        this.openTaskModal(task);
    },

    async loadTaskRuns(taskId) {
        this.taskRunsLoading = true;
        try {
            const res = await fetch('/api/tasks/' + taskId + '/runs', {
                headers: { 'Authorization': 'Bearer ' + this.apiKey }
            });
            if (res.ok) this.taskRuns = await res.json();
        } catch (err) {
            console.error('Failed to load runs:', err);
        } finally {
            this.taskRunsLoading = false;
        }
    },

    async triggerTask(taskId) {
        try {
            await fetch('/api/tasks/' + taskId + '/trigger', {
                method: 'POST',
                headers: { 'Authorization': 'Bearer ' + this.apiKey }
            });
            this.toast('Task queued for execution');
            setTimeout(() => {
                this.loadScheduledTasks();
                if (this.selectedTask && this.selectedTask.id === taskId) this.loadTaskRuns(taskId);
            }, 2000);
        } catch (err) {
            console.error('Failed to trigger task:', err);
        }
    },

    formatCron(expr) {
        const presets = {
            '* * * * *': 'Every minute', '*/5 * * * *': 'Every 5 min',
            '*/15 * * * *': 'Every 15 min', '0 * * * *': 'Hourly',
            '0 */2 * * *': 'Every 2 hours', '0 0 * * *': 'Daily midnight',
            '0 9 * * *': 'Daily 9 AM', '0 9 * * 1-5': 'Weekdays 9 AM',
            '0 0 * * 0': 'Weekly Sunday', '0 0 1 * *': 'Monthly 1st',
        };
        return presets[expr] || expr;
    },

    formatRelativeTime(dateStr) {
        if (!dateStr) return 'never';
        const date = new Date(dateStr.endsWith('Z') ? dateStr : dateStr + 'Z');
        const now = new Date();
        const diff = now - date;
        const mins = Math.floor(diff / 60000);
        if (mins < 1) return 'just now';
        if (mins < 60) return mins + 'm ago';
        const hrs = Math.floor(mins / 60);
        if (hrs < 24) return hrs + 'h ago';
        return Math.floor(hrs / 24) + 'd ago';
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
