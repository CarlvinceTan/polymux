---
title: Models and agents
slug: models-and-agents
description: Edit agent profiles in the app and configure the Go server's model separately.
section: Customising Polymux
sectionOrder: 3
order: 1
published: true
---

# Models and agents

Agent profiles and model configuration are different things. The native app lets you edit an agent's identity and instructions. The Go server configures the model used for its agent runs.

## Edit an agent

Click the agent's header in a direct conversation to open **Edit profile**. You can change its name, role, role description, and avatar. Press **Save** for profile changes. Schedule controls appear in the same panel.

Create another agent from the plus button inside **New Chat**. Agents can participate in individual or group conversations; an agent profile is not an ACP runtime selector or an operating-system security boundary.

## Configure the text model on the server

The current durable runtime uses an OpenAI-compatible provider. Its startup configuration is `model.json` in the server data directory, which defaults to `~/.polymux/data`.

For example, the file can reference a key supplied to the server process without containing the key itself:

```json
{
  "url": "https://your-model-endpoint.example/v1",
  "model": "your-model-id",
  "keyEnv": "MODEL_API_KEY"
}
```

Use the endpoint and model identifier required by your compatible service. The URL above is a placeholder, not a working provider. Supply credentials through your process or service's secret-injection mechanism, not a chat message.

`POLYMUX_MODEL_URL` and `POLYMUX_MODEL` override the file's URL and model. `POLYMUX_MODEL_KEY` takes precedence over the named `keyEnv`. An absolute `keyFile` is also supported, but must be readable only by its owner. Configuration is read at server startup; do not assume editing the file changes an active session.

The provider registry API is separate from this runtime configuration. Saving an entry in that registry does not by itself select the model used for durable agent runs.

## Voice is separate

**Settings → Voice** offers Cascaded and Full Duplex modes, with Gemini provider, model, voice, and API-key controls as applicable. Those settings do not replace the text agent's server model configuration.

The native app has no general **Providers**, **Models**, or **ACP** settings page. Older instructions for those pages describe the Electron app.
