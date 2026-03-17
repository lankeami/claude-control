import SwiftUI

struct InstructionSheet: View {
    @Environment(\.dismiss) var dismiss
    @State private var message: String = ""
    @State private var isSending = false
    @State private var showConfirmation = false
    var onSend: (String) async -> Void

    var body: some View {
        NavigationStack {
            VStack(spacing: 16) {
                Text("This instruction will be delivered when Claude finishes its current turn.")
                    .font(.caption)
                    .foregroundColor(.secondary)

                TextEditor(text: $message)
                    .frame(minHeight: 120)
                    .overlay(
                        RoundedRectangle(cornerRadius: 8)
                            .stroke(Color.gray.opacity(0.3))
                    )

                if showConfirmation {
                    Label("Instruction queued", systemImage: "checkmark.circle.fill")
                        .foregroundColor(.green)
                }
            }
            .padding()
            .navigationTitle("New Instruction")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Send") {
                        isSending = true
                        Task {
                            await onSend(message)
                            isSending = false
                            showConfirmation = true
                            try? await Task.sleep(nanoseconds: 1_500_000_000)
                            dismiss()
                        }
                    }
                    .disabled(message.isEmpty || isSending)
                }
            }
        }
    }
}
