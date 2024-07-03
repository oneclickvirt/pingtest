package pt

import (
	"fmt"
	"os/exec"
)

func TTT() {
	cmd := exec.Command("ping", "-h")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println("Error:", err)
	}
	fmt.Println(string(output))
}
