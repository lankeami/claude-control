import SwiftUI

struct SettingsView: View {
    @Environment(\.dismiss) var dismiss
    @ObservedObject var polling: PollingService
    @State private var configs: [ServerConfig] = KeychainService.loadConfigs()
    @State private var showPairing = false
    var onSelectConfig: (ServerConfig) -> Void

    var body: some View {
        NavigationStack {
            List {
                Section("Paired Servers") {
                    ForEach(configs) { config in
                        HStack {
                            VStack(alignment: .leading) {
                                Text(config.label ?? config.url)
                                    .font(.body)
                                Text(config.url)
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                            }

                            Spacer()

                            Circle()
                                .fill(polling.isConnected ? Color.green : Color.red)
                                .frame(width: 10, height: 10)
                        }
                        .contentShape(Rectangle())
                        .onTapGesture {
                            onSelectConfig(config)
                            dismiss()
                        }
                    }
                    .onDelete { indexSet in
                        for i in indexSet {
                            KeychainService.removeConfig(url: configs[i].url)
                        }
                        configs = KeychainService.loadConfigs()
                    }

                    Button(action: { showPairing = true }) {
                        Label("Add Server", systemImage: "plus")
                    }
                }

                Section("Archived Sessions") {
                    ForEach(polling.sessions.filter { $0.archived }) { session in
                        HStack {
                            Text(session.displayName)
                                .foregroundColor(.secondary)
                            Spacer()
                            Button("Unarchive") {
                                Task {
                                    // TODO: Call unarchive API
                                }
                            }
                            .font(.caption)
                        }
                    }
                }
            }
            .navigationTitle("Settings")
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
            .sheet(isPresented: $showPairing) {
                PairingView { config in
                    configs = KeychainService.loadConfigs()
                    onSelectConfig(config)
                }
            }
        }
    }
}
