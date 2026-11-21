package privacy

// Items reports the macOS permissions as this process sees them. macOS grants them to an app, not
// to a command line, so from a terminal they are the terminal's; the app shows the server's.
func Items() []Item {
	status := Check()
	pane := func(name string) []string {
		return []string{"open 'x-apple.systempreferences:com.apple.settings.PrivacySecurity.extension?" + name + "'"}
	}
	return []Item{
		{Name: "Full Disk Access", OK: status.FullDiskAccess, Detail: "protected folders", Fix: pane("Privacy_AllFiles")},
		{Name: "Screen Recording", OK: status.ScreenRecording, Detail: "screen, windows and audio", Fix: pane("Privacy_ScreenCapture")},
		{Name: "Accessibility", OK: status.Accessibility, Detail: "reading and controlling apps", Fix: pane("Privacy_Accessibility")},
	}
}
