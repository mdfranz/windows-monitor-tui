# 3rd Party Libraries

This document lists the third-party libraries used in the Windows Monitor TUI project, classified by their primary purpose.

## TUI Framework & Styling
Libraries used for building the terminal user interface, handling input, and applying styles/colors.

- **[Bubble Tea](https://github.com/charmbracelet/bubbletea)**: A Go framework based on the Elm architecture, used for managing the TUI lifecycle and events.
- **[Lip Gloss](https://github.com/charmbracelet/lipgloss)**: A library for defining terminal styles using CSS-like primitives.
- **[Bubbles](https://github.com/charmbracelet/bubbles)**: A collection of common TUI components (used specifically for the `viewport` component).

## System Metrics & Monitoring
Libraries used to retrieve real-time performance data from the operating system.

- **[gopsutil](https://github.com/shirou/gopsutil)**: A Go port of psutil, used to gather CPU, memory, disk, and network statistics.
  - `gopsutil/v3/cpu`
  - `gopsutil/v3/disk`
  - `gopsutil/v3/mem`
  - `gopsutil/v3/net`

## Data Visualization
Libraries used for rendering graphical data in the terminal.

- **[asciigraph](https://github.com/guptarohit/asciigraph)**: Used to generate lightweight ASCII line graphs for performance metrics.

## Windows Specific Operations
Libraries providing specialized Windows API interactions.

- **[winops](https://github.com/google/winops)**: (Transitive/Dependency) Google's Windows operations library for Go.

## Indirect Dependencies
Other supporting libraries managed by the Go module system:
- `github.com/go-ole/go-ole`: Win32 COM implementation for Go (required by gopsutil).
- `github.com/yusufpapurcu/wmi`: WMI (Windows Management Instrumentation) client for Go.
- `github.com/muesli/termenv`: Advanced terminal environment features.
- `github.com/muesli/ansi`: ANSI escape code processing.
