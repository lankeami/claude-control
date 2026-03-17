import SwiftUI

struct PromptCardView: View {
    let prompt: Prompt
    @State private var responseText: String = ""
    var onRespond: (String) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Circle()
                    .fill(prompt.isPending ? Color.green : Color.gray.opacity(0.4))
                    .frame(width: 10, height: 10)

                if prompt.isPending {
                    Text("Claude is waiting...")
                        .font(.caption)
                        .fontWeight(.semibold)
                        .foregroundColor(.green)
                } else if prompt.isNotification {
                    Text(prompt.createdAt, style: .relative)
                        .font(.caption)
                        .foregroundColor(.secondary)
                } else {
                    Text(prompt.createdAt, style: .relative)
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
            }

            Text(prompt.claudeMessage)
                .font(.body)
                .lineLimit(nil)

            if prompt.isPending {
                HStack {
                    TextField("Type your response...", text: $responseText)
                        .textFieldStyle(.roundedBorder)

                    Button("Send") {
                        guard !responseText.isEmpty else { return }
                        onRespond(responseText)
                        responseText = ""
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(responseText.isEmpty)
                }
            } else if let response = prompt.response {
                Text("Replied: \(response)")
                    .font(.caption)
                    .foregroundColor(.secondary)
                    .italic()
            }
        }
        .padding()
        .background(prompt.isPending ? Color.green.opacity(0.05) : Color.clear)
        .cornerRadius(12)
    }
}
