package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/guptarohit/asciigraph"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

type timeScale int

const (
	scale1m  timeScale = 60
	scale5m  timeScale = 300
	scale15m timeScale = 900
)

var scales = []timeScale{scale1m, scale5m, scale15m}

type tickMsg time.Time
type logMsg []WinEvent
type logMetricsMsg struct{}
type pingsMsg struct {
	google     float64
	cloudflare float64
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FAFAFA")).Background(lipgloss.Color("#7D56F4")).Padding(0, 1)
	metricStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Bold(true)
	graphStyle  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 1)
	logStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	axisStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)

	// Legend Styles
	greenLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // Green
	redLabel   = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // Red
	blueLabel  = lipgloss.NewStyle().Foreground(lipgloss.Color("4")) // Blue
	cyanLabel  = lipgloss.NewStyle().Foreground(lipgloss.Color("6")) // Cyan
)

type model struct {
	logger          *slog.Logger
	logFile         *os.File
	cpuBuffer       []float64
	cpuFreq         float64
	baseFreq        float64
	diskReadBuffer  []float64
	diskWriteBuffer []float64
	netSentBuffer   []float64
	netRecvBuffer   []float64
	pingGoogleBuf   []float64
	pingCloudBuf    []float64
	memUsed         float64
	lastRead        uint64
	lastWrite       uint64
	lastSent        uint64
	lastRecv        uint64
	rawLogs         []WinEvent
	formattedLogs   []string
	seenErrors      map[string]bool
	viewport        viewport.Model
	width           int
	height          int
	ready           bool
	scaleIdx        int
}

func initialModel() model {
	// Initialize slog
	f, _ := os.OpenFile("monitor.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	logger := slog.New(slog.NewJSONHandler(f, nil))
	slog.SetDefault(logger)

	base := 0.0
	out, _ := exec.Command("powershell", "-NoProfile", "-Command", "Get-CimInstance Win32_Processor | Select-Object -ExpandProperty MaxClockSpeed").Output()
	if f, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64); err == nil {
		base = f
	}

	return model{
		logger:          logger,
		logFile:         f,
		cpuBuffer:       []float64{0, 0},
		baseFreq:        base,
		diskReadBuffer:  []float64{0, 0},
		diskWriteBuffer: []float64{0, 0},
		netSentBuffer:   []float64{0, 0},
		netRecvBuffer:   []float64{0, 0},
		pingGoogleBuf:   []float64{0.1, 0.1},
		pingCloudBuf:    []float64{0.1, 0.1},
		seenErrors:      make(map[string]bool),
		formattedLogs:   []string{helpStyle.Render("[System] Monitoring started...")},
		scaleIdx:        0,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tick(), checkLogs(), doPings(), m.logMetricsTicker())
}

