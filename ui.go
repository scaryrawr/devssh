package devssh

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type selectionModel struct {
	title    string
	choices  []string
	cursor   int
	selected int
	done     bool
}

func (m selectionModel) Init() tea.Cmd {
	return nil
}

func (m selectionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}
		case "enter", " ":
			m.selected = m.cursor
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m selectionModel) View() string {
	title := m.title
	if title == "" {
		title = "Choose a host:"
	}

	var b strings.Builder
	b.Grow(len(title) + len(m.choices)*32 + 32)
	fmt.Fprintf(&b, "%s\n\n", title)

	for i, choice := range m.choices {
		cursor := " "
		if m.cursor == i {
			cursor = ">"
		}
		fmt.Fprintf(&b, "%s %s\n", cursor, choice)
	}

	b.WriteString("\nPress q to quit.\n")
	return b.String()
}

// showSelection presents a list of options and returns the index of the
// chosen entry. The title is shown as a header above the list.
func showSelection(title string, options []string) (int, error) {
	model := selectionModel{
		title:   title,
		choices: options,
	}

	p := tea.NewProgram(model)
	finalModel, err := p.Run()
	if err != nil {
		return -1, fmt.Errorf("selection failed: %w", err)
	}

	result := finalModel.(selectionModel)
	if !result.done {
		return -1, fmt.Errorf("no selection made")
	}

	return result.selected, nil
}
