import Foundation

/// The silhouette a node takes. Every kind lands in one, so a new kind never draws blank.
public enum WorkShape: String, Sendable, CaseIterable {
    /// Who does the work: a person, a bot or an outside agent.
    case actor
    /// Where work happens: an app, a device, a server or a repository.
    case place
    /// What work touches or makes: a file, a task or a pull request.
    case thing
}

/// Kinds that share a colour and one entry in the legend.
public enum WorkFamily: String, Sendable, CaseIterable, Codable {
    case people, bots, agents, apps, files, media, data, code, devices, tasks

    public var title: String {
        switch self {
        case .people: "People"
        case .bots: "Bots"
        case .agents: "Agents"
        case .apps: "Apps"
        case .files: "Files"
        case .media: "Media"
        case .data: "Data"
        case .code: "Code"
        case .devices: "Devices"
        case .tasks: "Tasks"
        }
    }

    public var symbol: String {
        switch self {
        case .people: "person.crop.circle"
        case .bots: "face.smiling"
        case .agents: "sparkle"
        case .apps: "macwindow"
        case .files: "doc"
        case .media: "photo"
        case .data: "cylinder.split.1x2"
        case .code: "arrow.triangle.branch"
        case .devices: "laptopcomputer"
        case .tasks: "checklist"
        }
    }
}

/// How one kind of node or cargo looks, before a report overrides any of it.
public struct WorkLook: Hashable, Sendable {
    public var title: String
    public var symbol: String
    public var family: WorkFamily
    public var shape: WorkShape
}

/// Everything the graph knows how to draw. A kind is a word, so reports can use ones added later.
public enum WorkKind {
    public static let person = "person"
    public static let agent = "agent"
    public static let worker = "worker"
    public static let app = "app"
    public static let terminal = "terminal"
    public static let web = "web"
    public static let tab = "tab"
    public static let tool = "tool"
    public static let plugin = "plugin"
    public static let files = "files"
    public static let studio = "studio"
    public static let group = "group"
    public static let file = "file"
    public static let document = "document"
    public static let code = "code"
    public static let pdf = "pdf"
    public static let archive = "archive"
    public static let folder = "folder"
    public static let design = "design"
    public static let image = "image"
    public static let video = "video"
    public static let audio = "audio"
    public static let data = "data"
    public static let table = "table"
    public static let link = "link"
    public static let repo = "repo"
    public static let github = "github"
    public static let pull = "pull"
    public static let issue = "issue"
    public static let device = "device"
    public static let phone = "phone"
    public static let tablet = "tablet"
    public static let laptop = "laptop"
    public static let desktop = "desktop"
    public static let server = "server"
    public static let task = "task"
    public static let pipeline = "pipeline"
    public static let step = "step"
    public static let message = "message"
    public static let question = "question"
    public static let result = "result"
    public static let status = "status"
    public static let command = "command"

    private static let looks: [String: WorkLook] = [
        person: WorkLook(title: "Person", symbol: "person.crop.circle", family: .people, shape: .actor),
        agent: WorkLook(title: "Bot", symbol: "face.smiling", family: .bots, shape: .actor),
        worker: WorkLook(title: "Agent", symbol: "sparkle", family: .agents, shape: .actor),
        app: WorkLook(title: "App", symbol: "macwindow", family: .apps, shape: .place),
        terminal: WorkLook(title: "Terminal", symbol: "terminal", family: .apps, shape: .place),
        web: WorkLook(title: "Web", symbol: "globe", family: .apps, shape: .place),
        plugin: WorkLook(title: "Plugin", symbol: "puzzlepiece.extension", family: .apps, shape: .place),
        files: WorkLook(title: "Files", symbol: "folder", family: .files, shape: .place),
        studio: WorkLook(title: "Media", symbol: "photo.on.rectangle", family: .media, shape: .place),
        group: WorkLook(title: "Group", symbol: "square.stack", family: .bots, shape: .place),
        tab: WorkLook(title: "Tab", symbol: "macwindow", family: .apps, shape: .thing),
        tool: WorkLook(title: "Tool call", symbol: "wrench.and.screwdriver", family: .apps, shape: .thing),
        command: WorkLook(title: "Command", symbol: "chevron.right", family: .apps, shape: .thing),
        file: WorkLook(title: "File", symbol: "doc", family: .files, shape: .thing),
        document: WorkLook(title: "Document", symbol: "doc.text", family: .files, shape: .thing),
        code: WorkLook(title: "Code", symbol: "chevron.left.forwardslash.chevron.right", family: .files, shape: .thing),
        pdf: WorkLook(title: "PDF", symbol: "doc.richtext", family: .files, shape: .thing),
        archive: WorkLook(title: "Archive", symbol: "archivebox", family: .files, shape: .thing),
        folder: WorkLook(title: "Folder", symbol: "folder", family: .files, shape: .thing),
        design: WorkLook(title: "Design", symbol: "paintbrush.pointed", family: .media, shape: .thing),
        image: WorkLook(title: "Image", symbol: "photo", family: .media, shape: .thing),
        video: WorkLook(title: "Video", symbol: "film", family: .media, shape: .thing),
        audio: WorkLook(title: "Audio", symbol: "waveform", family: .media, shape: .thing),
        data: WorkLook(title: "Data", symbol: "cylinder.split.1x2", family: .data, shape: .thing),
        table: WorkLook(title: "Table", symbol: "tablecells", family: .data, shape: .thing),
        link: WorkLook(title: "Link", symbol: "link", family: .data, shape: .thing),
        repo: WorkLook(title: "Repository", symbol: "shippingbox", family: .code, shape: .place),
        github: WorkLook(title: "GitHub", symbol: "arrow.triangle.branch", family: .code, shape: .place),
        pull: WorkLook(title: "Pull request", symbol: "arrow.triangle.pull", family: .code, shape: .thing),
        issue: WorkLook(title: "Issue", symbol: "exclamationmark.circle", family: .code, shape: .thing),
        device: WorkLook(title: "Device", symbol: "laptopcomputer", family: .devices, shape: .place),
        phone: WorkLook(title: "Phone", symbol: "iphone", family: .devices, shape: .place),
        tablet: WorkLook(title: "Tablet", symbol: "ipad", family: .devices, shape: .place),
        laptop: WorkLook(title: "Laptop", symbol: "laptopcomputer", family: .devices, shape: .place),
        desktop: WorkLook(title: "Desktop", symbol: "desktopcomputer", family: .devices, shape: .place),
        server: WorkLook(title: "Server", symbol: "server.rack", family: .devices, shape: .place),
        task: WorkLook(title: "Task", symbol: "checklist", family: .tasks, shape: .thing),
        pipeline: WorkLook(title: "Pipeline", symbol: "point.topleft.down.to.point.bottomright.curvepath", family: .tasks, shape: .place),
        step: WorkLook(title: "Step", symbol: "flag", family: .tasks, shape: .place),
        message: WorkLook(title: "Message", symbol: "bubble.left", family: .tasks, shape: .thing),
        question: WorkLook(title: "Question", symbol: "questionmark.bubble", family: .tasks, shape: .thing),
        result: WorkLook(title: "Result", symbol: "checkmark.seal", family: .tasks, shape: .thing),
        status: WorkLook(title: "Status", symbol: "info.circle", family: .tasks, shape: .thing),
    ]

