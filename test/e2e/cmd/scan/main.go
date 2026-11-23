// Comando de apoio do harness e2e: imprime portscan.Scan como JSON.
//
// É cross-compilado para Linux e copiado para dentro do container, onde roda
// como dev e como root para comparar o que cada usuário enxerga.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/rafaelb13/agentport/internal/portscan"
)

func main() {
	ls, err := portscan.Scan(context.Background(), portscan.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "scan:", err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(ls); err != nil {
		fmt.Fprintln(os.Stderr, "encode:", err)
		os.Exit(1)
	}
}
