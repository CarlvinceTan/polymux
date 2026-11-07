import CoreTransferable
import Foundation
import UniformTypeIdentifiers

public enum SidebarItem: Codable, Equatable, Sendable, Transferable {
    case chat(String)
    case section(String)

    public static let contentType = UTType(exportedAs: "com.flarehq.polymux.sidebar-item", conformingTo: .data)

    public static var transferRepresentation: some TransferRepresentation {
        CodableRepresentation(contentType: contentType)
    }

    public func provider() -> NSItemProvider {
        let provider = NSItemProvider()
        provider.registerDataRepresentation(forTypeIdentifier: Self.contentType.identifier, visibility: .all) { received in
            do { received(try JSONEncoder().encode(self), nil) }
            catch { received(nil, error) }
            return nil
        }
        return provider
    }
}
