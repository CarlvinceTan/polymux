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

## Fallbacks

Each bot decides what happens when its model can't answer. Open the bot's profile, choose **Capabilities**, and press the gear beside **Model**.

- **Auto** moves to the same model on another provider, then to each remaining provider's preferred model, following your provider order. New bots start here.
- **Custom** tries the models you list, in order, each with its own reasoning level.
- **Off** stays on the bot's model.

When every model a bot can use is out of allowance, rate-limited or down, the bot waits rather than failing, and carries on as soon as one is back. That's at the reset time the provider gave, or within two minutes when it gave none, such as after a top-up. Sending a message makes it check straight away. A bad API key still stops the reply with an error.

## Voice

**Settings → Voice** uses Gemini Live or OpenAI Realtime with the API key of your connected Gemini or OpenAI provider. Talk naturally and interrupt replies; the live spokesperson consults your agents for tasks and context. Voice settings don't change the default text model.
