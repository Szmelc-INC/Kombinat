# ARCHITECTURE SPECIFICATION: KOMBINAT 2.0 (MkII) -> v3 "LOGOS"
> **Org:** [Szmelc™ / Szmelc-INC](https://github.com/Szmelc-INC)
> **Leadership:** `Sernik` (CEO) | `Polski Kotek` (CTO) | `Henderson` (CDO)
> **Core Runtime:** `Go` + `PHP` (POSIX/Unix-first: Arch > Nix > WSL > macOS/zsh)

---

## 1. ONTOLOGY (What It Is)
* **Minimalist Kernel + Infinite Plugin Topology:** A single, central API & backend spine surrounded by an ecosystem of hot-swappable, toggleable modules (`JSON`/`TOML` configured). Everything beyond the bare-metal core is a plugin.
* **The "Ultimate" Self-Contained Module Format:** Every module is a complete, atomic micro-ecosystem bundling code, versioned templates, documentation, and embedded meta-agent prompts (`AGENTS.md` / `SKILL.md`) for autonomous setup, debugging, and operation.
* **Substrate-Agnostic Compute ("Infinite Tokens"):** Compute is treated as a fungible, routed utility. Via `szmelc-router` and wrapper APIs (`WrAPI` interfacing Google AI Mode, Meta AI, etc.), the system abstracts away vendor lock-in and token scarcity.

---

## 2. EPISTEMOLOGY (How It Knows, Governs & Remembers)
* **Deterministic Guardrails over Stochastic Agents:**
  * `Assertions` (Assertion-Driven Architecture): Hard logical invariants enforcing predictable agent behavior.
  * `Capabilities`: Strict allow-listing; agents cannot execute actions unless explicitly registered.
  * `Policies`: Risk-threshold gating and multi-layer access control.
* **Total Observability & State Persistence:**
  * `Memory 5`: Unified persistence layer combining Vector DBs, RAG, journals, and structured execution logs.
  * `Live Terminal`: Real-time, unfiltered verbose telemetry exposing every agent thought, tool call, and context window.
  * `Evaluations`: Continuous multi-dimensional feedback loops scoring outputs and system states.
* **Bilingual Semantic Mapping (`EN` + `PL`):** English serves as the strict structural/code substrate; Polish acts as the native conversational layer, bridged dynamically via automated `translate-shell` localization packs.

---

## 3. METHODOLOGY (How It Builds & Collaborates)
* **CI/CD Iterative Unification:** Core framework first → incremental porting of legacy `v1 "Szmelc"` chaos into strict `v2` modules without breaking system integrity.
* **Decentralized Distribution (`KUR` — Kombinat User Repositories):** A GitHub-native package manager and marketplace enabling frictionless sharing of modules, presets, and configs.
* **Multi-Agent & Multi-Operator Co-Op:**
  * `AGENTS.md` Conventions: Shared behavioral contracts allowing multiple AI agents to develop concurrently without merge conflicts or duplicated effort.
  * `Co-Op Multiplayer & Discord C2`: Two-way command-and-control via Discord (Components v2) enabling Agent-to-Agent (`A2A`) messaging and shared task dashboards across independent Kombinat instances.

---

## 4. METAPHYSICS & TELEOLOGY (The Path to v3 "Logos")
* **From "Command Center" to Universal Substrate:** Kombinat 2.0 is the scaffolding ("SkyNET / Centrum Dowodzenia Wszechświatem") that tames raw entropy into modular order.
* **Autonomous Self-Assembly:** Through `Proposals` (proactive action generation) + `Schedule` (temporal execution) + `Fleet` (universal model/tool wrapping), the system transitions from a passive tool into an active, self-orchestrating organism.
* **The Absolute Horizon (`v3 "Logos"`):** The point where `v2` conventions become completely transparent — a zero-friction, self-indexing, self-fuzzing meta-protocol where human intent compiles directly into distributed execution.

---

### ARCHITECTURAL STACK MATRIX

| Layer | v2 Implementation (`Kombinat MkII`) | Absolute Ideal (`v3 "Logos"`) |
| :--- | :--- | :--- |
| **Core & Compute** | Central Go/PHP API + `szmelc-router` / `WrAPI` | Infinite, zero-cost, substrate-free compute mesh |
| **Modularity** | `KUR` GitHub Market + `JSON`/`TOML` Plugins | Self-assembling, auto-fuzzing fractal holons |
| **Control & Safety** | `Assertions` + `Capabilities` + `Policies` | Provably deterministic execution contracts |
| **Interface & C2** | Modular WebUI, TUI, Live Terminal, Discord Bot | Direct frictionless operator-to-system symbiosis |
| **Memory & Loop** | `Memory 5` + `Schedule` + `Proposals` + `Evals` | Autonomous, continuous self-improving ontology |