    private static let unknown = WorkLook(title: "", symbol: "circle.dashed", family: .apps, shape: .thing)

    /// An unknown kind draws as a plain thing, so an older app still shows what a newer report sent.
    public static func look(_ kind: String?) -> WorkLook {
        looks[kind?.lowercased() ?? ""] ?? unknown
    }

    public static func known(_ kind: String?) -> Bool { looks[kind?.lowercased() ?? ""] != nil }

    /// How one node draws: its kind's look, unless the report names its own icon or shape.
    public static func look(of node: WorkNode) -> WorkLook {
        var look = look(node.kind)
        if let symbol = node.symbol, !symbol.isEmpty { look.symbol = symbol }
        if node.agentID?.isEmpty == false { look.shape = .actor }
        switch node.shape {
        case WorkNodeShape.circle: look.shape = .actor
        case WorkNodeShape.pill: look.shape = .thing
        case WorkNodeShape.card: look.shape = .place
        default: break
        }
        return look
    }

    private static let extensions: [String: [String]] = [
        code: ["swift", "go", "py", "js", "jsx", "ts", "tsx", "mjs", "rs", "c", "h", "cc", "cpp", "m", "java", "kt", "rb", "php", "sh", "zsh", "sql",
               "html", "css", "scss", "svelte", "vue", "lua", "dart", "cs", "toml", "yaml", "yml", "lock", "mod", "gradle", "xml", "plist"],
        document: ["md", "markdown", "txt", "rtf", "doc", "docx", "pages", "odt", "tex", "log"],
        pdf: ["pdf"],
        image: ["png", "jpg", "jpeg", "gif", "webp", "heic", "tif", "tiff", "bmp", "svg", "ico", "avif"],
        video: ["mp4", "mov", "m4v", "webm", "mkv", "avi"],
        audio: ["mp3", "wav", "m4a", "aac", "flac", "ogg", "opus", "aiff"],
        table: ["csv", "tsv", "xls", "xlsx", "numbers", "parquet"],
        data: ["json", "jsonl", "ndjson", "db", "sqlite", "sqlite3", "kdbx", "bin", "dat", "proto"],
        archive: ["zip", "tgz", "gz", "tar", "bz2", "xz", "7z", "dmg", "pkg", "ipa"],
        design: ["fig", "sketch", "psd", "ai", "xd", "key", "ppt", "pptx"],
    ]

    private static let byExtension: [String: String] = {
        var out: [String: String] = [:]
        for (kind, list) in extensions { for item in list { out[item] = kind } }
        return out
    }()

    /// The kind of device a name suggests, since a name is all a paired device reports.
    public static func device(named name: String) -> String {
        let words = name.lowercased()
        if words.contains("iphone") || words.contains("phone") || words.contains("pixel") || words.contains("galaxy") { return phone }
        if words.contains("ipad") || words.contains("tablet") { return tablet }
        if words.contains("macbook") || words.contains("laptop") || words.contains("book") { return laptop }
        if words.contains("imac") || words.contains("mini") || words.contains("studio") || words.contains("mac pro") || words.contains("desktop") { return desktop }
        return device
    }

    /// The kind of file a path names, from its extension. A trailing slash is a folder.
    public static func file(at path: String) -> String {
        let trimmed = path.trimmingCharacters(in: .whitespaces)
        if trimmed.hasSuffix("/") { return folder }
        let name = trimmed.split(separator: "/").last.map(String.init) ?? trimmed
        guard let dot = name.lastIndex(of: "."), dot != name.startIndex else { return file }
        return byExtension[name[name.index(after: dot)...].lowercased()] ?? file
    }
}
