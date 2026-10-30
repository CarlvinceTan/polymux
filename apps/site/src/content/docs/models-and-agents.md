---
title: Models
slug: models-and-agents
description: Connect a model provider and choose the model your agents use.
section: Guides
sectionOrder: 1
order: 4
published: true
---

# Models

Choose your own provider, such as OpenAI, Anthropic, Google Gemini, xAI, or OpenRouter. You can also connect local models.

## Add a provider

Open **Settings → Models**, find **Providers**, and press **Add provider**.

- **Sign in** where the provider supports it.
- **API key** to use your own provider key.
- **Manual** for Ollama, LM Studio, or a compatible custom endpoint.

Enter keys in provider settings, not chat messages. Provider access and usage charges depend on the service you choose.

## Choose a model

Select a **Default model** under **Models**. Set a reasoning level if your model supports it.

Provider settings belong to the server, so your other clients use the same setup. A local model endpoint must be reachable from the **server Mac**, not only the client Mac.

## Voice

**Settings → Voice** chooses how voice works. **Cascaded** speaks with your agent directly, using the device's speech recognition and voice. **Full Duplex** uses Gemini Live or OpenAI Realtime, with the API key of your connected Gemini or OpenAI provider. Voice settings don't change the default text model.
