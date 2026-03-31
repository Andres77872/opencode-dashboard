package tui

type keyMap struct {
	Quit     []string
	Help     []string
	Refresh  []string
	PrevTab  []string
	NextTab  []string
	Up       []string
	Down     []string
	Top      []string
	Bottom   []string
	PrevPage []string
	NextPage []string
	Metric   []string
	Toggle   []string
	Filter   []string
	Sort     []string
	Close    []string
}

func defaultKeyMap() keyMap {
	return keyMap{
		Quit:     []string{"q", "ctrl+c"},
		Help:     []string{"?"},
		Refresh:  []string{"r"},
		PrevTab:  []string{"left", "h"},
		NextTab:  []string{"right", "l"},
		Up:       []string{"up", "k"},
		Down:     []string{"down", "j"},
		Top:      []string{"g"},
		Bottom:   []string{"G"},
		PrevPage: []string{"p"},
		NextPage: []string{"n"},
		Metric:   []string{"t"},
		Toggle:   []string{"enter", " "},
		Filter:   []string{"/"},
		Sort:     []string{"s"},
		Close:    []string{"esc"},
	}
}

func matches(key string, options ...string) bool {
	for _, option := range options {
		if key == option {
			return true
		}
	}
	return false
}
