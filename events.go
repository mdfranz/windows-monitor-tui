package main

import (
	"strings"

	"github.com/google/winops/winlog"
	"golang.org/x/sys/windows"
)

// FetchErrors returns new error strings found in the last hour
func FetchErrors(seenMap map[string]bool) []string {
	xpath := "*[System[(Level=1 or Level=2) and TimeCreated[timediff(@SystemTime) <= 3600000]]]"
	queries := map[string]string{
		"Application": xpath,
		"System":      xpath,
		"Setup":       xpath,
	}

	xmlQuery, err := winlog.BuildStructuredXMLQuery(queries)
	if err != nil {
		return nil
	}

	queryPtr := windows.StringToUTF16Ptr(string(xmlQuery))
	config := &winlog.SubscribeConfig{
		Query: queryPtr,
		Flags: 0x1,
	}

	hSubscription, err := winlog.Subscribe(config)
	if err != nil {
		return nil
	}
	defer windows.Close(hSubscription)

	publisherCache := make(map[string]windows.Handle)
	defer func() {
		for _, h := range publisherCache {
			windows.Close(h)
		}
	}()

	events, err := winlog.GetRenderedEvents(config, publisherCache, hSubscription, 50, 0)
	if err != nil {
		return nil
	}

	var newLogs []string
	for _, event := range events {
		if !seenMap[event] {
			// Extract a summary or just use the XML. 
			// For TUI, we'll trim it to keep it readable.
			cleanEvent := strings.TrimSpace(event)
			newLogs = append(newLogs, cleanEvent)
			seenMap[event] = true
		}
	}
	return newLogs
}
