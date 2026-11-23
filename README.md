# agentport

English | [Português (Brasil)](README.pt-BR.md) | [Español](README.es.md) | [繁體中文](README.zh-Hant.md)

agentport will let a coding agent running on an SSH server use the ports of
your machine, with your authorization: see what is listening and open or close
forwards through the SSH tunnel that already exists. It runs a local HTTP
bridge, a reverse SSH tunnel and an MCP server over `stdio`, registered
automatically in the agents.

> **Work in progress.** The project still carries the AgentClip base: the
> Companion, the tunnel, the agent registration and the releases work, but the
> port tools do not exist yet. The MCP server exposes only `agentport_status`.

## Credits

agentport started as a fork of [AgentClip](https://github.com/wendellrocha/agentclip), by [Wendell Rocha](https://github.com/wendellrocha), distributed under the MIT license. We reuse the Companion base, the reverse SSH tunnel, the MCP server over stdio, the automatic registration in the agents and the verifiable release pipeline. Thanks to the original project. 💙
