package tui

type tabID int

const (
	tabOverview tabID = iota
	tabDaily
	tabModels
	tabTools
	tabProjects
	tabSessions
	tabConfig
)

type tabDefinition struct {
	ID         tabID
	Index      int
	Label      string
	ShortLabel string
}

var allTabs = []tabDefinition{
	{ID: tabOverview, Index: 1, Label: "Overview", ShortLabel: "Over"},
	{ID: tabDaily, Index: 2, Label: "Daily", ShortLabel: "Daily"},
	{ID: tabModels, Index: 3, Label: "Models", ShortLabel: "Models"},
	{ID: tabTools, Index: 4, Label: "Tools", ShortLabel: "Tools"},
	{ID: tabProjects, Index: 5, Label: "Projects", ShortLabel: "Proj"},
	{ID: tabSessions, Index: 6, Label: "Sessions", ShortLabel: "Sess"},
	{ID: tabConfig, Index: 7, Label: "Config", ShortLabel: "Cfg"},
}

func (t tabID) definition() tabDefinition {
	for _, tab := range allTabs {
		if tab.ID == t {
			return tab
		}
	}
	return allTabs[0]
}

func nextTab(current tabID) tabID {
	idx := int(current) + 1
	if idx >= len(allTabs) {
		return allTabs[0].ID
	}
	return allTabs[idx].ID
}

func previousTab(current tabID) tabID {
	idx := int(current) - 1
	if idx < 0 {
		return allTabs[len(allTabs)-1].ID
	}
	return allTabs[idx].ID
}

func tabFromKey(key string) (tabID, bool) {
	for _, tab := range allTabs {
		if key == string(rune('0'+tab.Index)) {
			return tab.ID, true
		}
	}
	return tabOverview, false
}

func tabLabel(tab tabDefinition, width int) string {
	if width >= 110 {
		return tab.Label
	}
	return tab.ShortLabel
}
