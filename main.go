package main

import (
	"fmt"
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
	scale1Min  timeScale = 60
	scale5Min  timeScale = 300
	scale15Min timeScale = 900
)

type tickMsg time.Time
type logMsg []string
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
	cpuBuffer       []float64
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
	logs            []string
	seenErrors      map[string]bool
	viewport        viewport.Model
	width           int
	height          int
	graphHeight     int
	ready           bool
	scale           timeScale
}

func initialModel() model {
	return model{
		cpuBuffer:       []float64{0, 0},
		diskReadBuffer:  []float64{0, 0},
		diskWriteBuffer: []float64{0, 0},
		netSentBuffer:   []float64{0, 0},
		netRecvBuffer:   []float64{0, 0},
		pingGoogleBuf:   []float64{0.1, 0.1},
		pingCloudBuf:    []float64{0.1, 0.1},
		seenErrors:      make(map[string]bool),
		logs:            []string{"[System] Monitoring started..."},
		scale:           scale1Min,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tick(), checkLogs(m.seenErrors), doPings())
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func checkLogs(seen map[string]bool) tea.Cmd {
	return func() tea.Msg { return logMsg(FetchErrors(seen)) }
}

func runPing(target string) float64 {
	out, err := exec.Command("ping", "-n", "1", "-w", "2000", target).Output()
	if err != nil { return 0 }
	re := regexp.MustCompile(`time[=<]([0-9.]+)ms`)
	matches := re.FindStringSubmatch(string(out))
	if len(matches) > 1 {
		ms, _ := strconv.ParseFloat(matches[1], 64)
		if ms == 0 { return 0.5 }
		return ms
	}
	return 0
}

func doPings() tea.Cmd {
	return func() tea.Msg {
		return pingsMsg{
			google:     runPing("8.8.8.8"),
			cloudflare: runPing("1.1.1.1"),
		}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c": return m, tea.Quit
		case "1": m.scale = scale1Min
		case "2": m.scale = scale5Min
		case "3": m.scale = scale15Min
		}

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		logViewHeight := 3
		m.graphHeight = (m.height - 18 - logViewHeight) / 4
		if m.graphHeight < 3 { m.graphHeight = 3 }

		if !m.ready {
			m.viewport = viewport.New(msg.Width-4, logViewHeight)
			m.ready = true
		} else {
			m.viewport.Width, m.viewport.Height = msg.Width-4, logViewHeight
		}

	case tickMsg:
		p, _ := cpu.Percent(0, false)
		if len(p) > 0 { m.cpuBuffer = append(m.cpuBuffer, p[0]) }
		v, _ := mem.VirtualMemory()
		if v != nil { m.memUsed = v.UsedPercent }
		d, _ := disk.IOCounters()
		if len(d) > 0 {
			var totalRead, totalWrite uint64
			for _, io := range d { totalRead += io.ReadBytes; totalWrite += io.WriteBytes }
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
		m.pingGoogleBuf = append(m.pingGoogleBuf, msg.google)
		m.pingCloudBuf = append(m.pingCloudBuf, msg.cloudflare)
		if len(m.pingGoogleBuf) > 900 {
			m.pingGoogleBuf = m.pingGoogleBuf[1:]
			m.pingCloudBuf = m.pingCloudBuf[1:]
		}
		cmds = append(cmds, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return doPings()()
		}))

	case logMsg:
		if len(msg) > 0 {
			for _, l := range msg {
				m.logs = append(m.logs, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), l))
			}
			m.viewport.SetContent(strings.Join(m.logs, "\n"))
			m.viewport.GotoBottom()
		}
		cmds = append(cmds, tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
			return checkLogs(m.seenErrors)()
		}))
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m *model) trimBuffers(max int) {
	if len(m.cpuBuffer) > max { m.cpuBuffer = m.cpuBuffer[1:] }
	if len(m.diskReadBuffer) > max { m.diskReadBuffer = m.diskReadBuffer[1:] }
	if len(m.diskWriteBuffer) > max { m.diskWriteBuffer = m.diskWriteBuffer[1:] }
	if len(m.netSentBuffer) > max { m.netSentBuffer = m.netSentBuffer[1:] }
	if len(m.netRecvBuffer) > max { m.netRecvBuffer = m.netRecvBuffer[1:] }
}