func (m model) logMetricsTicker() tea.Cmd {
	return tea.Tick(10*time.Second, func(t time.Time) tea.Msg {
		return logMetricsMsg{}
	})
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func checkLogs() tea.Cmd {
	return func() tea.Msg { return logMsg(FetchErrors()) }
}

func runPing(target string) float64 {
	// 2000ms timeout
	out, err := exec.Command("ping", "-n", "1", "-w", "2000", target).Output()
	if err != nil {
		return -1.0 // Use -1.0 to represent timeout/error
	}
	re := regexp.MustCompile(`time[=<]([0-9.]+)ms`)
	matches := re.FindStringSubmatch(string(out))
	if len(matches) > 1 {
		ms, _ := strconv.ParseFloat(matches[1], 64)
		if ms == 0 {
			return 0.5
		}
		return ms
	}
	return -1.0
}

func doPings() tea.Cmd {
	return func() tea.Msg {
		return pingsMsg{
			google:     runPing("8.8.8.8"),
			cloudflare: runPing("1.1.1.1"),
		}
	}
}

func (m *model) formatAndWrapLogs() {
	if m.width == 0 {
		return
	}
	wrapWidth := m.width - 12
	if wrapWidth < 10 {
		wrapWidth = 10
	}

	wrapStyle := lipgloss.NewStyle().Width(wrapWidth)
	m.formattedLogs = nil
	m.formattedLogs = append(m.formattedLogs, helpStyle.Render("[System] Monitoring started..."))

	for _, e := range m.rawLogs {
		// Sanitize: Replace all newlines and carriage returns with spaces
		msgText := strings.ReplaceAll(e.Message, "\n", " ")
		msgText = strings.ReplaceAll(msgText, "\r", " ")
		msgText = strings.Join(strings.Fields(msgText), " ") // Collapse multiple spaces

		if msgText == "" {
			msgText = fmt.Sprintf("Event ID %d", e.Id)
		}

		// Map Level to Severity Label and Color
		sevLabel := "INFO"
		sevStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6")) // Cyan

		switch e.Level {
		case 1: // Critical
			sevLabel = "CRIT"
			sevStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true)
		case 2: // Error
			sevLabel = "ERR "
			sevStyle = redLabel.Copy().Bold(true)
		case 3: // Warning
			sevLabel = "WARN"
			sevStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")) // Orange
		}

		rawEntry := fmt.Sprintf("[%s] %s", e.ProviderName, msgText)
		wrapped := wrapStyle.Render(rawEntry)
		lines := strings.Split(wrapped, "\n")
		timestamp := helpStyle.Render(time.Now().Format("15:04"))

		for i, line := range lines {
			if i == 0 {
				m.formattedLogs = append(m.formattedLogs, fmt.Sprintf("%s %s %s", timestamp, sevStyle.Render(sevLabel), line))
			} else {
				m.formattedLogs = append(m.formattedLogs, fmt.Sprintf("           %s", line))
			}
		}
		m.formattedLogs = append(m.formattedLogs, "")
	}

	if m.ready {
		m.viewport.SetContent(strings.Join(m.formattedLogs, "\n"))
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case logMetricsMsg:
		cpuVal := 0.0
		if len(m.cpuBuffer) > 0 {
			cpuVal = m.cpuBuffer[len(m.cpuBuffer)-1]
		}
		m.logger.Info("metrics",
			"cpu_percent", cpuVal,
			"cpu_freq_mhz", m.cpuFreq,
			"ram_percent", m.memUsed,
		)
		cmds = append(cmds, m.logMetricsTicker())

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			if m.logFile != nil {
				m.logFile.Close()
			}
			return m, tea.Quit
		case "1":
			m.scaleIdx = 0
		case "2":
			m.scaleIdx = 1
		case "3":
			m.scaleIdx = 2
		}

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

		logHeight := (m.height / 2) - 2
		if logHeight < 5 {
			logHeight = 5
		}

		if !m.ready {
			m.viewport = viewport.New(m.width-4, logHeight-1)
			m.ready = true
		} else {
			m.viewport.Width, m.viewport.Height = m.width-4, logHeight-1
		}
		m.formatAndWrapLogs()
		m.viewport.GotoBottom()

	case tickMsg:
		p, _ := cpu.Percent(0, false)
		if len(p) > 0 {
			m.cpuBuffer = append(m.cpuBuffer, p[0])
		}

		if m.baseFreq > 0 {
			psCmd := "Get-CimInstance Win32_PerfFormattedData_Counters_ProcessorInformation | Where-Object { $_.Name -eq '_Total' } | Select-Object -ExpandProperty PercentProcessorPerformance"
			out, _ := exec.Command("powershell", "-NoProfile", "-Command", psCmd).Output()
			if perf, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64); err == nil {
				m.cpuFreq = (m.baseFreq * perf) / 100
			}
		}

		v, _ := mem.VirtualMemory()
		if v != nil {
			m.memUsed = v.UsedPercent
		}
		d, _ := disk.IOCounters()
		if len(d) > 0 {
			var totalRead, totalWrite uint64
			for _, io := range d {
				totalRead += io.ReadBytes
				totalWrite += io.WriteBytes
			}
			if m.lastRead > 0 {
				m.diskReadBuffer = append(m.diskReadBuffer, float64(totalRead-m.lastRead)/1024)
				m.diskWriteBuffer = append(m.diskWriteBuffer, float64(totalWrite-m.lastWrite)/1024)
			}
			m.lastRead, m.lastWrite = totalRead, totalWrite
		}
		n, _ := net.IOCounters(false)
		if len(n) > 0 {
			totalSent, totalRecv := n[0].BytesSent, n[0].BytesRecv
			if m.lastSent > 0 {
				m.netSentBuffer = append(m.netSentBuffer, float64(totalSent-m.lastSent)/1024)
				m.netRecvBuffer = append(m.netRecvBuffer, float64(totalRecv-m.lastRecv)/1024)
			}
			m.lastSent, m.lastRecv = totalSent, totalRecv
		}
		m.trimBuffers(900)
		cmds = append(cmds, tick())

	case pingsMsg:
		// Check for timeouts to log them
		if msg.google < 0 {
			m.logger.Warn("ping_timeout", "target", "8.8.8.8")
			m.rawLogs = append(m.rawLogs, WinEvent{
				ProviderName: "Network",
				Message:      "Timeout pinging Google (8.8.8.8)",
				Level:        3, // Warning
				TimeCreated:  time.Now().Format(time.RFC3339),
			})
		}
		if msg.cloudflare < 0 {
			m.logger.Warn("ping_timeout", "target", "1.1.1.1")
			m.rawLogs = append(m.rawLogs, WinEvent{
				ProviderName: "Network",
				Message:      "Timeout pinging Cloudflare (1.1.1.1)",
				Level:        3, // Warning
				TimeCreated:  time.Now().Format(time.RFC3339),
			})
		}

		m.pingGoogleBuf = append(m.pingGoogleBuf, msg.google)
		m.pingCloudBuf = append(m.pingCloudBuf, msg.cloudflare)
		if len(m.pingGoogleBuf) > 900 {
			m.pingGoogleBuf = m.pingGoogleBuf[1:]
			m.pingCloudBuf = m.pingCloudBuf[1:]
		}
		m.formatAndWrapLogs() // Refresh logs with potential timeouts
		cmds = append(cmds, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return doPings()()
		}))

	case logMsg:
		hasNew := false
		for _, e := range msg {
			key := fmt.Sprintf("%s-%d-%s", e.TimeCreated, e.Id, e.Message)
			if !m.seenErrors[key] {
				m.seenErrors[key] = true
				m.rawLogs = append(m.rawLogs, e)
				hasNew = true

				// Log the Windows event to file
				m.logger.Warn("windows_event",
					"provider", e.ProviderName,
					"id", e.Id,
					"message", e.Message,
					"time", e.TimeCreated,
				)
			}
		}
		if hasNew {
			m.formatAndWrapLogs()
			m.viewport.GotoBottom()
		}
		cmds = append(cmds, tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
			return checkLogs()()
		}))
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m *model) trimBuffers(max int) {
	if len(m.cpuBuffer) > max {
		m.cpuBuffer = m.cpuBuffer[1:]
	}
	if len(m.diskReadBuffer) > max {
		m.diskReadBuffer = m.diskReadBuffer[1:]
	}
	if len(m.diskWriteBuffer) > max {
		m.diskWriteBuffer = m.diskWriteBuffer[1:]
	}
	if len(m.netSentBuffer) > max {
		m.netSentBuffer = m.netSentBuffer[1:]
	}
	if len(m.netRecvBuffer) > max {
		m.netRecvBuffer = m.netRecvBuffer[1:]
	}
}

