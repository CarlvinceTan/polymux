#if os(macOS)
import AppKit
import SwiftUI

struct RowMenu: NSViewRepresentable {
    let profile: () -> Void
    var pinned = false
    var pin: (() -> Void)? = nil
    var avatar: (() -> Void)? = nil
    var delete: (() -> Void)? = nil
    var title = "Edit profile"

    func makeNSView(context: Context) -> MenuAnchor {
        let view = MenuAnchor()
        updateNSView(view, context: context)
        return view
    }

    func updateNSView(_ view: MenuAnchor, context: Context) {
        view.profile = profile
        view.pinned = pinned
        view.pin = pin
        view.avatar = avatar
        view.delete = delete
        view.title = title
        view.enabled = context.environment.isEnabled
        view.attach()
    }

    static func dismantleNSView(_ view: MenuAnchor, coordinator: ()) { view.detach() }
}

final class MenuAnchor: NSView {
    var profile: (() -> Void)?
    var pinned = false
    var pin: (() -> Void)?
    var avatar: (() -> Void)?
    var delete: (() -> Void)?
    var title = "Edit profile"
    var enabled = true
    private var keyboardMenu: TableMenu?

    override var acceptsFirstResponder: Bool { false }

    override func hitTest(_ point: NSPoint) -> NSView? {
        guard enabled, let event = NSApp.currentEvent,
              event.type == .rightMouseDown || (event.type == .leftMouseDown && event.modifierFlags.contains(.control)) else { return nil }
        return super.hitTest(point)
    }

    override func rightMouseDown(with event: NSEvent) { showMenu(event) }
    override func mouseDown(with event: NSEvent) { showMenu(event) }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        if window == nil { detach() } else { attach() }
    }

    override func viewDidMoveToSuperview() {
        super.viewDidMoveToSuperview()
        attach()
    }

    func attach() {
        guard window != nil, keyboardMenu == nil else { return }
        var ancestor = superview
        while let view = ancestor {
            if let table = view as? NSTableView {
                if let existing = table.menu?.delegate as? TableMenu {
                    keyboardMenu = existing
                    existing.rows.add(self)
                } else if table.menu == nil {
                    let owner = TableMenu(table: table)
                    keyboardMenu = owner
                    owner.rows.add(self)
                    table.menu = owner.menu
                }
                return
            }
            ancestor = view.superview
        }
    }

    func detach() {
        if let owner = keyboardMenu {
            owner.rows.remove(self)
            if owner.rows.count == 0, owner.table?.menu === owner.menu { owner.table?.menu = nil }
        }
        keyboardMenu = nil
    }

    func populate(_ menu: NSMenu) {
        menu.removeAllItems()
        menu.autoenablesItems = false
        if pin != nil { add(pinned ? "Unpin" : "Pin", symbol: pinned ? "pin.slash" : "pin", action: #selector(togglePin), to: menu) }
        add(title, symbol: "pencil", action: #selector(editProfile), to: menu)
        if avatar != nil { add("Customise avatar", symbol: "face.smiling", action: #selector(editAvatar), to: menu) }
        if delete != nil {
            menu.addItem(.separator())
            let item = add("Delete", symbol: "trash", action: #selector(deleteAgent), to: menu)
            item.attributedTitle = NSAttributedString(string: "Delete", attributes: [.foregroundColor: NSColor.systemRed])
        }
    }

    @discardableResult private func add(_ title: String, symbol: String, action: Selector, to menu: NSMenu) -> NSMenuItem {
        let item = NSMenuItem(title: title, action: action, keyEquivalent: "")
        item.target = self
        item.isEnabled = enabled
        item.image = NSImage(systemSymbolName: symbol, accessibilityDescription: nil)
        menu.addItem(item)
        return item
    }

    private func showMenu(_ event: NSEvent) {
        guard enabled else { return }
        let menu = NSMenu()
        populate(menu)
        NSMenu.popUpContextMenu(menu, with: event, for: self)
    }

    @objc private func togglePin() { pin?() }
    @objc private func editProfile() { profile?() }
    @objc private func editAvatar() { avatar?() }
    @objc private func deleteAgent() { delete?() }
}

@MainActor
final class TableMenu: NSObject, NSMenuDelegate {
    weak var table: NSTableView?
    let rows = NSHashTable<MenuAnchor>.weakObjects()
    let menu = NSMenu()

    init(table: NSTableView) {
        self.table = table
        super.init()
        menu.delegate = self
    }

    func menuNeedsUpdate(_ menu: NSMenu) {
        menu.removeAllItems()
        guard let table, table.selectedRow >= 0 else { return }
        for row in rows.allObjects where table.row(for: row) == table.selectedRow {
            row.populate(menu)
            return
        }
    }
}
#endif
