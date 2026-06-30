package fast

import tea "github.com/charmbracelet/bubbletea"

func Run() error {
	urls, err := targets(connections)
	if err != nil {
		return err
	}

	_, err = tea.NewProgram(newModel(urls)).Run()
	return err
}
