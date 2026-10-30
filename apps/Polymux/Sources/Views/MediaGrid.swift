import SwiftUI
import PolymuxKit

struct MediaGrid: View {
    let attachments: [Attachment]
    let client: APIClient
    let choose: (Attachment) -> Void

    var body: some View {
        LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 4), count: 3), spacing: 4) {
            ForEach(attachments, id: \.id) { attachment in
                GeometryReader { geometry in
                    Button { choose(attachment) } label: {
                        AttachmentPreview(attachment: attachment, client: client, showsName: false,
                                          square: geometry.size.width, radius: PanelStyle.cardRadius)
                            .overlay(alignment: .bottomLeading) {
                                if attachment.kind == AttachmentKind.video {
                                    Image(systemName: "play.fill")
                                        .font(.caption)
                                        .foregroundStyle(.white)
                                        .padding(6)
                                        .background(.black.opacity(0.65), in: Circle())
                                        .padding(6)
                                }
                            }
                            .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Show \(attachment.name) in chat")
                }
                .aspectRatio(1, contentMode: .fit)
            }
        }
    }
}
