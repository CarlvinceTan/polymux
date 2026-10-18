#if os(macOS)
import SwiftUI
import AVFoundation
import AppKit
import Observation

struct CameraView: View {
    let capture: (URL) -> Void
    @Environment(\.panelClose) private var panelClose
    @State private var store = CameraStore()

    var body: some View {
        PanelView("Camera", onClose: close) {
            ScrollView {
                Group {
                    if let issue = store.issue {
                        ContentUnavailableView {
                            Label(store.denied ? "Camera access is off" : "Camera unavailable", systemImage: "camera")
                        } description: {
                            Text(issue)
                        } actions: {
                            if store.denied {
                                Button("Open System Settings") {
                                    if let url = URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_Camera") {
                                        NSWorkspace.shared.open(url)
                                    }
                                }
                                .buttonStyle(.glass)
                            }
                        }
                    } else if store.ready {
                        CameraPreview(session: store.driver.session)
                            .aspectRatio(4 / 3, contentMode: .fit)
                            .clipShape(.rect(cornerRadius: 16))
                            .accessibilityLabel("Camera preview")
                    } else {
                        ProgressView("Opening camera…")
                    }
                }
                .frame(maxWidth: .infinity)
                .padding(16)
            }
        } actions: {
            Button("Cancel", action: close)
            Button("Take photo", systemImage: "camera") {
                Task {
                    if let url = await store.photo() {
                        capture(url)
                        close()
                    }
                }
            }
            .disabled(!store.ready || store.capturing || store.issue != nil)
        }
        .panelDismiss(onClose: close)
        .task { await store.start() }
        .onDisappear { store.stop() }
    }

    private func close() {
        store.stop()
        panelClose()
    }
}

@Observable @MainActor
private final class CameraStore {
    let driver = CameraCapture()
    var ready = false
    var capturing = false
    var denied = false
    var issue: String?
    private var closed = false

    func start() async {
        let allowed: Bool
        switch AVCaptureDevice.authorizationStatus(for: .video) {
        case .authorized: allowed = true
        case .notDetermined: allowed = await AVCaptureDevice.requestAccess(for: .video)
        default: allowed = false
        }
        guard !closed, !Task.isCancelled else { return }
        guard allowed else {
            denied = true
            issue = "Allow Polymux to use the camera in System Settings."
            return
        }
        do {
            try await driver.start()
            guard !closed, !Task.isCancelled else { driver.stop(); return }
            ready = true
        } catch is CancellationError {} catch { issue = error.localizedDescription }
    }

    func photo() async -> URL? {
        guard ready, !capturing, !closed else { return nil }
        capturing = true
        defer { capturing = false }
        do {
            let url = try await driver.photo()
            guard !closed, !Task.isCancelled else {
                try? FileManager.default.removeItem(at: url)
                return nil
            }
            return url
        } catch is CancellationError {} catch { issue = error.localizedDescription }
        return nil
    }

    func stop() {
        guard !closed else { return }
        closed = true
        ready = false
        driver.stop()
    }
}

private struct CameraPreview: NSViewRepresentable {
    let session: AVCaptureSession

    func makeNSView(context: Context) -> CameraLayer {
        let view = CameraLayer(frame: .zero)
        view.preview.session = session
        return view
    }

    func updateNSView(_ view: CameraLayer, context: Context) {}

    static func dismantleNSView(_ view: CameraLayer, coordinator: ()) {
        view.preview.session = nil
    }
}

private final class CameraLayer: NSView {
    let preview = AVCaptureVideoPreviewLayer()

    override init(frame: NSRect) {
        super.init(frame: frame)
        wantsLayer = true
        preview.videoGravity = .resizeAspectFill
        layer?.addSublayer(preview)
    }

    required init?(coder: NSCoder) { nil }

    override func layout() {
        super.layout()
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        preview.frame = bounds
        CATransaction.commit()
    }
}

