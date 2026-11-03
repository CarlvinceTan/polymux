import Foundation

/// Reading and changing the people behind a reaction.
public enum HubReactions {
    private static let shown = 3
    public static let you = HubMember(id: "you", name: "You", avatar: "")

    /// Reads as a sentence: "Sam and Noor", or "You, Maya, Leo and 1 other".
    public static func names(_ people: [HubMember]) -> String {
        let list = people.map(\.name)
        if list.count > shown {
            let rest = list.count - shown
            return list.prefix(shown).joined(separator: ", ") + " and \(rest) other" + (rest == 1 ? "" : "s")
        }
        guard let last = list.last else { return "" }
        return list.count == 1 ? last : list.dropLast().joined(separator: ", ") + " and " + last
    }

    /// The bar straight after this account's tap, ahead of the server's answer.
    public static func toggled(_ reactions: [HubReaction], key: String) -> [HubReaction] {
        var out = reactions
        guard let index = out.firstIndex(where: { $0.key == key }) else {
            return out + [HubReaction(key: key, count: 1, mine: true, people: [you])]
        }
        if out[index].mine {
            out[index].mine = false
            out[index].count -= 1
            out[index].people.removeAll { $0.name == you.name }
            if out[index].count <= 0 { out.remove(at: index) }
        } else {
            out[index].mine = true
            out[index].count += 1
            out[index].people.insert(you, at: 0)
        }
        return out
    }
}
