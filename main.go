package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/huh"
)

// ProcessInfo содержит данные о процессе, использующем порт
type ProcessInfo struct {
	PID     int
	Name    string
	Port    string
	Proto   string
	User    string
	CmdLine string
}

func main() {
	fmt.Println("🔍 Сканирование портов и процессов...")

	processes, err := getListeningProcesses()
	if err != nil {
		fmt.Printf("❌ Ошибка при получении списка процессов: %v\n", err)
		os.Exit(1)
	}

	if len(processes) == 0 {
		fmt.Println("✅ Нет активных процессов, прослушивающих порты (доступных в текущем окружении).")
		return
	}

	// 1. Вывод таблицы процессов
	printProcessTable(processes)

	// 2. Интерактивный выбор процессов для завершения
	pidsToKill := selectProcessesToKill(processes)
	if len(pidsToKill) == 0 {
		fmt.Println("👋 Завершение работы без изменений.")
		return
	}

	// 3. Подтверждение удаления
	var confirm bool
	err = huh.NewConfirm().
		Title(fmt.Sprintf("Вы уверены, что хотите завершить %d процесс(ов)?", len(pidsToKill))).
		Affirmative("Да, завершить").
		Negative("Отмена").
		Value(&confirm).
		Run()

	if err != nil || !confirm {
		fmt.Println("🚫 Операция отменена.")
		return
	}

	// 4. Завершение выбранных процессов
	for _, pid := range pidsToKill {
		if err := killProcess(pid); err != nil {
			fmt.Printf("❌ Ошибка завершения PID %d: %v\n", pid, err)
		} else {
			fmt.Printf("✅ Процесс PID %d успешно завершен.\n", pid)
		}
	}
}

// getListeningProcesses определяет активные порты в зависимости от ОС
func getListeningProcesses() ([]ProcessInfo, error) {
	if runtime.GOOS == "windows" {
		return getProcessesWindows()
	}
	return getProcessesUnix()
}

// getProcessesUnix получает данные для Linux / macOS через lsof
func getProcessesUnix() ([]ProcessInfo, error) {
	// Проверяем, установлена ли lsof вообще
	if _, err := exec.LookPath("lsof"); err != nil {
		return nil, fmt.Errorf("утилита 'lsof' не найдена в PATH. Установите её: pkg install lsof")
	}

	cmd := exec.Command("lsof", "-nP", "-iTCP", "-iUDP", "-sTCP:LISTEN")
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	// Запускаем lsof. На Android/Linux без root lsof может вернуть код ошибки (например, 1),
    // если не смог прочитать некоторые чужие файлы, но данные в stdout всё равно могут быть.
	_ = cmd.Run()

	if out.Len() == 0 {
		return nil, fmt.Errorf("lsof не вернул данных. Возможно, нет запущенных процессов на портах или нужны права root. Ошибка: %s", stderr.String())
	}

	lines := strings.Split(out.String(), "\n")
	var list []ProcessInfo
	seen := make(map[string]bool)

	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}

		name := fields[0]
		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		user := fields[2]
		proto := fields[4]
		nameAddr := fields[8]

		parts := strings.Split(nameAddr, ":")
		port := parts[len(parts)-1]

		key := fmt.Sprintf("%d-%s", pid, port)
		if seen[key] {
			continue
		}
		seen[key] = true

		list = append(list, ProcessInfo{
			PID:   pid,
			Name:  name,
			Port:  port,
			Proto: proto,
			User:  user,
		})
	}

	return list, nil
}

// getProcessesWindows получает данные для Windows через netstat
func getProcessesWindows() ([]ProcessInfo, error) {
	cmd := exec.Command("netstat", "-ano", "-p", "tcp")
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return nil, err
	}

	lines := strings.Split(out.String(), "\n")
	var list []ProcessInfo
	seen := make(map[string]bool)

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "TCP" || fields[3] != "LISTENING" {
			continue
		}

		addr := fields[1]
		parts := strings.Split(addr, ":")
		port := parts[len(parts)-1]
		pid, _ := strconv.Atoi(fields[4])

		key := fmt.Sprintf("%d-%s", pid, port)
		if seen[key] {
			continue
		}
		seen[key] = true

		procName := getWinProcessName(pid)

		list = append(list, ProcessInfo{
			PID:   pid,
			Name:  procName,
			Port:  port,
			Proto: "TCP",
			User:  "N/A",
		})
	}

	return list, nil
}

// getWinProcessName получает имя процесса в Windows по PID
func getWinProcessName(pid int) string {
	cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "Unknown"
	}
	parts := strings.Split(out.String(), `","`)
	if len(parts) > 0 {
		return strings.Trim(parts[0], `"`)
	}
	return "Unknown"
}

// printProcessTable выводит аккуратную таблицу в консоль с помощью standard tabwriter
func printProcessTable(processes []ProcessInfo) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "PID\tPROCESS NAME\tPORT\tPROTOCOL\tUSER")
	fmt.Fprintln(w, "----\t------------\t----\t--------\t----")

	for _, p := range processes {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", p.PID, p.Name, p.Port, p.Proto, p.User)
	}
	w.Flush()
	fmt.Println()
}

// selectProcessesToKill форматирует меню выбора процессов с помощью huh
func selectProcessesToKill(processes []ProcessInfo) []int {
	var selectedPIDs []int

	options := make([]huh.Option[int], len(processes))
	for i, p := range processes {
		label := fmt.Sprintf("Port %-6s | PID %-6d | %s", p.Port, p.PID, p.Name)
		options[i] = huh.NewOption(label, p.PID)
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[int]().
				Title("Выберите процессы для завершения (Пробел — выбрать, Enter — подтвердить):").
				Options(options...).
				Value(&selectedPIDs),
		),
	)

	if err := form.Run(); err != nil {
		fmt.Printf("Ошибка интерфейса: %v\n", err)
		return nil
	}

	return selectedPIDs
}

// killProcess отправляет сигнал на завершение процесса
func killProcess(pid int) error {
	if runtime.GOOS == "windows" {
		return exec.Command("taskkill", "/F", "/PID", strconv.Itoa(pid)).Run()
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}
