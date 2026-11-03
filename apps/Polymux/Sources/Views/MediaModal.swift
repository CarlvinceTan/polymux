import SwiftUI
import QuickLook
#if os(iOS)
import AVKit
#endif

extension View {
    /// Opens a downloaded file. On iPhone a photo or video opens in `MediaModal`, because
    /// QuickLook's black page can't be recoloured. `poster` shows until the file is ready.
    func mediaPreview(_ file: Binding<URL?>, kind: MediaKind?, poster: CGImage?) -> some View {
        modifier(MediaPreview(file: file, kind: kind, poster: poster))
    }
}

private struct MediaPreview: ViewModifier {
    @Binding var file: URL?
    let kind: MediaKind?
    let poster: CGImage?
    #if os(iOS)
    @Namespace private var zoom
    #endif

    func body(content: Content) -> some View {
        #if os(iOS)
        content
            .matchedTransitionSource(id: "media", in: zoom)
            .quickLookPreview(Binding { kind == nil ? file : nil } set: { file = $0 })
            .fullScreenCover(item: Binding { kind == nil ? nil : file.map(MediaFile.init) } set: { file = $0?.url }) { media in
                if let kind {
                    MediaModal(file: media.url, kind: kind, poster: poster)
                        .navigationTransition(.zoom(sourceID: "media", in: zoom))
                }
            }
        #else
        content.quickLookPreview($file)
        #endif
    }
}

#if os(iOS)
private struct MediaFile: Identifiable {
    let url: URL
    var id: URL { url }
}

/// A photo or video over the window, on the app's surface.
private struct MediaModal: View {
    let file: URL
    let kind: MediaKind
    let poster: CGImage?
    @Environment(\.colorScheme) private var scheme
    @Environment(\.dismiss) private var dismiss
    @State private var photo: CGImage?
    @State private var player: AVPlayer?
    @State private var ready = false
    @State private var failed = false

    init(file: URL, kind: MediaKind, poster: CGImage?) {
        self.file = file
        self.kind = kind
        self.poster = poster
        _player = State(initialValue: kind == .video ? AVPlayer(url: file) : nil)
    }

    var body: some View {
        NavigationStack {
            ZStack {
                AppBackground.color(scheme).ignoresSafeArea()
                if let player {
                    if !ready { standIn }
                    VideoSurface(player: player, ready: $ready, failed: $failed).opacity(ready ? 1 : 0)
                } else if let image = photo ?? poster {
                    ZoomImage(image: image).ignoresSafeArea()
                } else {
                    standIn
                }
            }
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackgroundVisibility(.hidden, for: .navigationBar)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close", systemImage: "xmark") { dismiss() }
                }
                ToolbarItem(placement: .primaryAction) {
                    ShareLink(item: file)
                }
            }
        }
        .presentationBackground(AppBackground.color(scheme))
        .task {
            player?.play()
            guard kind == .image else { return }
            photo = try? await Task.detached { [file] in try DecodedImages.decode(file, limit: 4096) }.value
            failed = photo == nil
        }
        .onDisappear { player?.pause() }
    }

    @ViewBuilder private var standIn: some View {
        if failed {
            Text("Couldn’t open this file.").foregroundStyle(.muted)
        } else if let poster {
            Image(decorative: poster, scale: 1).resizable().scaledToFit()
        } else {
            ProgressView()
        }
    }
}

/// The system player, hidden until its first frame is ready and clear around the video.
private struct VideoSurface: UIViewControllerRepresentable {
    let player: AVPlayer
    @Binding var ready: Bool
    @Binding var failed: Bool

    final class Coordinator {
        var display: NSKeyValueObservation?
        var status: NSKeyValueObservation?
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeUIViewController(context: Context) -> AVPlayerViewController {
        let controller = AVPlayerViewController()
        controller.player = player
        controller.view.backgroundColor = .clear
        let ready = $ready, failed = $failed
        context.coordinator.display = controller.observe(\.isReadyForDisplay) { controller, _ in
            let shown = controller.isReadyForDisplay
            Task { @MainActor in ready.wrappedValue = shown }
        }
        context.coordinator.status = player.currentItem?.observe(\.status) { item, _ in
            let broken = item.status == .failed
            Task { @MainActor in failed.wrappedValue = broken }
        }
        return controller
    }

    func updateUIViewController(_ controller: AVPlayerViewController, context: Context) {}
}

private struct ZoomImage: UIViewRepresentable {
    let image: CGImage

    func makeUIView(context: Context) -> ZoomView { ZoomView() }
    func updateUIView(_ view: ZoomView, context: Context) { view.photo.image = UIImage(cgImage: image) }
}

/// Pinch and double-tap zoom, which SwiftUI has no native view for.
private final class ZoomView: UIScrollView, UIScrollViewDelegate {
    let photo = UIImageView()

    init() {
        super.init(frame: .zero)
        delegate = self
        maximumZoomScale = 4
        // Panning only once zoomed, so a swipe down at rest reaches the modal's dismiss gesture.
        isScrollEnabled = false
        showsHorizontalScrollIndicator = false
        showsVerticalScrollIndicator = false
        contentInsetAdjustmentBehavior = .never
        photo.contentMode = .scaleAspectFit
        addSubview(photo)
        let tap = UITapGestureRecognizer(target: self, action: #selector(toggle))
        tap.numberOfTapsRequired = 2
        addGestureRecognizer(tap)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError() }

    override func layoutSubviews() {
        super.layoutSubviews()
        guard zoomScale == minimumZoomScale else { return }
        photo.frame = bounds
        contentSize = bounds.size
    }

    func viewForZooming(in scrollView: UIScrollView) -> UIView? { photo }

    func scrollViewDidZoom(_ scrollView: UIScrollView) {
        isScrollEnabled = zoomScale > minimumZoomScale
    }

    @objc private func toggle() {
        setZoomScale(zoomScale > minimumZoomScale ? minimumZoomScale : 2.5, animated: true)
    }
}
#endif