func (m model) downsample(data []float64, targetWidth int) []float64 {
	windowSeconds := int(m.scale)
	startIdx := len(data) - windowSeconds
	if startIdx < 0 { startIdx = 0 }
	activeData := data[startIdx:]
	if len(activeData) <= targetWidth { return activeData }
	result := make([]float64, targetWidth)
	bucketSize := float64(len(activeData)) / float64(targetWidth)
	for i := 0; i < targetWidth; i++ {
		start, end := int(float64(i)*bucketSize), int(float64(i+1)*bucketSize)
		if end > len(activeData) { end = len(activeData) }
		var sum float64
		count := 0
		for j := start; j < end; j++ { sum += activeData[j]; count++ }
		if count > 0 { result[i] = sum / float64(count) }
	}
	return result
}

func renderTimeAxis(width int, scale timeScale) string {
	duration := time.Duration(scale) * time.Second
	start, mid, end := fmt.Sprintf("-%v", duration), fmt.Sprintf("-%v", duration/2), "Now"
	totalLen := len(start) + len(mid) + len(end)
	if width < totalLen+10 { return axisStyle.Render(start + " " + mid + " " + end) }
	spaces := (width - totalLen) / 2
	return axisStyle.Render(start + strings.Repeat(" ", spaces) + mid + strings.Repeat(" ", width-spaces-totalLen) + end)
}

func (m model) View() string {
	if !m.ready { return "Initializing..." }
	header := titleStyle.Render("SYSTEM MONITOR")
	metrics := metricStyle.Render(fmt.Sprintf(" RAM: %.1f%%  %s", m.memUsed, time.Now().Format("15:04:05")))
	topBar := lipgloss.JoinHorizontal(lipgloss.Center, header, " ", metrics)

	w, gH := m.width-12, m.graphHeight
	timeAxis := "      " + renderTimeAxis(w, m.scale)

	// CPU
	cpuD := m.downsample(m.cpuBuffer, w)
	cpuG := graphStyle.Render(asciigraph.Plot(cpuD, asciigraph.Height(gH), asciigraph.Width(w), asciigraph.Caption("CPU (%)")))

	// Disk
	dr, dw := m.downsample(m.diskReadBuffer, w), m.downsample(m.diskWriteBuffer, w)
	diskCaption := fmt.Sprintf("Disk KB/s (%s, %s)", greenLabel.Render("Read"), redLabel.Render("Write"))
	diskG := graphStyle.Render(asciigraph.PlotMany([][]float64{dr, dw}, 
		asciigraph.Height(gH), 
		asciigraph.Width(w), 
		asciigraph.Caption(diskCaption),
		asciigraph.SeriesColors(asciigraph.Green, asciigraph.Red),
	))

	// Net
	ns, nr := m.downsample(m.netSentBuffer, w), m.downsample(m.netRecvBuffer, w)
	netCaption := fmt.Sprintf("Net KB/s (%s, %s)", greenLabel.Render("Up"), redLabel.Render("Down"))
	netG := graphStyle.Render(asciigraph.PlotMany([][]float64{ns, nr}, 
		asciigraph.Height(gH), 
		asciigraph.Width(w), 
		asciigraph.Caption(netCaption),
		asciigraph.SeriesColors(asciigraph.Green, asciigraph.Red),
	))

	// Latency
	pg, pc := m.downsample(m.pingGoogleBuf, w), m.downsample(m.pingCloudBuf, w)
	pingCaption := fmt.Sprintf("Latency ms (%s, %s)", blueLabel.Render("Google 8.8.8.8"), cyanLabel.Render("Cloudflare 1.1.1.1"))
	pingG := graphStyle.Render(asciigraph.PlotMany([][]float64{pg, pc}, 
		asciigraph.Height(gH), 
		asciigraph.Width(w), 
		asciigraph.Caption(pingCaption),
		asciigraph.SeriesColors(asciigraph.Blue, asciigraph.Cyan),
	))

	return lipgloss.JoinVertical(lipgloss.Left, topBar, cpuG, diskG, netG, pingG, timeAxis, " ERROR LOGS:", logStyle.Render(m.viewport.View()), helpStyle.Render(" 1,2,3: Scale • q: Quit"))
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil { fmt.Printf("Error: %v", err) }
}
