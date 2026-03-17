import SwiftUI
import AVFoundation

struct PairingView: View {
    @Environment(\.dismiss) var dismiss
    @State private var scannedCode: String?
    @State private var manualURL: String = ""
    @State private var manualKey: String = ""
    @State private var showManualEntry = false
    @State private var isPairing = false
    @State private var errorMessage: String?
    var onPaired: (ServerConfig) -> Void

    var body: some View {
        NavigationStack {
            VStack(spacing: 20) {
                if showManualEntry {
                    manualEntryForm
                } else {
                    QRScannerView { code in
                        scannedCode = code
                        handleScannedCode(code)
                    }
                    .frame(maxHeight: 400)
                    .cornerRadius(12)

                    Text("Scan the QR code shown in your terminal")
                        .font(.subheadline)
                        .foregroundColor(.secondary)
                }

                if let error = errorMessage {
                    Text(error)
                        .foregroundColor(.red)
                        .font(.caption)
                }

                Button("Enter manually instead") {
                    showManualEntry.toggle()
                }
                .font(.subheadline)
            }
            .padding()
            .navigationTitle("Pair Server")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
        }
    }

    private var manualEntryForm: some View {
        VStack(spacing: 16) {
            TextField("Server URL (e.g. https://abc123.ngrok.io)", text: $manualURL)
                .textFieldStyle(.roundedBorder)
                .autocapitalization(.none)
                .disableAutocorrection(true)

            TextField("API Key (e.g. sk-...)", text: $manualKey)
                .textFieldStyle(.roundedBorder)
                .autocapitalization(.none)
                .disableAutocorrection(true)

            Button(action: {
                let config = ServerConfig(url: manualURL, key: manualKey, version: 1)
                pairWith(config)
            }) {
                if isPairing {
                    ProgressView()
                } else {
                    Text("Connect")
                }
            }
            .buttonStyle(.borderedProminent)
            .disabled(manualURL.isEmpty || manualKey.isEmpty || isPairing)
        }
    }

    private func handleScannedCode(_ code: String) {
        guard let data = code.data(using: .utf8),
              let config = try? JSONDecoder().decode(ServerConfig.self, from: data) else {
            errorMessage = "Invalid QR code"
            return
        }
        pairWith(config)
    }

    private func pairWith(_ config: ServerConfig) {
        isPairing = true
        errorMessage = nil

        Task {
            let client = APIClient(config: config)
            do {
                let _ = try await client.validatePairing()
                KeychainService.addConfig(config)
                onPaired(config)
                dismiss()
            } catch {
                errorMessage = "Failed to connect: \(error.localizedDescription)"
            }
            isPairing = false
        }
    }
}

// QR Scanner using AVCaptureSession
struct QRScannerView: UIViewControllerRepresentable {
    var onCodeScanned: (String) -> Void

    func makeUIViewController(context: Context) -> QRScannerViewController {
        let vc = QRScannerViewController()
        vc.onCodeScanned = onCodeScanned
        return vc
    }

    func updateUIViewController(_ uiViewController: QRScannerViewController, context: Context) {}
}

class QRScannerViewController: UIViewController, AVCaptureMetadataOutputObjectsDelegate {
    var onCodeScanned: ((String) -> Void)?
    private var captureSession: AVCaptureSession?

    override func viewDidLoad() {
        super.viewDidLoad()

        let session = AVCaptureSession()
        guard let device = AVCaptureDevice.default(for: .video),
              let input = try? AVCaptureDeviceInput(device: device) else { return }

        session.addInput(input)

        let output = AVCaptureMetadataOutput()
        session.addOutput(output)
        output.setMetadataObjectsDelegate(self, queue: .main)
        output.metadataObjectTypes = [.qr]

        let preview = AVCaptureVideoPreviewLayer(session: session)
        preview.frame = view.bounds
        preview.videoGravity = .resizeAspectFill
        view.layer.addSublayer(preview)

        captureSession = session
        session.startRunning()
    }

    func metadataOutput(_ output: AVCaptureMetadataOutput, didOutput metadataObjects: [AVMetadataObject], from connection: AVCaptureConnection) {
        guard let object = metadataObjects.first as? AVMetadataMachineReadableCodeObject,
              let code = object.stringValue else { return }
        captureSession?.stopRunning()
        onCodeScanned?(code)
    }
}