func (m model) downsample(data []float64, targetWidth int) []float64 {
	windowSeconds := int(scales[m.scaleIdx])
	startIdx := len(data) - windowSeconds
	if startIdx < 0 {
		startIdx = 0
	}
	activeData := data[startIdx:]
	if len(activeData) == 0 {
		return make([]float64, targetWidth)
	}

	result := make([]float64, targetWidth)
	for i := 0; i < targetWidth; i++ {
		srcIdx := float64(i) * float64(len(activeData)-1) / float64(targetWidth-1)
		if targetWidth == 1 {
			srcIdx = 0
		}
		idx := int(srcIdx)

		// Gap handling: If current point is a timeout, don't interpolate
		if activeData[idx] < 0 {
			result[i] = 0 // asciigraph doesn't support NaN well, so we'll drop to 0
			continue
		}

		if idx >= len(activeData)-1 {
			result[i] = activeData[len(activeData)-1]
		} else {
			// If neighbor is a timeout, don't interpolate (keep the gap sharp)
			if activeData[idx+1] < 0 {
				result[i] = activeData[idx]
			} else {
				frac := srcIdx - float64(idx)
				result[i] = activeData[idx]*(1-frac) + activeData[idx+1]*frac
			}
		}
	}
	return result
}

func renderTimeAxis(width int, scale timeScale) string {
	duration := time.Duration(scale) * time.Second
	start, mid, end := fmt.Sprintf("-%v", duration), fmt.Sprintf("-%v", duration/2), "Now"
	totalLen := len(start) + len(mid) + len(end)
	if width < totalLen+10 {
		return axisStyle.Render(start + " " + mid + " " + end)
	}
	spaces := (width - totalLen) / 2
	return axisStyle.Render(start + strings.Repeat(" ", spaces) + mid + strings.Repeat(" ", width-spaces-totalLen) + end)
}

