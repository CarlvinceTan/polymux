package server

// Rarely needed tools are deferred: they stay out of each request until the model finds them
// with tool_search, which keeps every turn's prompt small.

func (t *agentTool) Deferred() bool       { return t.action != "update" }
func (*groupTool) Deferred() bool         { return true }
func (*groupEdit) Deferred() bool         { return true }
func (*integrationTool) Deferred() bool   { return true }
func (*pluginInstallTool) Deferred() bool { return true }
func (*projectTool) Deferred() bool       { return true }
func (*triggerTool) Deferred() bool       { return true }
func (*sectionTool) Deferred() bool       { return true }
func (*mediaTool) Deferred() bool         { return true }

// Asking for a secret starts common flows; listing and forgetting them are rare.
func (t *credentialTool) Deferred() bool { return t.action != "request" }
