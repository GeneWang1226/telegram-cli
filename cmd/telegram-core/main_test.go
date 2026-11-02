package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"

	"github.com/gotd/td/tg"
	"github.com/imtaqin/telegram-cli/internal/corebridge"
)

func TestCoreVersionHelper(t *testing.T) {
	if os.Getenv("TELEGRAM_CORE_TEST_VERSION") != "1" {
		return
	}
	os.Args = []string{"telegram-core", "--version"}
	main()
	os.Exit(0)
}

func TestCoreVersionIsMachineReadableWithoutCredentials(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestCoreVersionHelper$")
	command.Env = append(os.Environ(), "TELEGRAM_CORE_TEST_VERSION=1")
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		Version  string `json:"version"`
		Protocol int    `json:"protocol"`
		Layer    int    `json:"layer"`
	}
	if json.Unmarshal(output, &info) != nil || info.Version == "" || info.Protocol != corebridge.Protocol || info.Layer != tg.Layer {
		t.Fatal("version handshake is invalid", string(output))
	}
}