// Capture configuration, running state, and pending photos stay on one serial queue.
private final class CameraCapture: NSObject, AVCapturePhotoCaptureDelegate, @unchecked Sendable {
    let session = AVCaptureSession()
    private let queue = DispatchQueue(label: "Polymux.camera", qos: .userInitiated)
    private let output = AVCapturePhotoOutput()
    private var closed = false
    private var pending: (id: Int64, result: CheckedContinuation<URL, Error>)?

    func start() async throws {
        try await withCheckedThrowingContinuation { (result: CheckedContinuation<Void, Error>) in
            queue.async {
                do {
                    guard !self.closed else { throw CancellationError() }
                    guard let device = AVCaptureDevice.default(for: .video), device.isConnected else {
                        throw CameraError.unavailable
                    }
                    let input = try AVCaptureDeviceInput(device: device)
                    self.session.beginConfiguration()
                    do {
                        if self.session.canSetSessionPreset(.photo) { self.session.sessionPreset = .photo }
                        guard self.session.canAddInput(input), self.session.canAddOutput(self.output) else {
                            throw CameraError.unavailable
                        }
                        self.session.addInput(input)
                        self.session.addOutput(self.output)
                        self.session.commitConfiguration()
                    } catch {
                        self.session.commitConfiguration()
                        throw error
                    }
                    self.session.startRunning()
                    guard self.session.isRunning else { throw CameraError.unavailable }
                    result.resume()
                } catch { result.resume(throwing: error) }
            }
        }
    }

    func photo() async throws -> URL {
        try await withCheckedThrowingContinuation { (result: CheckedContinuation<URL, Error>) in
            queue.async {
                guard !self.closed else { result.resume(throwing: CancellationError()); return }
                guard self.session.isRunning, self.pending == nil,
                      self.output.availablePhotoCodecTypes.contains(.jpeg) else {
                    result.resume(throwing: CameraError.unavailable)
                    return
                }
                let settings = AVCapturePhotoSettings(format: [AVVideoCodecKey: AVVideoCodecType.jpeg])
                self.pending = (settings.uniqueID, result)
                self.output.capturePhoto(with: settings, delegate: self)
            }
        }
    }

    func stop() {
        queue.async {
            guard !self.closed else { return }
            self.closed = true
            self.pending?.result.resume(throwing: CancellationError())
            self.pending = nil
            if self.session.isRunning { self.session.stopRunning() }
        }
    }

    func photoOutput(_ output: AVCapturePhotoOutput, didFinishProcessingPhoto photo: AVCapturePhoto, error: Error?) {
        let id = photo.resolvedSettings.uniqueID
        let result: Result<Data, CameraError>
        if let error { result = .failure(.failed(error.localizedDescription)) }
        else if let data = photo.fileDataRepresentation() { result = .success(data) }
        else { result = .failure(.failed("The photo could not be captured. Try again.")) }
        queue.async { self.finish(id, result: result) }
    }

    func photoOutput(_ output: AVCapturePhotoOutput, didFinishCaptureFor settings: AVCaptureResolvedPhotoSettings, error: Error?) {
        guard let error else { return }
        let id = settings.uniqueID
        let message = error.localizedDescription
        queue.async { self.finish(id, result: .failure(.failed(message))) }
    }

    private func finish(_ id: Int64, result: Result<Data, CameraError>) {
        guard let pending, pending.id == id else { return }
        self.pending = nil
        do {
            guard !closed else { throw CancellationError() }
            let data = try result.get()
            let url = FileManager.default.temporaryDirectory
                .appending(path: "Camera-\(UUID().uuidString.prefix(8)).jpg")
            try data.write(to: url, options: .atomic)
            pending.result.resume(returning: url)
        } catch { pending.result.resume(throwing: error) }
    }
}

private enum CameraError: Error, LocalizedError {
    case unavailable, failed(String)

    var errorDescription: String? {
        switch self {
        case .unavailable: "Connect or enable a camera to take a photo."
        case .failed(let message): message
        }
    }
}
#endif
