/// One entry in the + menu.
struct PluginAction {
    let title: String
    let symbol: String
    /// Replaces the symbol on iOS, where the menu can show an image.
    var icon: AvatarIcon?
    let run: () -> Void
}
