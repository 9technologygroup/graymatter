package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	graymatter "github.com/angelnicolasc/graymatter"
)

func recallCmd() *cobra.Command {
	var topK int
	cmd := &cobra.Command{
		Use:   "recall <agent-id> <query>",
		Short: "Retrieve relevant memories for an agent",
		Example: `  graymatter recall "sales-closer" "follow up Maria"
  graymatter recall "code-reviewer" "nil pointer" --top-k 5
  graymatter recall "sales-closer" "touchpoints" --json`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID, query := args[0], args[1]
			cfg := graymatter.DefaultConfig()
			cfg.DataDir = dataDir
			if topK > 0 {
				cfg.TopK = topK
			}

			mem, err := graymatter.NewWithConfig(cfg)
			if err != nil {
				return err
			}
			defer mem.Close()

			facts, err := mem.Recall(agentID, query)
			if err != nil {
				return err
			}

			if jsonOut {
				data, _ := json.Marshal(map[string]any{
					"agent_id": agentID,
					"query":    query,
					"facts":    facts,
					"count":    len(facts),
				})
				fmt.Println(string(data))
				return nil
			}

			if len(facts) == 0 {
				if !quiet {
					fmt.Printf("No memories found for agent %q matching %q.\n", agentID, query)
				}
				return nil
			}

			if !quiet {
				fmt.Printf("# Memory context for [%s] / %q\n\n", agentID, query)
			}
			fmt.Println(strings.Join(facts, "\n"))
			return nil
		},
	}
	cmd.Flags().IntVar(&topK, "top-k", 0, "maximum facts to return (default from config)")
	return cmd
}
