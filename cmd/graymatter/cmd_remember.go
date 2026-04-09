package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	graymatter "github.com/angelnicolasc/graymatter"
)

func rememberCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remember <agent-id> <text>",
		Short: "Store a fact for an agent",
		Example: `  graymatter remember "sales-closer" "Maria didn't reply Wednesday. Third touchpoint due Friday."
  graymatter remember "code-reviewer" "Always check for nil pointer dereferences in Go code."`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID, text := args[0], args[1]
			cfg := graymatter.DefaultConfig()
			cfg.DataDir = dataDir

			mem, err := graymatter.NewWithConfig(cfg)
			if err != nil {
				return err
			}
			defer mem.Close()

			if err := mem.Remember(agentID, text); err != nil {
				return err
			}

			if jsonOut {
				data, _ := json.Marshal(map[string]string{"agent_id": agentID, "status": "stored"})
				fmt.Println(string(data))
			} else if !quiet {
				fmt.Printf("Remembered: [%s] %s\n", agentID, text)
			}
			return nil
		},
	}
}
