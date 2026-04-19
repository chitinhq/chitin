# Operating Model

- **Primary execution box:** a single local RTX 3090 workstation. All
  Phase 1 telemetry lives on this machine. Capture and replay work fully
  offline.
- **Local inference:** local Ollama runs on the 3090 for bounded inference
  and execution support. Execution resource, not a telemetry dependency.
- **Cloud reasoning:** Ollama Cloud Pro and Claude provide cloud-side
  reasoning and planning. Not a source of ground truth for telemetry.
- **Observability vs. execution:** Chitin initially observes existing
  agent surfaces instead of replacing them. It does not take over the
  agent loop; it captures what the agent does.
- **Order of operations:** observability → governance → automation, in
  that order, per surface.
