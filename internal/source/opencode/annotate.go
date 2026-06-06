package opencode

import "opencode-dashboard/internal/stats"

const sourceID = "opencode"

func reportedCost() *stats.CostProvenance {
	return &stats.CostProvenance{
		Status:        stats.CostReported,
		Currency:      "USD",
		ReportedCount: 1,
		Note:          "cost reported by OpenCode data",
	}
}

func annotateOverview(v *stats.OverviewStats) {
	v.SourceID = sourceID
	v.CostStatus = stats.CostReported
	v.CostProvenance = reportedCost()
}

func annotateDaily(v *stats.DailyStats) {
	v.SourceID = sourceID
	v.CostStatus = stats.CostReported
	v.CostProvenance = reportedCost()
	for i := range v.Days {
		v.Days[i].SourceID = sourceID
		v.Days[i].CostStatus = stats.CostReported
		v.Days[i].CostProvenance = reportedCost()
	}
}

func annotateDailyDimension(v *stats.DailyDimensionStats) {
	v.SourceID = sourceID
	v.CostStatus = stats.CostReported
	v.CostProvenance = reportedCost()
	for i := range v.Days {
		v.Days[i].SourceID = sourceID
		v.Days[i].CostStatus = stats.CostReported
		v.Days[i].CostProvenance = reportedCost()
	}
}

func annotateModels(v *stats.ModelStats) {
	v.SourceID = sourceID
	v.CostStatus = stats.CostReported
	v.CostProvenance = reportedCost()
	for i := range v.Models {
		v.Models[i].SourceID = sourceID
		v.Models[i].CostStatus = stats.CostReported
		v.Models[i].CostProvenance = reportedCost()
	}
}

func annotateTools(v *stats.ToolStats) {
	v.SourceID = sourceID
	for i := range v.Tools {
		v.Tools[i].SourceID = sourceID
	}
}

func annotateProjects(v *stats.ProjectStats) {
	v.SourceID = sourceID
	v.CostStatus = stats.CostReported
	v.CostProvenance = reportedCost()
	for i := range v.Projects {
		v.Projects[i].SourceID = sourceID
		v.Projects[i].CostStatus = stats.CostReported
		v.Projects[i].CostProvenance = reportedCost()
	}
}

func annotateProjectDetail(v *stats.ProjectDetail) {
	if v == nil {
		return
	}
	v.SourceID = sourceID
	v.CostStatus = stats.CostReported
	v.CostProvenance = reportedCost()
	for i := range v.RecentSessions {
		annotateSessionEntry(&v.RecentSessions[i])
	}
}

func annotateSessionEntry(v *stats.SessionEntry) {
	v.SourceID = sourceID
	v.CostStatus = stats.CostReported
	v.CostProvenance = reportedCost()
}

func annotateSessions(v *stats.SessionList) {
	v.SourceID = sourceID
	v.CostStatus = stats.CostReported
	v.CostProvenance = reportedCost()
	for i := range v.Sessions {
		annotateSessionEntry(&v.Sessions[i])
	}
}

func annotateSessionDetail(v *stats.SessionDetail) {
	if v == nil {
		return
	}
	v.SourceID = sourceID
	v.CostStatus = stats.CostReported
	v.CostProvenance = reportedCost()
	for i := range v.Messages {
		v.Messages[i].SourceID = sourceID
		if v.Messages[i].Role == "assistant" {
			v.Messages[i].CostStatus = stats.CostReported
			v.Messages[i].CostProvenance = reportedCost()
		}
	}
}

func annotateMessages(v *stats.MessageList) {
	v.SourceID = sourceID
	v.CostStatus = stats.CostReported
	v.CostProvenance = reportedCost()
	for i := range v.Messages {
		annotateMessageEntry(&v.Messages[i])
	}
}

func annotateMessageEntry(v *stats.MessageEntry) {
	v.SourceID = sourceID
	if v.Role == "assistant" {
		v.CostStatus = stats.CostReported
		v.CostProvenance = reportedCost()
	}
}

func annotateMessageDetail(v *stats.MessageDetail) {
	if v == nil {
		return
	}
	annotateMessageEntry(&v.MessageEntry)
	for i := range v.Content.ToolParts {
		v.Content.ToolParts[i].SourceID = sourceID
	}
}

func annotateConfig(v *stats.ConfigView) {
	v.SourceID = sourceID
}
