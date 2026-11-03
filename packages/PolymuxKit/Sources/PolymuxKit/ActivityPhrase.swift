import Foundation

/// The activity line's wording, after Grok Bot: a phrase per kind of tool, or one drawn
/// from a set while a bot thinks or works, and names when several bots are active.
public enum ActivityPhrase {
    static let thinking = ["Thinking it through", "Mulling it over", "Putting it together"]
    static let working = ["Getting it done", "Making progress", "Working on it", "Handling it", "Chipping away"]

    /// The phrase for a running tool; nil when the tool has no phrase of its own.
    static func tool(_ name: String) -> String? {
        let name = String(name.split(separator: ".").last ?? "")
        let tool = name.components(separatedBy: "__").last ?? name
        if tool.lowercased().hasPrefix("browser_") { return "Clicking through pages" }
        let key = tool.lowercased().filter { $0.isLetter || $0.isNumber }
        let phrase: String? = switch key {
        case "websearch", "search", "placessearch", "weatherforecast": "Digging around"
        case "httprequest", "webfetch", "fetch": "Doing some reading"
        case "read", "readfile", "boxread", "ls", "listfiles", "find", "glob", "grep", "skill": "Reading the file"
        case "write", "writefile", "edit", "editfile", "patch", "applypatch": "Jotting it down"
        case "bash", "shell", "boxshell", "exec", "executecommand": "Running a few commands"
        case "machineshell", "fetchconnectedactivity": "Working on your computer"
        case "copytobox", "copyfrombox", "copybotfile", "copyfile", "movefile": "Moving files over"
        case "await", "awaitshell", "wait": "Waiting on results"
        case "generateimage": "Sketching something up"
        case "generatevideo": "Filming a clip"
        case "cloudagent": "Writing some code"
        case "screenshot", "computer", "requestboxhelp": "Clicking around"
        case "messageagent", "handoffagent", "spawntask", "sendtoagent", "updateagent": "Checking with another Bot"
        case "createagent", "sendtouser", "reacttomessage", "sendimessage": "Sending a message"
        case "askuser", "requestsecret", "requestuserform": "Needs your input"
        case "remapuserformtargets": "Clicking through pages"
        case "checksubscriptionusage": "Checking your usage"
        case "manageintegrations", "callmcptool", "getmcptools", "mcpauth", "searchplugins", "getplugin",
             "installplugin", "uninstallplugin", "getmcpserverstatus", "addmcpserver", "uninstallmcpserver",
             "authenticatemcpserver", "restartmcpservers", "setmcpinstructions", "searchmcpservers",
             "installmcpserver", "enableteamserver": "Working in a connected app"
        case _ where name.hasPrefix("mcp__"): "Working in a connected app"
        default: nil
        }
        return phrase
    }

    /// A phrase from the set, fixed for one run so it doesn't change while the line is shown.
    static func draw(_ phrases: [String], seed: String) -> String {
        let index = seed.unicodeScalars.reduce(0) { ($0 &* 31 &+ Int($1.value)) & 0xFFFF }
        return phrases[index % phrases.count]
    }

    /// The whole line for the bots active in a conversation.
    public static func line(_ members: [(name: String, view: ActivityView)]) -> String {
        guard let first = members.first else { return "" }
        // A model-authored line names the step, so it replaces the agents' names in a group.
        if let status = members.compactMap({ $0.view.status }).first {
            return members.count > 1 ? status + "…" : status
        }
        guard members.count > 1 else {
            let view = first.view
            return view.phrase ?? (view.typing ? "Typing…" : draw(working, seed: first.name))
        }
        let names = members.map(\.name)
        let listed = names.count <= 3 ? names : names.prefix(2) + ["\(names.count - 2) others"]
        let joined = listed.count == 1 ? listed[0] : listed.dropLast().joined(separator: ", ") + " and " + listed.last!
        return joined + (members.allSatisfy({ $0.view.typing && $0.view.phrase == nil }) ? " are typing…" : " are working…")
    }
}