func getMinMax(data []float64) (float64, float64) {
	if len(data) == 0 {
		return 0, 0
	}
	min, max := -1.0, -1.0
	for _, v := range data {
		if v < 0 {
			continue // Ignore timeouts in min/max
		}
		if min < 0 || v < min {
			min = v
		}
		if max < 0 || v > max {
			max = v
		}
	}
	if min < 0 {
		min = 0
	}
	if max < 0 {
		max = 0
	}
	return min, max
}

func (m model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	currentScale := scales[m.scaleIdx]

	// Rigid Top Bar
	header := titleStyle.Render("SYSTEM MONITOR")
	scaleStr := fmt.Sprintf("%-6v", time.Duration(currentScale)*time.Second)
	ramStr := fmt.Sprintf("%4.1f%%", m.memUsed)
	timeStr := time.Now().Format("15:04:05")
	metrics := metricStyle.Render(fmt.Sprintf(" RAM: %s  Scale: %s  %s", ramStr, scaleStr, timeStr))
	topBar := header + " " + metrics

	// Fixed dimensions logic
	logOuterH := (m.height / 2) - 2
	if logOuterH < 5 {
		logOuterH = 5
	}

	graphAreaH := m.height - logOuterH - 4
	boxW := m.width / 2
	boxH := graphAreaH / 2

	innerW := boxW - 12
	innerH := boxH - 4
	if innerW < 10 {
		innerW = 10
	}
	if innerH < 2 {
		innerH = 2
	}

	boxStyle := graphStyle.Copy().
		Width(boxW - 2).
		Height(boxH - 1).
		MaxWidth(boxW - 2).
		MaxHeight(boxH - 1).
		Padding(0, 1)

	// CPU
	cpuVal := 0.0
	if len(m.cpuBuffer) > 0 {
		cpuVal = m.cpuBuffer[len(m.cpuBuffer)-1]
	}
	_, cpuMax := getMinMax(m.cpuBuffer)
	cpuD := m.downsample(m.cpuBuffer, innerW)
	cpuCaption := fmt.Sprintf("CPU: %4.1f%% (Max: %4.1f) | Freq: %4.0f MHz", cpuVal, cpuMax, m.cpuFreq)
	cpuG := boxStyle.Render(asciigraph.Plot(cpuD, asciigraph.Height(innerH), asciigraph.Width(innerW), asciigraph.Caption(cpuCaption), asciigraph.Precision(1)))

	// Disk
	dRead, dWrite := 0.0, 0.0
	if len(m.diskReadBuffer) > 0 {
		dRead = m.diskReadBuffer[len(m.diskReadBuffer)-1]
	}
	if len(m.diskWriteBuffer) > 0 {
		dWrite = m.diskWriteBuffer[len(m.diskWriteBuffer)-1]
	}
	_, drMax := getMinMax(m.diskReadBuffer)
	_, dwMax := getMinMax(m.diskWriteBuffer)
	dr, dw := m.downsample(m.diskReadBuffer, innerW), m.downsample(m.diskWriteBuffer, innerW)
	diskCaption := fmt.Sprintf("Disk: R %5.1f / W %5.1f KB/s (Max: %5.1f/%5.1f)", dRead, dWrite, drMax, dwMax)
	diskG := boxStyle.Render(asciigraph.PlotMany([][]float64{dr, dw}, asciigraph.Height(innerH), asciigraph.Width(innerW), asciigraph.Caption(diskCaption), asciigraph.SeriesColors(asciigraph.Green, asciigraph.Red), asciigraph.Precision(0)))

	// Net
	nSent, nRecv := 0.0, 0.0
	if len(m.netSentBuffer) > 0 {
		nSent = m.netSentBuffer[len(m.netSentBuffer)-1]
	}
	if len(m.netRecvBuffer) > 0 {
		nRecv = m.netRecvBuffer[len(m.netRecvBuffer)-1]
	}
	_, nsMax := getMinMax(m.netSentBuffer)
	_, nrMax := getMinMax(m.netRecvBuffer)
	ns, nr := m.downsample(m.netSentBuffer, innerW), m.downsample(m.netRecvBuffer, innerW)
	netCaption := fmt.Sprintf("Net: U %5.1f / D %5.1f KB/s (Max: %5.1f/%5.1f)", nSent, nRecv, nsMax, nrMax)
	netG := boxStyle.Render(asciigraph.PlotMany([][]float64{ns, nr}, asciigraph.Height(innerH), asciigraph.Width(innerW), asciigraph.Caption(netCaption), asciigraph.SeriesColors(asciigraph.Green, asciigraph.Red), asciigraph.Precision(0)))

	// Latency
	pGoogle, pCloud := 0.0, 0.0
	if len(m.pingGoogleBuf) > 0 {
		pGoogle = m.pingGoogleBuf[len(m.pingGoogleBuf)-1]
	}
	if len(m.pingCloudBuf) > 0 {
		pCloud = m.pingCloudBuf[len(m.pingCloudBuf)-1]
	}
	_, pgMax := getMinMax(m.pingGoogleBuf)
	_, pcMax := getMinMax(m.pingCloudBuf)
	pg, pc := m.downsample(m.pingGoogleBuf, innerW), m.downsample(m.pingCloudBuf, innerW)
	
	// Format ping values for caption, showing 'TO' if it was a timeout
	pGStr, pCStr := fmt.Sprintf("%4.1f", pGoogle), fmt.Sprintf("%4.1f", pCloud)
	if pGoogle < 0 { pGStr = "  TO" }
	if pCloud < 0 { pCStr = "  TO" }
	
	pingCaption := fmt.Sprintf("Ping: G %s / C %s ms (Max: %4.1f/%4.1f)", pGStr, pCStr, pgMax, pcMax)
	pingG := boxStyle.Render(asciigraph.PlotMany([][]float64{pg, pc}, asciigraph.Height(innerH), asciigraph.Width(innerW), asciigraph.Caption(pingCaption), asciigraph.SeriesColors(asciigraph.Blue, asciigraph.Cyan), asciigraph.Precision(0)))

	logHeader := lipgloss.NewStyle().Bold(true).PaddingLeft(1).Render("EVENT LOGS (Last 1h)")
	logPane := logStyle.Width(m.width - 2).Height(logOuterH).MaxWidth(m.width - 2).MaxHeight(logOuterH).Render(lipgloss.JoinVertical(lipgloss.Left, logHeader, m.viewport.View()))

	row1 := lipgloss.JoinHorizontal(lipgloss.Top, cpuG, diskG)
	row2 := lipgloss.JoinHorizontal(lipgloss.Top, netG, pingG)

	axisContent := renderTimeAxis(m.width-4, currentScale)
	timeAxis := lipgloss.NewStyle().Width(m.width-2).PaddingLeft(1).Render(axisContent)

	graphPane := lipgloss.JoinVertical(lipgloss.Left, row1, row2, timeAxis)

	return lipgloss.JoinVertical(lipgloss.Left, topBar, logPane, graphPane, helpStyle.Render(" 1,2,3: Scale • q: Quit"))
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
	}
}
