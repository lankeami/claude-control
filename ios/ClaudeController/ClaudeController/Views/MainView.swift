import SwiftUI

struct MainView: View {
    @StateObject private var polling = PollingService()
    @State private var selectedConfig: ServerConfig?
    @State private var showPairing = false
    @State private var showInstruction = false
    @State private var showSettings = false

    private var configs: [ServerConfig] { KeychainService.loadConfigs() }
    private var apiClient: APIClient? {
        guard let config = selectedConfig else { return nil }
        return APIClient(config: config)
    }

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                if polling.sessions.isEmpty && !polling.isConnected {
                    ContentUnavailableView(
                        "No Server Connected",
                        systemImage: "antenna.radiowaves.left.and.right.slash",
                        description: Text("Pair with your computer to get started")
                    )
                } else {
                    // Session selector
                    if !polling.sessions.isEmpty {
                        Picker("Session", selection: $polling.selectedSessionId) {
                            ForEach(polling.sessions) { session in
                                HStack {
                                    Circle()
                                        .fill(session.status == "waiting" ? Color.green : Color.gray)
                                        .frame(width: 8, height: 8)
                                    Text(session.displayName)
                                }
                                .tag(Optional(session.id))
                            }
                        }
                        .padding(.horizontal)
                    }

                    Divider()

                    // Prompt list
                    List {
                        let prompts = polling.selectedSessionId != nil
                            ? polling.allPrompts
                            : polling.pendingPrompts

                        if prompts.isEmpty {
                            Text("No prompts")
                                .foregroundColor(.secondary)
                        }

                        ForEach(prompts) { prompt in
                            PromptCardView(prompt: prompt) { response in
                                Task {
                                    try? await apiClient?.respondToPrompt(
                                        promptId: prompt.id,
                                        response: response
                                    )
                                }
                            }
                            .listRowInsets(EdgeInsets())
                            .listRowSeparator(.hidden)
                        }
                    }
                    .listStyle(.plain)
                }
            }
            .navigationTitle("Claude Controller")
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Menu {
                        Button(action: { showInstruction = true }) {
                            Label("New Instruction", systemImage: "plus.message")
                        }
                        .disabled(polling.selectedSessionId == nil)

                        Button(action: { showPairing = true }) {
                            Label("Pair Server", systemImage: "qrcode.viewfinder")
                        }

                        Button(action: { showSettings = true }) {
                            Label("Settings", systemImage: "gear")
                        }
                    } label: {
                        Image(systemName: "ellipsis.circle")
                    }
                }
            }
            .sheet(isPresented: $showPairing) {
                PairingView { config in
                    selectedConfig = config
                    if let client = apiClient {
                        polling.configure(client: client)
                    }
                }
            }
            .sheet(isPresented: $showInstruction) {
                InstructionSheet { message in
                    guard let sid = polling.selectedSessionId else { return }
                    try? await apiClient?.sendInstruction(sessionId: sid, message: message)
                }
            }
            .sheet(isPresented: $showSettings) {
                SettingsView(
                    polling: polling,
                    onSelectConfig: { config in
                        selectedConfig = config
                        if let client = apiClient {
                            polling.configure(client: client)
                        }
                    }
                )
            }
            .onAppear {
                if let first = configs.first {
                    selectedConfig = first
                    if let client = apiClient {
                        polling.configure(client: client)
                    }
                } else {
                    showPairing = true
                }
            }
            .badge(polling.pendingCount)
        }
    }
}
